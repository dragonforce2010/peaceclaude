package audit

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"peaceclaude/internal/config"
)

func TestSanitizationAndPersistence(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	s := NewStore(true, 10)

	hdr := http.Header{
		"Authorization":       {"Bearer sk-ant-secret-123"},
		"X-Api-Key":           {"sk-ant-key-999"},
		"Cookie":              {"sessionKey=top-secret-cookie"},
		"Proxy-Authorization": {"Basic dXNlcjpwYXNz"},
		"User-Agent":          {"ClaudeCode/2.1"},
	}
	clean := SanitizeHeaders(hdr)
	for _, k := range []string{"Authorization", "X-Api-Key", "Cookie", "Proxy-Authorization"} {
		if clean[k] != RedactedToken {
			t.Fatalf("header %s was not redacted: %q", k, clean[k])
		}
	}
	if clean["User-Agent"] != "ClaudeCode/2.1" {
		t.Fatalf("safe header altered: %q", clean["User-Agent"])
	}

	s.RecordCapture(CaptureRecord{
		Protocol:       "HTTP",
		Method:         "GET",
		Host:           "platform.claude.com",
		Path:           "/oauth/callback?code=secret-oauth-code&state=secret-state&mode=cli",
		Category:       "anthropic",
		Action:         "proxy",
		RequestHeaders: clean,
	})
	rawCap, err := os.ReadFile(config.CaptureLogPath())
	if err != nil {
		t.Fatalf("read capture file: %v", err)
	}
	blob := string(rawCap)
	for _, forbidden := range []string{"sk-ant-secret-123", "top-secret-cookie", "secret-oauth-code", "secret-state", "dXNlcjpwYXNz"} {
		if strings.Contains(blob, forbidden) {
			t.Fatalf("capture file leaked secret %q: %s", forbidden, blob)
		}
	}

	s.RecordLeak(LeakEvent{
		Kind:   "split_ip_warning",
		Host:   "ipwho.is",
		Action: "proxy",
		Reason: "non-claude under local-proxy",
	})
	leaks := s.ListLeaks(time.Hour, 10)
	if len(leaks) != 1 || leaks[0].Host != "ipwho.is" {
		t.Fatalf("unexpected leaks: %+v", leaks)
	}
}
