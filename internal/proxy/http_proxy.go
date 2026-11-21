package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"peaceclaude/internal/audit"
	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/policy"
	"peaceclaude/internal/upstream"
)

type Gateway struct {
	mu           sync.RWMutex
	cfg          *config.Config
	policy       *policy.Engine
	gate         *egress.Gate
	audit        *audit.Store
	dialer       *upstream.Dialer
	backupDialer *upstream.Dialer
	allowed      []netip.Prefix

	connMu         sync.Mutex
	protectedConns map[net.Conn]struct{}
}

func NewGateway(cfg *config.Config, eng *policy.Engine, gate *egress.Gate, store *audit.Store) *Gateway {
	gw := &Gateway{
		cfg:            cfg,
		policy:         eng,
		gate:           gate,
		audit:          store,
		dialer:         upstream.FromConfig(cfg.Upstream),
		backupDialer:   upstream.BackupFromConfig(cfg.Upstream),
		protectedConns: make(map[net.Conn]struct{}),
	}
	gw.rebuildAllowedSubnetsLocked(cfg)
	return gw
}

func (g *Gateway) UpdateConfig(cfg *config.Config) {
	g.mu.Lock()
	oldDialer := g.dialer
	oldBackup := g.backupDialer
	g.cfg = cfg
	g.policy.UpdateConfig(cfg)
	g.dialer = upstream.FromConfig(cfg.Upstream)
	g.backupDialer = upstream.BackupFromConfig(cfg.Upstream)
	g.rebuildAllowedSubnetsLocked(cfg)
	g.mu.Unlock()

	oldDialer.DrainPool()
	oldBackup.DrainPool()
}

// WarmUpstream pre-warms TCP connections to the active remote upstream proxy
// and checks once whether direct-http supports dual-protocol SOCKS5 bypass for OAuth domains.
func (g *Gateway) WarmUpstream(ctx context.Context) {
	g.mu.RLock()
	d := g.dialer
	g.mu.RUnlock()
	if d != nil {
		d.WarmPool(ctx, 2)
		go d.ProbeSOCKS5Support(context.Background())
	}
}

func (g *Gateway) rebuildAllowedSubnetsLocked(cfg *config.Config) {
	prefixes := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
	}
	if cfg != nil && cfg.Listen.VMProxyEnabled {
		for _, s := range cfg.Listen.VMProxySubnets {
			if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil {
				prefixes = append(prefixes, p)
			}
		}
	}
	g.allowed = prefixes
}

func parseRemoteAddr(remoteAddr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap().WithZone(""), true
}

func (g *Gateway) isLoopbackClient(remoteAddr string) bool {
	addr, ok := parseRemoteAddr(remoteAddr)
	return ok && addr.IsLoopback()
}

func (g *Gateway) ClientAllowed(remoteAddr string) bool {
	addr, ok := parseRemoteAddr(remoteAddr)
	if !ok {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, p := range g.allowed {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func (g *Gateway) trackProtected(c net.Conn) {
	g.connMu.Lock()
	g.protectedConns[c] = struct{}{}
	g.connMu.Unlock()
}

func (g *Gateway) untrackProtected(c net.Conn) {
	g.connMu.Lock()
	delete(g.protectedConns, c)
	g.connMu.Unlock()
}

// CloseProtectedConns immediately terminates all active proxied connections (fail-closed cut)
// and drains pre-warmed upstream sockets.
func (g *Gateway) CloseProtectedConns() int {
	g.mu.RLock()
	d := g.dialer
	bd := g.backupDialer
	g.mu.RUnlock()
	d.DrainPool()
	bd.DrainPool()

	g.connMu.Lock()
	conns := make([]net.Conn, 0, len(g.protectedConns))
	for c := range g.protectedConns {
		conns = append(conns, c)
	}
	g.protectedConns = make(map[net.Conn]struct{})
	g.connMu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
	return len(conns)
}

type trackedConn struct {
	net.Conn
	gw   *Gateway
	once sync.Once
}

func (t *trackedConn) Close() error {
	t.once.Do(func() {
		t.gw.untrackProtected(t)
	})
	return t.Conn.Close()
}

var copyBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 64*1024)
		return &buf
	},
}

func isTransientDialError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"eof",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"timeout",
		"connection refused",
		"network is unreachable",
		"socks5",
		"connect response",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func dialWithRetry(ctx context.Context, primary, backup *upstream.Dialer, targetAddr string) (net.Conn, error) {
	if primary == nil {
		return nil, errors.New("upstream dialer is nil")
	}

	remaining := 15 * time.Second
	if dl, ok := ctx.Deadline(); ok {
		if r := time.Until(dl); r > 0 {
			remaining = r
		}
	}

	firstBudget := remaining
	if backup != nil && remaining > 6*time.Second {
		firstBudget = 4500 * time.Millisecond
	} else if backup == nil && remaining > 8500*time.Millisecond {
		// Allow slightly wider budget when there is no backup upstream (e.g., cross-Pacific SOCKS5 OAuth).
		firstBudget = 7500 * time.Millisecond
	} else if backup != nil && remaining > 2*time.Second {
		firstBudget = remaining / 2
	}
	firstCtx, firstCancel := context.WithTimeout(ctx, firstBudget)
	conn, err := primary.DialContext(firstCtx, "tcp", targetAddr)
	firstCancel()
	if err == nil {
		return conn, nil
	}

	firstErr := err
	if ctx.Err() == nil && isTransientDialError(err) {
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, firstErr
		case <-timer.C:
		}
		retryCtx := ctx
		var retryCancel context.CancelFunc
		if backup != nil {
			rem := 10 * time.Second
			if dl, ok := ctx.Deadline(); ok {
				rem = time.Until(dl)
			}
			// Always reserve >=3.5s for backup failover when available.
			if rem > 4500*time.Millisecond {
				retryBudget := rem / 2
				if rem-retryBudget < 3500*time.Millisecond {
					retryBudget = rem - 3500*time.Millisecond
				}
				if retryBudget > 4500*time.Millisecond {
					retryBudget = 4500 * time.Millisecond
				}
				if retryBudget >= 500*time.Millisecond {
					retryCtx, retryCancel = context.WithTimeout(ctx, retryBudget)
				}
			}
		}
		retryConn, retryErr := primary.DialContext(retryCtx, "tcp", targetAddr)
		if retryCancel != nil {
			retryCancel()
		}
		if retryErr == nil {
			return retryConn, nil
		}
		firstErr = retryErr
	}
	if backup != nil && ctx.Err() == nil {
		if backupConn, backupErr := backup.DialContext(ctx, "tcp", targetAddr); backupErr == nil {
			return backupConn, nil
		} else {
			return nil, fmt.Errorf("primary failed (%v); backup failed (%v)", firstErr, backupErr)
		}
	}
	return nil, firstErr
}

func (g *Gateway) dialTarget(ctx context.Context, dec policy.Decision, targetAddr string) (net.Conn, error) {
	switch dec.Action {
	case policy.ActionDirectLocal, policy.ActionDirectCorporate:
		d := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
		return d.DialContext(ctx, "tcp", targetAddr)
	case policy.ActionProxy:
		g.mu.RLock()
		dialer := g.dialer
		backup := g.backupDialer
		g.mu.RUnlock()
		return dialWithRetry(ctx, dialer, backup, targetAddr)
	default:
		return nil, fmt.Errorf("blocked by policy (%s): %s", dec.Action, dec.Reason)
	}
}

func (g *Gateway) recordDecision(dec policy.Decision) {
	g.audit.CountDecision(string(dec.Action), dec.SplitIPWarning)
	switch {
	case dec.Action == policy.ActionBlockFailClosed:
		g.audit.RecordLeak(audit.LeakEvent{
			Kind:   "blocked_failclosed",
			Host:   dec.Host,
			Port:   dec.Port,
			Action: string(dec.Action),
			Reason: dec.Reason,
		})
	case dec.Action == policy.ActionBlockTelemetry:
		g.audit.RecordLeak(audit.LeakEvent{
			Kind:   "blocked_telemetry",
			Host:   dec.Host,
			Port:   dec.Port,
			Action: string(dec.Action),
			Reason: dec.Reason,
		})
	case dec.Action == policy.ActionBlockStrictSplit:
		g.audit.RecordLeak(audit.LeakEvent{
			Kind:   "blocked_strict_split",
			Host:   dec.Host,
			Port:   dec.Port,
			Action: string(dec.Action),
			Reason: dec.Reason,
		})
	case dec.SplitIPWarning:
		g.audit.RecordLeak(audit.LeakEvent{
			Kind:   "split_ip_warning",
			Host:   dec.Host,
			Port:   dec.Port,
			Action: string(dec.Action),
			Reason: dec.Reason,
		})
	case dec.Action == policy.ActionDirectCorporate:
		g.audit.RecordLeak(audit.LeakEvent{
			Kind:   "direct_corporate",
			Host:   dec.Host,
			Port:   dec.Port,
			Action: string(dec.Action),
			Reason: dec.Reason,
		})
	}
}

// ServeHTTP handles HTTP and HTTPS CONNECT forward-proxy requests.
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !g.ClientAllowed(r.RemoteAddr) {
		http.Error(w, "peaceclaude: client IP not allowed", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		g.handleConnect(w, r)
		return
	}
	g.handlePlainHTTP(w, r)
}

func (g *Gateway) evaluateForClient(remoteAddr, target string) policy.Decision {
	gateOpen, gateReason := g.gate.AllowTraffic()
	dec := g.policy.Evaluate(target, gateOpen, gateReason)
	if dec.Category == policy.CategoryLoopback && !g.isLoopbackClient(remoteAddr) {
		dec.Action = policy.ActionBlockPrivate
		dec.Reason = "blocked non-loopback client access to host loopback target"
	}
	return dec
}

func (g *Gateway) recheckProxyAllowed(target string) (bool, policy.Decision) {
	gateOpen, gateReason := g.gate.AllowTraffic()
	dec := g.policy.Evaluate(target, gateOpen, gateReason)
	if dec.Action != policy.ActionProxy {
		return false, dec
	}
	return true, dec
}

func (g *Gateway) handleConnect(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	target := r.Host
	if _, _, err := net.SplitHostPort(target); err != nil {
		target = net.JoinHostPort(target, "443")
	}
	dec := g.evaluateForClient(r.RemoteAddr, target)
	g.recordDecision(dec)
	egressIP := g.gate.Snapshot().LastGood.EgressIP

	if dec.Action == policy.ActionBlockFailClosed || dec.Action == policy.ActionBlockTelemetry ||
		dec.Action == policy.ActionBlockPrivate || dec.Action == policy.ActionBlockStrictSplit {
		code := http.StatusForbidden
		if dec.Action == policy.ActionBlockFailClosed {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("X-PeaceClaude-Block", string(dec.Action))
		http.Error(w, fmt.Sprintf("peaceclaude blocked %s: %s", target, dec.Reason), code)
		g.audit.RecordCapture(audit.CaptureRecord{
			Protocol:       "CONNECT",
			Method:         http.MethodConnect,
			Host:           dec.Host,
			Port:           dec.Port,
			Category:       string(dec.Category),
			Action:         string(dec.Action),
			EgressIP:       egressIP,
			StatusCode:     code,
			DurationMs:     time.Since(start).Milliseconds(),
			Error:          dec.Reason,
			RequestHeaders: audit.SanitizeHeaders(r.Header),
		})
		return
	}

	dialCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	upConn, err := g.dialTarget(dialCtx, dec, target)
	cancel()
	if err != nil {
		w.Header().Set("X-PeaceClaude-Block", "upstream-dial-error")
		http.Error(w, fmt.Sprintf("peaceclaude upstream failed (fail-closed, no direct fallback): %v", err), http.StatusBadGateway)
		g.audit.RecordCapture(audit.CaptureRecord{
			Protocol:       "CONNECT",
			Method:         http.MethodConnect,
			Host:           dec.Host,
			Port:           dec.Port,
			Category:       string(dec.Category),
			Action:         string(dec.Action),
			EgressIP:       egressIP,
			StatusCode:     http.StatusBadGateway,
			DurationMs:     time.Since(start).Milliseconds(),
			Error:          err.Error(),
			RequestHeaders: audit.SanitizeHeaders(r.Header),
		})
		return
	}
	defer upConn.Close()

	// Secondary fail-closed TOCTOU check before admitting the established tunnel.
	if dec.Action == policy.ActionProxy {
		if allowed, postDec := g.recheckProxyAllowed(target); !allowed {
			g.recordDecision(postDec)
			w.Header().Set("X-PeaceClaude-Block", string(postDec.Action))
			http.Error(w, fmt.Sprintf("peaceclaude blocked %s after dial (fail-closed): %s", target, postDec.Reason), http.StatusServiceUnavailable)
			return
		}
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, bufRW, err := hj.Hijack()
	if err != nil {
		return
	}
	defer clientConn.Close()

	if dec.Action == policy.ActionProxy {
		g.trackProtected(upConn)
		g.trackProtected(clientConn)
		defer g.untrackProtected(upConn)
		defer g.untrackProtected(clientConn)
	}
	if _, err := io.WriteString(clientConn, "HTTP/1.1 200 Connection Established\r\nProxy-Agent: peaceclaude\r\n\r\n"); err != nil {
		return
	}
	if dec.IsClaudeStream {
		g.gate.BeginStream()
		defer g.gate.EndStream()
	}

	var clientReader io.Reader = clientConn
	if bufRW != nil && bufRW.Reader.Buffered() > 0 {
		clientReader = io.MultiReader(bufRW.Reader, clientConn)
	}

	bytesOut, bytesIn := tunnelBidirectional(clientConn, clientReader, upConn)
	g.audit.RecordCapture(audit.CaptureRecord{
		Protocol:       "CONNECT",
		Method:         http.MethodConnect,
		Host:           dec.Host,
		Port:           dec.Port,
		Category:       string(dec.Category),
		Action:         string(dec.Action),
		EgressIP:       egressIP,
		StatusCode:     http.StatusOK,
		BytesIn:        bytesIn,
		BytesOut:       bytesOut,
		DurationMs:     time.Since(start).Milliseconds(),
		RequestHeaders: audit.SanitizeHeaders(r.Header),
	})
}

func (g *Gateway) handlePlainHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if !r.URL.IsAbs() && r.Host == "" {
		http.Error(w, "peaceclaude proxy requires absolute URL or Host header", http.StatusBadRequest)
		return
	}
	host := r.URL.Host
	if host == "" {
		host = r.Host
	}
	target := host
	if _, _, err := net.SplitHostPort(target); err != nil {
		scheme := r.URL.Scheme
		if scheme == "https" {
			target = net.JoinHostPort(target, "443")
		} else {
			target = net.JoinHostPort(target, "80")
		}
	}

	dec := g.evaluateForClient(r.RemoteAddr, target)
	g.recordDecision(dec)
	egressIP := g.gate.Snapshot().LastGood.EgressIP

	if dec.Action == policy.ActionBlockFailClosed || dec.Action == policy.ActionBlockTelemetry ||
		dec.Action == policy.ActionBlockPrivate || dec.Action == policy.ActionBlockStrictSplit {
		code := http.StatusForbidden
		if dec.Action == policy.ActionBlockFailClosed {
			code = http.StatusServiceUnavailable
		}
		w.Header().Set("X-PeaceClaude-Block", string(dec.Action))
		http.Error(w, fmt.Sprintf("peaceclaude blocked %s: %s", target, dec.Reason), code)
		g.audit.RecordCapture(audit.CaptureRecord{
			Protocol:       "HTTP",
			Method:         r.Method,
			Host:           dec.Host,
			Port:           dec.Port,
			Path:           r.URL.RequestURI(),
			Category:       string(dec.Category),
			Action:         string(dec.Action),
			EgressIP:       egressIP,
			StatusCode:     code,
			DurationMs:     time.Since(start).Milliseconds(),
			Error:          dec.Reason,
			RequestHeaders: audit.SanitizeHeaders(r.Header),
		})
		return
	}

	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	if outReq.URL.Scheme == "" {
		outReq.URL.Scheme = "http"
	}
	if outReq.URL.Host == "" {
		outReq.URL.Host = host
	}
	removeHopByHop(outReq.Header)

	var dialMu sync.Mutex
	var spawned []*trackedConn
	defer func() {
		dialMu.Lock()
		list := spawned
		dialMu.Unlock()
		for _, tc := range list {
			_ = tc.Close()
		}
	}()

	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			upConn, err := g.dialTarget(ctx, dec, addr)
			if err != nil {
				return nil, err
			}
			if dec.Action == policy.ActionProxy {
				if allowed, postDec := g.recheckProxyAllowed(addr); !allowed {
					_ = upConn.Close()
					g.recordDecision(postDec)
					return nil, fmt.Errorf("fail-closed after dial: %s", postDec.Reason)
				}
				tc := &trackedConn{Conn: upConn, gw: g}
				g.trackProtected(tc)
				dialMu.Lock()
				spawned = append(spawned, tc)
				dialMu.Unlock()
				return tc, nil
			}
			return upConn, nil
		},
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()

	resp, err := tr.RoundTrip(outReq)
	if err != nil {
		w.Header().Set("X-PeaceClaude-Block", "upstream-roundtrip-error")
		http.Error(w, fmt.Sprintf("peaceclaude upstream failed (fail-closed): %v", err), http.StatusBadGateway)
		g.audit.RecordCapture(audit.CaptureRecord{
			Protocol:       "HTTP",
			Method:         r.Method,
			Host:           dec.Host,
			Port:           dec.Port,
			Path:           r.URL.RequestURI(),
			Category:       string(dec.Category),
			Action:         string(dec.Action),
			EgressIP:       egressIP,
			StatusCode:     http.StatusBadGateway,
			DurationMs:     time.Since(start).Milliseconds(),
			Error:          err.Error(),
			RequestHeaders: audit.SanitizeHeaders(r.Header),
		})
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	bytesIn, _ := io.Copy(w, resp.Body)

	g.audit.RecordCapture(audit.CaptureRecord{
		Protocol:       "HTTP",
		Method:         r.Method,
		Host:           dec.Host,
		Port:           dec.Port,
		Path:           r.URL.RequestURI(),
		Category:       string(dec.Category),
		Action:         string(dec.Action),
		EgressIP:       egressIP,
		StatusCode:     resp.StatusCode,
		BytesIn:        bytesIn,
		DurationMs:     time.Since(start).Milliseconds(),
		RequestHeaders: audit.SanitizeHeaders(r.Header),
	})
}

func tunnelBidirectional(clientConn net.Conn, clientReader io.Reader, upConn net.Conn) (bytesOut, bytesIn int64) {
	var clientDone atomic.Bool
	var bytesInAtomic atomic.Int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		bufPtr := copyBufPool.Get().(*[]byte)
		var copyErr error
		bytesOut, copyErr = io.CopyBuffer(upConn, clientReader, *bufPtr)
		copyBufPool.Put(bufPtr)
		clientDone.Store(true)
		if tc, ok := upConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		if copyErr != nil {
			// Client reset abnormally: release upstream read waiter promptly.
			_ = upConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		} else if bytesInAtomic.Load() == 0 {
			// Client half-closed/aborted before upstream sent any byte (e.g., hung TLS handshake):
			// wait up to 8s instead of lingering 60s and leaking stream gauges.
			_ = upConn.SetReadDeadline(time.Now().Add(8 * time.Second))
		} else {
			_ = upConn.SetReadDeadline(time.Now().Add(20 * time.Second))
		}
	}()
	go func() {
		defer wg.Done()
		bufPtr := copyBufPool.Get().(*[]byte)
		buf := *bufPtr
		defer copyBufPool.Put(bufPtr)
		var totalIn int64
		for {
			if clientDone.Load() {
				if bytesInAtomic.Load() == 0 {
					_ = upConn.SetReadDeadline(time.Now().Add(8 * time.Second))
				} else {
					_ = upConn.SetReadDeadline(time.Now().Add(20 * time.Second))
				}
			}
			nr, rErr := upConn.Read(buf)
			if nr > 0 {
				nw, wErr := clientConn.Write(buf[:nr])
				if nw > 0 {
					totalIn += int64(nw)
					bytesInAtomic.Add(int64(nw))
				}
				if wErr != nil || nw != nr {
					break
				}
			}
			if rErr != nil {
				break
			}
		}
		bytesIn = totalIn
		if tc, ok := clientConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		// Upstream response stream completed: bound client half-open linger so SSE/stream counters don't leak.
		_ = clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	}()
	wg.Wait()
	return bytesOut, bytesIn
}

func removeHopByHop(h http.Header) {
	for _, k := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		h.Del(k)
	}
}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// ServeSOCKS5 accepts and handles local SOCKS5 connections with identical fail-closed policy.
func (g *Gateway) ServeSOCKS5(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go g.handleSOCKS5Conn(conn)
	}
}

func (g *Gateway) handleSOCKS5Conn(conn net.Conn) {
	defer conn.Close()
	if !g.ClientAllowed(conn.RemoteAddr().String()) {
		return
	}
	start := time.Now()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))

	var verN [2]byte
	if _, err := io.ReadFull(conn, verN[:]); err != nil || verN[0] != 0x05 {
		return
	}
	methods := make([]byte, verN[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	// Select NO AUTH (0x00)
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	var reqHdr [4]byte
	if _, err := io.ReadFull(conn, reqHdr[:]); err != nil || reqHdr[0] != 0x05 {
		return
	}
	if reqHdr[1] != 0x01 { // Only CONNECT is permitted (no UDP ASSOCIATE leak)
		_, _ = conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	switch reqHdr[3] {
	case 0x01:
		var b [4]byte
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return
		}
		host = net.IP(b[:]).String()
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			return
		}
		b := make([]byte, l[0])
		if _, err := io.ReadFull(conn, b); err != nil {
			return
		}
		host = string(b)
		if host == "" || strings.ContainsAny(host, "\r\n\t \x00") {
			_, _ = conn.Write([]byte{0x05, 0x02, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			return
		}
	case 0x04:
		var b [16]byte
		if _, err := io.ReadFull(conn, b[:]); err != nil {
			return
		}
		host = net.IP(b[:]).String()
	default:
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var portBuf [2]byte
	if _, err := io.ReadFull(conn, portBuf[:]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBuf[:])
	target := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	dec := g.evaluateForClient(conn.RemoteAddr().String(), target)
	g.recordDecision(dec)
	egressIP := g.gate.Snapshot().LastGood.EgressIP

	if dec.Action == policy.ActionBlockFailClosed || dec.Action == policy.ActionBlockTelemetry ||
		dec.Action == policy.ActionBlockPrivate || dec.Action == policy.ActionBlockStrictSplit {
		// 0x02 = connection not allowed by ruleset
		_, _ = conn.Write([]byte{0x05, 0x02, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		g.audit.RecordCapture(audit.CaptureRecord{
			Protocol:   "SOCKS5",
			Host:       dec.Host,
			Port:       dec.Port,
			Category:   string(dec.Category),
			Action:     string(dec.Action),
			EgressIP:   egressIP,
			StatusCode: 403,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      dec.Reason,
		})
		return
	}

	dialCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	upConn, err := g.dialTarget(dialCtx, dec, target)
	cancel()
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		g.audit.RecordCapture(audit.CaptureRecord{
			Protocol:   "SOCKS5",
			Host:       dec.Host,
			Port:       dec.Port,
			Category:   string(dec.Category),
			Action:     string(dec.Action),
			EgressIP:   egressIP,
			StatusCode: 502,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		})
		return
	}
	defer upConn.Close()

	// Secondary fail-closed TOCTOU check before acknowledging SOCKS5 tunnel.
	if dec.Action == policy.ActionProxy {
		if allowed, postDec := g.recheckProxyAllowed(target); !allowed {
			g.recordDecision(postDec)
			_, _ = conn.Write([]byte{0x05, 0x02, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
			return
		}
		g.trackProtected(upConn)
		g.trackProtected(conn)
		defer g.untrackProtected(upConn)
		defer g.untrackProtected(conn)
	}

	_ = conn.SetDeadline(time.Time{})
	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}
	if dec.IsClaudeStream {
		g.gate.BeginStream()
		defer g.gate.EndStream()
	}

	bytesOut, bytesIn := tunnelBidirectional(conn, conn, upConn)
	g.audit.RecordCapture(audit.CaptureRecord{
		Protocol:   "SOCKS5",
		Host:       dec.Host,
		Port:       dec.Port,
		Category:   string(dec.Category),
		Action:     string(dec.Action),
		EgressIP:   egressIP,
		StatusCode: 200,
		BytesIn:    bytesIn,
		BytesOut:   bytesOut,
		DurationMs: time.Since(start).Milliseconds(),
	})
}
