package launcher

import (
	"context"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/host"
	"peaceclaude/internal/profile"
	"peaceclaude/internal/version"
)

//go:embed app_macos.swift
var macOSAppSwiftSrc string

// ForbiddenClaudeDesktopSwitches lists Chromium switches explicitly rejected by
// official Claude Desktop v6+ startup security checks ("refusing to start —
// a debugging or network-override switch is present on the command line").
var ForbiddenClaudeDesktopSwitches = []string{
	"remote-debugging-port",
	"remote-debugging-pipe",
	"ignore-certificate-errors",
	"host-resolver-rules",
	"host-rules",
	"disable-web-security",
	"log-net-log",
	"net-log-capture-mode",
	"ssl-key-log-file",
	"renderer-cmd-prefix",
	"utility-cmd-prefix",
	"gpu-launcher",
	"zygote-cmd-prefix",
	"ppapi-plugin-launcher",
	"nacl-loader-cmd-prefix",
	"browser-subprocess-path",
}

type DesktopSpec struct {
	Binary  string   `json:"binary"`
	Args    []string `json:"args"`
	Env     []string `json:"-"`
	Profile string   `json:"profile"`
	DataDir string   `json:"data_dir"`
}

func BuildDesktopSpec(cfg *config.Config, prof *profile.Profile, gate egress.GateSnapshot, extraArgs []string) (*DesktopSpec, error) {
	appTarget := strings.TrimSpace(cfg.Binaries.ClaudeDesktopApp)
	bin := appTarget
	if runtime.GOOS == "darwin" || strings.HasSuffix(strings.ToLower(appTarget), ".app") {
		bin = filepath.Join(appTarget, "Contents/MacOS/Claude")
	}
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("未找到 Claude Desktop (%s)，请确认已安装桌面客户端", bin)
	}
	lang := cfg.Identity.Language
	if prof.Language != "" {
		lang = prof.Language
	}
	if lang == "" {
		lang = "en-US"
	}
	appleLocale := strings.ReplaceAll(lang, "-", "_")

	// IMPORTANT: Official Claude Desktop v6+ explicitly rejects --host-resolver-rules
	// (printing "refusing to start — a debugging or network-override switch is present")
	// while internally disabling DNS prefetching (dnsPrefetchingEnabled=false).
	// Passing --proxy-server + --disable-quic + proxy env vars routes all HTTP/HTTPS/WS
	// traffic and hostname resolution through PeaceClaude's local gateway cleanly.
	args := []string{
		"--user-data-dir=" + prof.DesktopDir,
		"--proxy-server=" + cfg.HTTPProxyURL(),
		"--proxy-bypass-list=127.0.0.1;localhost;::1;*.local",
		"--lang=" + lang,
		"--accept-lang=" + lang + ",en;q=0.9",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
	}
	if runtime.GOOS == "darwin" {
		args = append(args, "-AppleLanguages", fmt.Sprintf("(%s)", lang), "-AppleLocale", appleLocale)
	}
	if cfg.Identity.DisableDesktopQUIC {
		args = append(args, "--disable-quic")
	}
	args = append(args, extraArgs...)

	env := BuildIsolatedEnv(cfg, prof, gate, os.Environ())
	return &DesktopSpec{
		Binary:  bin,
		Args:    args,
		Env:     env,
		Profile: prof.Name,
		DataDir: prof.DesktopDir,
	}, nil
}

func readLogTailFrom(path string, offset int64, maxBytes int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if offset > 0 {
		_, _ = f.Seek(offset, io.SeekStart)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

type DesktopLaunchOptions struct {
	ExtraArgs       []string
	DryRun          bool
	ExclusiveSwitch bool
}

// LaunchDesktop spawns an isolated Claude Desktop instance directly (bypassing `open -a` flag loss),
// verifies the process survives initial startup checks, and brings its window to the foreground.
func LaunchDesktop(ctx context.Context, cfg *config.Config, selfBin, profileName string, extraArgs []string, dryRun bool) (*DesktopSpec, int, error) {
	spec, pid, _, err := LaunchDesktopWithOptions(ctx, cfg, selfBin, profileName, DesktopLaunchOptions{
		ExtraArgs:       extraArgs,
		DryRun:          dryRun,
		ExclusiveSwitch: cfg != nil && cfg.Identity.SingleActiveDesktop,
	})
	return spec, pid, err
}

// LaunchDesktopWithOptions launches an isolated Claude Desktop instance with configurable
// single-active-instance switching so macOS LaunchServices claude:// OAuth deep-links
// always route to the target profile window instead of a stale background profile.
func LaunchDesktopWithOptions(ctx context.Context, cfg *config.Config, selfBin, profileName string, opts DesktopLaunchOptions) (*DesktopSpec, int, []string, error) {
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	prof, err := profile.Ensure(cfg, profileName, "")
	if err != nil {
		return nil, 0, nil, err
	}
	gate, err := Preflight(ctx, cfg, selfBin, opts.DryRun)
	if err != nil {
		return nil, 0, nil, err
	}
	spec, err := BuildDesktopSpec(cfg, prof, gate, opts.ExtraArgs)
	if err != nil {
		return nil, 0, nil, err
	}
	if opts.DryRun {
		return spec, 0, nil, nil
	}

	var stoppedOthers []string
	if opts.ExclusiveSwitch {
		stoppedOthers = StopOtherIsolatedDesktops(cfg, prof.DesktopDir)
	}

	// If an isolated Claude Desktop for the exact same profile is already running,
	// bring it cleanly to the foreground (and deliver any claude:// deep links via AppleEvent)
	// instead of triggering an Electron second-instance exit.
	if existingPID := findRunningIsolatedDesktopPID(cfg, prof.DesktopDir); existingPID > 0 {
		for _, arg := range opts.ExtraArgs {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), "claude://") {
				deliverDeepLinkToRunningDesktop(arg)
			}
		}
		focusProcessWindow(existingPID)
		return spec, existingPID, stoppedOthers, nil
	}

	_ = config.EnsureDirs()
	logPath := config.AppLauncherLog()
	var startOffset int64
	if fi, err := os.Stat(logPath); err == nil {
		startOffset = fi.Size()
	}
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)

	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Env = spec.Env
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	setDetachedProcessAttr(cmd)
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return spec, 0, stoppedOthers, fmt.Errorf("launch Claude Desktop: %w", err)
	}
	if logFile != nil {
		// Child process owns its own duplicated fd; close parent handle safely without SIGPIPE risk.
		_ = logFile.Close()
	}

	pid := cmd.Process.Pid

	// Monitor initial startup for 1.5s so any immediate Claude.app refusal/crash is surfaced
	// immediately instead of silently returning exit 0.
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case exitErr := <-waitCh:
		detail := readLogTailFrom(logPath, startOffset, 8192)
		if detail != "" {
			lines := strings.Split(detail, "\n")
			if len(lines) > 6 {
				lines = lines[len(lines)-6:]
			}
			detail = strings.Join(lines, " | ")
		}
		if exitErr != nil {
			return spec, 0, stoppedOthers, fmt.Errorf("Claude Desktop 启动后立即退出 (%v): %s", exitErr, detail)
		}
		// Exit 0 can occur if Electron handed off to an already-running instance for the same userDataDir.
		if existingPID := findRunningIsolatedDesktopPID(cfg, prof.DesktopDir); existingPID > 0 {
			focusProcessWindow(existingPID)
			return spec, existingPID, stoppedOthers, nil
		}
		return spec, 0, stoppedOthers, fmt.Errorf("Claude Desktop 启动后提前退出 (exit 0): %s", detail)
	case <-time.After(1500 * time.Millisecond):
		// Process survived startup checks; bring its window to the front and notify user.
		focusProcessWindow(pid)
		if os.Getenv("PEACECLAUDE_NO_NOTIFY") != "1" {
			ip := gate.LastGood.EgressIP
			if ip == "" {
				ip = gate.LastProbe.EgressIP
			}
			msg := fmt.Sprintf("档案 %s 已进入安心模式 · 静态出口 %s", spec.Profile, ip)
			if len(stoppedOthers) > 0 {
				msg = fmt.Sprintf("已切换至档案 %s（停用旧窗口以防登录回调串号）· 出口 %s", spec.Profile, ip)
			}
			runDetached("/usr/bin/osascript", "-e",
				fmt.Sprintf(`display notification "%s" with title "PeaceClaude 已就绪"`, EscapeAppleScript(msg)))
		}
		return spec, pid, stoppedOthers, nil
	}
}

// EscapeAppleScript safely escapes backslashes and quotes for AppleScript double-quoted string literals.
func EscapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

func runDetached(name string, args ...string) {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return
	}
	go func() {
		_ = cmd.Wait()
	}()
}

// ExtractClaudeDeepLinkArgs parses claude:// deep-link URLs from a raw process command line, deduplicated.
func ExtractClaudeDeepLinkArgs(rawCmd string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tok := range strings.Fields(rawCmd) {
		clean := strings.Trim(tok, `"'`)
		if strings.HasPrefix(strings.ToLower(clean), "claude://") && len(clean) > len("claude://") {
			if !seen[clean] {
				seen[clean] = true
				out = append(out, clean)
			}
		}
	}
	return out
}

// ExtractUserDataDir extracts the --user-data-dir=<path> flag value from a raw process command line.
func ExtractUserDataDir(rawCmd string) string {
	const prefix = "--user-data-dir="
	idx := strings.Index(rawCmd, prefix)
	if idx < 0 {
		return ""
	}
	rest := rawCmd[idx+len(prefix):]
	if rest == "" {
		return ""
	}
	if rest[0] == '"' || rest[0] == '\'' {
		q := rest[0]
		if end := strings.IndexByte(rest[1:], q); end >= 0 {
			return filepath.Clean(rest[1 : 1+end])
		}
	}
	// Stop at next Chromium/Apple flag marker (" --" or " -Apple") to tolerate paths containing spaces.
	cut := len(rest)
	for _, sep := range []string{" --", " -Apple", " claude://"} {
		if p := strings.Index(rest, sep); p >= 0 && p < cut {
			cut = p
		}
	}
	return filepath.Clean(strings.TrimSpace(rest[:cut]))
}

// StopOtherIsolatedDesktops terminates running isolated Claude Desktop instances belonging to other profiles
// so macOS LaunchServices routes claude:// OAuth callbacks exclusively to the active target profile.
func StopOtherIsolatedDesktops(cfg *config.Config, targetDataDir string) []string {
	rep := host.InspectRunningDesktop(cfg)
	wantDir := filepath.Clean(targetDataDir)
	var stopPIDs []string
	for _, p := range rep.Processes {
		cmd := p.RawCommand
		if cmd == "" {
			cmd = p.Command
		}
		dataDir := ExtractUserDataDir(cmd)
		if p.Isolated && dataDir != "" && dataDir != wantDir {
			stopPIDs = append(stopPIDs, p.PID)
		}
	}
	if len(stopPIDs) == 0 {
		return nil
	}
	TerminatePIDs(stopPIDs, 4*time.Second)
	// Allow LaunchServices a brief moment to unregister the exited bundle handle before relaunching.
	time.Sleep(250 * time.Millisecond)
	return stopPIDs
}

func deliverDeepLinkToRunningDesktop(deepLink string) {
	url := strings.TrimSpace(deepLink)
	if !strings.HasPrefix(strings.ToLower(url), "claude://") {
		return
	}
	runDetached("/usr/bin/open", "-b", "com.anthropic.claudefordesktop", url)
}

func findRunningIsolatedDesktopPID(cfg *config.Config, dataDir string) int {
	rep := host.InspectRunningDesktop(cfg)
	wantDir := filepath.Clean(dataDir)
	for _, p := range rep.Processes {
		cmd := p.RawCommand
		if cmd == "" {
			cmd = p.Command
		}
		if p.Isolated && ExtractUserDataDir(cmd) == wantDir {
			if pid, err := strconv.Atoi(p.PID); err == nil && pid > 1 {
				return pid
			}
		}
	}
	return 0
}

func focusProcessWindow(pid int) {
	if pid <= 1 {
		return
	}
	script := fmt.Sprintf(`tell application "System Events"
	try
		set frontmost of first process whose unix id is %d to true
	end try
end tell`, pid)
	runDetached("/usr/bin/osascript", "-e", script)
}

// TerminatePIDs cleanly terminates exposed (non-isolated) Claude Desktop main PIDs
// with SIGTERM first, escalating to SIGKILL only if still alive after timeout.
func TerminatePIDs(pids []string, wait time.Duration) int {
	if wait <= 0 {
		wait = 4 * time.Second
	}
	var numeric []int
	for _, s := range pids {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 1 {
			numeric = append(numeric, n)
		}
	}
	if len(numeric) == 0 {
		return 0
	}
	return terminatePIDNumeric(numeric, wait)
}

// cfBundleBuildVersion normalizes a semver string into a pure numeric CFBundleVersion.
func cfBundleBuildVersion(ver string) string {
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ver), "v"))
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "0.2.0"
	}
	return v
}

func isPathWithin(path, parent string) bool {
	cleanPath := filepath.Clean(path)
	cleanParent := filepath.Clean(parent)
	return cleanPath == cleanParent || strings.HasPrefix(cleanPath, cleanParent+string(filepath.Separator))
}

func copyExecutableFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".peaceclaude-bin-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// InstallDesktopLauncherAppTo builds a self-contained `PeaceClaude.app` inside targetDir,
// embedding the Go CLI at `Contents/Resources/bin/peaceclaude` when available.
func InstallDesktopLauncherAppTo(binPath, targetDir string) (string, error) {
	if strings.TrimSpace(targetDir) == "" {
		return "", fmt.Errorf("目标目录不能为空")
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}

	appDir := filepath.Join(targetDir, "PeaceClaude.app")
	contents := filepath.Join(appDir, "Contents")
	macOSDir := filepath.Join(contents, "MacOS")
	resDir := filepath.Join(contents, "Resources")
	for _, d := range []string{macOSDir, resDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}

	nativeBinPath := filepath.Join(macOSDir, "PeaceClaude")
	launcherPath := filepath.Join(macOSDir, "launcher")
	execName := "launcher"

	if builtNative := buildNativeMacOSAppBinary(nativeBinPath); builtNative {
		execName = "PeaceClaude"
		_ = os.Remove(launcherPath)
	} else {
		_ = os.Remove(nativeBinPath)
		launcherScript := fmt.Sprintf(`#!/bin/sh
# Generated by peaceclaude setup; rewritten on upgrade, do not edit manually.
'%s' ui >>'%s' 2>&1
`, binPath, config.AppLauncherLog())
		if err := os.WriteFile(launcherPath, []byte(launcherScript), 0o755); err != nil {
			return "", err
		}
	}

	bundledCLIPath := filepath.Join(resDir, "bin", "peaceclaude")
	hasBundledCLI := false
	if cleanBin := strings.TrimSpace(binPath); cleanBin != "" && !isPathWithin(cleanBin, appDir) {
		if fi, err := os.Stat(cleanBin); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			if err := copyExecutableFile(cleanBin, bundledCLIPath); err != nil {
				return "", fmt.Errorf("embed peaceclaude binary: %w", err)
			}
			hasBundledCLI = true
		}
	}

	shortVer := strings.TrimSpace(version.Version)
	if shortVer == "" {
		shortVer = "0.2.0"
	}
	bundleVer := cfBundleBuildVersion(shortVer)

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleDisplayName</key>
  <string>PeaceClaude</string>
  <key>CFBundleExecutable</key>
  <string>%s</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>CFBundleIdentifier</key>
  <string>com.peaceclaude.launcher.app</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>PeaceClaude</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>%s</string>
  <key>CFBundleVersion</key>
  <string>%s</string>
  <key>NSHighResolutionCapable</key>
  <true/>
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsLocalNetworking</key>
    <true/>
  </dict>
</dict>
</plist>
`, execName, shortVer, bundleVer)
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		return "", err
	}

	// Generate crisp custom shield-lock icon (.icns)
	_ = generateAppIcon(filepath.Join(resDir, "AppIcon.icns"))

	if codesign, err := exec.LookPath("codesign"); err == nil {
		if hasBundledCLI {
			_ = exec.Command(codesign, "--force", "--sign", "-", bundledCLIPath).Run()
		}
		if execName == "PeaceClaude" {
			_ = exec.Command(codesign, "--force", "--sign", "-", nativeBinPath).Run()
		}
		_ = exec.Command(codesign, "--force", "--deep", "--sign", "-", appDir).Run()
	}
	return appDir, nil
}

// InstallDesktopLauncherApp builds `/Applications/PeaceClaude.app` (or ~/Applications fallback)
// as a native Swift/WKWebView management console app (falling back to shell launcher if swiftc is unavailable).
func InstallDesktopLauncherApp(binPath string) (string, error) {
	targetDir := "/Applications"
	if f, err := os.CreateTemp(targetDir, ".peaceclaude-write-test-*"); err == nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
	} else {
		home, _ := os.UserHomeDir()
		targetDir = filepath.Join(home, "Applications")
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return "", err
		}
	}

	// Clean up legacy launcher names if present so there is one unambiguous PeaceClaude.app in /Applications.
	_ = os.RemoveAll(filepath.Join(targetDir, "CageClaude.app"))
	_ = os.RemoveAll(filepath.Join(targetDir, "Claude (cageclaude).app"))
	_ = os.RemoveAll(filepath.Join(targetDir, "Claude (peaceclaude).app"))

	appDir, err := InstallDesktopLauncherAppTo(binPath, targetDir)
	if err != nil {
		return "", err
	}
	// Register with LaunchServices so Spotlight & Applications show PeaceClaude immediately
	_ = exec.Command("touch", appDir).Run()
	_ = exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister", "-f", appDir).Run()
	return appDir, nil
}

func buildNativeMacOSAppBinary(outBin string) bool {
	swiftc, err := exec.LookPath("swiftc")
	if err != nil {
		return false
	}
	tmpDir, err := os.MkdirTemp("", "peaceclaude-swift-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(tmpDir)
	srcPath := filepath.Join(tmpDir, "main.swift")
	if err := os.WriteFile(srcPath, []byte(macOSAppSwiftSrc), 0o600); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, swiftc, "-O", "-framework", "Cocoa", "-framework", "WebKit", srcPath, "-o", outBin)
	return cmd.Run() == nil
}

func generateAppIcon(icnsDst string) error {
	tmpDir, err := os.MkdirTemp("", "peaceclaude-icon-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	iconset := filepath.Join(tmpDir, "AppIcon.iconset")
	if err := os.MkdirAll(iconset, 0o755); err != nil {
		return err
	}

	sizes := []struct {
		name string
		size int
	}{
		{"icon_16x16.png", 16},
		{"icon_32x32.png", 32},
		{"icon_128x128.png", 128},
		{"icon_256x256.png", 256},
		{"icon_512x512.png", 512},
	}
	for _, s := range sizes {
		img := drawPeaceIcon(s.size)
		f, err := os.Create(filepath.Join(iconset, s.name))
		if err != nil {
			return err
		}
		_ = png.Encode(f, img)
		_ = f.Close()
	}
	if err := exec.Command("iconutil", "-c", "icns", iconset, "-o", icnsDst).Run(); err != nil {
		return err
	}
	return nil
}

// drawPeaceIcon renders a high-contrast macOS squircle with a warm terracotta shield & dark obsidian backdrop.
func drawPeaceIcon(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fSize := float64(size)
	cx, cy := fSize/2, fSize/2
	r := fSize * 0.44
	corner := fSize * 0.22

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			// Rounded square SDF
			dx := math.Abs(fx-cx) - (r - corner)
			dy := math.Abs(fy-cy) - (r - corner)
			dist := math.Hypot(math.Max(dx, 0), math.Max(dy, 0)) + math.Min(math.Max(dx, dy), 0) - corner
			if dist > 0.8 {
				continue
			}
			alpha := uint8(255)
			if dist > -0.8 {
				alpha = uint8(255 * (0.8 - dist) / 1.6)
			}

			// Deep obsidian-emerald gradient background
			t := float64(y) / fSize
			bgR := uint8(18 + t*14)
			bgG := uint8(24 + t*20)
			bgB := uint8(28 + t*18)
			col := color.RGBA{bgR, bgG, bgB, alpha}

			// Subtle terracotta border glow
			if dist > -fSize*0.03 {
				col = color.RGBA{217, 119, 87, alpha}
			}

			// Draw central shield + keyhole silhouette
			nx := (fx - cx) / (fSize * 0.26)
			ny := (fy - cy) / (fSize * 0.28)
			inShield := false
			if ny >= -0.85 && ny <= 0.95 {
				halfW := 0.82
				if ny > 0.05 {
					halfW = 0.82 * (1.0 - math.Pow((ny-0.05)/0.92, 1.5))
				}
				if math.Abs(nx) <= halfW {
					inShield = true
				}
			}
			if inShield {
				// Terracotta Claude brand accent shield
				col = color.RGBA{224, 122, 85, alpha}
				// Inner dark shield cutout to form a crisp cage outline
				innerW := 0.56
				if ny > 0.05 {
					innerW = 0.56 * (1.0 - math.Pow((ny-0.05)/0.72, 1.5))
				}
				if ny >= -0.58 && ny <= 0.72 && math.Abs(nx) <= innerW {
					col = color.RGBA{20, 26, 30, alpha}
				}
				// Emerald static-IP core dot & vertical lock bar
				if math.Hypot(nx, ny+0.12) <= 0.20 {
					col = color.RGBA{52, 211, 153, alpha}
				} else if math.Abs(nx) <= 0.09 && ny >= 0.05 && ny <= 0.42 {
					col = color.RGBA{52, 211, 153, alpha}
				}
			}
			img.SetRGBA(x, y, col)
		}
	}
	return img
}
