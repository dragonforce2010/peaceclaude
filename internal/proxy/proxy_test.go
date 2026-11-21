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
	"sync"
	"sync/atomic"
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

func TestRenderPACCoversGoogleOAuthAndKeepsWWWGoogleDirect(t *testing.T) {
	cfg := config.Default()
	pac := RenderPAC(cfg, "claude")
	for _, want := range []string{
		"accounts.google.com",
		"apis.google.com",
		"gstatic.com",
		"googleapis.com",
		"googleusercontent.com",
		"hcaptcha.com",
		"challenges.cloudflare.com",
		"stripe.network",
		"return \"DIRECT\";",
	} {
		if !strings.Contains(pac, want) {
			t.Fatalf("PAC missing %q", want)
		}
	}
	if strings.Contains(pac, "www.google.com") || strings.Contains(pac, `".google.com"`) {
		t.Fatalf("PAC must NOT force general google.com traffic through Claude proxy:\n%s", pac)
	}
}

func TestGatewayConnectRetriesTransientUpstreamFailureAndBackupFailover(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("retry-ok"))
	}))
	defer backend.Close()
	backendAddr := strings.TrimPrefix(backend.URL, "http://")

	// Flaky primary upstream: drops the very first connection immediately with EOF, succeeds on retry.
	var attempts atomic.Int32
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n := attempts.Add(1)
			go func(conn net.Conn, attempt int32) {
				defer conn.Close()
				if attempt == 1 {
					return
				}
				br := bufio.NewReader(conn)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				dst, err := net.Dial("tcp", backendAddr)
				if err != nil {
					return
				}
				defer dst.Close()
				_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go func() { _, _ = io.Copy(dst, br) }()
				_, _ = io.Copy(conn, dst)
			}(c, n)
		}
	}()

	cfg := config.Default()
	cfg.Upstream.Mode = "direct-http"
	cfg.Upstream.Address = ln.Addr().String()
	cfg.Egress.ExpectedIP = "198.51.100.10"
	gate := egress.NewGate(cfg)
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) egress.ProbeResult {
		return egress.ProbeResult{CheckedAt: time.Now().UTC(), OK: true, EgressIP: "198.51.100.10", Country: "US"}
	})
	gate.Check(context.Background(), false)

	gw := NewGateway(cfg, policy.NewEngine(cfg), gate, audit.NewStore(false, 10))
	httpSrv := httptest.NewServer(gw)
	defer httpSrv.Close()
	proxyURL, _ := url.Parse(httpSrv.URL)

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get("http://claude.ai/")
	if err != nil {
		t.Fatalf("expected transparent retry on EOF, got err: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "retry-ok" {
		t.Fatalf("unexpected retry response: code=%d body=%q", resp.StatusCode, string(body))
	}
	if attempts.Load() < 2 {
		t.Fatalf("expected >=2 upstream attempts, got %d", attempts.Load())
	}

	// Backup failover test: point primary at closed port, backup at healthy fake upstream.
	deadLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := deadLn.Addr().String()
	_ = deadLn.Close()

	backupAddr, stopBackup := startFakeUpstreamHTTPProxy(t, backendAddr)
	defer stopBackup()

	cfgBackup := config.Default()
	cfgBackup.Upstream.Mode = "direct-http"
	cfgBackup.Upstream.Address = deadAddr
	cfgBackup.Upstream.BackupMode = "direct-http"
	cfgBackup.Upstream.BackupAddress = backupAddr
	cfgBackup.Egress.ExpectedIP = "198.51.100.10"
	gw.UpdateConfig(cfgBackup)

	resp2, err := client.Get("http://accounts.google.com/")
	if err != nil {
		t.Fatalf("expected failover to backup upstream, got err: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK || string(body2) != "retry-ok" {
		t.Fatalf("unexpected backup response: code=%d body=%q", resp2.StatusCode, string(body2))
	}
}

func TestGatewayConcurrentConnectAndHotReloadRace(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("concurrent-ok"))
	}))
	defer backend.Close()
	backendAddr := strings.TrimPrefix(backend.URL, "http://")

	upAddr, stopUp := startFakeUpstreamHTTPProxy(t, backendAddr)
	defer stopUp()

	cfg := config.Default()
	cfg.Upstream.Mode = "direct-http"
	cfg.Upstream.Address = upAddr
	cfg.Egress.ExpectedIP = "198.51.100.10"

	gate := egress.NewGate(cfg)
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) egress.ProbeResult {
		return egress.ProbeResult{CheckedAt: time.Now().UTC(), OK: true, EgressIP: "198.51.100.10", Country: "US"}
	})
	gate.Check(context.Background(), false)

	gw := NewGateway(cfg, policy.NewEngine(cfg), gate, audit.NewStore(false, 100))
	httpSrv := httptest.NewServer(gw)
	defer httpSrv.Close()
	proxyURL, _ := url.Parse(httpSrv.URL)

	socksLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socksLn.Close()
	go func() { _ = gw.ServeSOCKS5(socksLn) }()

	stopReload := make(chan struct{})
	var reloadWG sync.WaitGroup
	reloadWG.Add(1)
	go func() {
		defer reloadWG.Done()
		for i := 0; ; i++ {
			select {
			case <-stopReload:
				return
			default:
				next := config.Default()
				next.Upstream.Mode = "direct-http"
				next.Upstream.Address = upAddr
				next.Egress.ExpectedIP = "198.51.100.10"
				next.Shield.AllowCorporateDirect = (i%2 == 0)
				gw.UpdateConfig(next)
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	const workers = 16
	const iters = 4
	errCh := make(chan error, workers*iters)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := &http.Client{
				Transport: &http.Transport{
					Proxy:             http.ProxyURL(proxyURL),
					DisableKeepAlives: true,
				},
				Timeout: 5 * time.Second,
			}
			sDialer := &upstream.Dialer{Mode: "direct-socks5", Address: socksLn.Addr().String(), Timeout: 4 * time.Second}
			for j := 0; j < iters; j++ {
				if (id+j)%2 == 0 {
					resp, err := client.Get("http://api.anthropic.com/v1/messages")
					if err != nil {
						errCh <- err
						return
					}
					body, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode != http.StatusOK || string(body) != "concurrent-ok" {
						errCh <- context.DeadlineExceeded
						return
					}
				} else {
					c, err := sDialer.DialContext(context.Background(), "tcp", "api.anthropic.com:80")
					if err != nil {
						errCh <- err
						return
					}
					_, _ = io.WriteString(c, "GET / HTTP/1.1\r\nHost: api.anthropic.com\r\nConnection: close\r\n\r\n")
					raw, _ := io.ReadAll(c)
					_ = c.Close()
					if !strings.Contains(string(raw), "concurrent-ok") {
						errCh <- context.DeadlineExceeded
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()
	close(stopReload)
	reloadWG.Wait()
	close(errCh)

	for e := range errCh {
		t.Fatalf("concurrent request failed under hot reload: %v", e)
	}
	if active := gate.ActiveStreams(); active != 0 {
		t.Fatalf("expected 0 active streams after completion, got %d", active)
	}
}

func TestTunnelBidirectionalHalfCloseCleansUpNoLeak(t *testing.T) {
	// Subcase 1: Client sends request then immediately CloseWrite (half-close);
	// upstream delays 250ms before sending response (simulating slow/SSE reply).
	// tunnelBidirectional MUST NOT prematurely terminate the upstream read stream.
	// Use real loopback TCP listeners because net.Pipe lacks CloseWrite().
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln1.Close()
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()

	clientTCP, err := net.Dial("tcp", ln1.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientTCP.Close()
	gwClientSide, err := ln1.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer gwClientSide.Close()

	srvSideCh := make(chan net.Conn, 1)
	go func() {
		c, _ := ln2.Accept()
		srvSideCh <- c
	}()
	gwUpSide, err := net.Dial("tcp", ln2.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer gwUpSide.Close()
	upServer := <-srvSideCh
	defer upServer.Close()

	tunnelDone := make(chan struct{})
	go func() {
		tunnelBidirectional(gwClientSide, gwClientSide, gwUpSide)
		close(tunnelDone)
	}()

	_, _ = io.WriteString(clientTCP, "PING")
	_ = clientTCP.(*net.TCPConn).CloseWrite()

	buf := make([]byte, 4)
	if _, err := io.ReadFull(upServer, buf); err != nil || string(buf) != "PING" {
		t.Fatalf("server failed reading request: %v %q", err, string(buf))
	}
	time.Sleep(250 * time.Millisecond)
	_, _ = io.WriteString(upServer, "PONG-OK")
	_ = upServer.Close()

	gotResp, err := io.ReadAll(clientTCP)
	if err != nil || string(gotResp) != "PONG-OK" {
		t.Fatalf("upstream response truncated after client CloseWrite: err=%v body=%q", err, string(gotResp))
	}
	select {
	case <-tunnelDone:
	case <-time.After(2 * time.Second):
		t.Fatal("tunnel did not finish promptly after both halves closed")
	}

	// Subcase 2: Upstream finishes writing and closes, while client lingers without closing its write side.
	// tunnelBidirectional MUST reap within the bounded ~3s idle window (< 4.5s).
	clientTCP2, err := net.Dial("tcp", ln1.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientTCP2.Close()
	gwClient2, err := ln1.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer gwClient2.Close()

	srvSideCh2 := make(chan net.Conn, 1)
	go func() {
		c, _ := ln2.Accept()
		srvSideCh2 <- c
	}()
	gwUp2, err := net.Dial("tcp", ln2.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer gwUp2.Close()
	upServer2 := <-srvSideCh2
	defer upServer2.Close()

	start := time.Now()
	tunnelDone2 := make(chan struct{})
	go func() {
		tunnelBidirectional(gwClient2, gwClient2, gwUp2)
		close(tunnelDone2)
	}()

	_, _ = io.WriteString(upServer2, "DONE")
	_ = upServer2.Close()

	got2, _ := io.ReadAll(clientTCP2)
	if string(got2) != "DONE" {
		t.Fatalf("expected DONE, got %q", string(got2))
	}
	select {
	case <-tunnelDone2:
		if elapsed := time.Since(start); elapsed > 4500*time.Millisecond {
			t.Fatalf("half-open linger reaper took too long: %v", elapsed)
		}
	case <-time.After(4500 * time.Millisecond):
		t.Fatal("tunnel leaked goroutine when client kept write side open after upstream close")
	}
}

func TestSecurityAndRoutingRegressions(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())

	// 1. PAC normalizes trailing dot FQDN and skips www.google.com
	cfg := config.Default()
	pac := RenderPAC(cfg, "claude")
	if !strings.Contains(pac, `replace(/\.+$/, "")`) {
		t.Fatalf("expected PAC JS to strip trailing dots from FQDN:\n%s", pac)
	}

	// 2. VMProxy non-loopback client cannot access loopback control targets
	cfgVM := config.Default()
	cfgVM.Listen.VMProxyEnabled = true
	cfgVM.Listen.VMProxySubnets = []string{"192.168.64.0/24"}
	gateVM := egress.NewGate(cfgVM)
	gwVM := NewGateway(cfgVM, policy.NewEngine(cfgVM), gateVM, audit.NewStore(false, 10))
	decVM := gwVM.evaluateForClient("192.168.64.5:51234", "127.0.0.1:18899")
	if decVM.Action != policy.ActionBlockPrivate {
		t.Fatalf("expected VMProxy non-loopback client to be blocked from host loopback, got %+v", decVM)
	}

	// 3. SOCKS5 ATYP=DOMAIN rejects CRLF/whitespace hostnames
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = gwVM.ServeSOCKS5(ln) }()

	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte{0x05, 0x01, 0x00})
	var m [2]byte
	_, _ = io.ReadFull(c, m[:])
	badHost := "claude.ai\r\nEvil: 1"
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(badHost))}
	req = append(req, []byte(badHost)...)
	req = append(req, 0x01, 0xbb)
	_, _ = c.Write(req)
	var rep [10]byte
	_, err = io.ReadFull(c, rep[:])
	if err != nil || rep[1] != 0x02 {
		t.Fatalf("expected SOCKS5 0x02 reject for CRLF host, got err=%v rep=%v", err, rep)
	}

	// 4. Secondary Fail-Closed TOCTOU guard: gate locks while upstream CONNECT dial is in flight -> MUST 503
	gateTOCTOU := egress.NewGate(cfg)
	gateTOCTOU.SetProbeFunc(func(_ context.Context, _ *config.Config) egress.ProbeResult {
		return egress.ProbeResult{CheckedAt: time.Now().UTC(), OK: true, EgressIP: "198.51.100.10", Country: "US"}
	})
	gateTOCTOU.Check(context.Background(), false)

	// Slow upstream accept that locks the gate right before responding 200.
	slowUpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer slowUpLn.Close()
	go func() {
		for {
			uc, err := slowUpLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				br := bufio.NewReader(conn)
				_, _ = http.ReadRequest(br)
				gateTOCTOU.SetManualLock(true, "locked during in-flight dial")
				time.Sleep(50 * time.Millisecond)
				_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
				time.Sleep(200 * time.Millisecond)
			}(uc)
		}
	}()

	cfgTOCTOU := config.Default()
	cfgTOCTOU.Upstream.Mode = "direct-http"
	cfgTOCTOU.Upstream.Address = slowUpLn.Addr().String()
	gwTOCTOU := NewGateway(cfgTOCTOU, policy.NewEngine(cfgTOCTOU), gateTOCTOU, audit.NewStore(false, 10))
	proxySrv := httptest.NewServer(gwTOCTOU)
	defer proxySrv.Close()

	rawConn, err := net.DialTimeout("tcp", strings.TrimPrefix(proxySrv.URL, "http://"), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer rawConn.Close()
	_, _ = io.WriteString(rawConn, "CONNECT claude.ai:443 HTTP/1.1\r\nHost: claude.ai:443\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(rawConn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("read CONNECT response failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after post-dial gate lock TOCTOU check, got HTTP %d", resp.StatusCode)
	}
}
