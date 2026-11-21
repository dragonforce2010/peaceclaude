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

func TestPruneOlderThan30Days(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	s := NewStore(true, 50)
	now := time.Now().UTC()

	s.RecordLeak(LeakEvent{Timestamp: now.Add(-35 * 24 * time.Hour), Kind: "old_leak", Host: "old.example.com", Action: "warn", Reason: "expired"})
	s.RecordLeak(LeakEvent{Timestamp: now.Add(-2 * 24 * time.Hour), Kind: "fresh_leak", Host: "fresh.example.com", Action: "warn", Reason: "recent"})
	s.RecordCapture(CaptureRecord{Timestamp: now.Add(-40 * 24 * time.Hour), Protocol: "HTTPS", Host: "old.claude.ai", Category: "anthropic", Action: "proxy"})
	s.RecordCapture(CaptureRecord{Timestamp: now.Add(-1 * time.Hour), Protocol: "HTTPS", Host: "api.anthropic.com", Category: "anthropic", Action: "proxy"})

	removedLeaks, removedCaptures := s.PruneOlderThan(DefaultRetention)
	if removedLeaks != 1 || removedCaptures != 1 {
		t.Fatalf("expected 1 leak and 1 capture pruned, got leaks=%d captures=%d", removedLeaks, removedCaptures)
	}

	s2 := NewStore(true, 50)
	leaks := s2.ListLeaks(0, 10)
	caps := s2.ListCaptures(10)
	if len(leaks) != 1 || leaks[0].Host != "fresh.example.com" {
		t.Fatalf("unexpected remaining leaks: %+v", leaks)
	}
	if len(caps) != 1 || caps[0].Host != "api.anthropic.com" {
		t.Fatalf("unexpected remaining captures: %+v", caps)
	}
}
