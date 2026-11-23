package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
)

const LaunchAgentLabel = "com.peaceclaude.daemon"

type ServiceStatus struct {
	Running           bool   `json:"running"`
	PID               int    `json:"pid,omitempty"`
	Platform          string `json:"platform,omitempty"`
	LaunchAgentPath   string `json:"launch_agent_path"`
	LaunchAgentSet    bool   `json:"launch_agent_installed"`
	LaunchAgentLoaded bool   `json:"launch_agent_loaded"`
	LaunchAgentValid  bool   `json:"launch_agent_valid"`
	LaunchAgentBinary string `json:"launch_agent_binary,omitempty"`
	AutoStartEnabled  bool   `json:"auto_start_enabled"`
	ControlURL        string `json:"control_url"`
	HTTPProxyURL      string `json:"http_proxy_url"`
	SOCKS5ProxyURL    string `json:"socks5_proxy_url"`
	Error             string `json:"error,omitempty"`
}

type EnsureLaunchAgentResult struct {
	Action          string `json:"action"` // skipped | installed | repaired | restarted | disabled | disabled_in_sandbox | skipped_ephemeral_binary
	Installed       bool   `json:"installed"`
	Loaded          bool   `json:"loaded"`
	Running         bool   `json:"running"`
	PID             int    `json:"pid,omitempty"`
	LaunchAgentPath string `json:"launch_agent_path"`
	BinaryPath      string `json:"binary_path,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type launchAgentOps struct {
	canManage   bool
	plistPath   string
	readFile    func(string) ([]byte, error)
	isExec      func(string) bool
	isLoaded    func() bool
	isHealthy   func() (bool, int)
	resolveBin  func(string) (string, bool)
	installFile func(string) (string, error)
	stopBare    func()
	startLaunch func(plist string) error
	waitHealthy func() (bool, int)
}

func LaunchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/LaunchAgents", LaunchAgentLabel+".plist")
}

func canManageHostLaunchAgent() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	if os.Getenv("PEACECLAUDE_NO_LAUNCHD") == "1" {
		return false
	}
	if strings.TrimSpace(os.Getenv("PEACECLAUDE_HOME")) != "" && os.Getenv("PEACECLAUDE_FORCE_LAUNCHD") != "1" {
		return false
	}
	return true
}

// CanManageHostLaunchAgent reports whether this process is allowed to inspect
// and mutate host OS autostart registration (always false in tests/sandboxes).
func CanManageHostLaunchAgent() bool {
	if runtime.GOOS == "darwin" {
		return canManageHostLaunchAgent()
	}
	return canManageHostAutostartAnyOS()
}

func xmlEscapePlist(s string) string {
	repl := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return repl.Replace(s)
}

// ParseLaunchAgentProgram extracts the executable path (first element) from
// <key>ProgramArguments</key><array><string>...</string>...</array>.
func ParseLaunchAgentProgram(plistContent string) string {
	keyIdx := strings.Index(plistContent, "<key>ProgramArguments</key>")
	if keyIdx < 0 {
		return ""
	}
	rest := plistContent[keyIdx:]
	arrStart := strings.Index(rest, "<array>")
	arrEnd := strings.Index(rest, "</array>")
	if arrStart < 0 || arrEnd < 0 || arrEnd <= arrStart {
		return ""
	}
	arr := rest[arrStart:arrEnd]
	sStart := strings.Index(arr, "<string>")
	if sStart < 0 {
		return ""
	}
	after := arr[sStart+len("<string>"):]
	sEnd := strings.Index(after, "</string>")
	if sEnd < 0 {
		return ""
	}
	prog := strings.TrimSpace(after[:sEnd])
	prog = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(prog)
	return prog
}

func isRegularExecutable(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode()&0o111 != 0
}

// ValidateLaunchAgentPlist verifies structural validity and checks that the referenced binary exists and is executable.
func ValidateLaunchAgentPlist(plistContent string, isExec func(string) bool) (string, bool) {
	if isExec == nil {
		isExec = isRegularExecutable
	}
	bin := ParseLaunchAgentProgram(plistContent)
	if bin == "" || !isExec(bin) {
		return bin, false
	}
	for _, req := range []string{"<string>service</string>", "<string>run</string>", "<key>RunAtLoad</key>", "<key>KeepAlive</key>"} {
		if !strings.Contains(plistContent, req) {
			return bin, false
		}
	}
	return bin, true
}

func isLaunchAgentLoaded(uid string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "launchctl", "print", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run() == nil
}

func resolveLaunchAgentBinary(binPath string) (string, bool) {
	isEphemeral := func(p string) bool {
		clean := filepath.Clean(p)
		return strings.HasPrefix(clean, "/Volumes/") || strings.Contains(clean, "/AppTranslocation/")
	}
	clean := filepath.Clean(strings.TrimSpace(binPath))
	if clean != "" && !isEphemeral(clean) && isRegularExecutable(clean) {
		return clean, false
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local/bin/peaceclaude"),
		"/opt/homebrew/bin/peaceclaude",
		"/usr/local/bin/peaceclaude",
		"/Applications/PeaceClaude.app/Contents/Resources/bin/peaceclaude",
		filepath.Join(home, "Applications/PeaceClaude.app/Contents/Resources/bin/peaceclaude"),
	}
	for _, cand := range candidates {
		if !isEphemeral(cand) && isRegularExecutable(cand) {
			return cand, false
		}
	}
	if clean != "" && isEphemeral(clean) {
		return "", true
	}
	return clean, false
}

func RenderLaunchAgentPlist(binPath string) string {
	home, _ := os.UserHomeDir()
	logPath := config.DaemonLogPath()
	errPath := filepath.Join(config.LogsDir(), "daemon.err.log")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>service</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>%s</string>
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, LaunchAgentLabel, xmlEscapePlist(binPath), xmlEscapePlist(home), xmlEscapePlist(logPath), xmlEscapePlist(errPath))
}

func DaemonHealth(cfg *config.Config) (bool, int) {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(cfg.ControlURL() + "/api/health")
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, 0
	}
	var out struct {
		OK  bool `json:"ok"`
		PID int  `json:"pid"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out); err != nil {
		return false, 0
	}
	return out.OK, out.PID
}

// InspectLaunchAgentStatus inspects LaunchAgent / systemd / Windows Run autostart state
// without making any self-referential HTTP calls, so /api/status inside daemon can call it safely.
func InspectLaunchAgentStatus(cfg *config.Config, running bool, pid int) ServiceStatus {
	if runtime.GOOS != "darwin" {
		return inspectCrossPlatformAutostart(cfg, running, pid)
	}
	plist := LaunchAgentPath()
	autoEnabled := cfg.ShouldAutoStartLaunchAgent()
	var (
		installed bool
		valid     bool
		loaded    bool
		binPath   string
	)
	if canManageHostLaunchAgent() {
		if raw, err := os.ReadFile(plist); err == nil {
			installed = true
			binPath, valid = ValidateLaunchAgentPlist(string(raw), isRegularExecutable)
		}
		loaded = isLaunchAgentLoaded(strconv.Itoa(os.Getuid()))
	}
	return ServiceStatus{
		Running:           running,
		PID:               pid,
		Platform:          runtime.GOOS,
		LaunchAgentPath:   plist,
		LaunchAgentSet:    installed,
		LaunchAgentLoaded: loaded,
		LaunchAgentValid:  valid,
		LaunchAgentBinary: binPath,
		AutoStartEnabled:  autoEnabled,
		ControlURL:        cfg.ControlURL(),
		HTTPProxyURL:      cfg.HTTPProxyURL(),
		SOCKS5ProxyURL:    cfg.SOCKS5ProxyURL(),
	}
}

func GetServiceStatus(cfg *config.Config) ServiceStatus {
	running, pid := DaemonHealth(cfg)
	return InspectLaunchAgentStatus(cfg, running, pid)
}

func InstallLaunchAgent(binPath string) (string, error) {
	if !canManageHostLaunchAgent() {
		return LaunchAgentPath(), nil
	}
	return InstallLaunchAgentPlistOnly(binPath)
}

// InstallLaunchAgentPlistOnly writes the host OS autostart definition cleanly without restarting the active daemon.
func InstallLaunchAgentPlistOnly(binPath string) (string, error) {
	if err := config.EnsureDirs(); err != nil {
		return "", err
	}
	if runtime.GOOS == "linux" {
		if !canManageHostAutostartAnyOS() {
			return LinuxSystemdUnitPath(), nil
		}
		unitPath := LinuxSystemdUnitPath()
		if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(unitPath, []byte(RenderSystemdUserUnit(binPath)), 0o644); err != nil {
			return "", err
		}
		if _, err := exec.LookPath("systemctl"); err == nil {
			_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
			_ = exec.Command("systemctl", "--user", "enable", LinuxSystemdUnitName).Run()
		}
		return unitPath, nil
	}
	if runtime.GOOS == "windows" {
		regPath := WindowsRunRegKey + `\` + WindowsRunValueName
		if !canManageHostAutostartAnyOS() {
			return regPath, nil
		}
		_ = exec.Command("reg", "add", WindowsRunRegKey, "/v", WindowsRunValueName, "/t", "REG_SZ", "/d", RenderWindowsRunCommand(binPath), "/f").Run()
		return regPath, nil
	}
	resolved, ephemeral := resolveLaunchAgentBinary(binPath)
	if !ephemeral && resolved != "" {
		binPath = resolved
	}
	plist := LaunchAgentPath()
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(plist, []byte(RenderLaunchAgentPlist(binPath)), 0o644); err != nil {
		return "", err
	}
	return plist, nil
}

// DisableLaunchAgentAutostart removes the host autostart entry so future logins do not auto-start,
// without killing the currently running daemon serving the Web UI HTTP request.
func DisableLaunchAgentAutostart() error {
	if runtime.GOOS != "darwin" {
		return disableCrossPlatformAutostart()
	}
	if !canManageHostLaunchAgent() {
		return nil
	}
	plist := LaunchAgentPath()
	if err := os.Remove(plist); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func stopBareDaemonForHandoff(cfg *config.Config) {
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, cfg.ControlURL()+"/api/shutdown", nil)
	_, _ = client.Do(req)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := DaemonHealth(cfg); !ok {
			_ = os.Remove(config.DaemonPIDPath())
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	if raw, err := os.ReadFile(config.DaemonPIDPath()); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 1 {
			_ = signalTermPID(pid)
			time.Sleep(300 * time.Millisecond)
		}
	}
	_ = os.Remove(config.DaemonPIDPath())
}

func defaultLaunchAgentOps(cfg *config.Config) launchAgentOps {
	uid := strconv.Itoa(os.Getuid())
	return launchAgentOps{
		canManage:  canManageHostLaunchAgent(),
		plistPath:  LaunchAgentPath(),
		readFile:   os.ReadFile,
		isExec:     isRegularExecutable,
		isLoaded:   func() bool { return isLaunchAgentLoaded(uid) },
		isHealthy:  func() (bool, int) { return DaemonHealth(cfg) },
		resolveBin: resolveLaunchAgentBinary,
		installFile: func(bin string) (string, error) {
			return InstallLaunchAgentPlistOnly(bin)
		},
		stopBare: func() { stopBareDaemonForHandoff(cfg) },
		startLaunch: func(plist string) error {
			_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run()
			if err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%s", uid), plist).Run(); err != nil {
				return err
			}
			_ = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run()
			if !waitForDaemon(cfg, 8*time.Second) {
				// Prevent KeepAlive LaunchAgent from fighting with any detached fallback daemon.
				_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run()
				return fmt.Errorf("launchd started agent but control API %s did not respond", cfg.ControlURL())
			}
			return nil
		},
		waitHealthy: func() (bool, int) {
			return DaemonHealth(cfg)
		},
	}
}

func ensureLaunchAgentWithOps(cfg *config.Config, binPath string, respectConfig bool, ops launchAgentOps) (EnsureLaunchAgentResult, error) {
	res := EnsureLaunchAgentResult{
		LaunchAgentPath: ops.plistPath,
	}
	if !ops.canManage {
		running, pid := ops.isHealthy()
		res.Action = "disabled_in_sandbox"
		res.Running = running
		res.PID = pid
		res.Reason = "sandbox or non-macOS environment; host LaunchAgent untouched"
		return res, nil
	}
	if respectConfig && !cfg.ShouldAutoStartLaunchAgent() {
		running, pid := ops.isHealthy()
		res.Action = "disabled"
		res.Running = running
		res.PID = pid
		res.Reason = "auto_start_launch_agent=false"
		return res, nil
	}

	raw, err := ops.readFile(ops.plistPath)
	plistExists := err == nil
	var plistBin string
	var plistValid bool
	if plistExists {
		plistBin, plistValid = ValidateLaunchAgentPlist(string(raw), ops.isExec)
	}
	loaded := ops.isLoaded()
	running, pid := ops.isHealthy()

	// Fast idempotent skip path: valid plist exists, loaded in launchd, and daemon healthy.
	if plistExists && plistValid && loaded && running {
		res.Action = "skipped"
		res.Installed = true
		res.Loaded = true
		res.Running = true
		res.PID = pid
		res.BinaryPath = plistBin
		res.Reason = "LaunchAgent 已注册且守护进程运行中"
		return res, nil
	}

	var targetBin string
	if plistValid && plistBin != "" {
		// Preserve existing valid binary path to avoid thrashing between ~/.local/bin and .app bundle.
		targetBin = plistBin
	} else {
		resolved, ephemeral := ops.resolveBin(binPath)
		if ephemeral || resolved == "" {
			res.Action = "skipped_ephemeral_binary"
			res.Installed = plistExists
			res.Loaded = loaded
			res.Running = running
			res.PID = pid
			res.Reason = "running from read-only volume or AppTranslocation without installed stable binary"
			return res, nil
		}
		targetBin = resolved
	}
	res.BinaryPath = targetBin

	action := "restarted"
	if !plistExists {
		action = "installed"
	} else if !plistValid {
		action = "repaired"
	}

	needWrite := !plistExists || !plistValid
	plistPath := ops.plistPath
	if needWrite {
		p, werr := ops.installFile(targetBin)
		if werr != nil {
			return res, werr
		}
		if p != "" {
			plistPath = p
		}
	}
	res.LaunchAgentPath = plistPath
	res.Installed = true

	// If an un-managed daemon is currently holding the proxy/control ports, hand off cleanly
	// before calling launchctl bootstrap so the LaunchAgent never hits EADDRINUSE.
	if running && !loaded && ops.stopBare != nil {
		ops.stopBare()
	}

	if err := ops.startLaunch(plistPath); err != nil {
		res.Action = action
		res.Loaded = ops.isLoaded()
		r2, p2 := ops.waitHealthy()
		res.Running = r2
		res.PID = p2
		return res, err
	}

	r2, p2 := ops.waitHealthy()
	res.Action = action
	res.Loaded = true
	res.Running = r2
	res.PID = p2
	return res, nil
}

// EnsureLaunchAgent idempotently checks host autostart registration & daemon health across
// macOS (LaunchAgent), Linux (systemd --user), and Windows (HKCU Run).
func EnsureLaunchAgent(cfg *config.Config, binPath string, respectConfig bool) (EnsureLaunchAgentResult, error) {
	if runtime.GOOS != "darwin" {
		return ensureCrossPlatformAutostart(cfg, binPath, respectConfig)
	}
	return ensureLaunchAgentWithOps(cfg, binPath, respectConfig, defaultLaunchAgentOps(cfg))
}

func startDetachedDaemon(cfg *config.Config, binPath string) error {
	logFile, err := os.OpenFile(config.DaemonLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(binPath, "service", "run")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	setDetachedProcessAttr(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start daemon: %w", err)
	}
	_ = logFile.Close()
	_ = os.WriteFile(config.DaemonPIDPath(), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600)

	if !waitForDaemon(cfg, 8*time.Second) {
		return fmt.Errorf("daemon started (pid=%d) but control API %s did not become ready; check %s",
			cmd.Process.Pid, cfg.ControlURL(), config.DaemonLogPath())
	}
	return nil
}

func StartService(cfg *config.Config, binPath string, useLaunchd bool) error {
	if useLaunchd && CanManageHostLaunchAgent() {
		if err := config.EnsureDirs(); err != nil {
			return err
		}
		res, err := EnsureLaunchAgent(cfg, binPath, false)
		if err == nil && res.Running {
			return nil
		}
		if ok, _ := DaemonHealth(cfg); ok {
			return nil
		}
	} else {
		if ok, _ := DaemonHealth(cfg); ok {
			return nil
		}
		if err := config.EnsureDirs(); err != nil {
			return err
		}
	}

	// Fallback: spawn detached daemon process directly (works cleanly in tests, sandboxes & non-launchd sessions).
	return startDetachedDaemon(cfg, binPath)
}

func StopService(cfg *config.Config) error {
	if canManageHostLaunchAgent() {
		uid := strconv.Itoa(os.Getuid())
		plist := LaunchAgentPath()
		if _, err := os.Stat(plist); err == nil {
			_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run()
		}
	}

	// Ask daemon to shutdown gracefully if reachable.
	client := &http.Client{Timeout: 2 * time.Second}
	req, _ := http.NewRequest(http.MethodPost, cfg.ControlURL()+"/api/shutdown", nil)
	_, _ = client.Do(req)

	// Wait briefly, then fallback to PID file signal.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := DaemonHealth(cfg); !ok {
			_ = os.Remove(config.DaemonPIDPath())
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	if raw, err := os.ReadFile(config.DaemonPIDPath()); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 1 {
			_ = signalTermPID(pid)
		}
	}
	_ = os.Remove(config.DaemonPIDPath())
	return nil
}

func UninstallLaunchAgent(cfg *config.Config) error {
	_ = StopService(cfg)
	if !canManageHostLaunchAgent() {
		return nil
	}
	plist := LaunchAgentPath()
	if err := os.Remove(plist); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func waitForDaemon(cfg *config.Config, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok, _ := DaemonHealth(cfg); ok {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

// FetchGateSnapshot queries the running daemon's Egress Gate state.
func FetchGateSnapshot(ctx context.Context, cfg *config.Config, forceCheck bool) (egress.GateSnapshot, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	endpoint := cfg.ControlURL() + "/api/status"
	method := http.MethodGet
	if forceCheck {
		endpoint = cfg.ControlURL() + "/api/check"
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return egress.GateSnapshot{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return egress.GateSnapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return egress.GateSnapshot{}, fmt.Errorf("daemon HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if forceCheck {
		var res struct {
			Gate egress.GateSnapshot `json:"gate"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			return egress.GateSnapshot{}, err
		}
		return res.Gate, nil
	}
	var status struct {
		Gate egress.GateSnapshot `json:"gate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return egress.GateSnapshot{}, err
	}
	return status.Gate, nil
}

// Preflight ensures the daemon is running and the Fail-Closed Egress Gate is OPEN before launching any Claude client.
// When auto_start_launch_agent is enabled on the host, it also idempotently registers/reuses the macOS LaunchAgent.
func Preflight(ctx context.Context, cfg *config.Config, selfBin string, skipGate bool) (egress.GateSnapshot, error) {
	useLaunchd := cfg != nil && cfg.ShouldAutoStartLaunchAgent() && canManageHostLaunchAgent()
	if ok, _ := DaemonHealth(cfg); !ok || useLaunchd {
		if err := StartService(cfg, selfBin, useLaunchd); err != nil {
			return egress.GateSnapshot{}, fmt.Errorf("无法启动 PeaceClaude 本地安全网关: %w", err)
		}
	}
	snap, err := FetchGateSnapshot(ctx, cfg, false)
	if err != nil {
		return egress.GateSnapshot{}, err
	}
	if !snap.Open && !snap.ManualLock {
		// Retry once with an immediate probe before refusing launch.
		if refreshed, err2 := FetchGateSnapshot(ctx, cfg, true); err2 == nil {
			snap = refreshed
		}
	}
	if !skipGate && cfg.Egress.FailClosed && !snap.Open {
		reason := snap.LockReason
		if reason == "" {
			reason = snap.LastProbe.FailureReason
		}
		return snap, fmt.Errorf("Egress Gate 已熔断（Fail-Closed 拒绝启动，防止直连泄漏）: %s", reason)
	}
	return snap, nil
}
