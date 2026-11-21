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
	"strconv"
	"strings"
	"syscall"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
)

const LaunchAgentLabel = "com.peaceclaude.daemon"

type ServiceStatus struct {
	Running         bool   `json:"running"`
	PID             int    `json:"pid,omitempty"`
	LaunchAgentPath string `json:"launch_agent_path"`
	LaunchAgentSet  bool   `json:"launch_agent_installed"`
	ControlURL      string `json:"control_url"`
	HTTPProxyURL    string `json:"http_proxy_url"`
	SOCKS5ProxyURL  string `json:"socks5_proxy_url"`
	Error           string `json:"error,omitempty"`
}

func LaunchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library/LaunchAgents", LaunchAgentLabel+".plist")
}

func RenderLaunchAgentPlist(binPath string) string {
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
    <key>PATH</key>
    <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
  </dict>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, LaunchAgentLabel, binPath, logPath, errPath)
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

func GetServiceStatus(cfg *config.Config) ServiceStatus {
	running, pid := DaemonHealth(cfg)
	plist := LaunchAgentPath()
	_, err := os.Stat(plist)
	return ServiceStatus{
		Running:         running,
		PID:             pid,
		LaunchAgentPath: plist,
		LaunchAgentSet:  err == nil,
		ControlURL:      cfg.ControlURL(),
		HTTPProxyURL:    cfg.HTTPProxyURL(),
		SOCKS5ProxyURL:  cfg.SOCKS5ProxyURL(),
	}
}

func InstallLaunchAgent(binPath string) (string, error) {
	if err := config.EnsureDirs(); err != nil {
		return "", err
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

func StartService(cfg *config.Config, binPath string, useLaunchd bool) error {
	if ok, _ := DaemonHealth(cfg); ok {
		return nil
	}
	if err := config.EnsureDirs(); err != nil {
		return err
	}

	if useLaunchd {
		plist, err := InstallLaunchAgent(binPath)
		if err == nil {
			uid := strconv.Itoa(os.Getuid())
			_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run()
			if err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%s", uid), plist).Run(); err == nil {
				_ = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run()
				if waitForDaemon(cfg, 8*time.Second) {
					return nil
				}
				// Prevent KeepAlive LaunchAgent from fighting with the detached fallback daemon.
				_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run()
			}
		}
	}

	// Fallback: spawn detached daemon process directly (works cleanly in tests & non-launchd sessions).
	logFile, err := os.OpenFile(config.DaemonLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(binPath, "service", "run")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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

func StopService(cfg *config.Config) error {
	uid := strconv.Itoa(os.Getuid())
	plist := LaunchAgentPath()
	if _, err := os.Stat(plist); err == nil {
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%s/%s", uid, LaunchAgentLabel)).Run()
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
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}
	_ = os.Remove(config.DaemonPIDPath())
	return nil
}

func UninstallLaunchAgent(cfg *config.Config) error {
	_ = StopService(cfg)
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
func Preflight(ctx context.Context, cfg *config.Config, selfBin string, skipGate bool) (egress.GateSnapshot, error) {
	if ok, _ := DaemonHealth(cfg); !ok {
		if err := StartService(cfg, selfBin, false); err != nil {
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
