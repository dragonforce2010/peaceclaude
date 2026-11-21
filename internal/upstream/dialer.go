package upstream

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"peaceclaude/internal/config"
)

const (
	defaultMaxWarmIdle = 4
	warmConnMaxAge     = 18 * time.Second
)

type warmConn struct {
	conn      net.Conn
	createdAt time.Time
}

// Dialer dials outbound target addresses strictly through the configured upstream proxy.
// It never falls back to direct host connections.
type Dialer struct {
	Mode        string // "local-proxy" | "direct-socks5" | "direct-http"
	Address     string
	Username    string
	Password    string
	Timeout     time.Duration
	MaxWarmIdle int

	poolMu    sync.Mutex
	warmPool  []warmConn
	poolEpoch uint64
	refilling atomic.Int32
}

func FromConfig(cfg config.UpstreamConfig) *Dialer {
	return &Dialer{
		Mode:        cfg.Mode,
		Address:     cfg.Address,
		Username:    cfg.Username,
		Password:    cfg.Password,
		Timeout:     12 * time.Second,
		MaxWarmIdle: defaultMaxWarmIdle,
	}
}

func BackupFromConfig(cfg config.UpstreamConfig) *Dialer {
	if strings.TrimSpace(cfg.BackupAddress) == "" {
		return nil
	}
	mode := cfg.BackupMode
	if mode == "" {
		mode = cfg.Mode
	}
	return &Dialer{
		Mode:        mode,
		Address:     cfg.BackupAddress,
		Username:    cfg.BackupUsername,
		Password:    cfg.BackupPassword,
		Timeout:     12 * time.Second,
		MaxWarmIdle: defaultMaxWarmIdle,
	}
}

func (d *Dialer) RedactedString() string {
	auth := ""
	if d.Username != "" || d.Password != "" {
		auth = " (auth)"
	}
	return fmt.Sprintf("%s://%s%s", d.Mode, d.Address, auth)
}

// DialContext connects to targetAddr ("host:port") strictly via the upstream proxy.
// Hostnames are always sent verbatim to the upstream so DNS resolves remotely.
func (d *Dialer) DialContext(ctx context.Context, network, targetAddr string) (net.Conn, error) {
	if d == nil || strings.TrimSpace(d.Address) == "" {
		return nil, errors.New("upstream proxy address is empty")
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported network %q (TCP only through upstream)", network)
	}
	switch d.Mode {
	case "direct-socks5":
		return d.dialSOCKS5(ctx, targetAddr)
	case "direct-http", "local-proxy":
		return d.dialHTTPConnect(ctx, targetAddr)
	default:
		return nil, fmt.Errorf("unsupported upstream mode %q", d.Mode)
	}
}

// NewHTTPClient returns an http.Client whose transport dials exclusively through this upstream
// and ignores ambient process HTTP_PROXY/HTTPS_PROXY environment variables.
func (d *Dialer) NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	tr := &http.Transport{
		Proxy:                 nil, // Never read ambient env proxies
		DialContext:           d.DialContext,
		ForceAttemptHTTP2:     false,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 2 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}
}

func hasCRLFOrSpace(s string) bool {
	return strings.ContainsAny(s, "\r\n\t \x00")
}

func (d *Dialer) shouldPool() bool {
	if d == nil || d.MaxWarmIdle <= 0 || strings.TrimSpace(d.Address) == "" {
		return false
	}
	return d.Mode == "direct-http" || d.Mode == "direct-socks5"
}

func (d *Dialer) applyConnDeadline(ctx context.Context, conn net.Conn) {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
}

func (d *Dialer) dialRawProxyFresh(ctx context.Context) (net.Conn, error) {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	nd := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 15 * time.Second,
	}
	conn, err := nd.DialContext(ctx, "tcp", d.Address)
	if err != nil {
		return nil, fmt.Errorf("connect upstream %s: %w", d.Address, err)
	}
	if tc, ok := conn.(*net.TCPConn); ok {
		// Disable Nagle so proxy CONNECT / SOCKS5 greeting + TLS ClientHello flush immediately over high-RTT links.
		_ = tc.SetNoDelay(true)
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(15 * time.Second)
	}
	d.applyConnDeadline(ctx, conn)
	return conn, nil
}

// DrainPool immediately closes and clears all pre-warmed idle TCP connections
// (used on fail-closed lock or config rotation).
func (d *Dialer) DrainPool() {
	if d == nil {
		return
	}
	d.poolMu.Lock()
	d.poolEpoch++
	conns := d.warmPool
	d.warmPool = nil
	d.poolMu.Unlock()
	for _, wc := range conns {
		_ = wc.conn.Close()
	}
}

// WarmPool pre-warms up to target idle TCP connections to the configured remote proxy.
func (d *Dialer) WarmPool(ctx context.Context, target int) {
	if !d.shouldPool() {
		return
	}
	if target <= 0 {
		target = 2
	}
	if target > d.MaxWarmIdle {
		target = d.MaxWarmIdle
	}

	now := time.Now()
	var expired []net.Conn
	d.poolMu.Lock()
	kept := d.warmPool[:0]
	for _, wc := range d.warmPool {
		if now.Sub(wc.createdAt) < warmConnMaxAge {
			kept = append(kept, wc)
		} else {
			expired = append(expired, wc.conn)
		}
	}
	d.warmPool = kept
	epoch := d.poolEpoch
	need := target - len(d.warmPool)
	d.poolMu.Unlock()

	for _, c := range expired {
		_ = c.Close()
	}
	if need <= 0 {
		return
	}

	for i := 0; i < need; i++ {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		if d.refilling.Add(1) > 4 {
			d.refilling.Add(-1)
			return
		}
		go func() {
			defer d.refilling.Add(-1)
			dialCtx, cancel := context.WithTimeout(context.Background(), 4500*time.Millisecond)
			defer cancel()
			conn, err := d.dialRawProxyFresh(dialCtx)
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Time{})

			d.poolMu.Lock()
			if d.poolEpoch != epoch || len(d.warmPool) >= d.MaxWarmIdle {
				d.poolMu.Unlock()
				_ = conn.Close()
				return
			}
			d.warmPool = append(d.warmPool, warmConn{
				conn:      conn,
				createdAt: time.Now(),
			})
			d.poolMu.Unlock()
		}()
	}
}

func (d *Dialer) acquireRawProxy(ctx context.Context) (net.Conn, bool, error) {
	if d.shouldPool() {
		now := time.Now()
		var popped net.Conn
		var stale []net.Conn

		d.poolMu.Lock()
		for len(d.warmPool) > 0 {
			lastIdx := len(d.warmPool) - 1
			wc := d.warmPool[lastIdx]
			d.warmPool = d.warmPool[:lastIdx]
			if now.Sub(wc.createdAt) < warmConnMaxAge {
				popped = wc.conn
				break
			}
			stale = append(stale, wc.conn)
		}
		d.poolMu.Unlock()

		for _, c := range stale {
			_ = c.Close()
		}
		if popped != nil {
			d.applyConnDeadline(ctx, popped)
			// Top up asynchronously so subsequent bursts skip cross-Pacific SYN RTT.
			d.WarmPool(context.Background(), 2)
			return popped, true, nil
		}
	}

	conn, err := d.dialRawProxyFresh(ctx)
	if err != nil {
		return nil, false, err
	}
	if d.shouldPool() {
		d.WarmPool(context.Background(), 2)
	}
	return conn, false, nil
}

func (d *Dialer) dialHTTPConnect(ctx context.Context, targetAddr string) (net.Conn, error) {
	if hasCRLFOrSpace(targetAddr) {
		return nil, fmt.Errorf("invalid CONNECT target %q", targetAddr)
	}
	conn, fromPool, err := d.acquireRawProxy(ctx)
	if err != nil {
		return nil, err
	}
	tun, retryable, err := d.tryHTTPConnect(conn, targetAddr)
	if err == nil {
		return tun, nil
	}
	if fromPool && retryable && ctx.Err() == nil {
		freshConn, ferr := d.dialRawProxyFresh(ctx)
		if ferr != nil {
			return nil, ferr
		}
		tun2, _, err2 := d.tryHTTPConnect(freshConn, targetAddr)
		return tun2, err2
	}
	return nil, err
}

func (d *Dialer) tryHTTPConnect(conn net.Conn, targetAddr string) (tun net.Conn, retryable bool, err error) {
	var sb strings.Builder
	sb.WriteString("CONNECT ")
	sb.WriteString(targetAddr)
	sb.WriteString(" HTTP/1.1\r\nHost: ")
	sb.WriteString(targetAddr)
	sb.WriteString("\r\nProxy-Connection: Keep-Alive\r\n")
	if d.Username != "" || d.Password != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(d.Username + ":" + d.Password))
		sb.WriteString("Proxy-Authorization: Basic ")
		sb.WriteString(cred)
		sb.WriteString("\r\n")
	}
	sb.WriteString("\r\n")

	if _, err := io.WriteString(conn, sb.String()); err != nil {
		_ = conn.Close()
		return nil, true, fmt.Errorf("write CONNECT to %s: %w", d.Address, err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, true, fmt.Errorf("read CONNECT response from %s: %w", d.Address, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, false, fmt.Errorf("upstream %s rejected CONNECT %s: HTTP %d", d.Address, targetAddr, resp.StatusCode)
	}
	// Clear handshake deadline for tunnel phase.
	_ = conn.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, br: br}, false, nil
	}
	return conn, false, nil
}

func (d *Dialer) dialSOCKS5(ctx context.Context, targetAddr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid target %q: %w", targetAddr, err)
	}
	if hasCRLFOrSpace(host) || host == "" {
		return nil, fmt.Errorf("invalid target host %q", host)
	}
	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum < 1 || portNum > 65535 {
		return nil, fmt.Errorf("invalid port %q", portStr)
	}

	conn, fromPool, err := d.acquireRawProxy(ctx)
	if err != nil {
		return nil, err
	}
	tun, retryable, err := d.trySOCKS5Handshake(conn, host, portNum, targetAddr)
	if err == nil {
		return tun, nil
	}
	if fromPool && retryable && ctx.Err() == nil {
		freshConn, ferr := d.dialRawProxyFresh(ctx)
		if ferr != nil {
			return nil, ferr
		}
		tun2, _, err2 := d.trySOCKS5Handshake(freshConn, host, portNum, targetAddr)
		return tun2, err2
	}
	return nil, err
}

func (d *Dialer) trySOCKS5Handshake(conn net.Conn, host string, portNum int, targetAddr string) (net.Conn, bool, error) {
	// 1. Greeting
	useAuth := d.Username != "" || d.Password != ""
	var greeting []byte
	if useAuth {
		greeting = []byte{0x05, 0x02, 0x00, 0x02}
	} else {
		greeting = []byte{0x05, 0x01, 0x00}
	}
	if _, err := conn.Write(greeting); err != nil {
		_ = conn.Close()
		return nil, true, fmt.Errorf("socks5 greeting write: %w", err)
	}
	var methodResp [2]byte
	if _, err := io.ReadFull(conn, methodResp[:]); err != nil {
		_ = conn.Close()
		return nil, true, fmt.Errorf("socks5 greeting read: %w", err)
	}
	if methodResp[0] != 0x05 {
		_ = conn.Close()
		return nil, false, fmt.Errorf("socks5 invalid version 0x%02x", methodResp[0])
	}
	switch methodResp[1] {
	case 0x00:
		// No auth required
	case 0x02:
		if !useAuth {
			_ = conn.Close()
			return nil, false, errors.New("socks5 upstream requires username/password authentication")
		}
		if len(d.Username) > 255 || len(d.Password) > 255 {
			_ = conn.Close()
			return nil, false, errors.New("socks5 credentials exceed 255 bytes")
		}
		authReq := make([]byte, 0, 3+len(d.Username)+len(d.Password))
		authReq = append(authReq, 0x01, byte(len(d.Username)))
		authReq = append(authReq, []byte(d.Username)...)
		authReq = append(authReq, byte(len(d.Password)))
		authReq = append(authReq, []byte(d.Password)...)
		if _, err := conn.Write(authReq); err != nil {
			_ = conn.Close()
			return nil, true, fmt.Errorf("socks5 auth write: %w", err)
		}
		var authResp [2]byte
		if _, err := io.ReadFull(conn, authResp[:]); err != nil {
			_ = conn.Close()
			return nil, true, fmt.Errorf("socks5 auth read: %w", err)
		}
		if authResp[1] != 0x00 {
			_ = conn.Close()
			return nil, false, fmt.Errorf("socks5 authentication rejected by %s (code=0x%02x)", d.Address, authResp[1])
		}
	case 0xFF:
		_ = conn.Close()
		return nil, false, fmt.Errorf("socks5 upstream %s offered no acceptable auth method", d.Address)
	default:
		_ = conn.Close()
		return nil, false, fmt.Errorf("socks5 unsupported auth method 0x%02x", methodResp[1])
	}

	// 2. CONNECT request (always ATYP=DOMAIN for hostnames -> zero local DNS leak)
	req := make([]byte, 0, 7+len(host))
	req = append(req, 0x05, 0x01, 0x00)
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 0x01)
			req = append(req, ip4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			_ = conn.Close()
			return nil, false, fmt.Errorf("hostname too long for socks5: %s", host)
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	var portBuf [2]byte
	binary.BigEndian.PutUint16(portBuf[:], uint16(portNum))
	req = append(req, portBuf[:]...)

	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("socks5 connect write: %w", err)
	}

	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("socks5 connect reply header: %w", err)
	}
	if hdr[0] != 0x05 {
		_ = conn.Close()
		return nil, false, fmt.Errorf("socks5 invalid reply version 0x%02x", hdr[0])
	}
	if hdr[1] != 0x00 {
		_ = conn.Close()
		return nil, false, fmt.Errorf("socks5 upstream %s connect %s failed (rep=0x%02x)", d.Address, targetAddr, hdr[1])
	}
	// Consume BND.ADDR + BND.PORT
	switch hdr[3] {
	case 0x01:
		var skip [6]byte
		if _, err := io.ReadFull(conn, skip[:]); err != nil {
			_ = conn.Close()
			return nil, false, err
		}
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			_ = conn.Close()
			return nil, false, err
		}
		skip := make([]byte, int(l[0])+2)
		if _, err := io.ReadFull(conn, skip); err != nil {
			_ = conn.Close()
			return nil, false, err
		}
	case 0x04:
		var skip [18]byte
		if _, err := io.ReadFull(conn, skip[:]); err != nil {
			_ = conn.Close()
			return nil, false, err
		}
	default:
		_ = conn.Close()
		return nil, false, fmt.Errorf("socks5 unknown bound atyp 0x%02x", hdr[3])
	}

	_ = conn.SetDeadline(time.Time{})
	return conn, false, nil
}

type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.br.Read(p)
}
