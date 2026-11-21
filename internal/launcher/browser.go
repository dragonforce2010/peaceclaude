package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/profile"
)

func DetectLoginBrowser(cfg *config.Config) string {
	if cfg != nil && cfg.Binaries.LoginBrowserApp != "" {
		if _, err := os.Stat(cfg.Binaries.LoginBrowserApp); err == nil {
			return cfg.Binaries.LoginBrowserApp
		}
	}
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"/Applications/Arc.app/Contents/MacOS/Arc",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// LaunchLoginBrowser opens an isolated Chromium/Chrome browser profile routed strictly
// through PeaceClaude's static-IP proxy with matching TZ & en-US locale for clean OAuth login.
func LaunchLoginBrowser(ctx context.Context, cfg *config.Config, selfBin, profileName, targetURL string, dryRun bool) (string, []string, error) {
	browserBin, _, args, err := LaunchLoginBrowserWithPID(ctx, cfg, selfBin, profileName, targetURL, dryRun)
	return browserBin, args, err
}

// LaunchLoginBrowserWithPID opens an isolated login browser, verifies startup health for 700ms,
// focuses its window, and returns its executable path, process PID, and launch args.
func LaunchLoginBrowserWithPID(ctx context.Context, cfg *config.Config, selfBin, profileName, targetURL string, dryRun bool) (string, int, []string, error) {
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	if targetURL == "" {
		targetURL = "https://claude.ai/login"
	}
	prof, err := profile.Ensure(cfg, profileName, "")
	if err != nil {
		return "", 0, nil, err
	}
	gate, err := Preflight(ctx, cfg, selfBin, dryRun)
	if err != nil {
		return "", 0, nil, err
	}
	browserBin := DetectLoginBrowser(cfg)
	if browserBin == "" {
		return "", 0, nil, fmt.Errorf("未找到受支持的 Chromium 内核浏览器（Chrome/Brave/Edge/Arc），请通过 config.binaries.login_browser_app 指定")
	}

	lang := cfg.Identity.Language
	if prof.Language != "" {
		lang = prof.Language
	}
	if lang == "" {
		lang = "en-US"
	}

	_ = ensureBrowserDNSPrivacyPrefs(prof.BrowserDir)

	args := []string{
		"--user-data-dir=" + prof.BrowserDir,
		"--proxy-server=" + cfg.HTTPProxyURL(),
		"--proxy-bypass-list=127.0.0.1;localhost;::1;*.local",
		"--lang=" + lang,
		"--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--no-first-run",
		"--no-default-browser-check",
		targetURL,
	}

	if dryRun {
		return browserBin, 0, args, nil
	}

	_ = config.EnsureDirs()
	logPath := filepath.Join(config.LogsDir(), "browser-launcher.log")
	var startOffset int64
	if fi, serr := os.Stat(logPath); serr == nil {
		startOffset = fi.Size()
	}
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)

	cmd := exec.Command(browserBin, args...)
	cmd.Env = BuildIsolatedEnv(cfg, prof, gate, os.Environ())
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return browserBin, 0, args, fmt.Errorf("launch isolated browser (%s): %w", filepath.Base(browserBin), err)
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	pid := cmd.Process.Pid

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case exitErr := <-waitCh:
		detail := readLogTailFrom(logPath, startOffset, 4096)
		if exitErr == nil {
			if existing := findRunningBrowserPID(prof.BrowserDir); existing > 0 {
				focusProcessWindow(existing)
				return browserBin, existing, args, nil
			}
			// Even if the handoff exited 0 cleanly to an already open window, succeed.
			return browserBin, pid, args, nil
		}
		return browserBin, 0, args, fmt.Errorf("隔离浏览器 (%s) 启动后退出 (%v): %s", filepath.Base(browserBin), exitErr, detail)
	case <-time.After(700 * time.Millisecond):
		focusProcessWindow(pid)
		return browserBin, pid, args, nil
	}
}

func findRunningBrowserPID(userDataDir string) int {
	want := filepath.Clean(userDataDir)
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return 0
	}
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || !strings.Contains(ln, "--user-data-dir=") {
			continue
		}
		idx := strings.IndexAny(ln, " \t")
		if idx <= 0 {
			continue
		}
		pidStr := strings.TrimSpace(ln[:idx])
		cmdLine := strings.TrimSpace(ln[idx+1:])
		if strings.Contains(cmdLine, "--type=") {
			continue
		}
		if ExtractUserDataDir(cmdLine) == want {
			if pid, err := strconv.Atoi(pidStr); err == nil && pid > 1 {
				return pid
			}
		}
	}
	return 0
}

func ensureBrowserDNSPrivacyPrefs(browserDir string) error {
	if strings.TrimSpace(browserDir) == "" {
		return nil
	}
	defDir := filepath.Join(browserDir, "Default")
	if err := os.MkdirAll(defDir, 0o700); err != nil {
		return err
	}
	prefsPath := filepath.Join(defDir, "Preferences")
	prefs := map[string]any{}
	if raw, err := os.ReadFile(prefsPath); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
		if uerr := json.Unmarshal(raw, &prefs); uerr != nil {
			// Avoid corrupting existing user preferences if file is partial/locked.
			return nil
		}
	}
	prefs["dns_prefetching"] = map[string]any{"enabled": false}
	prefs["async_dns"] = map[string]any{"enabled": false}
	// Block HTML5 Geolocation API unconditionally inside the isolated browser profile so
	// sites cannot query macOS CoreLocation / Wi-Fi positioning behind the static US exit.
	profileMap, _ := prefs["profile"].(map[string]any)
	if profileMap == nil {
		profileMap = map[string]any{}
	}
	contentSettings, _ := profileMap["default_content_setting_values"].(map[string]any)
	if contentSettings == nil {
		contentSettings = map[string]any{}
	}
	contentSettings["geolocation"] = 2
	profileMap["default_content_setting_values"] = contentSettings
	prefs["profile"] = profileMap
	out, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	return os.WriteFile(prefsPath, out, 0o600)
}
