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
	"time"

	"peaceclaude/internal/audit"
	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/policy"
	"peaceclaude/internal/upstream"
)

type Gateway struct {
	mu       sync.RWMutex
	cfg      *config.Config
	policy   *policy.Engine
	gate     *egress.Gate
	audit    *audit.Store
	dialer   *upstream.Dialer
	allowed  []netip.Prefix

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
		protectedConns: make(map[net.Conn]struct{}),
	}
	gw.rebuildAllowedSubnetsLocked(cfg)
	return gw
}

func (g *Gateway) UpdateConfig(cfg *config.Config) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cfg = cfg
	g.policy.UpdateConfig(cfg)
	g.dialer = upstream.FromConfig(cfg.Upstream)
	g.rebuildAllowedSubnetsLocked(cfg)
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

func (g *Gateway) ClientAllowed(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	addr = addr.Unmap().WithZone("")
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

// CloseProtectedConns immediately terminates all active proxied connections (fail-closed cut).
func (g *Gateway) CloseProtectedConns() int {
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

func (g *Gateway) dialTarget(ctx context.Context, dec policy.Decision, targetAddr string) (net.Conn, error) {
	switch dec.Action {
	case policy.ActionDirectLocal, policy.ActionDirectCorporate:
		d := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}
		return d.DialContext(ctx, "tcp", targetAddr)
	case policy.ActionProxy:
		g.mu.RLock()
		dialer := g.dialer
		g.mu.RUnlock()
		return dialer.DialContext(ctx, "tcp", targetAddr)
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

func (g *Gateway) handleConnect(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	target := r.Host
	if _, _, err := net.SplitHostPort(target); err != nil {
		target = net.JoinHostPort(target, "443")
	}
	gateOpen, gateReason := g.gate.AllowTraffic()
	dec := g.policy.Evaluate(target, gateOpen, gateReason)
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

	_, _ = io.WriteString(clientConn, "HTTP/1.1 200 Connection Established\r\nProxy-Agent: peaceclaude\r\n\r\n")

	if dec.Action == policy.ActionProxy {
		g.trackProtected(upConn)
		g.trackProtected(clientConn)
		defer g.untrackProtected(upConn)
		defer g.untrackProtected(clientConn)
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

	gateOpen, gateReason := g.gate.AllowTraffic()
	dec := g.policy.Evaluate(target, gateOpen, gateReason)
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

	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return g.dialTarget(ctx, dec, addr)
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
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		bytesOut, _ = io.Copy(upConn, clientReader)
		if tc, ok := upConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		bytesIn, _ = io.Copy(clientConn, upConn)
		if tc, ok := clientConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
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

	gateOpen, gateReason := g.gate.AllowTraffic()
	dec := g.policy.Evaluate(target, gateOpen, gateReason)
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

	_ = conn.SetDeadline(time.Time{})
	_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0})

	if dec.Action == policy.ActionProxy {
		g.trackProtected(upConn)
		g.trackProtected(conn)
		defer g.untrackProtected(upConn)
		defer g.untrackProtected(conn)
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
