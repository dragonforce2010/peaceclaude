package host

import (
	"fmt"
	"os/exec"
	"strings"

	"peaceclaude/internal/config"
)

type BrowserPACStatus struct {
	PACURL          string   `json:"pac_url"`
	SystemPACEnable bool     `json:"system_pac_enabled"`
	CurrentPACURL   string   `json:"current_pac_url,omitempty"`
	ActiveServices  []string `json:"active_services,omitempty"`
	MatchesPeace    bool     `json:"matches_peaceclaude"`
	Note            string   `json:"note"`
}

func PeacePACURL(cfg *config.Config) string {
	return cfg.ControlURL() + "/peaceclaude.pac"
}

func InspectBrowserPAC(cfg *config.Config) BrowserPACStatus {
	pacURL := PeacePACURL(cfg)
	st := BrowserPACStatus{
		PACURL: pacURL,
	}

	if out, err := exec.Command("scutil", "--proxy").Output(); err == nil {
		for _, ln := range strings.Split(string(out), "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(ln), ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			switch k {
			case "ProxyAutoConfigEnable":
				st.SystemPACEnable = (v == "1")
			case "ProxyAutoConfigURLString":
				st.CurrentPACURL = v
			}
		}
	}
	st.ActiveServices = listActiveNetworkServices()
	st.MatchesPeace = st.SystemPACEnable && strings.HasPrefix(st.CurrentPACURL, pacURL)
	if st.MatchesPeace {
		if cfg != nil && cfg.Shield.ProxyAuthDomains {
			st.Note = "系统 PAC 已指向 PeaceClaude：Claude 与 Google/Apple 登录辅助域将走静态 IP 网关，其余网站保持本机正常网络直连。"
		} else {
			st.Note = "系统 PAC 已指向 PeaceClaude（严格仅 Claude 模式）：仅 Claude/Anthropic 域名与官方 IP 走静态 IP 网关，Google 登录与一切其余网站均走本机正常直连。"
		}
	} else {
		st.Note = "普通浏览器目前未绑定 PeaceClaude PAC；默认严格仅将 Claude 相关域名路由至静态 IP，可在「网络与策略」或此处一键恢复开启。"
	}
	return st
}

func listActiveNetworkServices() []string {
	out, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return nil
	}
	var services []string
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		// Prefer standard desktop/laptop network interfaces; skip VPN virtual tunnels (e.g. Feilian/utun).
		lower := strings.ToLower(line)
		if strings.Contains(lower, "vpn") || strings.Contains(lower, "tailscale") || strings.Contains(lower, "clash") {
			continue
		}
		if info, err := exec.Command("networksetup", "-getinfo", line).Output(); err == nil {
			txt := string(info)
			if strings.Contains(txt, "IP address: ") && !strings.Contains(txt, "IP address: none") {
				services = append(services, line)
			}
		}
	}
	if len(services) == 0 {
		// Fallback to Wi-Fi if present even when offline.
		for _, raw := range strings.Split(string(out), "\n") {
			line := strings.TrimSpace(raw)
			if strings.EqualFold(line, "Wi-Fi") {
				services = append(services, line)
			}
		}
	}
	return services
}

func SetSystemBrowserPAC(cfg *config.Config, enabled bool, dryRun bool) (BrowserPACStatus, []string, error) {
	services := listActiveNetworkServices()
	if len(services) == 0 {
		return InspectBrowserPAC(cfg), nil, fmt.Errorf("未检测到活跃的 macOS 网络服务（如 Wi-Fi / 以太网）")
	}
	pacURL := PeacePACURL(cfg)
	var cmds []string
	for _, svc := range services {
		if enabled {
			cmds = append(cmds, fmt.Sprintf("networksetup -setautoproxyurl %q %q", svc, pacURL))
			cmds = append(cmds, fmt.Sprintf("networksetup -setautoproxystate %q on", svc))
			if !dryRun {
				if out, err := exec.Command("networksetup", "-setautoproxyurl", svc, pacURL).CombinedOutput(); err != nil {
					return InspectBrowserPAC(cfg), cmds, fmt.Errorf("setautoproxyurl %s: %v (%s)", svc, err, strings.TrimSpace(string(out)))
				}
				if out, err := exec.Command("networksetup", "-setautoproxystate", svc, "on").CombinedOutput(); err != nil {
					return InspectBrowserPAC(cfg), cmds, fmt.Errorf("setautoproxystate %s on: %v (%s)", svc, err, strings.TrimSpace(string(out)))
				}
			}
		} else {
			cmds = append(cmds, fmt.Sprintf("networksetup -setautoproxystate %q off", svc))
			if !dryRun {
				if out, err := exec.Command("networksetup", "-setautoproxystate", svc, "off").CombinedOutput(); err != nil {
					return InspectBrowserPAC(cfg), cmds, fmt.Errorf("setautoproxystate %s off: %v (%s)", svc, err, strings.TrimSpace(string(out)))
				}
			}
		}
	}
	return InspectBrowserPAC(cfg), cmds, nil
}
