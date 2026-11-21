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
	"time"

	"peaceclaude/internal/config"
)

// Dialer dials outbound target addresses strictly through the configured upstream proxy.
// It never falls back to direct host connections.
type Dialer struct {
	Mode     string // "local-proxy" | "direct-socks5" | "direct-http"
	Address  string
	Username string
	Password string
	Timeout  time.Duration
}

func FromConfig(cfg config.UpstreamConfig) *Dialer {
	return &Dialer{
		Mode:     cfg.Mode,
		Address:  cfg.Address,
		Username: cfg.Username,
		Password: cfg.Password,
		Timeout:  12 * time.Second,
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
		Mode:     mode,
		Address:  cfg.BackupAddress,
		Username: cfg.BackupUsername,
		Password: cfg.BackupPassword,
		Timeout:  12 * time.Second,
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

func (d *Dialer) dialRawProxy(ctx context.Context) (net.Conn, error) {
	nd := &net.Dialer{
		Timeout:   d.Timeout,
		KeepAlive: 15 * time.Second,
	}
	conn, err := nd.DialContext(ctx, "tcp", d.Address)
	if err != nil {
		return nil, fmt.Errorf("connect upstream %s: %w", d.Address, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else if d.Timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(d.Timeout))
	}
	return conn, nil
}

func (d *Dialer) dialHTTPConnect(ctx context.Context, targetAddr string) (net.Conn, error) {
	conn, err := d.dialRawProxy(ctx)
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("write CONNECT to %s: %w", d.Address, err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read CONNECT response from %s: %w", d.Address, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream %s rejected CONNECT %s: HTTP %d", d.Address, targetAddr, resp.StatusCode)
	}
	// Clear handshake deadline for tunnel phase.
	_ = conn.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, br: br}, nil
	}
	return conn, nil
}

func (d *Dialer) dialSOCKS5(ctx context.Context, targetAddr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid target %q: %w", targetAddr, err)
	}
	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum < 1 || portNum > 65535 {
		return nil, fmt.Errorf("invalid port %q", portStr)
	}

	conn, err := d.dialRawProxy(ctx)
	if err != nil {
		return nil, err
	}

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
		return nil, fmt.Errorf("socks5 greeting write: %w", err)
	}
	var methodResp [2]byte
	if _, err := io.ReadFull(conn, methodResp[:]); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 greeting read: %w", err)
	}
	if methodResp[0] != 0x05 {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 invalid version 0x%02x", methodResp[0])
	}
	switch methodResp[1] {
	case 0x00:
		// No auth required
	case 0x02:
		if !useAuth {
			_ = conn.Close()
			return nil, errors.New("socks5 upstream requires username/password authentication")
		}
		if len(d.Username) > 255 || len(d.Password) > 255 {
			_ = conn.Close()
			return nil, errors.New("socks5 credentials exceed 255 bytes")
		}
		authReq := make([]byte, 0, 3+len(d.Username)+len(d.Password))
		authReq = append(authReq, 0x01, byte(len(d.Username)))
		authReq = append(authReq, []byte(d.Username)...)
		authReq = append(authReq, byte(len(d.Password)))
		authReq = append(authReq, []byte(d.Password)...)
		if _, err := conn.Write(authReq); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("socks5 auth write: %w", err)
		}
		var authResp [2]byte
		if _, err := io.ReadFull(conn, authResp[:]); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("socks5 auth read: %w", err)
		}
		if authResp[1] != 0x00 {
			_ = conn.Close()
			return nil, fmt.Errorf("socks5 authentication rejected by %s (code=0x%02x)", d.Address, authResp[1])
		}
	case 0xFF:
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 upstream %s offered no acceptable auth method", d.Address)
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 unsupported auth method 0x%02x", methodResp[1])
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
			return nil, fmt.Errorf("hostname too long for socks5: %s", host)
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	var portBuf [2]byte
	binary.BigEndian.PutUint16(portBuf[:], uint16(portNum))
	req = append(req, portBuf[:]...)

	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 connect write: %w", err)
	}

	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 connect reply header: %w", err)
	}
	if hdr[0] != 0x05 {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 invalid reply version 0x%02x", hdr[0])
	}
	if hdr[1] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 upstream %s connect %s failed (rep=0x%02x)", d.Address, targetAddr, hdr[1])
	}
	// Consume BND.ADDR + BND.PORT
	switch hdr[3] {
	case 0x01:
		var skip [6]byte
		if _, err := io.ReadFull(conn, skip[:]); err != nil {
			_ = conn.Close()
			return nil, err
		}
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			_ = conn.Close()
			return nil, err
		}
		skip := make([]byte, int(l[0])+2)
		if _, err := io.ReadFull(conn, skip); err != nil {
			_ = conn.Close()
			return nil, err
		}
	case 0x04:
		var skip [18]byte
		if _, err := io.ReadFull(conn, skip[:]); err != nil {
			_ = conn.Close()
			return nil, err
		}
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("socks5 unknown bound atyp 0x%02x", hdr[3])
	}

	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.br.Read(p)
}
