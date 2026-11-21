package launcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

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
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	if targetURL == "" {
		targetURL = "https://claude.ai/login"
	}
	prof, err := profile.Ensure(cfg, profileName, "")
	if err != nil {
		return "", nil, err
	}
	gate, err := Preflight(ctx, cfg, selfBin, dryRun)
	if err != nil {
		return "", nil, err
	}
	browserBin := DetectLoginBrowser(cfg)
	if browserBin == "" {
		return "", nil, fmt.Errorf("未找到受支持的 Chromium 内核浏览器（Chrome/Brave/Edge/Arc），请通过 config.binaries.login_browser_app 指定")
	}

	lang := cfg.Identity.Language
	if prof.Language != "" {
		lang = prof.Language
	}
	if lang == "" {
		lang = "en-US"
	}

	args := []string{
		"--user-data-dir=" + prof.BrowserDir,
		"--proxy-server=" + cfg.HTTPProxyURL(),
		"--proxy-bypass-list=127.0.0.1;localhost;::1;*.local",
		"--lang=" + lang,
		"--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--no-first-run",
		"--no-default-browser-check",
	}
	if cfg.Identity.BlockDesktopLocalDNS {
		args = append(args, "--host-resolver-rules=MAP * ~NOTFOUND , EXCLUDE 127.0.0.1, EXCLUDE localhost")
	}
	args = append(args, targetURL)

	if dryRun {
		return browserBin, args, nil
	}

	cmd := exec.Command(browserBin, args...)
	cmd.Env = BuildIsolatedEnv(cfg, prof, gate, os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return browserBin, args, fmt.Errorf("launch isolated browser (%s): %w", filepath.Base(browserBin), err)
	}
	_ = cmd.Process.Release()
	return browserBin, args, nil
}
