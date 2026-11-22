package upstream

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func startEchoServer(t *testing.T) (string, func()) {
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
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

func startMockSOCKS5(t *testing.T, wantUser, wantPass, redirectTarget string) (string, chan string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	seenHost := make(chan string, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				var hdr [2]byte
				if _, err := io.ReadFull(conn, hdr[:]); err != nil {
					return
				}
				methods := make([]byte, hdr[1])
				if _, err := io.ReadFull(conn, methods); err != nil {
					return
				}
				if wantUser != "" {
					_, _ = conn.Write([]byte{0x05, 0x02})
					var ah [2]byte
					if _, err := io.ReadFull(conn, ah[:]); err != nil {
						return
					}
					ub := make([]byte, ah[1])
					_, _ = io.ReadFull(conn, ub)
					var pl [1]byte
					_, _ = io.ReadFull(conn, pl[:])
					pb := make([]byte, pl[0])
					_, _ = io.ReadFull(conn, pb)
					if string(ub) != wantUser || string(pb) != wantPass {
						_, _ = conn.Write([]byte{0x01, 0x01})
						return
					}
					_, _ = conn.Write([]byte{0x01, 0x00})
				} else {
					_, _ = conn.Write([]byte{0x05, 0x00})
				}
				var req [4]byte
				if _, err := io.ReadFull(conn, req[:]); err != nil {
					return
				}
				var host string
				switch req[3] {
				case 0x01:
					var b [4]byte
					_, _ = io.ReadFull(conn, b[:])
					host = net.IP(b[:]).String()
				case 0x03:
					var l [1]byte
					_, _ = io.ReadFull(conn, l[:])
					b := make([]byte, l[0])
					_, _ = io.ReadFull(conn, b)
					host = string(b)
				}
				var p [2]byte
				_, _ = io.ReadFull(conn, p[:])
				_ = binary.BigEndian.Uint16(p[:])
				seenHost <- host

				dst, err := net.Dial("tcp", redirectTarget)
				if err != nil {
					_, _ = conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
					return
				}
				defer dst.Close()
				_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0})
				go func() { _, _ = io.Copy(dst, conn) }()
				_, _ = io.Copy(conn, dst)
			}(c)
		}
	}()
	return ln.Addr().String(), seenHost, func() { _ = ln.Close() }
}

func TestSOCKS5DialerAuthAndRemoteDNS(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	socksAddr, seenHost, stopSocks := startMockSOCKS5(t, "wsuser", "wspass-secret", echoAddr)
	defer stopSocks()

	d := &Dialer{
		Mode:     "direct-socks5",
		Address:  socksAddr,
		Username: "wsuser",
		Password: "wspass-secret",
		Timeout:  3 * time.Second,
	}

	conn, err := d.DialContext(context.Background(), "tcp", "platform.claude.com:443")
	if err != nil {
		t.Fatalf("socks5 dial failed: %v", err)
	}
	defer conn.Close()

	// Verify hostname was sent verbatim (remote DNS resolution, ATYP=0x03).
	select {
	case h := <-seenHost:
		if h != "platform.claude.com" {
			t.Fatalf("expected remote domain platform.claude.com, got %q", h)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mock socks5 host")
	}

	_, _ = conn.Write([]byte("hello-cage"))
	buf := make([]byte, 10)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("echo read: %v", err)
	}
	if string(buf) != "hello-cage" {
		t.Fatalf("got %q, want hello-cage", string(buf))
	}

	// Negative test: wrong password must fail and never leak the secret.
	bad := &Dialer{
		Mode:     "direct-socks5",
		Address:  socksAddr,
		Username: "wsuser",
		Password: "wrong-password-xyz",
		Timeout:  3 * time.Second,
	}
	_, err = bad.DialContext(context.Background(), "tcp", "platform.claude.com:443")
	if err == nil {
		t.Fatal("expected auth failure")
	}
	if strings.Contains(err.Error(), "wrong-password-xyz") {
		t.Fatalf("error message leaked password: %v", err)
	}
}

func TestHTTPConnectDialerAuth(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("u1:p1-secret"))

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
				if req.Header.Get("Proxy-Authorization") != wantAuth {
					_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Auth Required\r\n\r\n")
					return
				}
				dst, err := net.Dial("tcp", echoAddr)
				if err != nil {
					return
				}
				defer dst.Close()
				_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go func() { _, _ = io.Copy(dst, br) }()
				_, _ = io.Copy(conn, dst)
			}(c)
		}
	}()

	d := &Dialer{
		Mode:     "direct-http",
		Address:  ln.Addr().String(),
		Username: "u1",
		Password: "p1-secret",
		Timeout:  3 * time.Second,
	}
	conn, err := d.DialContext(context.Background(), "tcp", "api.anthropic.com:443")
	if err != nil {
		t.Fatalf("http connect failed: %v", err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got %q", buf)
	}

	// Negative test: CRLF injection in HTTP CONNECT target MUST be rejected.
	if _, err := d.DialContext(context.Background(), "tcp", "claude.ai:443\r\nX-Inject: 1"); err == nil {
		t.Fatal("expected CRLF target in HTTP CONNECT to be rejected")
	}

	// WarmPool test: verifies pre-warmed connections populate & DrainPool closes them cleanly.
	dPool := &Dialer{
		Mode:        "direct-http",
		Address:     ln.Addr().String(),
		Username:    "u1",
		Password:    "p1-secret",
		Timeout:     3 * time.Second,
		MaxWarmIdle: 2,
	}
	dPool.WarmPool(context.Background(), 2)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		dPool.poolMu.Lock()
		n := len(dPool.warmPool)
		dPool.poolMu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	dPool.poolMu.Lock()
	warmed := len(dPool.warmPool)
	dPool.poolMu.Unlock()
	if warmed == 0 {
		t.Fatalf("expected WarmPool to pre-warm idle connections, got %d", warmed)
	}
	// Dial using warm pool then drain pool safely.
	conn2, err := dPool.DialContext(context.Background(), "tcp", "claude.ai:443")
	if err != nil {
		t.Fatalf("pooled http connect failed: %v", err)
	}
	_ = conn2.Close()
	dPool.DrainPool()
	dPool.poolMu.Lock()
	afterDrain := len(dPool.warmPool)
	dPool.poolMu.Unlock()
	if afterDrain != 0 {
		t.Fatalf("expected 0 warm connections after DrainPool, got %d", afterDrain)
	}
}

func TestDirectHTTPRoutesGoogleAndResetsViaSOCKS5(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

	// Dual-protocol upstream listener:
	// - Resets plaintext HTTP CONNECT (simulating DPI RST on Google / sensitive domains)
	// - Accepts binary SOCKS5 connections transparently on the same host:port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var httpConnectCount, socks5Count atomic.Int32
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				var first [1]byte
				if _, err := io.ReadFull(conn, first[:]); err != nil {
					return
				}
				if first[0] == 0x05 {
					socks5Count.Add(1)
					var nMethods [1]byte
					if _, err := io.ReadFull(conn, nMethods[:]); err != nil {
						return
					}
					methods := make([]byte, nMethods[0])
					if _, err := io.ReadFull(conn, methods); err != nil {
						return
					}
					_, _ = conn.Write([]byte{0x05, 0x02})
					var authHdr [2]byte
					if _, err := io.ReadFull(conn, authHdr[:]); err != nil {
						return
					}
					u := make([]byte, authHdr[1])
					_, _ = io.ReadFull(conn, u)
					var plen [1]byte
					_, _ = io.ReadFull(conn, plen[:])
					pw := make([]byte, plen[0])
					_, _ = io.ReadFull(conn, pw)
					_, _ = conn.Write([]byte{0x01, 0x00})
					var reqHdr [4]byte
					_, _ = io.ReadFull(conn, reqHdr[:])
					if reqHdr[3] == 0x03 {
						var l [1]byte
						_, _ = io.ReadFull(conn, l[:])
						d := make([]byte, l[0])
						_, _ = io.ReadFull(conn, d)
					}
					var pbuf [2]byte
					_, _ = io.ReadFull(conn, pbuf[:])
					dst, err := net.Dial("tcp", echoAddr)
					if err != nil {
						return
					}
					defer dst.Close()
					_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0})
					go func() { _, _ = io.Copy(dst, conn) }()
					_, _ = io.Copy(conn, dst)
					return
				}
				// Plaintext HTTP CONNECT -> abruptly close (simulates TCP reset DPI)
				httpConnectCount.Add(1)
			}(c)
		}
	}()

	d := &Dialer{
		Mode:     "direct-http",
		Address:  ln.Addr().String(),
		Username: "u1",
		Password: "p1",
		Timeout:  3 * time.Second,
	}

	// 1. First dial to accounts.google.com hits HTTP CONNECT reset, learns to fallback via SOCKS5
	c1, err := d.DialContext(context.Background(), "tcp", "accounts.google.com:443")
	if err != nil {
		t.Fatalf("accounts.google.com dial failed: %v", err)
	}
	_, _ = c1.Write([]byte("goog"))
	buf1 := make([]byte, 4)
	if _, err := io.ReadFull(c1, buf1); err != nil || string(buf1) != "goog" {
		t.Fatalf("unexpected google roundtrip: err=%v buf=%q", err, buf1)
	}
	_ = c1.Close()
	if httpConnectCount.Load() != 1 || socks5Count.Load() != 1 {
		t.Fatalf("expected 1 HTTP reset -> 1 SOCKS5 fallback, got http=%d socks=%d", httpConnectCount.Load(), socks5Count.Load())
	}

	// 2. Second dial to learned accounts.google.com goes straight to SOCKS5 without hitting HTTP reset
	c2, err := d.DialContext(context.Background(), "tcp", "accounts.google.com:443")
	if err != nil {
		t.Fatalf("accounts.google.com learned SOCKS5 dial failed: %v", err)
	}
	_, _ = c2.Write([]byte("clau"))
	buf2 := make([]byte, 4)
	if _, err := io.ReadFull(c2, buf2); err != nil || string(buf2) != "clau" {
		t.Fatalf("unexpected learned SOCKS5 roundtrip: err=%v buf=%q", err, buf2)
	}
	_ = c2.Close()
	if httpConnectCount.Load() != 1 || socks5Count.Load() != 2 {
		t.Fatalf("expected learned host to bypass HTTP CONNECT directly (http=1, socks=2), got http=%d socks=%d", httpConnectCount.Load(), socks5Count.Load())
	}
}


func TestIsLoopbackOrPrivateHost(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:5492", true},
		{"127.0.0.1:7897", true},
		{"localhost:18897", true},
		{"sub.localhost:8080", true},
		{"[::1]:1080", true},
		{"192.168.1.10:8080", true},
		{"10.4.235.1:8080", true},
		{"172.16.0.1:443", true},
		{"169.254.1.1:443", true},
		{"203.0.113.42:5492", false},
		{"198.51.100.18:443", false},
		{"proxy.example.com:8080", false},
	}
	for _, tc := range cases {
		if got := isLoopbackOrPrivateHost(tc.addr); got != tc.want {
			t.Fatalf("isLoopbackOrPrivateHost(%q) = %t, want %t", tc.addr, got, tc.want)
		}
	}
}

func TestParsePhysicalDefaultInterface(t *testing.T) {
	if got := ParsePhysicalDefaultForTest(
		"Destination        Gateway            Flags               Netif Expire\n"+
			"default            link#24            UCSIg               utun6       \n"+
			"default            192.168.1.1        UGScg                 en0       \n"+
			"10.0.0.0/8         10.0.0.1           UGSc                utun6       \n",
		func(n string) bool { return n == "en0" },
	); got != "en0" {
		t.Fatalf("expected en0 even when utun6 default appears first, got %q", got)
	}

	if got := ParsePhysicalDefaultForTest(
		"default            link#24            UCSIg               utun6       \n",
		func(string) bool { return true },
	); got != "" {
		t.Fatalf("expected empty when only utun6 exists, got %q", got)
	}
}

func TestUpstreamBypassVPNLocalhostNeverBinds(t *testing.T) {
	echoAddr, stopEcho := startEchoServer(t)
	defer stopEcho()

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
			go func(conn net.Conn) {
				defer conn.Close()
				br := bufio.NewReader(conn)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				_ = req.Body.Close()
				dst, err := net.Dial("tcp", echoAddr)
				if err != nil {
					return
				}
				defer dst.Close()
				_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go func() { _, _ = io.Copy(dst, br) }()
				_, _ = io.Copy(conn, dst)
			}(c)
		}
	}()

	d := &Dialer{
		Mode:      "direct-http",
		Address:   ln.Addr().String(),
		BypassVPN: true,
		Timeout:   2 * time.Second,
	}
	conn, err := d.DialContext(context.Background(), "tcp", "claude.ai:443")
	if err != nil {
		t.Fatalf("localhost direct-http with BypassVPN=true failed: %v", err)
	}
	_, _ = conn.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("roundtrip mismatch: err=%v buf=%q", err, buf)
	}
	_ = conn.Close()

	st := d.RouteStatus()
	if st.BoundPhysical || st.ActiveInterface != "system-default" || !strings.Contains(st.SkippedReason, "loopback") {
		t.Fatalf("expected localhost upstream to stay on system-default without binding, got %+v", st)
	}
}
