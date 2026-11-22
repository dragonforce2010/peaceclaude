package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"peaceclaude/internal/audit"
	"peaceclaude/internal/config"
	"peaceclaude/internal/daemon"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/host"
	"peaceclaude/internal/launcher"
	"peaceclaude/internal/profile"
	"peaceclaude/internal/upstream"
	"peaceclaude/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "peaceclaude: %v\n", err)
		os.Exit(1)
	}
}

func selfBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return "peaceclaude"
	}
	if realPath, err := filepath.EvalSymlinks(exe); err == nil {
		return realPath
	}
	return exe
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}
	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "-h", "--help", "help":
		printUsage()
		return nil
	case "-v", "--version", "version":
		fmt.Println(version.String())
		return nil
	case "cli":
		return cmdCLI(rest)
	case "app":
		return cmdApp(rest)
	case "login-browser", "browser":
		return cmdLoginBrowser(rest)
	case "browser-pac", "pac":
		return cmdBrowserPAC(rest)
	case "browser-shield", "shield":
		return cmdBrowserShield(rest)
	case "ui", "dashboard":
		return cmdUI(rest)
	case "service":
		return cmdService(rest)
	case "status":
		return cmdStatus(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "diagnose":
		return cmdDiagnose(rest)
	case "leaks":
		return cmdLeaks(rest)
	case "capture":
		return cmdCapture(rest)
	case "profile", "profiles", "org":
		return cmdProfile(rest)
	case "rules":
		return cmdRules(rest)
	case "clean", "fingerprint-clean":
		return cmdClean(rest)
	case "vm-proxy", "pf":
		return cmdVMProxy(rest)
	case "config":
		return cmdConfig(rest)
	case "lock":
		return cmdLock(true, rest)
	case "unlock":
		return cmdLock(false, rest)
	case "logs":
		return cmdLogs(rest)
	case "setup":
		return cmdSetup(rest)
	case "uninstall":
		return cmdUninstall(rest)
	default:
		return fmt.Errorf("未知命令 %q（运行 `peaceclaude --help` 查看全部命令；如需透传参数给 Claude Code CLI 请用 `peaceclaude cli %s`）", cmd, strings.Join(args, " "))
	}
}

func printUsage() {
	fmt.Print(`PeaceClaude — 省心、放心的 macOS 零虚拟机静态 IP Claude 全入口安全套件

用法: peaceclaude <命令> [参数]    (短别名: pclaude)

日常启动:
  cli [--profile P] [--dry-run] [-- <args...>]   启动隔离版 Claude Code CLI
  app [run|takeover|reset|install] [--profile P] 启动隔离版 Claude Desktop / 自动接管原生窗口 / 管理启动器
  login-browser [--profile P] [url]              打开隔离登录浏览器（独立 Cookie + 静态 IP + 美区时区/语言）
  browser-pac [status|on|off] [--dry-run]        让普通浏览器 (Chrome/Safari/Arc/Edge) 访问 Claude 自动走静态 IP 门禁
  browser-shield [status|sync|open]              生成/检查原生浏览器定向 MV3 护航扩展（仅护 Claude & 测试域）
  ui                                             打开本地可视化控制台 (http://127.0.0.1:18899)

隔离档案与规则:
  profile [list|create|use|delete]               管理多账号隔离档案（独立配置/Desktop数据/设备指纹）
  rules [status|on|off|sync]                     跨档案共享全局 CLAUDE.md / skills / agents / hooks
  clean [--profile P] [--dry-run] [--yes]        可恢复指纹清理（自动备份，保留配置与 skills）

守护服务与安全门禁:
  service [start|stop|restart|status|ensure-autostart|run]  管理后台 Fail-Closed 本地安全网关 (HTTP/SOCKS5/PAC/LaunchAgent)
  lock [reason]                                  立即紧急熔断（阻断一切出站并切断活跃连接）
  unlock                                         解除手动熔断并立即重检静态 IP 出口
  config [show|set-upstream|import-clash|gateway|set]  管理 Webshare / Clash 静态 IP 上游与测速

审计与排障:
  status [--json] [--check]                      查看全局门禁、静态出口 IP、Clash、pf 与客户端状态
  doctor [--json] [--fix]                        全链路防风控安全体检（0-100 评分与修复建议）
  diagnose [--json]                              403 / 断流智能三分诊 (healthy/network/auth/unknown)
  leaks [--since 24h] [--json]                   查看 Split-IP / 拦截记录与宿主机直连 Socket 审计
  capture [on|off|status|list|show|clear]        管理本地脱敏抓包（严禁落盘 Token/Cookie/密码）
  vm-proxy [status|render|on|off|install|uninstall]  macOS Cowork 透明代理与内核 pf 防直连护盾
  logs [-n 80]                                   查看守护进程日志

安装与维护:
  setup [--no-launchd]                           一键初始化配置、安装后台服务与 PeaceClaude.app
  uninstall [--purge-data]                       卸载后台服务与 .app 启动器（默认保留档案数据）
  version                                        显示版本信息
`)
}

func cmdCLI(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	var profName string
	var dryRun bool
	var passthrough []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			passthrough = append(passthrough, args[i+1:]...)
			break
		}
		if a == "--profile" && i+1 < len(args) {
			profName = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--profile=") {
			profName = strings.TrimPrefix(a, "--profile=")
			continue
		}
		if a == "--dry-run" {
			dryRun = true
			continue
		}
		passthrough = append(passthrough, args[i:]...)
		break
	}
	return launcher.LaunchCLI(context.Background(), cfg, selfBinary(), profName, passthrough, dryRun)
}

func cmdApp(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "run"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	profName := fs.String("profile", "", "指定隔离档案名")
	dir := fs.String("dir", "", "指定安装 .app 的目标目录（默认 /Applications，用于离线打包）")
	dryRun := fs.Bool("dry-run", false, "仅打印启动参数，不实际启动")
	yes := fs.Bool("yes", false, "跳过确认提示")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	switch sub {
	case "run":
		spec, pid, err := launcher.LaunchDesktop(context.Background(), cfg, selfBinary(), *profName, fs.Args(), *dryRun)
		if err != nil {
			return err
		}
		if *dryRun {
			printJSON(spec)
			return nil
		}
		fmt.Printf("✔ 已启动隔离版 Claude Desktop (档案=%s, PID=%d)\n  数据目录: %s\n  安全网关: %s\n",
			spec.Profile, pid, spec.DataDir, cfg.HTTPProxyURL())
		return nil
	case "install":
		var (
			appPath string
			err     error
		)
		if targetDir := strings.TrimSpace(*dir); targetDir != "" {
			appPath, err = launcher.InstallDesktopLauncherAppTo(selfBinary(), targetDir)
		} else {
			appPath, err = launcher.InstallDesktopLauncherApp(selfBinary())
		}
		if err != nil {
			return err
		}
		fmt.Printf("✔ 已安装隔离桌面启动器: %s\n", appPath)
		return nil
	case "takeover":
		if ok, _ := launcher.DaemonHealth(cfg); ok {
			raw, err := daemonPost(cfg, "/api/desktop/takeover", nil)
			if err != nil {
				return err
			}
			fmt.Printf("✔ 已接管未隔离原生 Claude.app 并切入安全隔离模式:\n%s\n", string(raw))
			return nil
		}
		rep := host.InspectRunningDesktop(cfg)
		var pids []string
		for _, p := range rep.Processes {
			if !p.Isolated {
				pids = append(pids, p.PID)
			}
		}
		if len(pids) > 0 {
			launcher.TerminatePIDs(pids, 4*time.Second)
		}
		spec, pid, err := launcher.LaunchDesktop(context.Background(), cfg, selfBinary(), *profName, nil, false)
		if err != nil {
			return err
		}
		fmt.Printf("✔ 已接管 %d 个未隔离实例并拉起隔离版 Claude Desktop (档案=%s, PID=%d)\n", len(pids), spec.Profile, pid)
		return nil
	case "reset":
		name := *profName
		if name == "" {
			name = cfg.ActiveProfile
		}
		rep, err := profile.CleanProfile(cfg, name, !*yes && *dryRun)
		if err != nil {
			return err
		}
		printJSON(rep)
		return nil
	default:
		return fmt.Errorf("未知 app 子命令 %q (可用: run | install | reset)", sub)
	}
}

func cmdLoginBrowser(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("login-browser", flag.ContinueOnError)
	profName := fs.String("profile", "", "指定隔离档案")
	dryRun := fs.Bool("dry-run", false, "仅打印命令参数")
	if err := fs.Parse(args); err != nil {
		return err
	}
	targetURL := "https://claude.ai/login"
	if fs.NArg() > 0 {
		targetURL = fs.Arg(0)
	}
	bin, bArgs, err := launcher.LaunchLoginBrowser(context.Background(), cfg, selfBinary(), *profName, targetURL, *dryRun)
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Printf("browser: %s\nargs: %v\n", bin, bArgs)
		return nil
	}
	fmt.Printf("✔ 已打开隔离登录浏览器 (%s) → %s\n  已注入静态 IP 网关、同步时区与 en-US 语言。\n", filepath.Base(bin), targetURL)
	return nil
}

func cmdBrowserPAC(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "status"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	fs := flag.NewFlagSet("browser-pac", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "仅打印将要执行的 networksetup 命令，不实际修改系统代理")
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	switch sub {
	case "status":
		st := host.InspectBrowserPAC(cfg)
		if *asJSON {
			printJSON(st)
			return nil
		}
		fmt.Printf("系统自动代理 PAC: %v\n", st.SystemPACEnable)
		fmt.Printf("目标 PAC 地址:    %s\n", st.PACURL)
		if st.CurrentPACURL != "" {
			fmt.Printf("当前 PAC 地址:    %s\n", st.CurrentPACURL)
		}
		if len(st.ActiveServices) > 0 {
			fmt.Printf("活跃网络服务:     %s\n", strings.Join(st.ActiveServices, ", "))
		}
		fmt.Printf("说明:             %s\n", st.Note)
		return nil
	case "on", "enable":
		st, cmds, err := host.SetSystemBrowserPAC(cfg, true, *dryRun)
		if err != nil {
			return err
		}
		if *asJSON {
			printJSON(map[string]any{"status": st, "commands": cmds, "dry_run": *dryRun})
			return nil
		}
		if *dryRun {
			fmt.Println("将执行以下系统 PAC 绑定命令（仅路由 Claude/Anthropic 域名，不影响飞连与普通网页）:")
			for _, c := range cmds {
				fmt.Println(" ", c)
			}
			return nil
		}
		fmt.Printf("✔ 已为活跃网络 (%s) 开启 PeaceClaude PAC: %s\n", strings.Join(st.ActiveServices, ", "), st.PACURL)
		fmt.Println("  Chrome / Safari / Arc / Edge 访问 claude.ai 将自动走静态 IP Fail-Closed 门禁，公司内网与普通网页保持直连。")
		fmt.Println("  提示：如需同时隔离时区/语言/Cookie 指纹，推荐用 `peaceclaude login-browser`。")
		return nil
	case "off", "disable":
		st, cmds, err := host.SetSystemBrowserPAC(cfg, false, *dryRun)
		if err != nil {
			return err
		}
		if *asJSON {
			printJSON(map[string]any{"status": st, "commands": cmds, "dry_run": *dryRun})
			return nil
		}
		if *dryRun {
			for _, c := range cmds {
				fmt.Println(" ", c)
			}
			return nil
		}
		fmt.Printf("✔ 已关闭系统自动代理 PAC (%s)\n", strings.Join(st.ActiveServices, ", "))
		return nil
	default:
		return fmt.Errorf("未知 browser-pac 子命令 %q (可用: status | on | off)", sub)
	}
}

func cmdBrowserShield(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "status"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	fs := flag.NewFlagSet("browser-shield", flag.ContinueOnError)
	browserID := fs.String("browser", "arc", "目标原生浏览器 ID (arc/chrome/brave/edge/chromium/vivaldi)")
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	var gate egress.GateSnapshot
	if ok, _ := launcher.DaemonHealth(cfg); ok {
		if snap, err := launcher.FetchGateSnapshot(context.Background(), cfg, false); err == nil {
			gate = snap
		}
	}

	extDir, err := launcher.EnsureNativeBrowserShield(cfg, gate)
	if err != nil {
		return err
	}

	switch sub {
	case "sync":
		if *asJSON {
			printJSON(map[string]any{"ok": true, "extension_dir": extDir})
			return nil
		}
		fmt.Printf("✔ 已生成并同步原生浏览器定向护航扩展: %s\n", extDir)
		return nil
	case "status":
		rep := launcher.InspectNativeBrowsers(cfg, host.InspectBrowserPAC(cfg), gate)
		if *asJSON {
			printJSON(rep)
			return nil
		}
		fmt.Printf("扩展目录:   %s (就绪: %t)\n", rep.ExtensionDir, rep.ExtensionReady)
		fmt.Printf("系统 PAC:   %t\n", rep.PACEnabled)
		fmt.Printf("目标指纹:   %s · %s\n", rep.TargetTimezone, rep.TargetLocale)
		fmt.Printf("护航域名:   %s\n", strings.Join(rep.ProtectedDomains, ", "))
		for _, b := range rep.Browsers {
			if !b.Installed {
				continue
			}
			fmt.Printf("  - %-14s [%-12s] 扩展=%t 启用=%t %s\n", b.Name, b.ProtectionLevel, b.ExtensionLoaded, b.ExtensionEnabled, b.Note)
		}
		return nil
	case "open", "guide":
		res, err := launcher.OpenNativeBrowserExtensionGuide(*browserID, extDir)
		if err != nil {
			return err
		}
		if *asJSON {
			printJSON(res)
			return nil
		}
		fmt.Printf("✔ %s\n", res.Message)
		for i, step := range res.Steps {
			fmt.Printf("  %d. %s\n", i+1, step)
		}
		return nil
	default:
		return fmt.Errorf("未知 browser-shield 子命令 %q (可用: status | sync | open)", sub)
	}
}

func cmdUI(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("ui", flag.ContinueOnError)
	noOpen := fs.Bool("no-open", false, "只打印 URL，不自动打开浏览器")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if ok, _ := launcher.DaemonHealth(cfg); !ok {
		if err := launcher.StartService(cfg, selfBinary(), false); err != nil {
			return err
		}
	}
	u := cfg.ControlURL() + "/#accounts"
	fmt.Printf("PeaceClaude 多账号安心工作台: %s\n", u)
	if !*noOpen {
		_ = exec.Command("open", u).Start()
	}
	return nil
}

func cmdService(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "status"
	rest := args
	if len(args) > 0 {
		sub = args[0]
		rest = args[1:]
	}
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	noLaunchd := fs.Bool("no-launchd", false, "直接后台启动进程而不注册 LaunchAgent")
	ifEnabled := fs.Bool("if-enabled", false, "仅在 auto_start_launch_agent 开启时注册/同步 LaunchAgent")
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	switch sub {
	case "run":
		log.SetFlags(log.LstdFlags)
		srv := daemon.New(cfg, selfBinary())
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		if err := srv.Start(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			shutCtx, shutCancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer shutCancel()
			srv.Shutdown(shutCtx)
		case <-srv.Done():
		}
		return nil
	case "start":
		if err := launcher.StartService(cfg, selfBinary(), !*noLaunchd); err != nil {
			return err
		}
		st := launcher.GetServiceStatus(cfg)
		if *asJSON {
			printJSON(st)
			return nil
		}
		fmt.Printf("✔ PeaceClaude 安全网关已运行 (PID=%d)\n  HTTP 代理:   %s\n  SOCKS5 代理: %s\n  Web 控制台:  %s\n",
			st.PID, st.HTTPProxyURL, st.SOCKS5ProxyURL, st.ControlURL)
		return nil
	case "ensure-autostart":
		self := selfBinary()
		res, err := launcher.EnsureLaunchAgent(cfg, self, *ifEnabled)
		if err != nil {
			return err
		}
		if (res.Action == "disabled" || res.Action == "disabled_in_sandbox" || res.Action == "skipped_ephemeral_binary") && !res.Running {
			if err := launcher.StartService(cfg, self, false); err != nil {
				return err
			}
			running, pid := launcher.DaemonHealth(cfg)
			res.Running = running
			res.PID = pid
		}
		if *asJSON {
			printJSON(res)
			return nil
		}
		switch res.Action {
		case "skipped":
			fmt.Printf("✔ LaunchAgent 开机项与守护进程均已就绪 (PID=%d, action=skipped)\n", res.PID)
		case "installed":
			fmt.Printf("✔ 已自动注册 LaunchAgent 开机项并启动守护进程 (PID=%d, plist=%s)\n", res.PID, res.LaunchAgentPath)
		case "repaired":
			fmt.Printf("✔ 已修复失效的 LaunchAgent 开机项并重启守护进程 (PID=%d, plist=%s)\n", res.PID, res.LaunchAgentPath)
		case "restarted":
			fmt.Printf("✔ 已重新加载 LaunchAgent 开机项并启动守护进程 (PID=%d)\n", res.PID)
		case "disabled":
			fmt.Printf("ℹ 已根据配置保持 LaunchAgent 开机自启关闭 (守护进程运行=%t, PID=%d)\n", res.Running, res.PID)
		default:
			fmt.Printf("ℹ ensure-autostart: action=%s running=%t pid=%d (%s)\n", res.Action, res.Running, res.PID, res.Reason)
		}
		return nil
	case "stop":
		if err := launcher.StopService(cfg); err != nil {
			return err
		}
		fmt.Println("✔ PeaceClaude 安全网关已停止")
		return nil
	case "restart":
		_ = launcher.StopService(cfg)
		time.Sleep(300 * time.Millisecond)
		if err := launcher.StartService(cfg, selfBinary(), !*noLaunchd); err != nil {
			return err
		}
		st := launcher.GetServiceStatus(cfg)
		fmt.Printf("✔ PeaceClaude 安全网关已重启 (PID=%d, UI=%s)\n", st.PID, st.ControlURL)
		return nil
	case "status":
		st := launcher.GetServiceStatus(cfg)
		if *asJSON {
			printJSON(st)
			return nil
		}
		state := "未运行"
		if st.Running {
			state = fmt.Sprintf("运行中 (PID %d)", st.PID)
		}
		fmt.Printf("后台服务: %s\nLaunchAgent: 已安装=%t 已加载=%t 有效=%t (开机自启策略=%t, 路径=%s)\nHTTP 代理: %s\nSOCKS5 代理: %s\nWeb 控制台: %s\n",
			state, st.LaunchAgentSet, st.LaunchAgentLoaded, st.LaunchAgentValid, st.AutoStartEnabled, st.LaunchAgentPath, st.HTTPProxyURL, st.SOCKS5ProxyURL, st.ControlURL)
		return nil
	default:
		return fmt.Errorf("未知 service 子命令 %q (可用: start|stop|restart|status|ensure-autostart|run)", sub)
	}
}

func cmdStatus(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "输出 JSON")
	checkOnly := fs.Bool("check", false, "只检查不自动启动守护进程")
	if err := fs.Parse(args); err != nil {
		return err
	}

	running, _ := launcher.DaemonHealth(cfg)
	if running {
		fetchStatus := func() ([]byte, map[string]any, error) {
			resp, err := http.Get(cfg.ControlURL() + "/api/status")
			if err != nil {
				return nil, nil, err
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			var st map[string]any
			if err := json.Unmarshal(raw, &st); err != nil {
				return raw, nil, err
			}
			return raw, st, nil
		}
		if raw, st, err := fetchStatus(); err == nil {
			if gateMap, ok := st["gate"].(map[string]any); ok {
				if reason, _ := gateMap["lock_reason"].(string); strings.Contains(reason, "initializing") {
					_, _ = daemonPost(cfg, "/api/check", nil)
					if raw2, st2, err2 := fetchStatus(); err2 == nil {
						raw, st = raw2, st2
					}
				}
			}
			if *asJSON {
				fmt.Println(strings.TrimSpace(string(raw)))
				return nil
			}
			printHumanStatus(cfg, true, st)
			return nil
		}
	}

	// Offline / check-only fallback
	_ = checkOnly
	gate := egress.NewGate(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	res := gate.Check(ctx, false)
	cancel()
	snap := gate.Snapshot()
	offline := map[string]any{
		"version":        version.Version,
		"daemon_running": false,
		"config":         cfg.Redacted(),
		"gate":           snap,
		"probe":          res,
		"clash":          host.InspectClash(cfg.Upstream.ClashSocket),
		"vm_proxy":       host.InspectVMProxy(cfg),
		"active_profile": cfg.ActiveProfile,
		"rules":          profile.GetRulesStatus(cfg),
	}
	if *asJSON {
		printJSON(offline)
		return nil
	}
	printHumanStatus(cfg, false, offline)
	return nil
}

func printHumanStatus(cfg *config.Config, running bool, st map[string]any) {
	fmt.Printf("[global]\n")
	fmt.Printf("  版本:       %s\n", version.String())
	if running {
		fmt.Printf("  安全网关:   ✔ 运行中 (HTTP %s | SOCKS5 %s | UI %s)\n",
			cfg.HTTPProxyURL(), cfg.SOCKS5ProxyURL(), cfg.ControlURL())
	} else {
		fmt.Printf("  安全网关:   ✖ 未运行（运行 `peaceclaude service start` 启动）\n")
	}
	fmt.Printf("  激活档案:   %s\n", cfg.ActiveProfile)

	fmt.Printf("\n[egress]\n")
	gateMap, _ := st["gate"].(map[string]any)
	open, _ := gateMap["open"].(bool)
	lockReason, _ := gateMap["lock_reason"].(string)
	probe, _ := gateMap["last_probe"].(map[string]any)
	ip, _ := probe["egress_ip"].(string)
	country, _ := probe["country"].(string)
	city, _ := probe["city"].(string)
	tz, _ := gateMap["effective_timezone"].(string)
	lang, _ := gateMap["effective_language"].(string)
	splitDetected, _ := probe["split_ip_detected"].(bool)
	splitIP, _ := probe["public_split_ip"].(string)

	gateLabel := "✔ OPEN (安全放行)"
	if !open {
		gateLabel = "✖ LOCKED (Fail-Closed 已熔断: " + lockReason + ")"
	}
	fmt.Printf("  出口门禁:   %s\n", gateLabel)
	fmt.Printf("  上游链路:   %s://%s\n", cfg.Upstream.Mode, cfg.Upstream.Address)
	if ur, ok := st["upstream_route"].(map[string]any); ok {
		activeIF, _ := ur["active_interface"].(string)
		boundPhys, _ := ur["bound_physical"].(bool)
		usingFB, _ := ur["using_fallback"].(bool)
		skipReason, _ := ur["skipped_reason"].(string)
		switch {
		case boundPhys && activeIF != "":
			fmt.Printf("  出站网卡:   %s (直连物理网卡绕开 VPN)\n", activeIF)
		case usingFB:
			fbReason, _ := ur["fallback_reason"].(string)
			fmt.Printf("  出站网卡:   系统默认路由 (物理绑定失败已自动回退: %s)\n", fbReason)
		default:
			fmt.Printf("  出站网卡:   系统默认路由 (%s)\n", nonEmpty(skipReason, "未绑定物理网卡"))
		}
	}
	fmt.Printf("  静态出口IP: %s (%s %s) | 预期锁定: %s\n",
		nonEmpty(ip, "未获取"), nonEmpty(country, "?"), city, nonEmpty(cfg.Egress.ExpectedIP, "未锁定"))
	fmt.Printf("  隔离指纹:   TZ=%s | LANG=%s\n", nonEmpty(tz, "America/New_York"), nonEmpty(lang, cfg.Identity.Language))
	if splitDetected {
		fmt.Printf("  ⚠ Split-IP: Claude=%s vs 泛公网=%s（建议切直连 Webshare SOCKS5 或开启严格白名单）\n", ip, splitIP)
	}

	fmt.Printf("\n[clients & defense]\n")
	cliBin, cliSrc := host.FindRealClaudeCLI(cfg)
	fmt.Printf("  Claude CLI: %s (%s)\n", nonEmpty(cliBin, "未找到"), cliSrc)
	fmt.Printf("  DesktopApp: %s\n", cfg.Binaries.ClaudeDesktopApp)
	fmt.Printf("  隔离启动器: %s\n", nonEmpty(host.FindLauncherAppPath(), "未安装（运行 `peaceclaude setup`）"))
	if vp, ok := st["vm_proxy"].(map[string]any); ok {
		fmt.Printf("  pf 护盾:    %v\n", vp["active_pf_note"])
	}
}

func cmdDoctor(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "输出 JSON")
	fix := fs.Bool("fix", false, "自动修复缺失的后台服务、档案配置与桌面启动器")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *fix {
		_, _ = profile.Ensure(cfg, cfg.ActiveProfile, "")
		if ok, _ := launcher.DaemonHealth(cfg); !ok {
			_ = launcher.StartService(cfg, selfBinary(), false)
		}
		if host.FindLauncherAppPath() == "" {
			_, _ = launcher.InstallDesktopLauncherApp(selfBinary())
		}
	}

	running, _ := launcher.DaemonHealth(cfg)
	var gateSnap egress.GateSnapshot
	if running {
		if snap, err := launcher.FetchGateSnapshot(context.Background(), cfg, true); err == nil {
			gateSnap = snap
		} else if snap2, err2 := launcher.FetchGateSnapshot(context.Background(), cfg, false); err2 == nil {
			gateSnap = snap2
		} else {
			return fmt.Errorf("无法获取守护进程门禁状态: %w", err)
		}
	} else {
		g := egress.NewGate(cfg)
		g.Check(context.Background(), false)
		gateSnap = g.Snapshot()
	}

	rep := host.RunDoctor(context.Background(), cfg, running, gateSnap)
	if *asJSON {
		printJSON(rep)
		return nil
	}
	fmt.Printf("PeaceClaude 网络与隔离体检报告 — 关键检查: %d/%d 通过 · 评分: %d/100 [%s]\n",
		rep.CriticalPassed, rep.CriticalTotal, rep.Score, strings.ToUpper(rep.Grade))
	fmt.Printf("宿主机时区: %s  →  隔离环境: TZ=%s, LANG=%s\n", rep.HostTimezone, rep.PeaceTimezone, rep.PeaceLanguage)
	fmt.Printf("Desktop 审计: %s\n\n", rep.DesktopAudit.Summary)
	for _, c := range rep.Checks {
		icon := "✔"
		if c.Status == "warn" {
			icon = "⚠"
		} else if c.Status == "fail" {
			icon = "✖"
		}
		fmt.Printf("  %s [%s] %s\n    %s\n", icon, strings.ToUpper(c.Status), c.Title, c.Detail)
		if c.Status != "pass" && c.FixHint != "" {
			fmt.Printf("    → 建议: %s\n", c.FixHint)
		}
	}
	return nil
}

func cmdDiagnose(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var snap egress.GateSnapshot
	if ok, _ := launcher.DaemonHealth(cfg); ok {
		snap, _ = launcher.FetchGateSnapshot(context.Background(), cfg, true)
	} else {
		g := egress.NewGate(cfg)
		g.Check(context.Background(), false)
		snap = g.Snapshot()
	}
	rep := host.Diagnose(context.Background(), cfg, snap)
	if *asJSON {
		printJSON(rep)
		return nil
	}
	fmt.Printf("PeaceClaude 403/网络故障三分诊\n")
	fmt.Printf("  根因分类: %s\n", strings.ToUpper(rep.Classification))
	fmt.Printf("  结论摘要: %s\n", rep.Summary)
	fmt.Printf("  推荐动作: %s\n\n", rep.Recommendation)
	fmt.Println("  三大核心端点连通性:")
	for _, ep := range rep.Endpoints {
		if ep.Error != "" {
			fmt.Printf("    ✖ %-44s ERR: %s\n", ep.URL, ep.Error)
		} else {
			fmt.Printf("    ✔ %-44s HTTP %d (%dms)\n", ep.URL, ep.StatusCode, ep.LatencyMs)
		}
	}
	if len(rep.Evidence) > 0 {
		fmt.Println("\n  真实日志证据:")
		for _, e := range rep.Evidence {
			fmt.Printf("    - %s\n", e)
		}
	}
	return nil
}

func cmdLeaks(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("leaks", flag.ContinueOnError)
	sinceStr := fs.String("since", "24h", "时间窗口，如 1h, 24h")
	asJSON := fs.Bool("json", false, "输出 JSON")
	clear := fs.Bool("clear", false, "清空记录")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *clear {
		if ok, _ := launcher.DaemonHealth(cfg); ok {
			_, _ = daemonPost(cfg, "/api/leaks/clear", nil)
		} else {
			audit.NewStore(false, 100).ClearLeaks()
		}
		fmt.Println("✔ 已清空泄漏审计记录")
		return nil
	}

	since, err := time.ParseDuration(*sinceStr)
	if err != nil {
		return fmt.Errorf("invalid --since %q: %w", *sinceStr, err)
	}
	var events []audit.LeakEvent
	var sockets []host.DirectSocketHit

	if ok, _ := launcher.DaemonHealth(cfg); ok {
		resp, err := http.Get(fmt.Sprintf("%s/api/leaks?since=%s&limit=200", cfg.ControlURL(), *sinceStr))
		if err == nil {
			defer resp.Body.Close()
			var out struct {
				Events  []audit.LeakEvent      `json:"events"`
				Sockets []host.DirectSocketHit `json:"host_direct_sockets"`
			}
			if json.NewDecoder(resp.Body).Decode(&out) == nil {
				events = out.Events
				sockets = out.Sockets
			}
		}
	} else {
		events = audit.NewStore(false, 100).ListLeaks(since, 200)
		sockets = host.ScanDirectAnthropicSockets(cfg.Egress.ExpectedIP)
	}

	if *asJSON {
		printJSON(map[string]any{"events": events, "host_direct_sockets": sockets})
		return nil
	}
	if len(sockets) == 0 {
		fmt.Println("✔ 宿主机 lsof 扫描正常：未发现直连 Anthropic CIDR 的活跃连接")
	} else {
		fmt.Printf("✖ 警告：检测到 %d 个直连 Anthropic Socket:\n", len(sockets))
		for _, s := range sockets {
			fmt.Printf("  - %s (PID %s) %s -> %s:%s\n", s.Process, s.PID, s.Protocol, s.DestIP, s.DestPort)
		}
	}
	fmt.Printf("\n近 %s 网关路由与拦截事件 (%d 条):\n", *sinceStr, len(events))
	for _, ev := range events {
		fmt.Printf("  [%s] %-20s %-32s %-18s %s\n",
			ev.Timestamp.Local().Format("15:04:05"), ev.Kind, ev.Host, ev.Action, ev.Reason)
	}
	return nil
}

func cmdCapture(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "status"
	rest := args
	if len(args) > 0 {
		sub = args[0]
		rest = args[1:]
	}
	running, _ := launcher.DaemonHealth(cfg)

	switch sub {
	case "on", "off":
		enabled := (sub == "on")
		if running {
			if _, err := daemonPost(cfg, "/api/capture/toggle", map[string]bool{"enabled": enabled}); err != nil {
				return err
			}
		} else {
			cfg.Capture.Enabled = enabled
			if err := config.Save(cfg); err != nil {
				return err
			}
		}
		fmt.Printf("✔ 本地脱敏抓包已%s\n", map[bool]string{true: "开启", false: "关闭"}[enabled])
		return nil
	case "status":
		st := audit.NewStore(cfg.Capture.Enabled, cfg.Capture.MaxEntries).Stats()
		fmt.Printf("脱敏抓包: %t | 已记录: %d 条 | 路径: %s\n", cfg.Capture.Enabled, st.CaptureCount, config.CaptureLogPath())
		return nil
	case "list":
		fs := flag.NewFlagSet("capture list", flag.ContinueOnError)
		limit := fs.Int("limit", 30, "返回条数")
		asJSON := fs.Bool("json", false, "输出 JSON")
		_ = fs.Parse(rest)
		var records []audit.CaptureRecord
		if running {
			resp, err := http.Get(fmt.Sprintf("%s/api/capture?limit=%d", cfg.ControlURL(), *limit))
			if err == nil {
				defer resp.Body.Close()
				var out struct {
					Records []audit.CaptureRecord `json:"records"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&out)
				records = out.Records
			}
		} else {
			records = audit.NewStore(cfg.Capture.Enabled, cfg.Capture.MaxEntries).ListCaptures(*limit)
		}
		if *asJSON {
			printJSON(records)
			return nil
		}
		for _, r := range records {
			fmt.Printf("  %-22s %s %-7s %-32s %-18s HTTP %-3d %dms\n",
				r.ID, r.Timestamp.Local().Format("15:04:05"), r.Protocol, r.Host+r.Path, r.Action, r.StatusCode, r.DurationMs)
		}
		return nil
	case "show":
		if len(rest) == 0 {
			return errors.New("用法: peaceclaude capture show <record-id>")
		}
		rec, ok := audit.NewStore(cfg.Capture.Enabled, cfg.Capture.MaxEntries).GetCapture(rest[0])
		if !ok {
			return fmt.Errorf("未找到抓包记录 %s", rest[0])
		}
		printJSON(rec)
		return nil
	case "clear":
		if running {
			_, _ = daemonPost(cfg, "/api/capture/clear", nil)
		} else {
			audit.NewStore(cfg.Capture.Enabled, cfg.Capture.MaxEntries).ClearCaptures()
		}
		fmt.Println("✔ 已清空本地抓包记录")
		return nil
	default:
		return fmt.Errorf("未知 capture 子命令 %q (可用: on|off|status|list|show|clear)", sub)
	}
}

func cmdProfile(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "list"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("profile list", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "输出 JSON")
		_ = fs.Parse(rest)
		list, err := profile.List(cfg)
		if err != nil {
			return err
		}
		if *asJSON {
			printJSON(list)
			return nil
		}
		for _, p := range list {
			mark := " "
			if p.Active {
				mark = "*"
			}
			age := p.Account.AccountAgeText
			if age == "" {
				age = "未记录"
			}
			email := p.Account.Email
			if email == "" {
				email = "-"
			}
			fmt.Printf("%s %-14s 套餐=%-8s 注册=%-12s 邮箱=%-28s CLI=%t Desktop=%t %s\n",
				mark, p.Name, p.Account.PlanLabel, age, email, p.HasCLICreds, p.HasDesktopDat, p.Note)
		}
		return nil
	case "create", "bind", "add":
		fs := flag.NewFlagSet("profile create", flag.ContinueOnError)
		note := fs.String("note", "", "备注")
		use := fs.Bool("use", false, "创建后立即设为默认")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			return errors.New("用法: peaceclaude profile create <name> [--note '...'] [--use]")
		}
		name := fs.Arg(0)
		p, err := profile.Ensure(cfg, name, *note)
		if err != nil {
			return err
		}
		if *use {
			cfg.ActiveProfile = name
			_ = config.Save(cfg)
		}
		fmt.Printf("✔ 已创建隔离档案 %q (UUID=%s)\n  CLI 目录:     %s\n  Desktop 目录: %s\n",
			p.Name, p.Fingerprint.DeviceUUID, p.CLIConfigDir, p.DesktopDir)
		return nil
	case "use":
		if len(rest) == 0 {
			return errors.New("用法: peaceclaude profile use <name>")
		}
		name := rest[0]
		if ok, _ := launcher.DaemonHealth(cfg); ok {
			if _, err := daemonPost(cfg, "/api/profiles/use", map[string]string{"name": name}); err != nil {
				return err
			}
		} else {
			if _, err := profile.Ensure(cfg, name, ""); err != nil {
				return err
			}
			cfg.ActiveProfile = name
			if err := config.Save(cfg); err != nil {
				return err
			}
		}
		fmt.Printf("✔ 默认隔离档案已切换为: %s\n", name)
		return nil
	case "delete", "rm":
		if len(rest) == 0 {
			return errors.New("用法: peaceclaude profile delete <name>")
		}
		if err := profile.Delete(cfg, rest[0]); err != nil {
			return err
		}
		fmt.Printf("✔ 已删除档案 %q\n", rest[0])
		return nil
	default:
		return fmt.Errorf("未知 profile 子命令 %q (可用: list|create|use|delete)", sub)
	}
}

func cmdRules(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "status"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	fs := flag.NewFlagSet("rules", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "输出 JSON")
	_ = fs.Parse(rest)

	var st profile.RulesStatus
	switch sub {
	case "status":
		st = profile.GetRulesStatus(cfg)
	case "on", "sync":
		st, err = profile.SyncAllRules(cfg, true)
		if err != nil {
			return err
		}
	case "off":
		st, err = profile.SyncAllRules(cfg, false)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知 rules 子命令 %q (可用: status|on|off|sync)", sub)
	}
	if *asJSON {
		printJSON(st)
		return nil
	}
	fmt.Printf("全局规则共享: %t\n共享目录: %s\n同步项: %s\n", st.Enabled, st.SharedDir, strings.Join(st.Items, ", "))
	for k, v := range st.Profiles {
		fmt.Printf("  - 档案 %-16s %s\n", k, v)
	}
	return nil
}

func cmdClean(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	profName := fs.String("profile", "", "指定隔离档案（默认当前激活档案）")
	dryRun := fs.Bool("dry-run", false, "仅盘点计划，不改动任何文件")
	yes := fs.Bool("yes", false, "确认直接执行清理")
	asJSON := fs.Bool("json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := *profName
	if name == "" {
		name = cfg.ActiveProfile
	}
	if !*dryRun && !*yes {
		fmt.Println("未传入 --yes，自动以 --dry-run 模式展示盘点结果（确认执行请追加 --yes）：")
		*dryRun = true
	}
	rep, err := profile.CleanProfile(cfg, name, *dryRun)
	if err != nil {
		return err
	}
	if *asJSON {
		printJSON(rep)
		return nil
	}
	mode := "执行完成"
	if rep.DryRun {
		mode = "DRY-RUN 盘点"
	}
	fmt.Printf("PeaceClaude 指纹清理 [%s] — 档案: %s\n", mode, rep.Profile)
	fmt.Printf("  备份路径:   %s\n", rep.BackupDir)
	fmt.Printf("  新设备UUID: %s\n", rep.NewDevice)
	fmt.Printf("  受保护保留: %s\n", strings.Join(rep.Preserved, ", "))
	fmt.Printf("  清理项 (%d): %v\n", len(rep.Removed), rep.Removed)
	if len(rep.Scrubbed) > 0 {
		fmt.Printf("  脱敏 JSON:  %v\n", rep.Scrubbed)
	}
	for _, n := range rep.Notes {
		fmt.Printf("  ※ %s\n", n)
	}
	return nil
}

func cmdVMProxy(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "status"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	fs := flag.NewFlagSet("vm-proxy", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "输出 JSON")
	_ = fs.Parse(rest)

	switch sub {
	case "status":
		st := host.InspectVMProxy(cfg)
		if *asJSON {
			printJSON(st)
			return nil
		}
		fmt.Printf("macOS pf 防直连护盾与 Cowork vm-proxy 状态:\n")
		fmt.Printf("  状态:         %s\n", st.ActivePFNote)
		fmt.Printf("  pf 语法校验:  %t (%s)\n", st.SyntaxValid, st.SyntaxDetail)
		fmt.Printf("  Cowork 开关:  %t (允许子网: %s)\n", st.ConfigEnabled, strings.Join(st.Subnets, ", "))
		fmt.Printf("  Cowork 网桥:  %t (%s)\n", st.CoworkBridgeActive, nonEmpty(st.CoworkBridgeIP, "未活跃"))
		fmt.Printf("  规则文件:     %s\n", st.RenderedAnchorPath)
		fmt.Printf("  安装命令:     sudo bash %s\n", st.InstallScriptPath)
		fmt.Printf("  卸载命令:     sudo bash %s\n", st.UninstallScriptPath)
		return nil
	case "render":
		fmt.Print(host.RenderPFAnchor(cfg))
		return nil
	case "on", "off":
		cfg.Listen.VMProxyEnabled = (sub == "on")
		if err := config.Save(cfg); err != nil {
			return err
		}
		st := host.InspectVMProxy(cfg)
		fmt.Printf("✔ Cowork vm-proxy 已%s，pf 规则已重新生成 (%s)\n  若守护进程正在运行，请执行 `peaceclaude service restart` 应用绑定更新。\n",
			map[bool]string{true: "开启", false: "关闭"}[cfg.Listen.VMProxyEnabled], st.RenderedAnchorPath)
		return nil
	case "install":
		st := host.InspectVMProxy(cfg)
		if !st.SyntaxValid {
			return fmt.Errorf("pf 规则语法校验失败: %s", st.SyntaxDetail)
		}
		if os.Geteuid() == 0 {
			cmd := exec.Command("bash", st.InstallScriptPath)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
		fmt.Printf("安装 macOS pf 内核级防直连护盾需要 root 权限（语法已通过 pfctl -n 校验，绝不清空飞连规则）：\n\n  sudo bash %s\n", st.InstallScriptPath)
		return nil
	case "uninstall":
		st := host.InspectVMProxy(cfg)
		if os.Geteuid() == 0 {
			cmd := exec.Command("bash", st.UninstallScriptPath)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
		fmt.Printf("卸载 PeaceClaude pf anchor 需要 root 权限：\n\n  sudo bash %s\n", st.UninstallScriptPath)
		return nil
	default:
		return fmt.Errorf("未知 vm-proxy 子命令 %q (可用: status|render|on|off|install|uninstall)", sub)
	}
}

func cmdConfig(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	sub := "show"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}

	switch sub {
	case "show":
		printJSON(cfg.Redacted())
		return nil
	case "set-upstream":
		fs := flag.NewFlagSet("config set-upstream", flag.ContinueOnError)
		mode := fs.String("mode", "direct-socks5", "local-proxy | direct-socks5 | direct-http")
		addr := fs.String("addr", "", "代理地址 host:port (如 198.51.100.10:6331)")
		user := fs.String("user", "", "代理用户名")
		pass := fs.String("pass", "", "代理密码")
		expectedIP := fs.String("expected-ip", "", "锁定静态出口 IP")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *addr == "" {
			return errors.New("用法: peaceclaude config set-upstream --mode direct-socks5 --addr HOST:PORT [--user U --pass P] [--expected-ip IP]")
		}
		cfg.Upstream.Mode = *mode
		cfg.Upstream.Address = *addr
		if *user != "" {
			cfg.Upstream.Username = *user
		}
		if *pass != "" {
			cfg.Upstream.Password = *pass
		}
		if *expectedIP != "" {
			cfg.Egress.ExpectedIP = *expectedIP
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("✔ 已更新上游链路: %s://%s (expected_ip=%s)\n  若守护进程运行中，请执行 `peaceclaude service restart` 生效。\n",
			cfg.Upstream.Mode, cfg.Upstream.Address, nonEmpty(cfg.Egress.ExpectedIP, "未锁定"))
		return nil
	case "import-clash":
		fs := flag.NewFlagSet("config import-clash", flag.ContinueOnError)
		file := fs.String("file", "", "Clash YAML 路径（留空自动查找本机 Clash Verge / sealed canonical.yaml）")
		nodeName := fs.String("node", "Claude-US-1", "要导入的节点名")
		apply := fs.Bool("apply", false, "实际写入配置（默认只预览节点摘要，绝不打印密码）")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		yamlPath := *file
		if yamlPath == "" {
			home, _ := os.UserHomeDir()
			for _, cand := range []string{
				filepath.Join(home, ".local/share/claude-egress/canonical.yaml"),
				filepath.Join(home, "Library/Application Support/io.github.clash-verge-rev.clash-verge-rev/clash-verge.yaml"),
			} {
				if _, err := os.Stat(cand); err == nil {
					yamlPath = cand
					break
				}
			}
		}
		if yamlPath == "" {
			return errors.New("未自动找到 Clash YAML，请通过 --file 指定路径")
		}
		node, err := config.ImportClashNodeFromFile(yamlPath, *nodeName)
		if err != nil {
			return err
		}
		fmt.Printf("检测到节点: %s\n", node.Summary())
		if node.DialerProxy != "" {
			fmt.Printf("⚠ 注意：该节点配置了前置跳板 dialer-proxy=%s。\n  如果在当前网络环境下无法直连 %s:%s，建议保持 local-proxy (127.0.0.1:7897) 复用 Clash 链式转发。\n",
				node.DialerProxy, node.Server, node.Port)
		}
		if !*apply {
			fmt.Println("（预览模式，未修改配置；如需切换为直连该节点请追加 `--apply`）")
			return nil
		}
		if node.Type == "socks5" {
			cfg.Upstream.Mode = "direct-socks5"
		} else {
			cfg.Upstream.Mode = "direct-http"
		}
		cfg.Upstream.Address = node.Server + ":" + node.Port
		cfg.Upstream.Username = node.Username
		cfg.Upstream.Password = node.Password
		if cfg.Egress.ExpectedIP == "" {
			cfg.Egress.ExpectedIP = node.Server
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("✔ 已导入节点 %q 为独立上游 (%s://%s)，凭据已安全写入 0600 配置文件。\n",
			node.Name, cfg.Upstream.Mode, cfg.Upstream.Address)
		return nil
	case "gateway":
		fmt.Printf("正在测速并验证上游链路: %s://%s ...\n", cfg.Upstream.Mode, cfg.Upstream.Address)
		g := egress.NewGate(cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		res := g.Check(ctx, false)
		cancel()
		if !res.OK {
			return fmt.Errorf("上游验证失败: %s", res.FailureReason)
		}
		fmt.Printf("✔ Claude 出口 IP: %s (%s · %s · AS%d · %s)\n", res.EgressIP, res.Country, res.City, res.ASN, res.ISP)
		fmt.Printf("✔ 探测延迟:       %dms (来源 %s)\n", res.LatencyMs, res.Source)
		fmt.Printf("✔ 建议同步时区:   %s\n", g.Snapshot().EffectiveTimezone)
		if res.SplitIPDetected {
			fmt.Printf("⚠ Split-IP 检测:  泛公网出口=%s 与 Claude 出口=%s 不一致！\n", res.PublicSplitIP, res.EgressIP)
		} else {
			fmt.Printf("✔ Split-IP 检测:  出口一致，无分流泄漏\n")
		}
		// Quick OAuth TLS check
		d := upstream.FromConfig(cfg.Upstream)
		client := d.NewHTTPClient(8 * time.Second)
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
		start := time.Now()
		resp, err := client.Get("https://platform.claude.com/oauth/authorize")
		if err != nil {
			fmt.Printf("⚠ OAuth 授权页:   TLS 失败 (%v)\n", err)
		} else {
			_ = resp.Body.Close()
			fmt.Printf("✔ OAuth 授权页:   HTTP %d (%dms)\n", resp.StatusCode, time.Since(start).Milliseconds())
		}
		return nil
	case "set":
		if len(rest) < 2 {
			return errors.New("用法: peaceclaude config set <expected_ip|strict_split|telemetry_shield|auto_peace_desktop|auto_start_launch_agent|bypass_vpn|outbound_interface|cli_path|timezone|language> <value>")
		}
		k, v := rest[0], rest[1]
		switch k {
		case "expected_ip":
			cfg.Egress.ExpectedIP = v
		case "strict_split":
			cfg.Egress.StrictDomainIsolation = (v == "true" || v == "1" || v == "on")
		case "telemetry_shield":
			cfg.Shield.TelemetryShield = (v == "true" || v == "1" || v == "on")
		case "auto_peace_desktop":
			cfg.Identity.AutoPeaceDesktop = (v == "true" || v == "1" || v == "on")
		case "auto_start_launch_agent":
			b := (v == "true" || v == "1" || v == "on")
			cfg.Listen.AutoStartLaunchAgent = &b
		case "bypass_vpn":
			b := (v == "true" || v == "1" || v == "on")
			cfg.Upstream.BypassVPN = &b
		case "outbound_interface":
			clean := strings.TrimSpace(v)
			if strings.EqualFold(clean, "auto") || clean == "-" {
				clean = ""
			}
			cfg.Upstream.OutboundInterface = clean
		case "cli_path":
			cfg.Binaries.ClaudeCLIPath = v
		case "timezone":
			cfg.Identity.Timezone = v
		case "language":
			cfg.Identity.Language = v
		default:
			return fmt.Errorf("不支持的配置项 %q", k)
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		fmt.Printf("✔ 已更新 %s = %s\n", k, v)
		return nil
	default:
		return fmt.Errorf("未知 config 子命令 %q (可用: show|set-upstream|import-clash|gateway|set)", sub)
	}
}

func cmdLock(locked bool, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	reason := strings.Join(args, " ")
	if locked && reason == "" {
		reason = "CLI manual emergency lock"
	}
	raw, err := daemonPost(cfg, "/api/lock", map[string]any{"locked": locked, "reason": reason})
	if err != nil {
		return err
	}
	if locked {
		fmt.Printf("✔ 已触发紧急熔断（Fail-Closed 已锁定，所有 Claude 连接已切断）\n%s\n", string(raw))
	} else {
		fmt.Printf("✔ 已解除手动熔断并完成重检\n%s\n", string(raw))
	}
	return nil
}

func cmdLogs(args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	n := fs.Int("n", 60, "显示最后 N 行")
	_ = fs.Parse(args)
	raw, err := os.ReadFile(config.DaemonLogPath())
	if err != nil {
		return fmt.Errorf("暂无日志 (%s)", config.DaemonLogPath())
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) > *n {
		lines = lines[len(lines)-*n:]
	}
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	noLaunchd := fs.Bool("no-launchd", false, "跳过安装 LaunchAgent 开机自启")
	noApp := fs.Bool("no-app", false, "跳过安装 PeaceClaude.app 启动器")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Println("→ 初始化隔离目录与默认档案…")
	if _, err := profile.Ensure(cfg, cfg.ActiveProfile, "Default isolated profile"); err != nil {
		return err
	}
	if copied, err := profile.SeedSharedRulesFromHost(); err == nil && len(copied) > 0 {
		fmt.Printf("✔ 已从宿主机 ~/.claude 安全导入共享规则项: %s\n", strings.Join(copied, ", "))
	}
	if cfg.Rules.SharedEnabled {
		_, _ = profile.SyncAllRules(cfg, true)
	}

	self := selfBinary()
	if !*noApp {
		fmt.Println("→ 生成 macOS 隔离桌面启动器…")
		appPath, err := launcher.InstallDesktopLauncherApp(self)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ 生成桌面启动器失败: %v\n", err)
		} else {
			fmt.Printf("✔ 隔离桌面启动器已就绪: %s\n", appPath)
		}
	}

	fmt.Println("→ 生成并校验 macOS pf 防直连护盾脚本…")
	vp := host.InspectVMProxy(cfg)
	fmt.Printf("✔ pf 规则已生成 (%s) | 语法校验: %t\n", vp.RenderedAnchorPath, vp.SyntaxValid)

	fmt.Println("→ 启动 PeaceClaude 本地 Fail-Closed 安全网关…")
	if err := launcher.StartService(cfg, self, !*noLaunchd); err != nil {
		return err
	}
	st := launcher.GetServiceStatus(cfg)
	fmt.Printf("\n✔ PeaceClaude 安装与初始化完成！\n")
	fmt.Printf("  - 本地 Web 控制台:  %s\n", st.ControlURL)
	fmt.Printf("  - 安全代理端口:     HTTP %s | SOCKS5 %s\n", st.HTTPProxyURL, st.SOCKS5ProxyURL)
	fmt.Printf("  - 启动隔离 CLI:     peaceclaude cli\n")
	fmt.Printf("  - 启动隔离 Desktop: 双击 PeaceClaude.app 或运行 `peaceclaude app`\n")
	fmt.Printf("  - 运行安全体检:     peaceclaude doctor\n")
	return nil
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge-data", false, "同时删除 ~/.peaceclaude 全部档案和日志")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, _ := config.Load()
	if cfg != nil {
		_ = launcher.UninstallLaunchAgent(cfg)
	}
	if appPath := host.FindLauncherAppPath(); appPath != "" {
		_ = os.RemoveAll(appPath)
		fmt.Printf("✔ 已移除启动器: %s\n", appPath)
	}
	if *purge {
		_ = os.RemoveAll(config.BaseDir())
		fmt.Printf("✔ 已清除数据目录: %s\n", config.BaseDir())
	} else {
		fmt.Printf("ℹ 已保留你的配置与隔离档案目录: %s\n", config.BaseDir())
	}
	fmt.Println("✔ PeaceClaude 服务与桌面入口已卸载。")
	return nil
}

func daemonPost(cfg *config.Config, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, cfg.ControlURL()+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接守护进程 (%s): %w（请先运行 `peaceclaude service start`）", cfg.ControlURL(), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
