package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
)

func TestParseLsofDirectHit(t *testing.T) {
	// Direct Anthropic IPv4 socket must be flagged
	line := "Claude 70783 testuser 44u IPv4 0x123 0t0 TCP 192.168.1.5:51234->160.79.104.10:443 (ESTABLISHED)"
	hit, ok := parseLsofDirectHit(line, "198.51.100.10")
	if !ok || hit.DestIP != "160.79.104.10" || hit.Process != "Claude" {
		t.Fatalf("expected direct Anthropic hit, got ok=%v hit=%+v", ok, hit)
	}

	// Expected proxy IP must NOT be flagged
	proxyLine := "verge-mih 1969 testuser 20u IPv4 0x123 0t0 TCP 192.168.1.5:51235->198.51.100.10:6331 (ESTABLISHED)"
	if _, ok := parseLsofDirectHit(proxyLine, "198.51.100.10"); ok {
		t.Fatal("proxy server IP should not be flagged as direct leak")
	}

	// Loopback must NOT be flagged
	loLine := "Claude 70783 testuser 45u IPv4 0x123 0t0 TCP 127.0.0.1:51236->127.0.0.1:18897 (ESTABLISHED)"
	if _, ok := parseLsofDirectHit(loLine, "198.51.100.10"); ok {
		t.Fatal("loopback socket should not be flagged")
	}
}

func TestRenderPFAnchorAndSyntax(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Upstream.Mode = "direct-socks5"
	cfg.Upstream.Address = "198.51.100.10:6331"
	cfg.Listen.VMProxyEnabled = true

	anchorPath, installPath, uninstallPath, err := WritePFArtifacts(cfg)
	if err != nil {
		t.Fatalf("WritePFArtifacts: %v", err)
	}
	if anchorPath == "" || installPath == "" || uninstallPath == "" {
		t.Fatal("expected non-empty pf artifact paths")
	}

	text := RenderPFAnchor(cfg)
	for _, need := range []string{"198.51.100.10", "160.79.104.0/23", "2607:6bc0::/32", "192.168.64.0/24", "18897"} {
		if !strings.Contains(text, need) {
			t.Fatalf("rendered pf anchor missing %q:\n%s", need, text)
		}
	}

	ok, detail := ValidatePFAnchorSyntax(anchorPath)
	if !ok {
		t.Fatalf("pfctl -n syntax check failed: %s\nanchor:\n%s", detail, text)
	}
}

func TestVerifyIsolatedTimezone(t *testing.T) {
	ok, out := verifyIsolatedTimezone("America/Denver", "en_US.UTF-8")
	if !ok || out == "" {
		t.Fatalf("verifyIsolatedTimezone failed: ok=%v out=%q", ok, out)
	}
	if !strings.Contains(out, "-0700") && !strings.Contains(out, "-0600") {
		t.Fatalf("expected Denver UTC offset (-0700/-0600), got %q", out)
	}
}

func TestDoctorPassedChecksHaveNoFixHint(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	_ = config.Save(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rep := RunDoctor(ctx, cfg, false, egress.GateSnapshot{
		EffectiveTimezone: "America/Denver",
		EffectiveLanguage: "en-US",
	})
	passCount := 0
	for _, c := range rep.Checks {
		if c.Status == "pass" {
			passCount++
			if c.FixHint != "" || c.Fixable {
				t.Fatalf("passed doctor check %q still has fix metadata: fix_hint=%q fixable=%v", c.ID, c.FixHint, c.Fixable)
			}
		}
	}
	if passCount < 4 {
		t.Fatalf("expected at least 4 passed checks in offline doctor test, got %d", passCount)
	}
}

func TestParseWindowsRegAutoConfigURLAndNotFound(t *testing.T) {
	sample := `
HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Internet Settings
    AutoConfigURL    REG_SZ    http://127.0.0.1:18899/peaceclaude.pac
`
	if got := parseWindowsRegAutoConfigURL(sample); got != "http://127.0.0.1:18899/peaceclaude.pac" {
		t.Fatalf("unexpected parsed AutoConfigURL: %q", got)
	}
	if got := parseWindowsRegAutoConfigURL("HKEY_CURRENT_USER\\...\n    ProxyEnable    REG_DWORD    0x0"); got != "" {
		t.Fatalf("expected empty when AutoConfigURL missing, got %q", got)
	}
	if !isWindowsRegNotFoundErr("ERROR: The system was unable to find the specified registry key or value.") {
		t.Fatal("expected English reg not-found to match")
	}
	if isWindowsRegNotFoundErr("ERROR: Access is denied.") {
		t.Fatal("access denied must NOT be treated as not-found")
	}
}
