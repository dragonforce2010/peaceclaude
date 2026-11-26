package launcher

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/profile"
	"peaceclaude/internal/version"
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
		"LANGUAGE=zh_CN:zh",
		"LC_TIME=zh_CN.UTF-8",
	}
	out := BuildIsolatedEnv(cfg, prof, gate, hostEnv)
	envMap := map[string]string{}
	for _, kv := range out {
		k, v, _ := strings.Cut(kv, "=")
		envMap[k] = v
	}

	for _, stripped := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_USE_BEDROCK", "LC_TIME"} {
		if _, exists := envMap[stripped]; exists {
			t.Fatalf("expected host env %s to be stripped", stripped)
		}
	}
	if envMap["LANGUAGE"] != "en_US:en" {
		t.Fatalf("expected LANGUAGE=en_US:en, got %q", envMap["LANGUAGE"])
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
	for _, need := range []string{
		LaunchAgentLabel,
		"/Users/test/.local/bin/peaceclaude",
		"<string>service</string>",
		"<string>run</string>",
		"<key>ThrottleInterval</key>",
		"<integer>1</integer>",
	} {
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
	// 1. Default "pac" mode: must emit --proxy-pac-url=.../peaceclaude.pac?mode=desktop and omit --proxy-server.
	spec, err := BuildDesktopSpec(cfg, prof, gate, nil)
	if err != nil {
		t.Fatalf("BuildDesktopSpec (pac): %v", err)
	}
	joined := strings.Join(spec.Args, " ")
	for _, want := range []string{"--user-data-dir=" + prof.DesktopDir, "--proxy-pac-url=" + cfg.DesktopPACURL(), "--disable-quic", "--lang=en-US"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("desktop args (pac) missing %q: %v", want, spec.Args)
		}
	}
	if strings.Contains(joined, "--proxy-server=") {
		t.Fatalf("desktop args (pac) should not contain --proxy-server: %v", spec.Args)
	}

	// Regression guard: official Claude Desktop v6+ refuses to start if any forbidden switch is present.
	assertNoForbiddenSwitches := func(args []string) {
		for _, arg := range args {
			clean := strings.TrimLeft(strings.ToLower(strings.TrimSpace(arg)), "-/")
			name, _, _ := strings.Cut(clean, "=")
			for _, forbidden := range ForbiddenClaudeDesktopSwitches {
				if strings.HasPrefix(name, forbidden) {
					t.Fatalf("BuildDesktopSpec emitted forbidden Claude Desktop switch %q (matches %q)", arg, forbidden)
				}
			}
		}
	}
	assertNoForbiddenSwitches(spec.Args)

	// 2. Explicit "proxy" mode: must emit --proxy-server and --proxy-bypass-list.
	cfg.Shield.DesktopProxyMode = "proxy"
	specProxy, err := BuildDesktopSpec(cfg, prof, gate, nil)
	if err != nil {
		t.Fatalf("BuildDesktopSpec (proxy): %v", err)
	}
	joinedProxy := strings.Join(specProxy.Args, " ")
	for _, want := range []string{"--user-data-dir=" + prof.DesktopDir, "--proxy-server=" + cfg.HTTPProxyURL(), "--proxy-bypass-list=", "--disable-quic", "--lang=en-US"} {
		if !strings.Contains(joinedProxy, want) {
			t.Fatalf("desktop args (proxy) missing %q: %v", want, specProxy.Args)
		}
	}
	assertNoForbiddenSwitches(specProxy.Args)
}

func TestEmbeddedMacOSSwiftSourceValid(t *testing.T) {
	for _, sym := range []string{
		"WKWebView",
		"NSApplicationDelegate",
		"runJavaScriptConfirmPanelWithMessage",
		"#accounts",
		"ensure-autostart",
		"restartGateway",
		"/api/service/restart",
		"重启安全网关",
	} {
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
	if !strings.Contains(string(rawPrefs), `"geolocation":2`) {
		t.Fatalf("expected geolocation blocked in Preferences, got: %s", string(rawPrefs))
	}
}

func TestBrowserPrivacyPrefsOverwritesChineseAndGeneratesValidShieldExtension(t *testing.T) {
	tmp := t.TempDir()
	browserDir := filepath.Join(tmp, "browser-profile")
	defDir := filepath.Join(browserDir, "Default")
	if err := os.MkdirAll(defDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Seed dirty Chrome preferences from a Chinese macOS host
	dirtyPrefs := `{"intl":{"accept_languages":"zh-CN,zh","selected_languages":"zh-CN,zh"},"other":{"keep":true}}`
	if err := os.WriteFile(filepath.Join(defDir, "Preferences"), []byte(dirtyPrefs), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ensureBrowserPrivacyPrefs(browserDir, "en-US"); err != nil {
		t.Fatalf("ensureBrowserPrivacyPrefs: %v", err)
	}
	rawPrefs, err := os.ReadFile(filepath.Join(defDir, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	var prefs map[string]any
	if err := json.Unmarshal(rawPrefs, &prefs); err != nil {
		t.Fatal(err)
	}
	if dnt, _ := prefs["enable_do_not_track"].(bool); !dnt {
		t.Fatalf("expected enable_do_not_track=true, got %v", prefs["enable_do_not_track"])
	}
	intl, _ := prefs["intl"].(map[string]any)
	if intl["accept_languages"] != "en-US,en" || intl["selected_languages"] != "en-US,en" {
		t.Fatalf("expected zh-CN overwritten with en-US,en, got %v", intl)
	}
	if other, _ := prefs["other"].(map[string]any); other["keep"] != true {
		t.Fatalf("expected unrelated preferences preserved, got %v", prefs["other"])
	}

	rawLS, err := os.ReadFile(filepath.Join(browserDir, "Local State"))
	if err != nil {
		t.Fatalf("expected Local State: %v", err)
	}
	var ls map[string]any
	if err := json.Unmarshal(rawLS, &ls); err != nil {
		t.Fatal(err)
	}
	lsIntl, _ := ls["intl"].(map[string]any)
	if lsIntl["app_locale"] != "en-US" {
		t.Fatalf("expected Local State intl.app_locale=en-US, got %v", lsIntl)
	}

	extDir, err := ensureBrowserShieldExtension(browserDir, "en-US")
	if err != nil {
		t.Fatalf("ensureBrowserShieldExtension: %v", err)
	}
	for _, fn := range []string{"manifest.json", "rules.json", "shield.js"} {
		if _, err := os.Stat(filepath.Join(extDir, fn)); err != nil {
			t.Fatalf("missing generated extension artifact %s: %v", fn, err)
		}
	}
	shieldBytes, err := os.ReadFile(filepath.Join(extDir, "shield.js"))
	if err != nil {
		t.Fatal(err)
	}
	shieldStr := string(shieldBytes)
	for _, want := range []string{"globalPrivacyControl", "Intl", "CJK_FONT_RE", "PingFang", "Hiragino Sans GB", "Microsoft YaHei", "CanvasRenderingContext2D"} {
		if !strings.Contains(shieldStr, want) {
			t.Fatalf("shield.js missing %q", want)
		}
	}
	if nodeBin, err := exec.LookPath("node"); err == nil {
		if out, err := exec.Command(nodeBin, "--check", filepath.Join(extDir, "shield.js")).CombinedOutput(); err != nil {
			t.Fatalf("shield.js syntax error: %v (%s)", err, string(out))
		}
	}
	if !supportsCDPShieldBrowser("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome") {
		t.Fatalf("expected Google Chrome to support CDP shield")
	}
	if supportsCDPShieldBrowser("/Applications/Arc.app/Contents/MacOS/Arc") {
		t.Fatalf("expected Arc to skip CDP shield")
	}
}

func TestCFBundleBuildVersionAndSelfContainedAppBundle(t *testing.T) {
	cases := map[string]string{
		"0.2.0":       "0.2.0",
		"v0.2.0-rc.1": "0.2.0",
		"":            "0.2.0",
		"  ":          "0.2.0",
	}
	for in, want := range cases {
		if got := cfBundleBuildVersion(in); got != want {
			t.Fatalf("cfBundleBuildVersion(%q) = %q, want %q", in, got, want)
		}
	}

	tmp := t.TempDir()
	fakeBin := filepath.Join(tmp, "peaceclaude-fake")
	fakeContent := "#!/bin/sh\necho fake-peaceclaude\n"
	if err := os.WriteFile(fakeBin, []byte(fakeContent), 0o755); err != nil {
		t.Fatal(err)
	}
	stageDir := filepath.Join(tmp, "apps")
	appDir, err := InstallDesktopLauncherAppTo(fakeBin, stageDir)
	if err != nil {
		t.Fatalf("InstallDesktopLauncherAppTo: %v", err)
	}
	plistBytes, err := os.ReadFile(filepath.Join(appDir, "Contents", "Info.plist"))
	if err != nil {
		t.Fatalf("read Info.plist: %v", err)
	}
	plistStr := string(plistBytes)
	if !strings.Contains(plistStr, "<string>"+version.Version+"</string>") {
		t.Fatalf("Info.plist missing version %q: %s", version.Version, plistStr)
	}
	bundledBin := filepath.Join(appDir, "Contents", "Resources", "bin", "peaceclaude")
	info, err := os.Stat(bundledBin)
	if err != nil {
		t.Fatalf("expected bundled CLI at %s: %v", bundledBin, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected bundled CLI executable, mode=%v", info.Mode())
	}
	gotContent, err := os.ReadFile(bundledBin)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotContent) != fakeContent {
		t.Fatalf("bundled CLI content mismatch")
	}
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(filepath.Join(appDir, "Contents", "Resources", "AppIcon.icns")); err != nil {
			t.Fatalf("expected AppIcon.icns on macOS: %v", err)
		}
	}
}

func TestParseLaunchAgentProgramAndValidity(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	binPath := "/Users/demo/.local/bin/peaceclaude"
	plist := RenderLaunchAgentPlist(binPath)
	if !strings.Contains(plist, "<key>HOME</key>") {
		t.Fatalf("expected RenderLaunchAgentPlist to include HOME env var")
	}
	if got := ParseLaunchAgentProgram(plist); got != binPath {
		t.Fatalf("ParseLaunchAgentProgram = %q, want %q", got, binPath)
	}
	if gotBin, ok := ValidateLaunchAgentPlist(plist, func(p string) bool { return p == binPath }); !ok || gotBin != binPath {
		t.Fatalf("ValidateLaunchAgentPlist valid case failed: bin=%q ok=%t", gotBin, ok)
	}
	if _, ok := ValidateLaunchAgentPlist(plist, func(string) bool { return false }); ok {
		t.Fatalf("ValidateLaunchAgentPlist MUST fail when binary is not executable/present")
	}
	missingRun := strings.ReplaceAll(plist, "<string>run</string>", "<string>status</string>")
	if _, ok := ValidateLaunchAgentPlist(missingRun, func(string) bool { return true }); ok {
		t.Fatalf("ValidateLaunchAgentPlist MUST fail when <string>run</string> is missing")
	}
	if got := ParseLaunchAgentProgram("<plist></plist>"); got != "" {
		t.Fatalf("expected empty program on malformed plist, got %q", got)
	}
}

func TestEnsureLaunchAgentIdempotentSkipAndRepair(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	validBin := "/Users/demo/.local/bin/peaceclaude"
	validPlist := RenderLaunchAgentPlist(validBin)

	// 1. Healthy & loaded & running -> skipped without touching files or launchd.
	installCalled := false
	startCalled := false
	res, err := ensureLaunchAgentWithOps(cfg, validBin, true, launchAgentOps{
		canManage:  true,
		plistPath:  "/tmp/test-agent.plist",
		readFile:   func(string) ([]byte, error) { return []byte(validPlist), nil },
		isExec:     func(p string) bool { return p == validBin },
		isLoaded:   func() bool { return true },
		isHealthy:  func() (bool, int) { return true, 4321 },
		resolveBin: func(p string) (string, bool) { return p, false },
		installFile: func(string) (string, error) {
			installCalled = true
			return "/tmp/test-agent.plist", nil
		},
		startLaunch: func(string) error {
			startCalled = true
			return nil
		},
		waitHealthy: func() (bool, int) { return true, 4321 },
	})
	if err != nil {
		t.Fatalf("unexpected error on skip case: %v", err)
	}
	if res.Action != "skipped" || !res.Installed || !res.Loaded || !res.Running || res.PID != 4321 {
		t.Fatalf("unexpected skip result: %+v", res)
	}
	if installCalled || startCalled {
		t.Fatalf("skip path MUST NOT rewrite plist (%t) or kickstart launchd (%t)", installCalled, startCalled)
	}

	// 2. Missing plist -> installed and loaded.
	installCalled = false
	startCalled = false
	res, err = ensureLaunchAgentWithOps(cfg, validBin, true, launchAgentOps{
		canManage:  true,
		plistPath:  "/tmp/missing.plist",
		readFile:   func(string) ([]byte, error) { return nil, os.ErrNotExist },
		isExec:     func(p string) bool { return p == validBin },
		isLoaded:   func() bool { return false },
		isHealthy:  func() (bool, int) { return false, 0 },
		resolveBin: func(p string) (string, bool) { return p, false },
		installFile: func(bin string) (string, error) {
			installCalled = true
			if bin != validBin {
				t.Fatalf("installFile got unexpected bin %q", bin)
			}
			return "/tmp/missing.plist", nil
		},
		startLaunch: func(string) error {
			startCalled = true
			return nil
		},
		waitHealthy: func() (bool, int) { return true, 9001 },
	})
	if err != nil || res.Action != "installed" || !installCalled || !startCalled || res.PID != 9001 {
		t.Fatalf("installed path mismatch: res=%+v err=%v", res, err)
	}

	// 3. Invalid binary in plist + bare daemon running without launchd -> repaired + stopBare handoff.
	stopBareCalled := false
	res, err = ensureLaunchAgentWithOps(cfg, validBin, true, launchAgentOps{
		canManage:  true,
		plistPath:  "/tmp/broken.plist",
		readFile:   func(string) ([]byte, error) { return []byte(RenderLaunchAgentPlist("/nonexistent/old/peaceclaude")), nil },
		isExec:     func(p string) bool { return p == validBin },
		isLoaded:   func() bool { return false },
		isHealthy:  func() (bool, int) { return true, 555 },
		resolveBin: func(p string) (string, bool) { return p, false },
		installFile: func(string) (string, error) {
			return "/tmp/broken.plist", nil
		},
		stopBare: func() { stopBareCalled = true },
		startLaunch: func(string) error {
			if !stopBareCalled {
				t.Fatalf("expected bare daemon handoff before launchd start")
			}
			return nil
		},
		waitHealthy: func() (bool, int) { return true, 556 },
	})
	if err != nil || res.Action != "repaired" || !stopBareCalled || res.PID != 556 {
		t.Fatalf("repaired path mismatch: res=%+v err=%v stopBare=%t", res, err, stopBareCalled)
	}

	// 4. Valid plist installed, not loaded -> restarted without rewriting plist.
	installCalled = false
	res, err = ensureLaunchAgentWithOps(cfg, validBin, true, launchAgentOps{
		canManage:  true,
		plistPath:  "/tmp/unloaded.plist",
		readFile:   func(string) ([]byte, error) { return []byte(validPlist), nil },
		isExec:     func(p string) bool { return p == validBin },
		isLoaded:   func() bool { return false },
		isHealthy:  func() (bool, int) { return false, 0 },
		resolveBin: func(p string) (string, bool) { return p, false },
		installFile: func(string) (string, error) {
			installCalled = true
			return "/tmp/unloaded.plist", nil
		},
		startLaunch: func(string) error { return nil },
		waitHealthy: func() (bool, int) { return true, 666 },
	})
	if err != nil || res.Action != "restarted" || installCalled || res.PID != 666 {
		t.Fatalf("restarted path mismatch: res=%+v err=%v installCalled=%t", res, err, installCalled)
	}

	// 5. Ephemeral DMG binary without stable installation -> skipped_ephemeral_binary.
	res, err = ensureLaunchAgentWithOps(cfg, "/Volumes/PeaceClaude/PeaceClaude.app/Contents/Resources/bin/peaceclaude", true, launchAgentOps{
		canManage:   true,
		plistPath:   "/tmp/dmg.plist",
		readFile:    func(string) ([]byte, error) { return nil, os.ErrNotExist },
		isExec:      func(string) bool { return false },
		isLoaded:    func() bool { return false },
		isHealthy:   func() (bool, int) { return false, 0 },
		resolveBin:  func(string) (string, bool) { return "", true },
		installFile: func(string) (string, error) { t.Fatal("must not write ephemeral plist"); return "", nil },
		startLaunch: func(string) error { t.Fatal("must not start ephemeral launchd"); return nil },
	})
	if err != nil || res.Action != "skipped_ephemeral_binary" {
		t.Fatalf("ephemeral path mismatch: res=%+v err=%v", res, err)
	}

	// 6. AutoStart disabled -> disabled (when respectConfig=true).
	disabledCfg := config.Default()
	f := false
	disabledCfg.Listen.AutoStartLaunchAgent = &f
	res, err = ensureLaunchAgentWithOps(disabledCfg, validBin, true, launchAgentOps{
		canManage:   true,
		plistPath:   "/tmp/disabled.plist",
		isHealthy:   func() (bool, int) { return true, 777 },
		installFile: func(string) (string, error) { t.Fatal("must not install when disabled"); return "", nil },
	})
	if err != nil || res.Action != "disabled" || !res.Running || res.PID != 777 {
		t.Fatalf("disabled path mismatch: res=%+v err=%v", res, err)
	}

	// 7. Sandbox guard -> disabled_in_sandbox.
	res, err = ensureLaunchAgentWithOps(cfg, validBin, true, launchAgentOps{
		canManage: false,
		plistPath: "/tmp/sandbox.plist",
		isHealthy: func() (bool, int) { return false, 0 },
	})
	if err != nil || res.Action != "disabled_in_sandbox" {
		t.Fatalf("sandbox guard mismatch: res=%+v err=%v", res, err)
	}
}

func TestCrossPlatformAutostartRenderers(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	unit := RenderSystemdUserUnit("/usr/bin/peaceclaude")
	for _, want := range []string{`ExecStart="/usr/bin/peaceclaude" service run`, "WantedBy=default.target"} {
		if !strings.Contains(unit, want) {
			t.Fatalf("systemd unit missing %q:\n%s", want, unit)
		}
	}
	if gotBin, ok := linuxSystemdUnitValid(unit, func(p string) bool { return p == "/usr/bin/peaceclaude" }); !ok || gotBin != "/usr/bin/peaceclaude" {
		t.Fatalf("linuxSystemdUnitValid failed: bin=%q ok=%t", gotBin, ok)
	}
	if _, ok := linuxSystemdUnitValid(unit, func(string) bool { return false }); ok {
		t.Fatal("linuxSystemdUnitValid must reject missing binary")
	}

	winCmd := RenderWindowsRunCommand(`C:\Program Files\PeaceClaude\peaceclaude.exe`)
	if winCmd != `"C:\Program Files\PeaceClaude\peaceclaude.exe" service run` {
		t.Fatalf("unexpected Windows Run command: %q", winCmd)
	}
}

func TestBuildProxyBypassListCorporateDirectAndAnthropicGuard(t *testing.T) {
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

	const baseBypass = "127.0.0.1;localhost;::1;*.local"

	// 1. Positive case: vetted corporate domains expand to Chromium wildcard + bare/exact entries.
	cfg := config.Default()
	cfg.Binaries.ClaudeDesktopApp = fakeApp
	cfg.Shield.DesktopProxyMode = "proxy"
	cfg.Shield.AllowCorporateDirect = true
	cfg.Shield.CorporateDomains = []string{".corp.example", "*.cdn.corp.net", "git.corp.org"}
	wantBypass := baseBypass + ";*.corp.example;corp.example;*.cdn.corp.net;cdn.corp.net;git.corp.org"
	if got := BuildProxyBypassList(cfg); got != wantBypass {
		t.Fatalf("BuildProxyBypassList = %q, want %q", got, wantBypass)
	}

	prof, err := profile.Ensure(cfg, "bypass-check", "")
	if err != nil {
		t.Fatal(err)
	}
	gate := egress.GateSnapshot{Open: true, EffectiveTimezone: "America/Denver", EffectiveLanguage: "en-US"}
	spec, err := BuildDesktopSpec(cfg, prof, gate, nil)
	if err != nil {
		t.Fatalf("BuildDesktopSpec: %v", err)
	}
	wantFlag := "--proxy-bypass-list=" + wantBypass
	foundFlag := false
	for _, arg := range spec.Args {
		if arg == wantFlag {
			foundFlag = true
			break
		}
	}
	if !foundFlag {
		t.Fatalf("BuildDesktopSpec args missing %q: %v", wantFlag, spec.Args)
	}

	// 2. Disabled allow_corporate_direct must revert strictly to loopback/local bypasses.
	cfg.Shield.AllowCorporateDirect = false
	if got := BuildProxyBypassList(cfg); got != baseBypass {
		t.Fatalf("BuildProxyBypassList (disabled) = %q, want %q", got, baseBypass)
	}
	if got := BuildProxyBypassList(nil); got != baseBypass {
		t.Fatalf("BuildProxyBypassList (nil) = %q, want %q", got, baseBypass)
	}

	// 3. Negative guard: Anthropic/Claude domains, broad TLD wildcards, and malformed/injection tokens must be rejected.
	cfg.Shield.AllowCorporateDirect = true
	cfg.Shield.CorporateDomains = []string{
		".anthropic.com",
		".claude.ai",
		"api.anthropic.com",
		"*.claude.com",
		".com",
		".ai",
		"*.bad;evil.com",
		"<local>",
		"",
		"*bad.example",
		"a.*.b.example",
		"-bad.example",
		".corp.example",
	}
	wantGuarded := baseBypass + ";*.corp.example;corp.example"
	if got := BuildProxyBypassList(cfg); got != wantGuarded {
		t.Fatalf("BuildProxyBypassList (guard) = %q, want %q", got, wantGuarded)
	}

	// 4. Custom ExtraClaudeDomains must also be protected from corporate bypass overrides.
	cfg.Shield.ExtraClaudeDomains = []string{"custom.claude.example"}
	cfg.Shield.CorporateDomains = []string{
		"custom.claude.example",
		"sub.custom.claude.example",
		".claude.example",
		".corp.example",
	}
	if got := BuildProxyBypassList(cfg); got != wantGuarded {
		t.Fatalf("BuildProxyBypassList (extra protected) = %q, want %q", got, wantGuarded)
	}
}

func TestBuildLoginBrowserProxyArgsUsesLoginPACAndProxyMode(t *testing.T) {
	cfg := config.Default()
	pacArgs := BuildLoginBrowserProxyArgs(cfg)
	joined := strings.Join(pacArgs, " ")
	if !strings.Contains(joined, "--proxy-pac-url="+cfg.LoginBrowserPACURL()) || !strings.Contains(joined, "mode=login") {
		t.Fatalf("expected BuildLoginBrowserProxyArgs to emit mode=login PAC, got %v", pacArgs)
	}

	cfg.Shield.DesktopProxyMode = "proxy"
	proxyArgs := BuildLoginBrowserProxyArgs(cfg)
	joinedProxy := strings.Join(proxyArgs, " ")
	if !strings.Contains(joinedProxy, "--proxy-server="+cfg.HTTPProxyURL()) || !strings.Contains(joinedProxy, "--proxy-bypass-list=") {
		t.Fatalf("expected proxy mode flags, got %v", proxyArgs)
	}
}

func TestInspectIsolatedBrowserProfileDetectsChinesePrefsAndDisabledShield(t *testing.T) {
	tmp := t.TempDir()
	bdir := filepath.Join(tmp, "browser-profile")

	// 1. Clean preferences + generated extension -> ready & clean
	if err := ensureBrowserPrivacyPrefs(bdir, "en-US"); err != nil {
		t.Fatal(err)
	}
	extDir, err := ensureBrowserShieldExtension(bdir, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range []string{"manifest.json", "rules.json", "background.js", "shield.js", "icon48.png", "icon128.png"} {
		if _, err := os.Stat(filepath.Join(extDir, fn)); err != nil {
			t.Fatalf("missing extension artifact %s: %v", fn, err)
		}
	}
	cleanHealth := InspectIsolatedBrowserProfile(bdir)
	if !cleanHealth.Initialized || !cleanHealth.ExtArtifactsReady || !cleanHealth.PrefsClean || len(cleanHealth.Issues) != 0 {
		t.Fatalf("expected clean profile health, got %+v", cleanHealth)
	}

	// 2. Dirty zh-CN preferences + Secure Preferences without shield -> flagged
	defDir := filepath.Join(bdir, "Default")
	if err := os.WriteFile(filepath.Join(defDir, "Preferences"), []byte(`{"intl":{"accept_languages":"zh-CN,zh","selected_languages":"zh-CN,zh"},"enable_do_not_track":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(defDir, "Secure Preferences"), []byte(`{"extensions":{"settings":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dirtyHealth := InspectIsolatedBrowserProfile(bdir)
	if dirtyHealth.PrefsClean || !dirtyHealth.HasSecurePrefs || dirtyHealth.ExtEnabled || len(dirtyHealth.Issues) < 2 {
		t.Fatalf("expected dirty prefs & missing shield flagged, got %+v", dirtyHealth)
	}

	// 3. Disabled shield entry in Secure Preferences -> ExtLoaded=true, ExtEnabled=false
	_ = ensureBrowserPrivacyPrefs(bdir, "en-US")
	secDisabled, _ := json.Marshal(map[string]any{
		"extensions": map[string]any{
			"settings": map[string]any{
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": map[string]any{
					"path":            extDir,
					"disable_reasons": []int{8192},
					"manifest":        map[string]any{"name": isolatedShieldExtName},
				},
			},
		},
	})
	_ = os.WriteFile(filepath.Join(defDir, "Secure Preferences"), secDisabled, 0o600)
	disHealth := InspectIsolatedBrowserProfile(bdir)
	if !disHealth.ExtLoaded || disHealth.ExtEnabled || len(disHealth.Issues) == 0 {
		t.Fatalf("expected disabled shield flagged, got %+v", disHealth)
	}
}

func TestRestartServiceWithOps(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	validBin := "/Users/demo/.local/bin/peaceclaude"
	validPlist := RenderLaunchAgentPlist(validBin)

	// 1. Loaded valid LaunchAgent on darwin -> kickstart -k directly without stopService/ensureAutostart.
	kickCalled := false
	stopCalled := false
	ensureCalled := false
	res, err := restartServiceWithOps(cfg, true, restartServiceOps{
		osName:        "darwin",
		canManageHost: true,
		plistPath:     "/tmp/test-agent.plist",
		readFile:      func(string) ([]byte, error) { return []byte(validPlist), nil },
		isExec:        func(p string) bool { return p == validBin },
		isLoaded:      func() bool { return true },
		isHealthy:     func() (bool, int) { return true, 1001 },
		kickstart: func() error {
			kickCalled = true
			return nil
		},
		stopService: func() error {
			stopCalled = true
			return nil
		},
		ensureAutostart: func() (EnsureLaunchAgentResult, error) {
			ensureCalled = true
			return EnsureLaunchAgentResult{}, nil
		},
		waitPIDChange: func(oldPID int) (bool, int) {
			if oldPID != 1001 {
				t.Fatalf("expected oldPID=1001, got %d", oldPID)
			}
			return true, 2002
		},
		inspectStatus: func(running bool, pid int) ServiceStatus {
			return ServiceStatus{Running: running, PID: pid, LaunchAgentLoaded: true}
		},
	})
	if err != nil {
		t.Fatalf("kickstart path failed: %v", err)
	}
	if !kickCalled || stopCalled || ensureCalled || !res.OK || res.OldPID != 1001 || res.NewPID != 2002 || res.ManagedBy != "launchd" {
		t.Fatalf("unexpected kickstart result: res=%+v kick=%t stop=%t ensure=%t", res, kickCalled, stopCalled, ensureCalled)
	}

	// 2. kickstart -k fails -> cleanly stops & falls back to ensureAutostart (bootout+bootstrap retry path), never silent detached.
	stopCalled = false
	waitDownCalled := false
	ensureCalled = false
	res, err = restartServiceWithOps(cfg, true, restartServiceOps{
		osName:         "darwin",
		canManageHost:  true,
		plistPath:      "/tmp/test-agent.plist",
		readFile:       func(string) ([]byte, error) { return []byte(validPlist), nil },
		isExec:         func(p string) bool { return p == validBin },
		isLoaded:       func() bool { return true },
		isHealthy:      func() (bool, int) { return true, 3001 },
		kickstart:      func() error { return errors.New("kickstart failed") },
		stopService:    func() error { stopCalled = true; return nil },
		waitDaemonDown: func() bool { waitDownCalled = true; return true },
		ensureAutostart: func() (EnsureLaunchAgentResult, error) {
			ensureCalled = true
			return EnsureLaunchAgentResult{Action: "restarted", Running: true, Loaded: true, PID: 3002}, nil
		},
		waitPIDChange: func(int) (bool, int) { return true, 3002 },
		inspectStatus: func(running bool, pid int) ServiceStatus {
			return ServiceStatus{Running: running, PID: pid, LaunchAgentLoaded: true}
		},
	})
	if err != nil || !stopCalled || !waitDownCalled || !ensureCalled || res.NewPID != 3002 || res.ManagedBy != "launchd" {
		t.Fatalf("cold launchd reload path mismatch: res=%+v err=%v stop=%t down=%t ensure=%t", res, err, stopCalled, waitDownCalled, ensureCalled)
	}

	// 3. ensureAutostart failure MUST surface explicit error (do not silently drop LaunchAgent custody).
	_, err = restartServiceWithOps(cfg, true, restartServiceOps{
		osName:          "darwin",
		canManageHost:   true,
		plistPath:       "/tmp/test-agent.plist",
		readFile:        func(string) ([]byte, error) { return nil, os.ErrNotExist },
		isExec:          func(string) bool { return true },
		isLoaded:        func() bool { return false },
		isHealthy:       func() (bool, int) { return false, 0 },
		stopService:     func() error { return nil },
		waitDaemonDown:  func() bool { return true },
		ensureAutostart: func() (EnsureLaunchAgentResult, error) { return EnsureLaunchAgentResult{}, errors.New("bootstrap rejected") },
		startDetached:   func() error { t.Fatal("must not fall back to detached when launchd explicitly failed"); return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "bootstrap rejected") {
		t.Fatalf("expected explicit LaunchAgent error, got %v", err)
	}

	// 4. Sandbox / --no-launchd mode -> detached restart flow.
	detachedCalled := false
	res, err = restartServiceWithOps(cfg, false, restartServiceOps{
		osName:         "darwin",
		canManageHost:  false,
		isHealthy:      func() (bool, int) { return true, 4001 },
		stopService:    func() error { return nil },
		waitDaemonDown: func() bool { return true },
		startDetached:  func() error { detachedCalled = true; return nil },
		waitPIDChange:  func(int) (bool, int) { return true, 4002 },
		inspectStatus:  func(running bool, pid int) ServiceStatus { return ServiceStatus{Running: running, PID: pid} },
	})
	if err != nil || !detachedCalled || res.OldPID != 4001 || res.NewPID != 4002 || res.ManagedBy != "detached" {
		t.Fatalf("detached restart mismatch: res=%+v err=%v detached=%t", res, err, detachedCalled)
	}
}
