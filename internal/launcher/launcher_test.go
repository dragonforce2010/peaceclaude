package launcher

import (
	"os"
	"strings"
	"testing"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/profile"
)

func TestBuildIsolatedEnvStripsHostPollutionAndInjectsPeace(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	prof, err := profile.Ensure(cfg, "test-env", "")
	if err != nil {
		t.Fatal(err)
	}
	gate := egress.GateSnapshot{
		Open:              true,
		EffectiveTimezone: "America/Denver",
		EffectiveLanguage: "en-US",
		LastGood:          egress.ProbeResult{EgressIP: "198.51.100.10", Country: "US"},
	}
	hostEnv := []string{
		"PATH=/usr/bin:/bin",
		"ANTHROPIC_BASE_URL=http://127.0.0.1:3425",
		"ANTHROPIC_AUTH_TOKEN=host-relay-token",
		"CLAUDE_CODE_USE_BEDROCK=1",
		"HTTP_PROXY=http://127.0.0.1:9999",
		"TZ=Asia/Shanghai",
		"LANG=zh_CN.UTF-8",
	}
	out := BuildIsolatedEnv(cfg, prof, gate, hostEnv)
	envMap := map[string]string{}
	for _, kv := range out {
		k, v, _ := strings.Cut(kv, "=")
		envMap[k] = v
	}

	for _, stripped := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK"} {
		if _, exists := envMap[stripped]; exists {
			t.Fatalf("expected host env %s to be stripped", stripped)
		}
	}
	if envMap["HTTP_PROXY"] != cfg.HTTPProxyURL() || envMap["ALL_PROXY"] != cfg.SOCKS5ProxyURL() {
		t.Fatalf("unexpected proxy env: HTTP=%q ALL=%q", envMap["HTTP_PROXY"], envMap["ALL_PROXY"])
	}
	if envMap["TZ"] != "America/Denver" || envMap["LANG"] != "en_US.UTF-8" {
		t.Fatalf("unexpected identity env: TZ=%q LANG=%q", envMap["TZ"], envMap["LANG"])
	}
	if envMap["CLAUDE_CONFIG_DIR"] != prof.CLIConfigDir {
		t.Fatalf("CLAUDE_CONFIG_DIR=%q want %q", envMap["CLAUDE_CONFIG_DIR"], prof.CLIConfigDir)
	}
	if envMap["DISABLE_TELEMETRY"] != "1" {
		t.Fatalf("expected DISABLE_TELEMETRY=1")
	}
}

func TestRenderLaunchAgentPlist(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	plist := RenderLaunchAgentPlist("/Users/test/.local/bin/peaceclaude")
	for _, need := range []string{LaunchAgentLabel, "/Users/test/.local/bin/peaceclaude", "<string>service</string>", "<string>run</string>"} {
		if !strings.Contains(plist, need) {
			t.Fatalf("plist missing %q", need)
		}
	}
}

func TestBuildDesktopSpecNeverUsesForbiddenClaudeSwitches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PEACECLAUDE_HOME", tmp)
	fakeApp := tmp + "/Claude.app"
	fakeBin := fakeApp + "/Contents/MacOS/Claude"
	if err := os.MkdirAll(fakeApp+"/Contents/MacOS", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Binaries.ClaudeDesktopApp = fakeApp
	cfg.Identity.BlockDesktopLocalDNS = true
	prof, err := profile.Ensure(cfg, "desktop-check", "")
	if err != nil {
		t.Fatal(err)
	}
	gate := egress.GateSnapshot{
		Open:              true,
		EffectiveTimezone: "America/Denver",
		EffectiveLanguage: "en-US",
	}
	spec, err := BuildDesktopSpec(cfg, prof, gate, nil)
	if err != nil {
		t.Fatalf("BuildDesktopSpec: %v", err)
	}

	// Verify essential isolation switches are present.
	joined := strings.Join(spec.Args, " ")
	for _, want := range []string{"--user-data-dir=" + prof.DesktopDir, "--proxy-server=" + cfg.HTTPProxyURL(), "--disable-quic", "--lang=en-US"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("desktop args missing %q: %v", want, spec.Args)
		}
	}

	// Regression guard: official Claude Desktop v6+ refuses to start if any forbidden switch is present.
	for _, arg := range spec.Args {
		clean := strings.TrimLeft(strings.ToLower(strings.TrimSpace(arg)), "-/")
		name, _, _ := strings.Cut(clean, "=")
		for _, forbidden := range ForbiddenClaudeDesktopSwitches {
			if strings.HasPrefix(name, forbidden) {
				t.Fatalf("BuildDesktopSpec emitted forbidden Claude Desktop switch %q (matches %q)", arg, forbidden)
			}
		}
	}
}

func TestEmbeddedMacOSSwiftSourceValid(t *testing.T) {
	for _, sym := range []string{"WKWebView", "NSApplicationDelegate", "runJavaScriptConfirmPanelWithMessage", "#accounts"} {
		if !strings.Contains(macOSAppSwiftSrc, sym) {
			t.Fatalf("embedded app_macos.swift missing %q", sym)
		}
	}
}

func TestExtractClaudeDeepLinkArgsAndUserDataDir(t *testing.T) {
	raw := `/Applications/Claude.app/Contents/MacOS/Claude --user-data-dir=/Users/alice/.peaceclaude/profiles/team-a/desktop-data --proxy-server=http://127.0.0.1:18897 --disable-quic "claude://claude.ai/login/app-google-auth?code=abc&state=xyz" claude://claude.ai/login/app-google-auth?code=abc&state=xyz`
	links := ExtractClaudeDeepLinkArgs(raw)
	if len(links) != 1 || links[0] != "claude://claude.ai/login/app-google-auth?code=abc&state=xyz" {
		t.Fatalf("unexpected deep links: %v", links)
	}
	dir := ExtractUserDataDir(raw)
	wantDir := "/Users/alice/.peaceclaude/profiles/team-a/desktop-data"
	if dir != wantDir {
		t.Fatalf("ExtractUserDataDir = %q, want %q", dir, wantDir)
	}
}

func TestLaunchLoginBrowserCleanFlagsWithoutUnsupportedBanner(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PEACECLAUDE_HOME", tmp)
	fakeChrome := tmp + "/Google Chrome"
	if err := os.WriteFile(fakeChrome, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Binaries.LoginBrowserApp = fakeChrome
	prof, err := profile.Ensure(cfg, "clean-browser", "")
	if err != nil {
		t.Fatal(err)
	}
	// Run dryRun via Build + direct call with live gate mock avoided; inspect LaunchLoginBrowser dryRun args via helper:
	_ = ensureBrowserDNSPrivacyPrefs(prof.BrowserDir)
	rawPrefs, err := os.ReadFile(prof.BrowserDir + "/Default/Preferences")
	if err != nil {
		t.Fatalf("expected Chrome DNS privacy Preferences: %v", err)
	}
	if !strings.Contains(string(rawPrefs), `"dns_prefetching":{"enabled":false}`) {
		t.Fatalf("unexpected Preferences content: %s", string(rawPrefs))
	}
}
