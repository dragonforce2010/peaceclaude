package host

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/profile"
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

func TestDesktopCmdlineHasPeaceProxyAcceptsServerAndPAC(t *testing.T) {
	cfg := config.Default()
	cfg.Listen.BindAddr = "127.0.0.1"
	cfg.Listen.HTTPPort = 18897
	cfg.Listen.ControlPort = 18899

	cases := []struct {
		name    string
		cmdline string
		want    bool
	}{
		{
			name:    "proxy-server mode",
			cmdline: "/Applications/Claude.app/Contents/MacOS/Claude --user-data-dir=/tmp/d --proxy-server=http://127.0.0.1:18897 --disable-quic",
			want:    true,
		},
		{
			name:    "pac desktop mode",
			cmdline: "/Applications/Claude.app/Contents/MacOS/Claude --user-data-dir=/tmp/d --proxy-pac-url=http://127.0.0.1:18899/peaceclaude.pac?mode=desktop --disable-quic",
			want:    true,
		},
		{
			name:    "pac base mode",
			cmdline: "/Applications/Claude.app/Contents/MacOS/Claude --user-data-dir=/tmp/d --proxy-pac-url=http://127.0.0.1:18899/peaceclaude.pac --disable-quic",
			want:    true,
		},
		{
			name:    "wrong pac host suffix spoofing",
			cmdline: "/Applications/Claude.app/Contents/MacOS/Claude --proxy-pac-url=http://127.0.0.1:18899/peaceclaude.pac.evil.com",
			want:    false,
		},
		{
			name:    "wrong proxy port",
			cmdline: "/Applications/Claude.app/Contents/MacOS/Claude --proxy-server=http://127.0.0.1:7897",
			want:    false,
		},
		{
			name:    "exposed no proxy flags",
			cmdline: "/Applications/Claude.app/Contents/MacOS/Claude --lang=en-US",
			want:    false,
		},
	}

	for _, tc := range cases {
		if got := desktopCmdlineHasPeaceProxy(tc.cmdline, cfg); got != tc.want {
			t.Errorf("%s: desktopCmdlineHasPeaceProxy = %v, want %v (cmd=%q)", tc.name, got, tc.want, tc.cmdline)
		}
	}
	if desktopCmdlineHasPeaceProxy("--proxy-server=http://127.0.0.1:18897", nil) {
		t.Fatal("nil config must return false")
	}
}

func TestAuditIsolatedBrowserShieldsDetectsDirtyPrefsAndUpgradableExt(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	prof, err := profile.Ensure(cfg, "audit-test", "")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Uninitialized browser-profile -> skipped cleanly with zero issues.
	if issues, cnt := auditIsolatedBrowserShields(cfg); cnt != 0 || len(issues) != 0 {
		t.Fatalf("expected 0 checked uninitialized profiles, got cnt=%d issues=%v", cnt, issues)
	}

	// 2. Clean Preferences + up-to-date extension artifacts -> passes cleanly.
	defDir := filepath.Join(prof.BrowserDir, "Default")
	extDir := filepath.Join(prof.BrowserDir, "peace-shield-ext")
	if err := os.MkdirAll(defDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(extDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cleanPrefs, _ := json.Marshal(map[string]any{
		"enable_do_not_track": true,
		"intl": map[string]any{
			"accept_languages":   "en-US,en",
			"selected_languages": "en-US,en",
		},
		"profile": map[string]any{
			"default_content_setting_values": map[string]any{"geolocation": 2},
		},
	})
	_ = os.WriteFile(filepath.Join(defDir, "Preferences"), cleanPrefs, 0o600)
	_ = os.WriteFile(filepath.Join(extDir, "manifest.json"), []byte(`{"manifest_version":3}`), 0o600)
	_ = os.WriteFile(filepath.Join(extDir, "rules.json"), []byte(`[]`), 0o600)
	_ = os.WriteFile(filepath.Join(extDir, "background.js"), []byte(`chrome.runtime.onInstalled.addListener(() => {});`), 0o600)
	_ = os.WriteFile(filepath.Join(extDir, "shield.js"), []byte(`/* FontFaceSet Worker shield */`), 0o600)

	if issues, cnt := auditIsolatedBrowserShields(cfg); cnt != 1 || len(issues) != 0 {
		t.Fatalf("expected 1 clean profile with 0 issues, got cnt=%d issues=%v", cnt, issues)
	}

	// 3. Inject zh-CN preferences + outdated shield.js + disabled Secure Preferences entry -> flagged with repair_browser_shields.
	_ = os.WriteFile(filepath.Join(defDir, "Preferences"), []byte(`{"intl":{"accept_languages":"zh-CN,zh","selected_languages":"zh-CN,zh"},"enable_do_not_track":false}`), 0o600)
	_ = os.WriteFile(filepath.Join(extDir, "shield.js"), []byte(`/* legacy canvas only */`), 0o600)
	_ = os.WriteFile(filepath.Join(defDir, "Secure Preferences"), []byte(`{"extensions":{"settings":{"id1":{"path":"`+extDir+`","disable_reasons":[8192],"manifest":{"name":"PeaceClaude Identity Shield"}}}}}`), 0o600)

	issues, cnt := auditIsolatedBrowserShields(cfg)
	if cnt != 1 || len(issues) != 1 {
		t.Fatalf("expected 1 flagged profile, got cnt=%d issues=%v", cnt, issues)
	}
	for _, wantSub := range []string{"audit-test", "Preferences含中文", "护盾扩展待升级", "护盾扩展被禁用"} {
		if !strings.Contains(issues[0], wantSub) {
			t.Fatalf("expected issue %q to mention %q", issues[0], wantSub)
		}
	}
	fixable, label, action := fixMetadataForCheck("browser_desktop_tz_lang", "warn", "已审计 1 个隔离浏览器档案，发现护盾/语言异常："+issues[0])
	if !fixable || action != "repair_browser_shields" || label == "" {
		t.Fatalf("unexpected fix metadata: fixable=%v label=%q action=%q", fixable, label, action)
	}
}
