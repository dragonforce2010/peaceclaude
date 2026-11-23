package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"peaceclaude/internal/config"
)

const (
	LinuxSystemdUnitName = "peaceclaude.service"
	WindowsRunRegKey     = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	WindowsRunValueName  = "PeaceClaude"
)

// LinuxSystemdUnitPath returns the user-level systemd unit file path (~/.config/systemd/user/peaceclaude.service).
func LinuxSystemdUnitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", LinuxSystemdUnitName)
}

// RenderSystemdUserUnit builds the systemd --user unit content for running the PeaceClaude daemon cleanly.
func RenderSystemdUserUnit(binPath string) string {
	home, _ := os.UserHomeDir()
	return fmt.Sprintf(`[Unit]
Description=PeaceClaude Static-IP Fail-Closed Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%q service run
Restart=always
RestartSec=3
Environment=HOME=%q
Environment=PATH=/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
`, binPath, home)
}

// RenderWindowsRunCommand builds the HKCU Run registry value launching PeaceClaude daemon cleanly.
func RenderWindowsRunCommand(binPath string) string {
	return fmt.Sprintf(`"%s" service run`, strings.TrimSpace(binPath))
}

func canManageHostAutostartAnyOS() bool {
	if os.Getenv("PEACECLAUDE_NO_LAUNCHD") == "1" {
		return false
	}
	if strings.TrimSpace(os.Getenv("PEACECLAUDE_HOME")) != "" && os.Getenv("PEACECLAUDE_FORCE_LAUNCHD") != "1" {
		return false
	}
	return true
}

func linuxSystemdUnitValid(content string, isExec func(string) bool) (string, bool) {
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, "ExecStart="))
		val = strings.TrimSuffix(val, " service run")
		val = strings.Trim(val, `"`)
		if val != "" && (isExec == nil || isExec(val)) && strings.Contains(line, "service run") {
			return val, true
		}
	}
	return "", false
}

func inspectCrossPlatformAutostart(cfg *config.Config, running bool, pid int) ServiceStatus {
	autoEnabled := cfg.ShouldAutoStartLaunchAgent()
	st := ServiceStatus{
		Running:          running,
		PID:              pid,
		Platform:         runtime.GOOS,
		AutoStartEnabled: autoEnabled,
		ControlURL:       cfg.ControlURL(),
		HTTPProxyURL:     cfg.HTTPProxyURL(),
		SOCKS5ProxyURL:   cfg.SOCKS5ProxyURL(),
	}

	switch runtime.GOOS {
	case "linux":
		unitPath := LinuxSystemdUnitPath()
		st.LaunchAgentPath = unitPath
		if canManageHostAutostartAnyOS() {
			if raw, err := os.ReadFile(unitPath); err == nil {
				st.LaunchAgentSet = true
				bin, valid := linuxSystemdUnitValid(string(raw), isRegularExecutable)
				st.LaunchAgentBinary = bin
				st.LaunchAgentValid = valid
			}
			if _, err := exec.LookPath("systemctl"); err == nil {
				if exec.Command("systemctl", "--user", "is-enabled", LinuxSystemdUnitName).Run() == nil {
					st.LaunchAgentLoaded = true
				}
			}
		}
	case "windows":
		st.LaunchAgentPath = WindowsRunRegKey + `\` + WindowsRunValueName
		if canManageHostAutostartAnyOS() {
			if out, err := exec.Command("reg", "query", WindowsRunRegKey, "/v", WindowsRunValueName).Output(); err == nil {
				for _, ln := range strings.Split(string(out), "\n") {
					if strings.Contains(ln, WindowsRunValueName) && strings.Contains(ln, "service run") {
						st.LaunchAgentSet = true
						st.LaunchAgentLoaded = true
						st.LaunchAgentValid = true
						break
					}
				}
			}
		}
	}
	return st
}

func ensureCrossPlatformAutostart(cfg *config.Config, binPath string, respectConfig bool) (EnsureLaunchAgentResult, error) {
	if !canManageHostAutostartAnyOS() {
		running, pid := DaemonHealth(cfg)
		p := LinuxSystemdUnitPath()
		if runtime.GOOS == "windows" {
			p = WindowsRunRegKey + `\` + WindowsRunValueName
		}
		return EnsureLaunchAgentResult{
			Action:          "disabled_in_sandbox",
			Running:         running,
			PID:             pid,
			LaunchAgentPath: p,
			Reason:          "sandbox environment; host autostart untouched",
		}, nil
	}
	if respectConfig && !cfg.ShouldAutoStartLaunchAgent() {
		running, pid := DaemonHealth(cfg)
		return EnsureLaunchAgentResult{
			Action:  "disabled",
			Running: running,
			PID:     pid,
			Reason:  "auto_start_launch_agent=false",
		}, nil
	}

	switch runtime.GOOS {
	case "linux":
		unitPath := LinuxSystemdUnitPath()
		res := EnsureLaunchAgentResult{LaunchAgentPath: unitPath, BinaryPath: binPath}
		raw, err := os.ReadFile(unitPath)
		existed := err == nil
		bin, valid := "", false
		if existed {
			bin, valid = linuxSystemdUnitValid(string(raw), isRegularExecutable)
		}
		hasSystemctl := false
		if _, lerr := exec.LookPath("systemctl"); lerr == nil {
			hasSystemctl = true
		}
		enabled := hasSystemctl && exec.Command("systemctl", "--user", "is-enabled", LinuxSystemdUnitName).Run() == nil
		running, pid := DaemonHealth(cfg)
		if existed && valid && enabled && running {
			res.Action = "skipped"
			res.Installed = true
			res.Loaded = true
			res.Running = true
			res.PID = pid
			res.BinaryPath = bin
			res.Reason = "systemd user unit active and healthy"
			return res, nil
		}
		if !existed || !valid {
			if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
				return res, err
			}
			if err := os.WriteFile(unitPath, []byte(RenderSystemdUserUnit(binPath)), 0o644); err != nil {
				return res, err
			}
		}
		res.Installed = true
		if hasSystemctl {
			_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
			_ = exec.Command("systemctl", "--user", "enable", "--now", LinuxSystemdUnitName).Run()
		}
		if !waitForDaemon(cfg, 6*time.Second) {
			_ = startDetachedDaemon(cfg, binPath)
		}
		r2, p2 := DaemonHealth(cfg)
		res.Action = "installed"
		if existed && !valid {
			res.Action = "repaired"
		} else if existed && valid {
			res.Action = "restarted"
		}
		res.Loaded = hasSystemctl
		res.Running = r2
		res.PID = p2
		return res, nil

	case "windows":
		regPath := WindowsRunRegKey + `\` + WindowsRunValueName
		res := EnsureLaunchAgentResult{LaunchAgentPath: regPath, BinaryPath: binPath}
		running, pid := DaemonHealth(cfg)
		st := inspectCrossPlatformAutostart(cfg, running, pid)
		if st.LaunchAgentSet && st.LaunchAgentValid && running {
			res.Action = "skipped"
			res.Installed = true
			res.Loaded = true
			res.Running = true
			res.PID = pid
			res.Reason = "Windows Run key present and daemon healthy"
			return res, nil
		}
		if !st.LaunchAgentSet || !st.LaunchAgentValid {
			cmdVal := RenderWindowsRunCommand(binPath)
			_ = exec.Command("reg", "add", WindowsRunRegKey, "/v", WindowsRunValueName, "/t", "REG_SZ", "/d", cmdVal, "/f").Run()
		}
		res.Installed = true
		if !running {
			if err := startDetachedDaemon(cfg, binPath); err != nil {
				return res, err
			}
		}
		r2, p2 := DaemonHealth(cfg)
		res.Action = "installed"
		if st.LaunchAgentSet {
			res.Action = "restarted"
		}
		res.Loaded = true
		res.Running = r2
		res.PID = p2
		return res, nil
	}

	return EnsureLaunchAgentResult{Action: "unsupported_os", Reason: runtime.GOOS}, nil
}

func disableCrossPlatformAutostart() error {
	if !canManageHostAutostartAnyOS() {
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		if _, err := exec.LookPath("systemctl"); err == nil {
			_ = exec.Command("systemctl", "--user", "disable", LinuxSystemdUnitName).Run()
		}
		if err := os.Remove(LinuxSystemdUnitPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
	case "windows":
		_ = exec.Command("reg", "delete", WindowsRunRegKey, "/v", WindowsRunValueName, "/f").Run()
	}
	return nil
}
