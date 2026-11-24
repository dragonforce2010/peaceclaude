package launcher

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// pickLoopbackPort allocates an unused ephemeral TCP port on 127.0.0.1.
func pickLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port, nil
}

// validateLoopbackWSURL strictly ensures CDP WebSocket connections only target localhost loopback.
func validateLoopbackWSURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid ws url: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "ws") {
		return nil, fmt.Errorf("refusing non-ws scheme %q for CDP", u.Scheme)
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	isLoopback := host == "127.0.0.1" || host == "localhost" || host == "::1"
	if !isLoopback {
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("refusing non-loopback CDP host %q", host)
		}
	}
	if u.Port() == "" {
		return nil, errors.New("CDP ws url missing port")
	}
	return u, nil
}

func computeWSAccept(key string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(key) + wsGUID))
	return base64.StdEncoding.EncodeToString(h[:])
}

func writeWSFrame(w io.Writer, opcode byte, payload []byte) error {
	var hdr [14]byte
	hdr[0] = 0x80 | (opcode & 0x0f) // FIN=1
	n := len(payload)
	idx := 2
	switch {
	case n < 126:
		hdr[1] = 0x80 | byte(n) // MASK=1
	case n <= 65535:
		hdr[1] = 0x80 | 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(n))
		idx = 4
	default:
		hdr[1] = 0x80 | 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(n))
		idx = 10
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	copy(hdr[idx:idx+4], mask[:])
	idx += 4

	buf := make([]byte, idx+n)
	copy(buf[:idx], hdr[:idx])
	for i := 0; i < n; i++ {
		buf[idx+i] = payload[i] ^ mask[i%4]
	}
	_, err := w.Write(buf)
	return err
}

func readWSFrame(r *bufio.Reader) (fin bool, opcode byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(r, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin = (h[0] & 0x80) != 0
	opcode = h[0] & 0x0f
	masked := (h[1] & 0x80) != 0
	plen := uint64(h[1] & 0x7f)

	switch plen {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		plen = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		plen = binary.BigEndian.Uint64(ext[:])
	}
	if plen > 8<<20 {
		return false, 0, nil, fmt.Errorf("CDP frame too large: %d bytes", plen)
	}

	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(r, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	if plen > 0 {
		payload = make([]byte, plen)
		if _, err = io.ReadFull(r, payload); err != nil {
			return false, 0, nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
	}
	return fin, opcode, payload, nil
}

// cdpCall executes a single JSON-RPC call over a loopback CDP WebSocket and immediately disconnects.
func cdpCall(ctx context.Context, wsURL string, method string, params map[string]any) (json.RawMessage, error) {
	u, err := validateLoopbackWSURL(wsURL)
	if err != nil {
		return nil, err
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	}

	var keyBytes [16]byte
	if _, err := rand.Read(keyBytes[:]); err != nil {
		return nil, err
	}
	wsKey := base64.StdEncoding.EncodeToString(keyBytes[:])

	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}

	reqRaw := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, u.Host, wsKey,
	)
	if _, err := io.WriteString(conn, reqRaw); err != nil {
		return nil, err
	}

	br := bufio.NewReader(conn)
	httpReq, _ := http.NewRequest(http.MethodGet, "http://"+u.Host+path, nil)
	resp, err := http.ReadResponse(br, httpReq)
	if err != nil {
		return nil, fmt.Errorf("CDP handshake read failed: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("CDP handshake rejected with HTTP %d", resp.StatusCode)
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") ||
		resp.Header.Get("Sec-WebSocket-Accept") != computeWSAccept(wsKey) {
		return nil, errors.New("CDP handshake failed Sec-WebSocket-Accept check")
	}

	if params == nil {
		params = map[string]any{}
	}
	reqBody, err := json.Marshal(map[string]any{
		"id":     1,
		"method": method,
		"params": params,
	})
	if err != nil {
		return nil, err
	}
	if err := writeWSFrame(conn, 0x1, reqBody); err != nil {
		return nil, err
	}

	var msgBuf bytes.Buffer
	inFrag := false
	for {
		fin, op, payload, err := readWSFrame(br)
		if err != nil {
			return nil, fmt.Errorf("CDP read failed: %w", err)
		}
		switch op {
		case 0x8:
			return nil, errors.New("CDP peer closed WebSocket before responding")
		case 0x9:
			if werr := writeWSFrame(conn, 0xA, payload); werr != nil {
				return nil, werr
			}
			continue
		case 0xA:
			continue
		case 0x1, 0x2:
			msgBuf.Reset()
			msgBuf.Write(payload)
			inFrag = !fin
		case 0x0:
			if !inFrag {
				continue
			}
			msgBuf.Write(payload)
			inFrag = !fin
		default:
			continue
		}

		if fin && msgBuf.Len() > 0 {
			var rpc struct {
				ID     int64           `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Code    int64  `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if jerr := json.Unmarshal(msgBuf.Bytes(), &rpc); jerr == nil && rpc.ID == 1 {
				_ = writeWSFrame(conn, 0x8, nil)
				if rpc.Error != nil {
					return nil, fmt.Errorf("CDP %s error (%d): %s", method, rpc.Error.Code, rpc.Error.Message)
				}
				return rpc.Result, nil
			}
			msgBuf.Reset()
		}
	}
}

func cdpFetchJSON(ctx context.Context, rawURL string, dst any) error {
	client := &http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(dst)
}

// cdpLoadUnpackedExtension polls the loopback CDP /json/version endpoint and loads extDir
// via Extensions.loadUnpacked, retrying transient initialization errors before ctx expires.
func cdpLoadUnpackedExtension(ctx context.Context, port int, extDir string) (string, error) {
	if port <= 0 || strings.TrimSpace(extDir) == "" {
		return "", errors.New("invalid CDP extension parameters")
	}
	verURL := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	var lastErr error
	attempts := 0
	for {
		var ver struct {
			WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
		}
		if err := cdpFetchJSON(ctx, verURL, &ver); err == nil && ver.WebSocketDebuggerURL != "" {
			attempts++
			resRaw, cerr := cdpCall(ctx, ver.WebSocketDebuggerURL, "Extensions.loadUnpacked", map[string]any{
				"path": extDir,
			})
			if cerr == nil {
				var out struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(resRaw, &out)
				return out.ID, nil
			}
			lastErr = cerr
			if attempts >= 3 || ctx.Err() != nil {
				return "", cerr
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
			continue
		} else if err != nil {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return "", fmt.Errorf("timed out waiting for CDP version endpoint: %w", lastErr)
			}
			return "", ctx.Err()
		case <-time.After(80 * time.Millisecond):
		}
	}
}

// cdpOpenNewTab opens targetURL as a new tab in a running CDP-enabled browser without attaching Runtime hooks.
func cdpOpenNewTab(ctx context.Context, port int, targetURL string) error {
	if port <= 0 || strings.TrimSpace(targetURL) == "" {
		return errors.New("invalid CDP createTarget parameters")
	}
	verURL := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	var ver struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := cdpFetchJSON(ctx, verURL, &ver); err != nil {
		return err
	}
	if ver.WebSocketDebuggerURL == "" {
		return errors.New("CDP version response missing webSocketDebuggerUrl")
	}
	_, err := cdpCall(ctx, ver.WebSocketDebuggerURL, "Target.createTarget", map[string]any{
		"url": targetURL,
	})
	return err
}

// cdpNavigateFirstPage finds the first page target on loopback port and executes Page.navigate
// without calling Runtime.enable so navigator.webdriver remains false and no runtime hook leaks.
func cdpNavigateFirstPage(ctx context.Context, port int, targetURL string) error {
	if port <= 0 || strings.TrimSpace(targetURL) == "" {
		return errors.New("invalid CDP navigate parameters")
	}
	listURL := fmt.Sprintf("http://127.0.0.1:%d/json", port)
	var lastErr error
	for {
		var targets []struct {
			Type                 string `json:"type"`
			URL                  string `json:"url"`
			WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
		}
		if err := cdpFetchJSON(ctx, listURL, &targets); err == nil {
			wsURL := ""
			for _, t := range targets {
				if t.Type == "page" && t.WebSocketDebuggerURL != "" &&
					!strings.HasPrefix(t.URL, "devtools://") && !strings.HasPrefix(t.URL, "chrome-extension://") {
					wsURL = t.WebSocketDebuggerURL
					break
				}
			}
			if wsURL == "" {
				for _, t := range targets {
					if t.Type == "page" && t.WebSocketDebuggerURL != "" {
						wsURL = t.WebSocketDebuggerURL
						break
					}
				}
			}
			if wsURL != "" {
				resRaw, cerr := cdpCall(ctx, wsURL, "Page.navigate", map[string]any{
					"url": targetURL,
				})
				if cerr != nil {
					return cerr
				}
				var navOut struct {
					ErrorText string `json:"errorText"`
				}
				if json.Unmarshal(resRaw, &navOut) == nil && navOut.ErrorText != "" {
					return fmt.Errorf("CDP Page.navigate failed: %s", navOut.ErrorText)
				}
				return nil
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timed out waiting for CDP page target: %w", lastErr)
			}
			return ctx.Err()
		case <-time.After(80 * time.Millisecond):
		}
	}
}
