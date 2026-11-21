package proxy

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"peaceclaude/internal/audit"
	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/policy"
	"peaceclaude/internal/upstream"
)

func startFakeUpstreamHTTPProxy(t *testing.T, backendAddr string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				br := bufio.NewReader(conn)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				if req.Method == http.MethodConnect {
					dst, err := net.Dial("tcp", backendAddr)
					if err != nil {
						_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
						return
					}
					defer dst.Close()
					_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
					go func() { _, _ = io.Copy(dst, br) }()
					_, _ = io.Copy(conn, dst)
				}
			}(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func TestGatewayFailClosedTelemetryAndSOCKS5(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())

	// Target backend simulating an Anthropic HTTP endpoint over tunnel.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied-ok"))
	}))
	defer backend.Close()
	backendAddr := strings.TrimPrefix(backend.URL, "http://")

	upAddr, stopUp := startFakeUpstreamHTTPProxy(t, backendAddr)
	defer stopUp()

	cfg := config.Default()
	cfg.Upstream.Mode = "direct-http"
	cfg.Upstream.Address = upAddr
	cfg.Egress.ExpectedIP = "198.51.100.10"
	cfg.Egress.FailClosed = true
	cfg.Shield.TelemetryShield = true
	cfg.Capture.Enabled = true

	gate := egress.NewGate(cfg)
	eng := policy.NewEngine(cfg)
	store := audit.NewStore(true, 100)
	gw := NewGateway(cfg, eng, gate, store)

	httpSrv := httptest.NewServer(gw)
	defer httpSrv.Close()
	proxyURL, _ := url.Parse(httpSrv.URL)

	// Start local SOCKS5 listener too.
	socksLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socksLn.Close()
	go func() { _ = gw.ServeSOCKS5(socksLn) }()

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   3 * time.Second,
	}

	// 1. Gate is LOCKED initially -> HTTP proxy to api.anthropic.com MUST return 503 Fail-Closed!
	resp, err := client.Get("http://api.anthropic.com/v1/messages")
	if err != nil {
		t.Fatalf("expected 503 response, got error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 fail-closed when locked, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-PeaceClaude-Block") != "block_failclosed" {
		t.Fatalf("missing block header: %q", resp.Header.Get("X-PeaceClaude-Block"))
	}

	// SOCKS5 MUST also reject when locked.
	sDialer := &upstream.Dialer{Mode: "direct-socks5", Address: socksLn.Addr().String(), Timeout: 2 * time.Second}
	if _, err := sDialer.DialContext(context.Background(), "tcp", "api.anthropic.com:443"); err == nil {
		t.Fatal("SOCKS5 must fail-closed when gate is locked")
	}

	// 2. Open gate via mock probe matching expected IP.
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) egress.ProbeResult {
		return egress.ProbeResult{CheckedAt: time.Now().UTC(), OK: true, EgressIP: "198.51.100.10", Country: "US"}
	})
	gate.Check(context.Background(), false)

	// 3. Telemetry MUST STILL be blocked even when gate is OPEN.
	resp, err = client.Get("http://o1158394.ingest.us.sentry.io/api/1/store/")
	if err != nil {
		t.Fatalf("telemetry request err: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for telemetry, got %d", resp.StatusCode)
	}

	// 4. Verified Anthropic request over HTTP proxy + SOCKS5 MUST succeed via upstream!
	req, _ := http.NewRequest("GET", "http://api.anthropic.com/v1/messages?api_key=sk-secret-abc", nil)
	req.Header.Set("Authorization", "Bearer sk-ant-secret-xyz")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("open anthropic request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "proxied-ok" {
		t.Fatalf("unexpected open response: code=%d body=%q", resp.StatusCode, string(body))
	}

	// Verify SOCKS5 succeeds when gate is open.
	sConn, err := sDialer.DialContext(context.Background(), "tcp", "api.anthropic.com:80")
	if err != nil {
		t.Fatalf("SOCKS5 open dial failed: %v", err)
	}
	_, _ = io.WriteString(sConn, "GET / HTTP/1.1\r\nHost: api.anthropic.com\r\nConnection: close\r\n\r\n")
	rawSocks, _ := io.ReadAll(sConn)
	_ = sConn.Close()
	if !strings.Contains(string(rawSocks), "proxied-ok") {
		t.Fatalf("unexpected SOCKS5 response: %q", string(rawSocks))
	}

	// 5. Verify captures recorded requests and NEVER leaked secrets.
	caps := store.ListCaptures(20)
	if len(caps) < 3 {
		t.Fatalf("expected >=3 captures, got %d", len(caps))
	}
	for _, c := range caps {
		if strings.Contains(c.Path, "sk-secret-abc") {
			t.Fatalf("capture path leaked query secret: %s", c.Path)
		}
		if v, ok := c.RequestHeaders["Authorization"]; ok && v != audit.RedactedToken {
			t.Fatalf("capture leaked Authorization header: %s", v)
		}
	}

	// 6. Verify PAC output contains expected domains and Anthropic CIDRs.
	pac := RenderPAC(cfg, "claude")
	for _, need := range []string{"anthropic.com", "claude.ai", "160.79.104.0", "18897"} {
		if !strings.Contains(pac, need) {
			t.Fatalf("PAC missing %q", need)
		}
	}
}
