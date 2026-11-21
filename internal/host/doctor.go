package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/policy"
	"peaceclaude/internal/profile"
	"peaceclaude/internal/upstream"
)

type CheckItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"` // pass | warn | fail
	Detail    string `json:"detail"`
	FixHint   string `json:"fix_hint,omitempty"`
	Fixable   bool   `json:"fixable,omitempty"`
	FixLabel  string `json:"fix_label,omitempty"`
	FixAction string `json:"fix_action,omitempty"`
	Weight    int    `json:"weight"`
	Critical  bool   `json:"critical"`
}

type DesktopProcAudit struct {
	PID          string   `json:"pid"`
	Isolated     bool     `json:"isolated"`
	FlagsPresent []string `json:"flags_present"`
	FlagsMissing []string `json:"flags_missing"`
	Command      string   `json:"command"`
	RawCommand   string   `json:"-"`
}

type DesktopInspectReport struct {
	CheckedAt     time.Time          `json:"checked_at"`
	RunningCount  int                `json:"running_count"`
	IsolatedCount int                `json:"isolated_count"`
	ExposedCount  int                `json:"exposed_count"`
	Processes     []DesktopProcAudit `json:"processes"`
	Summary       string             `json:"summary"`
}

type DoctorReport struct {
	GeneratedAt    time.Time            `json:"generated_at"`
	Score          int                  `json:"score"`
	Grade          string               `json:"grade"` // safe | caution | risk
	CriticalPassed int                  `json:"critical_passed"`
	CriticalTotal  int                  `json:"critical_total"`
	Checks         []CheckItem          `json:"checks"`
	Clash          ClashStatus          `json:"clash"`
	VMProxy        VMProxyStatus        `json:"vm_proxy"`
	DirectLeaks    []DirectSocketHit    `json:"direct_leaks"`
	DesktopAudit   DesktopInspectReport `json:"desktop_audit"`
	CLIBinary      string               `json:"cli_binary"`
	DesktopApp     string               `json:"desktop_app"`
	LauncherApp    string               `json:"launcher_app"`
	HostTimezone   string               `json:"host_timezone"`
	PeaceTimezone   string               `json:"peace_timezone"`
	PeaceLanguage   string               `json:"peace_language"`
}

// FindRealClaudeCLI locates the latest native Claude Code CLI binary, skipping broken symlinks/wrappers.
func FindRealClaudeCLI(cfg *config.Config) (string, string) {
	if cfg != nil && strings.TrimSpace(cfg.Binaries.ClaudeCLIPath) != "" {
		if fi, err := os.Stat(cfg.Binaries.ClaudeCLIPath); err == nil && !fi.IsDir() {
			return cfg.Binaries.ClaudeCLIPath, "configured"
		}
	}
	home, _ := os.UserHomeDir()
	versDir := filepath.Join(home, ".local/share/claude/versions")
	if entries, err := os.ReadDir(versDir); err == nil && len(entries) > 0 {
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			full := filepath.Join(versDir, e.Name())
			if fi, err := os.Stat(full); err == nil && fi.Mode()&0o111 != 0 && fi.Size() > 1024*1024 {
				names = append(names, e.Name())
			}
		}
		if len(names) > 0 {
			sort.Strings(names)
			return filepath.Join(versDir, names[len(names)-1]), "native-versions"
		}
	}
	if raw, err := os.ReadFile(filepath.Join(home, ".cac/real_claude")); err == nil {
		p := strings.TrimSpace(string(raw))
		if p != "" {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, "cac-real"
			}
		}
	}
	for _, cand := range []string{
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
		filepath.Join(home, ".local/bin/claude"),
	} {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return cand, "path"
		}
	}
	return "", "not-found"
}

func FindLauncherAppPath() string {
	for _, p := range []string{
		"/Applications/PeaceClaude.app",
		filepath.Join(os.Getenv("HOME"), "Applications/PeaceClaude.app"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func HostTimezoneName() string {
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if idx := strings.Index(target, "zoneinfo/"); idx >= 0 {
			return target[idx+len("zoneinfo/"):]
		}
	}
	name, _ := time.Now().Zone()
	return name
}

// InspectRunningDesktop audits all currently running Claude Desktop main processes to verify
// whether they were launched with PeaceClaude's isolation flags (--user-data-dir, --proxy-server, --disable-quic, --lang).
func InspectRunningDesktop(cfg *config.Config) DesktopInspectReport {
	rep := DesktopInspectReport{CheckedAt: time.Now().UTC()}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,command=").Output()
	if err != nil {
		rep.Summary = "无法枚举进程: " + err.Error()
		return rep
	}

	wantProxy := ""
	if cfg != nil {
		wantProxy = "--proxy-server=" + cfg.HTTPProxyURL()
	}
	peaceProfiles := config.ProfilesDir()

	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.Contains(line, "/Claude.app/Contents/MacOS/Claude") || strings.Contains(line, "Helper") {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) < 2 {
			continue
		}
		pid := strings.TrimSpace(fields[0])
		cmdline := strings.TrimSpace(fields[1])

		checks := []struct {
			name string
			ok   bool
		}{
			{"isolated-user-data-dir", strings.Contains(cmdline, "--user-data-dir="+peaceProfiles)},
			{"peace-proxy-server", wantProxy != "" && strings.Contains(cmdline, wantProxy)},
			{"disable-quic", strings.Contains(cmdline, "--disable-quic")},
			{"en-lang", strings.Contains(cmdline, "--lang=en")},
		}
		var present, missing []string
		for _, c := range checks {
			if c.ok {
				present = append(present, c.name)
			} else {
				missing = append(missing, c.name)
			}
		}
		isolated := len(missing) == 0
		rep.RunningCount++
		if isolated {
			rep.IsolatedCount++
		} else {
			rep.ExposedCount++
		}
		rawCmd := cmdline
		if len(cmdline) > 260 {
			cmdline = cmdline[:260] + "..."
		}
		rep.Processes = append(rep.Processes, DesktopProcAudit{
			PID:          pid,
			Isolated:     isolated,
			FlagsPresent: present,
			FlagsMissing: missing,
			Command:      cmdline,
			RawCommand:   rawCmd,
		})
	}

	switch {
	case rep.RunningCount == 0:
		rep.Summary = "当前未运行 Claude Desktop；请通过 PeaceClaude.app 或 `peaceclaude app` 启动隔离实例。"
	case rep.ExposedCount == 0:
		rep.Summary = fmt.Sprintf("全部 %d 个运行中的 Claude Desktop 实例均已正确注入 PeaceClaude 隔离标志。", rep.IsolatedCount)
	default:
		rep.Summary = fmt.Sprintf("发现 %d 个未隔离的原生 Claude.app 实例（共 %d 个运行中），建议关闭原生窗口改用 PeaceClaude.app！",
			rep.ExposedCount, rep.RunningCount)
	}
	return rep
}

func RunDoctor(ctx context.Context, cfg *config.Config, daemonRunning bool, gateSnap egress.GateSnapshot) DoctorReport {
	clash := InspectClash(cfg.Upstream.ClashSocket)
	vmProxy := InspectVMProxy(cfg)
	directLeaks := ScanDirectAnthropicSockets(cfg.Egress.ExpectedIP)
	desktopAudit := InspectRunningDesktop(cfg)
	cliBin, cliSource := FindRealClaudeCLI(cfg)
	launcherApp := FindLauncherAppPath()
	hostTZ := HostTimezoneName()

	rep := DoctorReport{
		GeneratedAt:  time.Now().UTC(),
		Clash:        clash,
		VMProxy:      vmProxy,
		DirectLeaks:  directLeaks,
		DesktopAudit: desktopAudit,
		CLIBinary:    cliBin,
		DesktopApp:   cfg.Binaries.ClaudeDesktopApp,
		LauncherApp:  launcherApp,
		HostTimezone: hostTZ,
		PeaceTimezone: gateSnap.EffectiveTimezone,
		PeaceLanguage: gateSnap.EffectiveLanguage,
	}

	var checks []CheckItem
	add := func(id, title, status, detail, fix string, weight int, critical bool) {
		item := CheckItem{
			ID: id, Title: title, Status: status, Detail: detail, FixHint: fix, Weight: weight, Critical: critical,
		}
		if status != "pass" {
			item.Fixable, item.FixLabel, item.FixAction = fixMetadataForCheck(id, status, detail)
		}
		checks = append(checks, item)
	}

	// =========================================================================
	// 11 Core Isolation & Network Security Checks
	// =========================================================================

	// 1. 环境外出站约束 (Outbound Constraint: pf + lsof + active direct IPv4 dial test)
	directBlocked, directErr := testDirectBlocked("160.79.104.10:443", 2*time.Second)
	if len(directLeaks) > 0 {
		var items []string
		for _, h := range directLeaks {
			items = append(items, fmt.Sprintf("%s(%s)->%s:%s", h.Process, h.PID, h.DestIP, h.DestPort))
		}
		add("outbound_constraint", "1. 环境外出站约束", "fail",
			"发现宿主机直连 Anthropic Socket: "+strings.Join(items, ", "),
			"开启浏览器 PAC 并安装 pf 内核防直连规则", 10, true)
	} else if (vmProxy.PeacePFAnchorExists || vmProxy.LegacyPFAnchorExists) && directBlocked {
		add("outbound_constraint", "1. 环境外出站约束", "pass",
			fmt.Sprintf("宿主机无直连泄漏，且实测直连 160.79.104.10:443 已被 pf 阻断 (%s)", directErr),
			"", 10, true)
	} else if vmProxy.PeacePFAnchorExists || vmProxy.LegacyPFAnchorExists {
		add("outbound_constraint", "1. 环境外出站约束", "warn",
			"已存在 pf anchor，但主动直连探测未超时拦截 ("+directErr+")，需加载生效规则",
			"点击一键授权安装/加载 PeaceClaude pf 规则", 10, true)
	} else {
		add("outbound_constraint", "1. 环境外出站约束", "warn",
			"进程级出站受控（无直连 socket），但尚未安装内核级 pf killswitch 兜底",
			fmt.Sprintf("点击一键授权安装或执行 `sudo bash %s`", vmProxy.InstallScriptPath), 10, true)
	}

	// 2. 终端检测通道 (Terminal Proxy Channel: live HTTP :18897 + SOCKS5 :18898 check)
	if !daemonRunning {
		add("terminal_channel", "2. 终端检测通道", "fail",
			"PeaceClaude 本地网关未运行，CLI 代理通道不可用",
			"执行 `peaceclaude service start`", 10, true)
	} else {
		termOK, termDetail := probeLocalGatewayChannel(ctx, cfg)
		if termOK {
			add("terminal_channel", "2. 终端检测通道", "pass", termDetail, "", 10, true)
		} else {
			add("terminal_channel", "2. 终端检测通道", "fail", termDetail,
				"一键重检出口并在必要时解锁门禁", 10, true)
		}
	}

	// 3. DNS 与解析约束 (Remote DNS via SOCKS5 ATYP=DOMAIN / HTTP CONNECT; Chrome profile disables dns_prefetching natively)
	dnsDetails := []string{"HTTP CONNECT/SOCKS5 远端域名解析", "Claude Desktop 与登录浏览器禁用本地 DNS 预解析（零警告横幅）"}
	dnsStatus := "pass"
	if !cfg.Identity.DisableDesktopQUIC {
		dnsStatus = "warn"
		dnsDetails = append(dnsDetails, "建议开启 QUIC 与非代理 UDP 禁用")
	}
	if _, err := os.Stat("/etc/resolver/anthropic.com"); err == nil {
		dnsStatus = "warn"
		dnsDetails = append(dnsDetails, "警告: /etc/resolver/anthropic.com 存在可能干扰解析")
	}
	add("dns_constraint", "3. DNS 与解析约束", dnsStatus, strings.Join(dnsDetails, " · "),
		"一键启用浏览器/Desktop 远端解析与防 UDP 泄漏配置", 8, true)

	// 4. 系统时区与语言 (Isolated CLI Process TZ & LANG verification)
	tzOK, tzOut := verifyIsolatedTimezone(gateSnap.EffectiveTimezone, cfg.Identity.Locale)
	if tzOK && !isCNTimezone(gateSnap.EffectiveTimezone) && strings.HasPrefix(strings.ToLower(cfg.Identity.Language), "en") {
		add("system_tz_lang", "4. 系统时区与语言 (CLI 隔离环境)", "pass",
			fmt.Sprintf("宿主机 %s → 隔离进程实测 TZ=%s (%s) · LANG=%s", hostTZ, gateSnap.EffectiveTimezone, tzOut, cfg.Identity.Locale),
			"", 8, true)
	} else {
		add("system_tz_lang", "4. 系统时区与语言 (CLI 隔离环境)", "warn",
			fmt.Sprintf("当前隔离注入 TZ=%s (%s) · LANG=%s，与美区静态 IP 存在偏差", gateSnap.EffectiveTimezone, tzOut, cfg.Identity.Locale),
			"保持 identity.sync_timezone=true 且 language=en-US", 8, true)
	}

	// 5. 浏览器与 Desktop 时区与语言 (Desktop/Browser launch flags + running process audit)
	if desktopAudit.ExposedCount > 0 {
		add("browser_desktop_tz_lang", "5. 浏览器与 Desktop 时区与语言", "warn",
			fmt.Sprintf("隔离启动参数已就绪 (--lang=%s, TZ=%s)，但检测到 %d 个未隔离的原生 Claude.app 仍在运行",
				cfg.Identity.Language, gateSnap.EffectiveTimezone, desktopAudit.ExposedCount),
			"关闭原生 Claude.app 窗口，改用 `/Applications/PeaceClaude.app` 或 `peaceclaude app`", 8, true)
	} else {
		add("browser_desktop_tz_lang", "5. 浏览器与 Desktop 时区与语言", "pass",
			fmt.Sprintf("Desktop & 隔离登录浏览器均强制注入 --lang=%s · --accept-lang · TZ=%s，且启用 Identity Shield（Intl/DNT/GPC/Geolocation 禁权与 Canvas 中文字体防护）",
				cfg.Identity.Language, gateSnap.EffectiveTimezone),
			"", 8, true)
	}

	// 6. 共享文件与宿主私有目录 (Profile isolation, permissions, host env stripping, zero-VM boundary note)
	prof, profErr := profile.Load(cfg, cfg.ActiveProfile)
	if profErr == nil && prof != nil {
		home, _ := os.UserHomeDir()
		separated := !strings.HasPrefix(prof.CLIConfigDir, filepath.Join(home, ".claude")) &&
			!strings.HasPrefix(prof.DesktopDir, filepath.Join(home, "Library/Application Support/Claude"))
		if separated && cfg.Identity.StripHostAnthropicEnv {
			add("shared_files_isolation", "6. 共享文件与宿主私有目录", "pass",
				fmt.Sprintf("档案 %q 配置/Desktop/登录浏览器均物理隔离于 ~/.peaceclaude/profiles/%s (0700)，已剥离宿主中转变量；零 VM 边界：同 macOS 用户空间仍可访问普通项目目录",
					prof.Name, prof.Name),
				"", 8, true)
		} else {
			add("shared_files_isolation", "6. 共享文件与宿主私有目录", "warn",
				"建议开启 identity.strip_host_anthropic_env 以防宿主机 ANTHROPIC_BASE_URL 污染隔离会话",
				"在配置中启用 strip_host_anthropic_env", 8, true)
		}
	} else {
		add("shared_files_isolation", "6. 共享文件与宿主私有目录", "warn",
			"默认隔离档案尚未初始化", "执行 `peaceclaude setup`", 8, true)
	}

	// 7. 宿主管理接口 (Loopback-only Control API + CSRF guard + 0600 config permission)
	cfgPermOK := false
	if fi, err := os.Stat(config.ConfigPath()); err == nil {
		cfgPermOK = (fi.Mode().Perm() == 0o600)
	}
	if cfg.Listen.BindAddr == "127.0.0.1" && cfgPermOK {
		add("host_mgmt_api", "7. 宿主管理接口", "pass",
			fmt.Sprintf("Control API 仅监听 %s:%d，启用 Host/Origin CSRF 防护，config.json 权限 0600 且全接口脱敏",
				cfg.Listen.BindAddr, cfg.Listen.ControlPort),
			"", 8, true)
	} else {
		add("host_mgmt_api", "7. 宿主管理接口", "warn",
			fmt.Sprintf("bind_addr=%s config_0600=%t", cfg.Listen.BindAddr, cfgPermOK),
			"保持 listen.bind_addr=127.0.0.1 且 config.json 权限为 0600", 8, true)
	}

	// 8. 代理故障关闭 (Fail-Closed self-verification)
	eng := policy.NewEngine(cfg)
	fcDec := eng.Evaluate("api.anthropic.com:443", false, "selftest")
	if cfg.Egress.FailClosed && fcDec.Action == policy.ActionBlockFailClosed {
		add("proxy_fail_closed", "8. 代理故障关闭 (Fail-Closed)", "pass",
			"策略自检通过：门禁锁定时 api.anthropic.com:443 硬返回 503 block_failclosed，并自动切断活跃连接，零直连回退",
			"", 10, true)
	} else {
		add("proxy_fail_closed", "8. 代理故障关闭 (Fail-Closed)", "fail",
			"Fail-Closed 开关被关闭！代理异常时存在直连泄漏风险",
			"立刻在设置中开启 egress.fail_closed=true", 10, true)
	}

	// 9. UDP 出站约束 (QUIC disabled + WebRTC non-proxied UDP disabled + SOCKS5 UDP refused + pf UDP drop)
	udpParts := []string{"SOCKS5 拒绝 UDP ASSOCIATE"}
	udpStatus := "pass"
	if cfg.Identity.DisableDesktopQUIC {
		udpParts = append(udpParts, "Desktop/浏览器 --disable-quic + 禁 WebRTC 非代理 UDP")
	} else {
		udpStatus = "warn"
		udpParts = append(udpParts, "Desktop QUIC 未禁用")
	}
	if vmProxy.PeacePFAnchorExists || vmProxy.LegacyPFAnchorExists {
		udpParts = append(udpParts, "pf 丢弃 Anthropic UDP")
	}
	add("udp_constraint", "9. UDP 出站约束", udpStatus, strings.Join(udpParts, " · "),
		"保持 identity.disable_desktop_quic=true 并启用 pf 规则", 8, true)

	// 10. IPv6 出站约束 (Active direct IPv6 dial test + Clash ipv6 check + pf inet6 drop)
	v6Blocked, v6Err := testDirectBlocked("[2607:6bc0::10]:443", 2*time.Second)
	if v6Blocked && (!clash.Available || !clash.IPv6 || cfg.Upstream.Mode != "local-proxy") {
		add("ipv6_constraint", "10. IPv6 出站约束", "pass",
			fmt.Sprintf("实测直连 Anthropic IPv6 [2607:6bc0::10]:443 已被阻断 (%s)，无 IPv6 泄漏路径", v6Err),
			"", 8, true)
	} else if v6Blocked && clash.IPv6 {
		add("ipv6_constraint", "10. IPv6 出站约束", "warn",
			fmt.Sprintf("宿主机直连 IPv6 已阻断 (%s)，但本机 Clash Verge 配置 ipv6=true，建议关闭以防代理内走 IPv6", v6Err),
			"在 Clash 配置中设置 ipv6: false", 8, true)
	} else {
		add("ipv6_constraint", "10. IPv6 出站约束", "fail",
			"直连 Anthropic IPv6 未被阻断！存在 IPv6 绕过风险",
			fmt.Sprintf("执行 `sudo bash %s` 启用 IPv6 pf 阻断", vmProxy.InstallScriptPath), 8, true)
	}

	// 11. 出口符合已保存规则 (Static IP match + Allowed country + OAuth TLS + Split-IP awareness)
	oauthOK, oauthDetail := checkOAuthReachable(ctx, cfg)
	if gateSnap.Open && gateSnap.LastProbe.OK && oauthOK {
		detail := fmt.Sprintf("出口 %s (%s · %s · AS%d) 符合已保存规则 · OAuth %s",
			gateSnap.LastProbe.EgressIP, gateSnap.LastProbe.Country, gateSnap.LastProbe.City,
			gateSnap.LastProbe.ASN, oauthDetail)
		status := "pass"
		hint := ""
		if gateSnap.LastProbe.SplitIPDetected && !cfg.Egress.StrictDomainIsolation {
			status = "warn"
			detail += fmt.Sprintf(" · 注意 Split-IP (泛公网=%s)", gateSnap.LastProbe.PublicSplitIP)
			hint = "建议升级为直连 Webshare SOCKS5 上游或在“上游与安全策略”开启严格域名隔离"
		}
		add("egress_rules_match", "11. 出口符合已保存规则", status, detail, hint, 14, true)
	} else {
		reason := gateSnap.LockReason
		if reason == "" {
			reason = gateSnap.LastProbe.FailureReason
		}
		if !oauthOK {
			reason += " | OAuth: " + oauthDetail
		}
		add("egress_rules_match", "11. 出口符合已保存规则", "fail",
			"出口规则核验未通过: "+reason,
			"核对上游静态 IP 与 OAuth 连通性（Webshare 需用 SOCKS5 规避 *.claude.com RST）", 14, true)
	}

	// =========================================================================
	// Supplementary Host Diagnostics (Telemetry / Clash TUN / Binaries)
	// =========================================================================
	if cfg.Shield.TelemetryShield {
		add("telemetry", "补充 · 遥测与错误上报屏蔽盾", "pass",
			"网关拦截 sentry.io / statsig / segment + CLI 注入 DISABLE_TELEMETRY=1", "", 0, false)
	} else {
		add("telemetry", "补充 · 遥测与错误上报屏蔽盾", "warn", "遥测屏蔽盾未开启", "建议开启 telemetry_shield", 0, false)
	}

	if cfg.Upstream.Mode == "local-proxy" && clash.Available && clash.TUNEnabled {
		add("clash_tun", "补充 · Clash TUN 冲突检查", "fail",
			"检测到 Clash TUN 已开启！极易与企业飞连 VPN 冲突断网", "在 Clash Verge 中关闭 TUN 模式", 0, false)
	}

	binDetail := fmt.Sprintf("CLI=%s (%s) · 隔离启动器=%s",
		filepath.Base(nonEmptyStr(cliBin, "未找到")), cliSource, nonEmptyStr(launcherApp, "未安装"))
	binStatus := "pass"
	if cliBin == "" || launcherApp == "" {
		binStatus = "warn"
	}
	add("binaries", "补充 · 客户端与启动器完整性", binStatus, binDetail, "运行 `peaceclaude setup` 补齐", 0, false)

	// Compute 11/11 critical counters and 0-100 score.
	critTotal := 0
	critPass := 0
	totalWeight := 0
	earned := 0
	for _, c := range checks {
		if c.Critical {
			critTotal++
			if c.Status == "pass" {
				critPass++
			}
		}
		if c.Weight > 0 {
			totalWeight += c.Weight
			switch c.Status {
			case "pass":
				earned += c.Weight
			case "warn":
				earned += c.Weight / 2
			}
		}
	}
	score := 0
	if totalWeight > 0 {
		score = (earned * 100) / totalWeight
	}
	grade := "safe"
	if score < 60 || critPass < 7 {
		grade = "risk"
	} else if score < 85 || critPass < critTotal {
		grade = "caution"
	}
	rep.CriticalPassed = critPass
	rep.CriticalTotal = critTotal
	rep.Checks = checks
	rep.Score = score
	rep.Grade = grade
	return rep
}

func testDirectBlocked(addr string, timeout time.Duration) (bool, string) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return true, err.Error()
	}
	defer conn.Close()
	// On macOS, `pf block drop` on IPv6 can cause non-blocking kqueue Dial to return nil
	// with an unconnected socket whose first Write immediately fails with ENOTCONN ("socket is not connected").
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, werr := conn.Write([]byte("HEAD / HTTP/1.0\r\n\r\n")); werr != nil {
		return true, werr.Error()
	}
	var buf [1]byte
	if _, rerr := conn.Read(buf[:]); rerr != nil {
		if errors.Is(rerr, io.EOF) {
			return false, "direct TCP connected (peer EOF)!"
		}
		return true, rerr.Error()
	}
	return false, "direct TCP connected!"
}

func probeLocalGatewayChannel(ctx context.Context, cfg *config.Config) (bool, string) {
	pctx, cancel := context.WithTimeout(ctx, 14*time.Second)
	defer cancel()
	proxyURL, err := url.Parse(cfg.HTTPProxyURL())
	if err != nil {
		return false, err.Error()
	}
	client := &http.Client{
		Timeout:   14 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true},
	}
	req, err := http.NewRequestWithContext(pctx, http.MethodGet, "https://claude.ai/cdn-cgi/trace", nil)
	if err != nil {
		return false, err.Error()
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("HTTP 代理通道 :%d 请求失败: %v", cfg.Listen.HTTPPort, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("HTTP 代理通道 :%d 返回 HTTP %d（可能门禁锁定）", cfg.Listen.HTTPPort, resp.StatusCode)
	}
	var ip string
	for _, ln := range strings.Split(string(body), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(ln), "="); ok && k == "ip" {
			ip = strings.TrimSpace(v)
		}
	}
	if cfg.Egress.ExpectedIP != "" && ip != cfg.Egress.ExpectedIP {
		return false, fmt.Sprintf("终端通道出口 %s 与预期 %s 不符", ip, cfg.Egress.ExpectedIP)
	}
	return true, fmt.Sprintf("实测本地网关 HTTP :%d + SOCKS5 :%d 畅通 · 终端通道出口=%s (%dms)",
		cfg.Listen.HTTPPort, cfg.Listen.SOCKS5Port, ip, time.Since(start).Milliseconds())
}

func verifyIsolatedTimezone(tz, locale string) (bool, string) {
	if tz == "" {
		return false, "unset"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/date", "+%Z %z")
	cmd.Env = []string{"TZ=" + tz, "LANG=" + locale, "LC_ALL=" + locale}
	out, err := cmd.Output()
	if err != nil {
		return false, err.Error()
	}
	return true, strings.TrimSpace(string(out))
}

func isCNTimezone(tz string) bool {
	return strings.Contains(tz, "Shanghai") || strings.Contains(tz, "Chongqing") ||
		strings.Contains(tz, "Urumqi") || strings.Contains(tz, "Harbin")
}

func nonEmptyStr(s, fb string) string {
	if strings.TrimSpace(s) == "" {
		return fb
	}
	return s
}

func checkOAuthReachable(ctx context.Context, cfg *config.Config) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dialer := upstream.FromConfig(cfg.Upstream)
	client := dialer.NewHTTPClient(10 * time.Second)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, "https://platform.claude.com/oauth/authorize", nil)
	if err != nil {
		return false, err.Error()
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Sprintf("TLS/连接失败: %v", err)
	}
	_ = resp.Body.Close()
	return true, fmt.Sprintf("可达 (HTTP %d · %dms)", resp.StatusCode, time.Since(start).Milliseconds())
}

func fixMetadataForCheck(id, status, detail string) (fixable bool, label, action string) {
	if status == "pass" {
		return false, "", ""
	}
	switch id {
	case "outbound_constraint":
		if strings.Contains(detail, "直连") && strings.Contains(detail, "Socket") {
			return true, "开启全局 PAC 并安装 pf", "install_pf_and_pac"
		}
		return true, "授权安装 pf 防直连规则", "install_pf"
	case "ipv6_constraint":
		return true, "授权启用 IPv6 阻断 (pf)", "install_pf"
	case "terminal_channel", "egress_rules_match":
		return true, "重检出口并解除门禁", "recheck_egress"
	case "browser_desktop_tz_lang":
		return true, "一键接管隔离 Desktop", "takeover_desktop"
	case "dns_constraint", "system_tz_lang", "shared_files_isolation", "host_mgmt_api", "proxy_fail_closed", "udp_constraint", "telemetry":
		return true, "一键启用安全防护配置", "enable_hardening"
	case "binaries":
		return true, "打开安装与设置指引", "repair_binaries"
	default:
		return false, "", ""
	}
}
