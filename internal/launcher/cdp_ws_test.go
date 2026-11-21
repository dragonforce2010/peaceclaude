package launcher

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestValidateLoopbackWSURL(t *testing.T) {
	for _, valid := range []string{
		"ws://127.0.0.1:9222/devtools/browser/abc",
		"ws://localhost:19222/devtools/page/1",
		"ws://[::1]:9222/devtools/page/2",
	} {
		if _, err := validateLoopbackWSURL(valid); err != nil {
			t.Fatalf("expected valid loopback url %q, got %v", valid, err)
		}
	}
	for _, bad := range []string{
		"wss://127.0.0.1:9222/x",
		"ws://example.com:9222/x",
		"ws://192.168.1.5:9222/x",
		"ws://127.0.0.1/no-port",
		"http://127.0.0.1:9222/x",
	} {
		if _, err := validateLoopbackWSURL(bad); err == nil {
			t.Fatalf("expected %q to be rejected by validateLoopbackWSURL", bad)
		}
	}
}

func writeServerWSFrame(w io.Writer, fin bool, opcode byte, payload []byte) error {
	var hdr [10]byte
	if fin {
		hdr[0] = 0x80 | (opcode & 0x0f)
	} else {
		hdr[0] = opcode & 0x0f
	}
	n := len(payload)
	idx := 2
	switch {
	case n < 126:
		hdr[1] = byte(n)
	case n <= 65535:
		hdr[1] = 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(n))
		idx = 4
	default:
		hdr[1] = 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(n))
		idx = 10
	}
	if _, err := w.Write(hdr[:idx]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func TestCDPLoadExtensionAndNavigateWithFragmentedPingFrames(t *testing.T) {
	port, err := pickLoopbackPort()
	if err != nil {
		t.Fatalf("pickLoopbackPort: %v", err)
	}
	if port <= 0 {
		t.Fatalf("unexpected port %d", port)
	}

	var mu sync.Mutex
	var gotMethods []string
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/browser/test"
		_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": wsURL})
	})
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/test"
		_ = json.NewEncoder(w).Encode([]map[string]string{{
			"type":                 "page",
			"url":                  "about:blank",
			"webSocketDebuggerUrl": wsURL,
		}})
	})
	mux.HandleFunc("/devtools/", func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijacker unsupported")
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		accept := computeWSAccept(r.Header.Get("Sec-WebSocket-Key"))
		_, _ = io.WriteString(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: "+accept+"\r\n\r\n")
		_ = rw.Flush()

		br := bufio.NewReader(conn)
		_, op, payload, err := readWSFrame(br)
		if err != nil || op != 0x1 {
			t.Errorf("expected text frame, got op=%d err=%v", op, err)
			return
		}
		var req struct {
			ID     int64          `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Errorf("bad rpc json: %v", err)
			return
		}
		mu.Lock()
		gotMethods = append(gotMethods, req.Method)
		mu.Unlock()

		// Send a server Ping + an async event before replying in two fragmented frames
		_ = writeServerWSFrame(conn, true, 0x9, []byte("ping"))
		// Consume client Pong
		_, pop, _, _ := readWSFrame(br)
		if pop != 0xA {
			t.Errorf("expected client pong 0xA, got %d", pop)
		}
		_ = writeServerWSFrame(conn, true, 0x1, []byte(`{"method":"Page.frameStartedLoading","params":{}}`))

		var respBytes []byte
		if req.Method == "Extensions.loadUnpacked" {
			respBytes = []byte(`{"id":1,"result":{"id":"ext-test-123"}}`)
		} else {
			respBytes = []byte(`{"id":1,"result":{"frameId":"frame-1"}}`)
		}
		mid := len(respBytes) / 2
		_ = writeServerWSFrame(conn, false, 0x1, respBytes[:mid])
		_ = writeServerWSFrame(conn, true, 0x0, respBytes[mid:])
		time.Sleep(30 * time.Millisecond)
	})

	srv = httptest.NewServer(mux)
	defer srv.Close()

	// Extract httptest port
	addr := strings.TrimPrefix(srv.URL, "http://")
	_, portStr, _ := strings.Cut(addr, ":")
	srvPort := 0
	for _, ch := range portStr {
		srvPort = srvPort*10 + int(ch-'0')
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	extID, err := cdpLoadUnpackedExtension(ctx, srvPort, "/tmp/fake-ext")
	if err != nil || extID != "ext-test-123" {
		t.Fatalf("cdpLoadUnpackedExtension = (%q, %v), want ext-test-123", extID, err)
	}
	if err := cdpNavigateFirstPage(ctx, srvPort, "https://fuck-claude.app/"); err != nil {
		t.Fatalf("cdpNavigateFirstPage failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gotMethods) != 2 || gotMethods[0] != "Extensions.loadUnpacked" || gotMethods[1] != "Page.navigate" {
		t.Fatalf("unexpected CDP methods (must never call Runtime.enable): %v", gotMethods)
	}
}
