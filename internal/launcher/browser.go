package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		pf := os.Getenv("ProgramFiles")
		pfx86 := os.Getenv("ProgramFiles(x86)")
		localApp := os.Getenv("LocalAppData")
		candidates = []string{
			filepath.Join(pf, "Google/Chrome/Application/chrome.exe"),
			filepath.Join(pfx86, "Google/Chrome/Application/chrome.exe"),
			filepath.Join(localApp, "Google/Chrome/Application/chrome.exe"),
			filepath.Join(pfx86, "Microsoft/Edge/Application/msedge.exe"),
			filepath.Join(pf, "Microsoft/Edge/Application/msedge.exe"),
			filepath.Join(pf, "BraveSoftware/Brave-Browser/Application/brave.exe"),
		}
	case "linux":
		for _, binName := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "brave-browser", "microsoft-edge"} {
			if p, err := exec.LookPath(binName); err == nil {
				return p
			}
		}
	default:
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Arc.app/Contents/MacOS/Arc",
		}
	}
	for _, c := range candidates {
		if strings.TrimSpace(c) == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func supportsCDPShieldBrowser(browserBin string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(browserBin)))
	if base == "" || strings.Contains(base, "arc") {
		return false
	}
	return strings.Contains(base, "chrome") ||
		strings.Contains(base, "chromium") ||
		strings.Contains(base, "brave") ||
		strings.Contains(base, "edge")
}

func normalizeBrowserLanguage(lang string) (primary, baseLang, acceptHeader string) {
	primary = strings.TrimSpace(lang)
	if primary == "" {
		primary = "en-US"
	}
	baseLang, _, _ = strings.Cut(primary, "-")
	baseLang = strings.ToLower(strings.TrimSpace(baseLang))
	if baseLang == "" {
		baseLang = "en"
	}
	switch {
	case strings.EqualFold(primary, baseLang):
		acceptHeader = primary
	case baseLang == "en":
		acceptHeader = primary + ",en"
	default:
		acceptHeader = primary + "," + baseLang + ",en"
	}
	return primary, baseLang, acceptHeader
}

const (
	isolatedShieldExtName   = "PeaceClaude Identity Shield"
	isolatedShieldStateFile = "peace-shield-state.json"
)

type BrowserLaunchResult struct {
	Browser       string   `json:"browser"`
	PID           int      `json:"pid"`
	Args          []string `json:"args"`
	ShieldLoaded  bool     `json:"shield_loaded"`
	ShieldExtID   string   `json:"shield_ext_id,omitempty"`
	ShieldHealed  bool     `json:"shield_healed,omitempty"`
	ShieldWarning string   `json:"shield_warning,omitempty"`
}

type RunningBrowserInfo struct {
	PID          int
	HasDebugFlag bool
	DebugPort    int
}

type IsolatedBrowserProfileHealth struct {
	Initialized       bool     `json:"initialized"`
	HasSecurePrefs    bool     `json:"has_secure_prefs"`
	ExtArtifactsReady bool     `json:"ext_artifacts_ready"`
	ExtLoaded         bool     `json:"ext_loaded"`
	ExtEnabled        bool     `json:"ext_enabled"`
	PrefsClean        bool     `json:"prefs_clean"`
	RuntimeStatePID   int      `json:"runtime_state_pid,omitempty"`
	RuntimeStateExtID string   `json:"runtime_state_ext_id,omitempty"`
	Issues            []string `json:"issues,omitempty"`
}

type shieldRuntimeState struct {
	PID       int    `json:"pid"`
	DebugPort int    `json:"debug_port"`
	ExtID     string `json:"ext_id"`
	LoadedAt  string `json:"loaded_at"`
}

func saveShieldRuntimeState(browserDir string, pid, debugPort int, extID string) {
	if strings.TrimSpace(browserDir) == "" || pid <= 0 || strings.TrimSpace(extID) == "" {
		return
	}
	raw, err := json.MarshalIndent(shieldRuntimeState{
		PID:       pid,
		DebugPort: debugPort,
		ExtID:     extID,
		LoadedAt:  time.Now().UTC().Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return
	}
	_ = writeFileIfChanged(filepath.Join(browserDir, isolatedShieldStateFile), append(raw, '\n'), 0o600)
}

func clearShieldRuntimeState(browserDir string) {
	if strings.TrimSpace(browserDir) == "" {
		return
	}
	_ = os.Remove(filepath.Join(browserDir, isolatedShieldStateFile))
}

func readShieldRuntimeState(browserDir string) (shieldRuntimeState, bool) {
	var st shieldRuntimeState
	if strings.TrimSpace(browserDir) == "" {
		return st, false
	}
	raw, err := os.ReadFile(filepath.Join(browserDir, isolatedShieldStateFile))
	if err != nil || len(raw) == 0 {
		return st, false
	}
	if json.Unmarshal(raw, &st) != nil || st.PID <= 0 || strings.TrimSpace(st.ExtID) == "" {
		return shieldRuntimeState{}, false
	}
	return st, true
}

func extractRemoteDebuggingPort(cmdLine string) int {
	for _, field := range strings.Fields(cmdLine) {
		clean := strings.Trim(field, `"'`)
		if strings.HasPrefix(strings.ToLower(clean), "--remote-debugging-port=") {
			_, rawPort, _ := strings.Cut(clean, "=")
			if p, err := strconv.Atoi(strings.TrimSpace(rawPort)); err == nil && p > 0 && p <= 65535 {
				return p
			}
		}
	}
	return 0
}

// LaunchLoginBrowser opens an isolated Chromium/Chrome browser profile routed strictly
// through PeaceClaude's static-IP proxy with matching TZ & en-US identity shielding for clean OAuth login.
func LaunchLoginBrowser(ctx context.Context, cfg *config.Config, selfBin, profileName, targetURL string, dryRun bool) (string, []string, error) {
	res, err := LaunchLoginBrowserDetailed(ctx, cfg, selfBin, profileName, targetURL, dryRun)
	if res == nil {
		return "", nil, err
	}
	return res.Browser, res.Args, err
}

// LaunchLoginBrowserWithPID opens an isolated login browser with hardened language/privacy preferences,
// loads the Identity Shield MV3 extension over loopback CDP on Chromium browsers,
// navigates directly to targetURL without attaching Runtime hooks, and focuses its window.
func LaunchLoginBrowserWithPID(ctx context.Context, cfg *config.Config, selfBin, profileName, targetURL string, dryRun bool) (string, int, []string, error) {
	res, err := LaunchLoginBrowserDetailed(ctx, cfg, selfBin, profileName, targetURL, dryRun)
	if res == nil {
		return "", 0, nil, err
	}
	return res.Browser, res.PID, res.Args, err
}

// LaunchLoginBrowserDetailed opens or reuses an isolated login browser profile, verifying and hot-healing
// the Identity Shield MV3 extension over loopback CDP so existing tabs stay intact whenever possible.
func LaunchLoginBrowserDetailed(ctx context.Context, cfg *config.Config, selfBin, profileName, targetURL string, dryRun bool) (*BrowserLaunchResult, error) {
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	if targetURL == "" {
		targetURL = "https://claude.ai/login"
	}
	prof, err := profile.Ensure(cfg, profileName, "")
	if err != nil {
		return nil, err
	}
	gate, err := Preflight(ctx, cfg, selfBin, dryRun)
	if err != nil {
		return nil, err
	}
	browserBin := DetectLoginBrowser(cfg)
	if browserBin == "" {
		return nil, fmt.Errorf("未找到受支持的 Chromium 内核浏览器（Chrome/Brave/Edge/Arc），请通过 config.binaries.login_browser_app 指定")
	}

	rawLang := cfg.Identity.Language
	if prof.Language != "" {
		rawLang = prof.Language
	}
	lang, _, acceptLangs := normalizeBrowserLanguage(rawLang)
	useCDPShield := supportsCDPShieldBrowser(browserBin)

	extDir, err := ensureBrowserShieldExtension(prof.BrowserDir, lang)
	if err != nil {
		return nil, fmt.Errorf("生成隔离浏览器指纹防护扩展失败: %w", err)
	}

	baseArgs := []string{
		"--user-data-dir=" + prof.BrowserDir,
	}
	baseArgs = append(baseArgs, BuildLoginBrowserProxyArgs(cfg)...)
	baseArgs = append(baseArgs,
		"--lang="+lang,
		"--accept-lang="+acceptLangs,
		"--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--no-first-run",
		"--no-default-browser-check",
	)

	if dryRun {
		if err := ensureBrowserPrivacyPrefs(prof.BrowserDir, lang); err != nil {
			return nil, fmt.Errorf("写入隔离浏览器隐私首选项失败: %w", err)
		}
		dryArgs := append([]string{}, baseArgs...)
		if useCDPShield {
			dryArgs = append(dryArgs, "--enable-unsafe-extension-debugging")
		}
		dryArgs = append(dryArgs, targetURL)
		return &BrowserLaunchResult{
			Browser:      browserBin,
			Args:         dryArgs,
			ShieldLoaded: useCDPShield,
		}, nil
	}

	_ = config.EnsureDirs()
	logPath := filepath.Join(config.LogsDir(), "browser-launcher.log")

	running := inspectRunningBrowserInfo(prof.BrowserDir)
	if running.PID > 0 {
		health := InspectIsolatedBrowserProfile(prof.BrowserDir)
		if useCDPShield && running.HasDebugFlag && running.DebugPort > 0 && health.PrefsClean {
			// Always perform an idempotent loadUnpacked refresh when reusing a running instance so
			// Chrome 158+ cannot silently drop the unpacked extension across sleep/updates and the latest
			// shield.js rules take effect immediately without closing existing login tabs.
			needHeal := running.PID != health.RuntimeStatePID || !health.ExtEnabled
			healCtx, healCancel := context.WithTimeout(ctx, 5*time.Second)
			extID, healErr := cdpLoadUnpackedExtension(healCtx, running.DebugPort, extDir)
			healCancel()
			if healErr == nil {
				saveShieldRuntimeState(prof.BrowserDir, running.PID, running.DebugPort, extID)
				tabCtx, tabCancel := context.WithTimeout(ctx, 4*time.Second)
				tabErr := cdpOpenNewTab(tabCtx, running.DebugPort, targetURL)
				tabCancel()
				if tabErr != nil {
					openArgs := append(append([]string{}, baseArgs...), targetURL)
					cmd := exec.Command(browserBin, openArgs...)
					cmd.Env = BuildIsolatedEnv(cfg, prof, gate, os.Environ())
					_ = cmd.Start()
				}
				focusProcessWindow(running.PID)
				return &BrowserLaunchResult{
					Browser:      browserBin,
					PID:          running.PID,
					Args:         baseArgs,
					ShieldLoaded: true,
					ShieldExtID:  extID,
					ShieldHealed: needHeal,
				}, nil
			}
			appendBrowserLog(logPath, fmt.Sprintf("CDP hot-heal failed for pid=%d port=%d, restarting: %v", running.PID, running.DebugPort, healErr))
		}
		if useCDPShield {
			// Stop unhardened or dirty-preference instance BEFORE rewriting Preferences so Chrome shutdown does not overwrite disk.
			TerminatePIDs([]string{strconv.Itoa(running.PID)}, 3*time.Second)
			clearShieldRuntimeState(prof.BrowserDir)
			running = RunningBrowserInfo{}
		} else {
			openArgs := append(append([]string{}, baseArgs...), targetURL)
			cmd := exec.Command(browserBin, openArgs...)
			cmd.Env = BuildIsolatedEnv(cfg, prof, gate, os.Environ())
			_ = cmd.Start()
			focusProcessWindow(running.PID)
			return &BrowserLaunchResult{
				Browser:       browserBin,
				PID:           running.PID,
				Args:          openArgs,
				ShieldLoaded:  false,
				ShieldWarning: fmt.Sprintf("%s 不支持无感 CDP 扩展注入，请安装「原生浏览器护盾」确保 2/100 低风险", filepath.Base(browserBin)),
			}, nil
		}
	}

	if err := ensureBrowserPrivacyPrefs(prof.BrowserDir, lang); err != nil {
		return nil, fmt.Errorf("写入隔离浏览器隐私首选项失败: %w", err)
	}
	clearShieldRuntimeState(prof.BrowserDir)

	var startOffset int64
	if fi, serr := os.Stat(logPath); serr == nil {
		startOffset = fi.Size()
	}

	cdpPort := 0
	if useCDPShield {
		if p, perr := pickLoopbackPort(); perr == nil {
			cdpPort = p
		} else {
			appendBrowserLog(logPath, fmt.Sprintf("pickLoopbackPort failed: %v", perr))
		}
	}

	launchArgs := append([]string{}, baseArgs...)
	if cdpPort > 0 {
		launchArgs = append(launchArgs,
			fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
			"--enable-unsafe-extension-debugging",
			"about:blank",
		)
	} else {
		launchArgs = append(launchArgs, targetURL)
	}

	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	cmd := exec.Command(browserBin, launchArgs...)
	cmd.Env = BuildIsolatedEnv(cfg, prof, gate, os.Environ())
	if logFile != nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	setDetachedProcessAttr(cmd)
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return &BrowserLaunchResult{Browser: browserBin, Args: launchArgs}, fmt.Errorf("launch isolated browser (%s): %w", filepath.Base(browserBin), err)
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	pid := cmd.Process.Pid

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	if cdpPort == 0 {
		warnMsg := ""
		if !useCDPShield {
			warnMsg = fmt.Sprintf("%s 不支持无感 CDP 扩展注入，请安装「原生浏览器护盾」确保 2/100 低风险", filepath.Base(browserBin))
		} else {
			warnMsg = "无法分配本地调试端口，指纹护盾扩展未注入"
		}
		select {
		case exitErr := <-waitCh:
			detail := readLogTailFrom(logPath, startOffset, 4096)
			if exitErr == nil {
				if existing := inspectRunningBrowserInfo(prof.BrowserDir); existing.PID > 0 {
					focusProcessWindow(existing.PID)
					return &BrowserLaunchResult{Browser: browserBin, PID: existing.PID, Args: launchArgs, ShieldWarning: warnMsg}, nil
				}
				return &BrowserLaunchResult{Browser: browserBin, PID: pid, Args: launchArgs, ShieldWarning: warnMsg}, nil
			}
			return &BrowserLaunchResult{Browser: browserBin, Args: launchArgs}, fmt.Errorf("隔离浏览器 (%s) 启动后退出 (%v): %s", filepath.Base(browserBin), exitErr, detail)
		case <-time.After(700 * time.Millisecond):
			focusProcessWindow(pid)
			return &BrowserLaunchResult{Browser: browserBin, PID: pid, Args: launchArgs, ShieldWarning: warnMsg}, nil
		}
	}

	// Watch browser exit once across both CDP extension loading and initial navigation.
	watchCtx, watchCancel := context.WithTimeout(ctx, 20*time.Second)
	defer watchCancel()
	doneWatch := make(chan struct{})
	defer close(doneWatch)
	go func() {
		select {
		case err := <-waitCh:
			if err != nil {
				watchCancel()
			}
		case <-doneWatch:
		}
	}()

	loadCtx, loadCancel := context.WithTimeout(watchCtx, 12*time.Second)
	extID, extErr := cdpLoadUnpackedExtension(loadCtx, cdpPort, extDir)
	loadCancel()

	shieldLoaded := false
	shieldWarning := ""
	if extErr != nil {
		appendBrowserLog(logPath, fmt.Sprintf("CDP loadUnpacked failed (%s): %v", extDir, extErr))
		shieldWarning = fmt.Sprintf("指纹护盾加载异常 (%v)，请关闭此窗口后重试或运行安全检测一键修复", extErr)
	} else {
		shieldLoaded = true
	}

	navCtx, navCancel := context.WithTimeout(watchCtx, 8*time.Second)
	if navErr := cdpNavigateFirstPage(navCtx, cdpPort, targetURL); navErr != nil {
		appendBrowserLog(logPath, fmt.Sprintf("CDP navigate fallback (%s): %v", targetURL, navErr))
		fallbackArgs := append(append([]string{}, baseArgs...), targetURL)
		fbCmd := exec.Command(browserBin, fallbackArgs...)
		fbCmd.Env = BuildIsolatedEnv(cfg, prof, gate, os.Environ())
		_ = fbCmd.Start()
	}
	navCancel()

	if cur := inspectRunningBrowserInfo(prof.BrowserDir); cur.PID > 0 {
		pid = cur.PID
	}
	if shieldLoaded && extID != "" {
		saveShieldRuntimeState(prof.BrowserDir, pid, cdpPort, extID)
	}
	focusProcessWindow(pid)
	return &BrowserLaunchResult{
		Browser:       browserBin,
		PID:           pid,
		Args:          launchArgs,
		ShieldLoaded:  shieldLoaded,
		ShieldExtID:   extID,
		ShieldWarning: shieldWarning,
	}, nil
}

func appendBrowserLog(logPath, msg string) {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), msg)
}

func inspectRunningBrowser(userDataDir string) (int, bool) {
	info := inspectRunningBrowserInfo(userDataDir)
	return info.PID, info.HasDebugFlag
}

func inspectRunningBrowserInfo(userDataDir string) RunningBrowserInfo {
	want := filepath.Clean(userDataDir)
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return RunningBrowserInfo{}
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
				return RunningBrowserInfo{
					PID:          pid,
					HasDebugFlag: strings.Contains(cmdLine, "--enable-unsafe-extension-debugging"),
					DebugPort:    extractRemoteDebuggingPort(cmdLine),
				}
			}
		}
	}
	return RunningBrowserInfo{}
}

// InspectIsolatedBrowserProfile checks a profile's isolated Chromium directory for privacy
// preferences (non-CN intl, DNT, blocked geolocation) and Identity Shield extension presence.
func InspectIsolatedBrowserProfile(browserDir string) IsolatedBrowserProfileHealth {
	h := IsolatedBrowserProfileHealth{PrefsClean: true}
	if strings.TrimSpace(browserDir) == "" {
		return h
	}
	defDir := filepath.Join(browserDir, "Default")
	prefsPath := filepath.Join(defDir, "Preferences")
	secPrefsPath := filepath.Join(defDir, "Secure Preferences")
	extDir := filepath.Join(browserDir, "peace-shield-ext")

	prefsRaw, prefsErr := os.ReadFile(prefsPath)
	secRaw, secErr := os.ReadFile(secPrefsPath)
	_, mfErr := os.Stat(filepath.Join(extDir, "manifest.json"))
	_, swErr := os.Stat(filepath.Join(extDir, "background.js"))
	_, shErr := os.Stat(filepath.Join(extDir, "shield.js"))
	h.HasSecurePrefs = secErr == nil && len(secRaw) > 0
	h.ExtArtifactsReady = mfErr == nil && swErr == nil && shErr == nil
	h.Initialized = (prefsErr == nil && len(prefsRaw) > 0) || h.HasSecurePrefs

	if st, ok := readShieldRuntimeState(browserDir); ok {
		h.RuntimeStatePID = st.PID
		h.RuntimeStateExtID = st.ExtID
	}

	if prefsErr == nil && len(prefsRaw) > 0 {
		var prefs map[string]any
		if json.Unmarshal(prefsRaw, &prefs) == nil {
			intl, _ := prefs["intl"].(map[string]any)
			acc, _ := intl["accept_languages"].(string)
			sel, _ := intl["selected_languages"].(string)
			accLow := strings.ToLower(strings.TrimSpace(acc))
			selLow := strings.ToLower(strings.TrimSpace(sel))
			if strings.Contains(accLow, "zh") || strings.Contains(selLow, "zh") || accLow == "" || selLow == "" {
				h.PrefsClean = false
				h.Issues = append(h.Issues, "Preferences 含中文/空白语言残留")
			}
			if dnt, _ := prefs["enable_do_not_track"].(bool); !dnt {
				h.PrefsClean = false
				h.Issues = append(h.Issues, "未启用 Do Not Track")
			}
			profMap, _ := prefs["profile"].(map[string]any)
			cs, _ := profMap["default_content_setting_values"].(map[string]any)
			if geo, ok := cs["geolocation"].(float64); !ok || int(geo) != 2 {
				h.PrefsClean = false
				h.Issues = append(h.Issues, "未禁止地理定位")
			}
		}
	}

	cleanExt := filepath.Clean(extDir)
	for _, raw := range [][]byte{secRaw, prefsRaw} {
		if len(raw) == 0 {
			continue
		}
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		exts, _ := doc["extensions"].(map[string]any)
		settings, _ := exts["settings"].(map[string]any)
		for _, val := range settings {
			entry, ok := val.(map[string]any)
			if !ok {
				continue
			}
			extPath, _ := entry["path"].(string)
			cleanPath := filepath.Clean(strings.TrimSpace(extPath))
			manifest, _ := entry["manifest"].(map[string]any)
			mName, _ := manifest["name"].(string)
			matchPath := extPath != "" && (cleanPath == cleanExt || strings.HasSuffix(cleanPath, string(filepath.Separator)+"peace-shield-ext"))
			matchName := strings.EqualFold(strings.TrimSpace(mName), isolatedShieldExtName)
			if !matchPath && !matchName {
				continue
			}
			h.ExtLoaded = true
			if isChromiumExtensionEntryEnabled(entry) {
				h.ExtEnabled = true
			}
		}
	}

	if h.Initialized && !h.ExtArtifactsReady {
		h.Issues = append(h.Issues, "护盾扩展文件缺失或待更新")
	}
	if h.HasSecurePrefs && !h.ExtEnabled {
		if h.ExtLoaded {
			h.Issues = append(h.Issues, "护盾扩展已被禁用")
		} else {
			h.Issues = append(h.Issues, "护盾扩展未加载")
		}
	}
	return h
}

// RepairIsolatedBrowserShields repairs stale Chinese Preferences on idle isolated browser profiles,
// updates peace-shield-ext artifacts across all initialized profiles, and hot-reloads running Chrome instances.
func RepairIsolatedBrowserShields(ctx context.Context, cfg *config.Config) (repaired []string, healedRunning []string, warnings []string) {
	if cfg == nil {
		return nil, nil, nil
	}
	profs, err := profile.List(cfg)
	if err != nil {
		return nil, nil, []string{err.Error()}
	}
	for _, prof := range profs {
		if strings.TrimSpace(prof.BrowserDir) == "" {
			continue
		}
		health := InspectIsolatedBrowserProfile(prof.BrowserDir)
		_, extStatErr := os.Stat(filepath.Join(prof.BrowserDir, "peace-shield-ext"))
		if !health.Initialized && extStatErr != nil {
			continue
		}
		rawLang := cfg.Identity.Language
		if prof.Language != "" {
			rawLang = prof.Language
		}
		lang, _, _ := normalizeBrowserLanguage(rawLang)
		extDir, extErr := ensureBrowserShieldExtension(prof.BrowserDir, lang)
		if extErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: 更新护盾扩展失败 (%v)", prof.Name, extErr))
			continue
		}

		running := inspectRunningBrowserInfo(prof.BrowserDir)
		if running.PID == 0 {
			if err := ensureBrowserPrivacyPrefs(prof.BrowserDir, lang); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: 清理隐私首选项失败 (%v)", prof.Name, err))
				continue
			}
			clearShieldRuntimeState(prof.BrowserDir)
			repaired = append(repaired, prof.Name)
			continue
		}

		// Browser is currently running: hot-load latest extension via CDP without killing active tabs.
		if running.HasDebugFlag && running.DebugPort > 0 {
			healCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			extID, cerr := cdpLoadUnpackedExtension(healCtx, running.DebugPort, extDir)
			cancel()
			if cerr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: 运行中浏览器热修复失败 (%v)，请关闭该浏览器窗口后重开", prof.Name, cerr))
				continue
			}
			saveShieldRuntimeState(prof.BrowserDir, running.PID, running.DebugPort, extID)
			healedRunning = append(healedRunning, prof.Name)
			if !health.PrefsClean {
				warnings = append(warnings, fmt.Sprintf("%s: 已热重载指纹护盾，但磁盘 Preferences 含中文残留，请在方便时关闭窗口重开一次彻底刷新", prof.Name))
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("%s: 浏览器正在运行但未开启调试加载端口，请点击对应档案「隔离浏览器」以重启接入护盾", prof.Name))
		}
	}
	return repaired, healedRunning, warnings
}

func ensureBrowserDNSPrivacyPrefs(browserDir string) error {
	return ensureBrowserPrivacyPrefs(browserDir, "en-US")
}

func ensureBrowserPrivacyPrefs(browserDir, lang string) error {
	if strings.TrimSpace(browserDir) == "" {
		return nil
	}
	primary, _, acceptLangs := normalizeBrowserLanguage(lang)

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
	prefs["enable_do_not_track"] = true

	intlMap, _ := prefs["intl"].(map[string]any)
	if intlMap == nil {
		intlMap = map[string]any{}
	}
	intlMap["accept_languages"] = acceptLangs
	intlMap["selected_languages"] = acceptLangs
	prefs["intl"] = intlMap

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
	if err := writeFileIfChanged(prefsPath, out, 0o600); err != nil {
		return err
	}

	localStatePath := filepath.Join(browserDir, "Local State")
	localState := map[string]any{}
	if raw, err := os.ReadFile(localStatePath); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
		_ = json.Unmarshal(raw, &localState)
	}
	lsIntl, _ := localState["intl"].(map[string]any)
	if lsIntl == nil {
		lsIntl = map[string]any{}
	}
	lsIntl["app_locale"] = primary
	localState["intl"] = lsIntl
	lsOut, err := json.Marshal(localState)
	if err != nil {
		return err
	}
	return writeFileIfChanged(localStatePath, lsOut, 0o600)
}

func buildAcceptLangSpec(lang string) (primary string, langsSlice []string, headerAccept string) {
	primary, baseLang, _ := normalizeBrowserLanguage(lang)
	switch {
	case strings.EqualFold(primary, baseLang):
		langsSlice = []string{primary}
		headerAccept = primary + ";q=0.9"
	case baseLang == "en":
		langsSlice = []string{primary, "en"}
		headerAccept = primary + ",en;q=0.9"
	default:
		langsSlice = []string{primary, baseLang, "en"}
		headerAccept = primary + "," + baseLang + ";q=0.9,en;q=0.8"
	}
	return primary, langsSlice, headerAccept
}

func ensureBrowserShieldExtension(browserDir, lang string) (string, error) {
	if strings.TrimSpace(browserDir) == "" {
		return "", nil
	}
	primary, langsSlice, headerAccept := buildAcceptLangSpec(lang)

	extDir := filepath.Join(browserDir, "peace-shield-ext")
	if err := os.MkdirAll(extDir, 0o700); err != nil {
		return "", err
	}

	manifest := map[string]any{
		"manifest_version": 3,
		"name":             isolatedShieldExtName,
		"description":      "Isolated profile identity, font, intl, worker, and privacy shield for PeaceClaude.",
		"version":          "1.1.0",
		"permissions":      []string{"declarativeNetRequest"},
		"host_permissions": []string{"<all_urls>"},
		"icons": map[string]string{
			"48":  "icon48.png",
			"128": "icon128.png",
		},
		"action": map[string]any{
			"default_title": "PeaceClaude Identity Shield Active",
			"default_icon": map[string]string{
				"48":  "icon48.png",
				"128": "icon128.png",
			},
		},
		"background": map[string]string{
			"service_worker": "background.js",
		},
		"declarative_net_request": map[string]any{
			"rule_resources": []map[string]any{{
				"id":      "headers",
				"enabled": true,
				"path":    "rules.json",
			}},
		},
		"content_scripts": []map[string]any{{
			"matches":    []string{"<all_urls>"},
			"js":         []string{"shield.js"},
			"run_at":     "document_start",
			"all_frames": true,
			"world":      "MAIN",
		}},
	}
	mRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeFileIfChanged(filepath.Join(extDir, "manifest.json"), append(mRaw, '\n'), 0o600); err != nil {
		return "", err
	}

	rules := []map[string]any{{
		"id":       1,
		"priority": 1,
		"action": map[string]any{
			"type": "modifyHeaders",
			"requestHeaders": []map[string]any{
				{"header": "Accept-Language", "operation": "set", "value": headerAccept},
				{"header": "Sec-GPC", "operation": "set", "value": "1"},
				{"header": "DNT", "operation": "set", "value": "1"},
			},
		},
		"condition": map[string]any{
			"urlFilter":     "*",
			"resourceTypes": sharedDNRResourceTypes,
		},
	}}
	rRaw, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeFileIfChanged(filepath.Join(extDir, "rules.json"), append(rRaw, '\n'), 0o600); err != nil {
		return "", err
	}

	backgroundJS := `chrome.runtime.onInstalled.addListener(() => {});
chrome.runtime.onStartup.addListener(() => {});
`
	if err := writeFileIfChanged(filepath.Join(extDir, "background.js"), []byte(backgroundJS), 0o600); err != nil {
		return "", err
	}

	localeJSON, _ := json.Marshal(primary)
	langsJSON, _ := json.Marshal(langsSlice)
	shieldJS := buildShieldContentScript(string(localeJSON), string(langsJSON))
	if err := writeFileIfChanged(filepath.Join(extDir, "shield.js"), []byte(shieldJS), 0o600); err != nil {
		return "", err
	}

	for _, icon := range []struct {
		name string
		size int
	}{
		{"icon48.png", 48},
		{"icon128.png", 128},
	} {
		pngBytes, err := renderIconPNGBytes(icon.size)
		if err != nil {
			return "", err
		}
		if err := writeFileIfChanged(filepath.Join(extDir, icon.name), pngBytes, 0o644); err != nil {
			return "", err
		}
	}
	return extDir, nil
}

var sharedDNRResourceTypes = []string{
	"main_frame",
	"sub_frame",
	"xmlhttprequest",
	"script",
	"stylesheet",
	"image",
	"font",
	"media",
	"websocket",
	"ping",
	"other",
}

const nativeFnMaskSnippet = `  const nativeFnMap = new WeakMap();
  const origFnToString = Function.prototype.toString;
  const patchedToString = function toString() {
    if (nativeFnMap.has(this)) return nativeFnMap.get(this);
    return origFnToString.call(this);
  };
  nativeFnMap.set(patchedToString, 'function toString() { [native code] }');
  try {
    Object.defineProperty(Function.prototype, 'toString', {
      value: patchedToString,
      writable: true,
      configurable: true,
    });
  } catch {}
  const maskNative = (fn, name) => {
    nativeFnMap.set(fn, 'function ' + name + '() { [native code] }');
    return fn;
  };`

const navigatorIdentityShieldSnippet = `  // Browser Language & Privacy Signals (main thread Navigator + WorkerNavigator; network info left native)
  try {
    const navProto =
      (typeof Navigator !== 'undefined' && Navigator.prototype) ||
      (typeof WorkerNavigator !== 'undefined' && WorkerNavigator.prototype) ||
      (typeof navigator !== 'undefined' && navigator ? Object.getPrototypeOf(navigator) : null);
    if (navProto) {
      Object.defineProperty(navProto, 'language', {
        get: maskNative(function language() { return TARGET_LOCALE; }, 'get language'),
        configurable: true,
      });
      Object.defineProperty(navProto, 'languages', {
        get: maskNative(function languages() { return TARGET_LANGS; }, 'get languages'),
        configurable: true,
      });
      Object.defineProperty(navProto, 'globalPrivacyControl', {
        get: maskNative(function globalPrivacyControl() { return true; }, 'get globalPrivacyControl'),
        configurable: true,
      });
      Object.defineProperty(navProto, 'doNotTrack', {
        get: maskNative(function doNotTrack() { return '1'; }, 'get doNotTrack'),
        configurable: true,
      });
    }
  } catch {}`

const cjkFontShieldSnippet = `  // CJK font-probing shield (Canvas, FontFaceSet.check, and DOM CSSStyleDeclaration setters)
  try {
    const CJK_FONT_RE = /(?:["']?)(?:Microsoft YaHei(?: UI)?|Microsoft JhengHei(?: UI)?|SimSun|NSimSun|SimHei|KaiTi|FangSong|DengXian|PingFang\s*(?:SC|TC|HK)|Hiragino Sans GB|STHeiti|STSong|Songti\s*(?:SC|TC)|Source Han (?:Sans|Serif)\s*(?:CN|SC|TW|HK)|Noto (?:Sans|Serif) CJK\s*(?:SC|TC|HK)?|WenQuanYi (?:Micro|Zen) Hei|PMingLiU|MingLiU|DFKai-SB)(?:["']?)\s*,?\s*/gi;
    const hasCJKFont = (val) => {
      CJK_FONT_RE.lastIndex = 0;
      const hit = CJK_FONT_RE.test(String(val || ''));
      CJK_FONT_RE.lastIndex = 0;
      return hit;
    };
    const stripCJKCanvasFont = (val) => {
      let next = String(val);
      if (!hasCJKFont(next)) return next;
      next = next.replace(CJK_FONT_RE, '')
        .replace(/,\s*,+/g, ',')
        .replace(/(\d+(?:\.\d+)?(?:px|pt|em|rem|ex|ch|vw|vh|%)\s*),+\s*/i, '$1 ')
        .replace(/,\s*$/g, '')
        .replace(/\s{2,}/g, ' ')
        .trim();
      CJK_FONT_RE.lastIndex = 0;
      if (/^(?:(?:normal|italic|oblique|bold|bolder|lighter|\d{3})\s+)*\d+(?:\.\d+)?(?:px|pt|em|rem|ex|ch|vw|vh|%)$/i.test(next)) {
        next = next + ' sans-serif';
      }
      return next;
    };
    const stripCJKFontFamily = (val) => {
      let next = String(val);
      if (!hasCJKFont(next)) return next;
      next = next.replace(CJK_FONT_RE, '')
        .replace(/^\s*,+|,+\s*$/g, '')
        .replace(/,\s*,+/g, ', ')
        .replace(/\s{2,}/g, ' ')
        .trim();
      CJK_FONT_RE.lastIndex = 0;
      return next || 'sans-serif';
    };
    const stripCJKCssText = (raw) => {
      const s = String(raw);
      if (!hasCJKFont(s) || !/font(?:-family)?\s*:/i.test(s)) return s;
      return s
        .replace(/(font-family\s*:\s*)([^;!]+)/gi, (_, pre, v) => pre + stripCJKFontFamily(v))
        .replace(/(^|[;\s])(font\s*:\s*)([^;!]+)/gi, (_, lead, pre, v) => lead + pre + stripCJKCanvasFont(v));
    };

    const patchFontProto = (proto) => {
      if (!proto) return;
      const desc = Object.getOwnPropertyDescriptor(proto, 'font');
      if (!desc || !desc.set || !desc.get) return;
      const origGet = desc.get;
      const origSet = desc.set;
      Object.defineProperty(proto, 'font', {
        get: maskNative(function font() { return origGet.call(this); }, 'get font'),
        set: maskNative(function font(val) {
          return origSet.call(this, stripCJKCanvasFont(val));
        }, 'set font'),
        configurable: true,
        enumerable: true,
      });
    };
    if (typeof CanvasRenderingContext2D !== 'undefined') patchFontProto(CanvasRenderingContext2D.prototype);
    if (typeof OffscreenCanvasRenderingContext2D !== 'undefined') patchFontProto(OffscreenCanvasRenderingContext2D.prototype);

    if (typeof FontFaceSet !== 'undefined' && FontFaceSet.prototype && typeof FontFaceSet.prototype.check === 'function') {
      const origFontCheck = FontFaceSet.prototype.check;
      Object.defineProperty(FontFaceSet.prototype, 'check', {
        value: maskNative(function check(font, text) {
          if (hasCJKFont(font)) return false;
          return origFontCheck.apply(this, arguments);
        }, 'check'),
        writable: true,
        configurable: true,
      });
    }

    if (typeof CSSStyleDeclaration !== 'undefined' && CSSStyleDeclaration.prototype) {
      const cssProto = CSSStyleDeclaration.prototype;
      const origGetProp = typeof cssProto.getPropertyValue === 'function' ? cssProto.getPropertyValue : null;
      const origSetProp = typeof cssProto.setProperty === 'function' ? cssProto.setProperty : null;

      const patchCSSFontProp = (jsProp, cssProp, cleanFn) => {
        const desc = Object.getOwnPropertyDescriptor(cssProto, jsProp);
        if (desc && desc.get && desc.set) {
          const origGet = desc.get;
          const origSet = desc.set;
          Object.defineProperty(cssProto, jsProp, {
            get: maskNative(function() { return origGet.call(this); }, 'get ' + jsProp),
            set: maskNative(function(val) { return origSet.call(this, cleanFn(val)); }, 'set ' + jsProp),
            configurable: true,
            enumerable: desc.enumerable !== false,
          });
        } else if (origGetProp && origSetProp) {
          Object.defineProperty(cssProto, jsProp, {
            get: maskNative(function() { return origGetProp.call(this, cssProp); }, 'get ' + jsProp),
            set: maskNative(function(val) { return origSetProp.call(this, cssProp, cleanFn(val)); }, 'set ' + jsProp),
            configurable: true,
            enumerable: true,
          });
        }
      };
      patchCSSFontProp('fontFamily', 'font-family', stripCJKFontFamily);
      patchCSSFontProp('font', 'font', stripCJKCanvasFont);

      if (origSetProp) {
        Object.defineProperty(cssProto, 'setProperty', {
          value: maskNative(function setProperty(prop, val, priority) {
            const name = String(prop || '').trim().toLowerCase();
            let nextVal = val;
            if (name === 'font-family') {
              nextVal = stripCJKFontFamily(val);
            } else if (name === 'font') {
              nextVal = stripCJKCanvasFont(val);
            }
            if (arguments.length >= 3) {
              return origSetProp.call(this, prop, nextVal, priority);
            }
            return origSetProp.call(this, prop, nextVal);
          }, 'setProperty'),
          writable: true,
          configurable: true,
        });
      }
      const cssTextDesc = Object.getOwnPropertyDescriptor(cssProto, 'cssText');
      if (cssTextDesc && cssTextDesc.get && cssTextDesc.set) {
        const origCTGet = cssTextDesc.get;
        const origCTSet = cssTextDesc.set;
        Object.defineProperty(cssProto, 'cssText', {
          get: maskNative(function cssText() { return origCTGet.call(this); }, 'get cssText'),
          set: maskNative(function cssText(val) {
            return origCTSet.call(this, stripCJKCssText(val));
          }, 'set cssText'),
          configurable: true,
          enumerable: cssTextDesc.enumerable !== false,
        });
      }
    }
    if (typeof Element !== 'undefined' && Element.prototype && typeof Element.prototype.setAttribute === 'function') {
      const origSetAttr = Element.prototype.setAttribute;
      Object.defineProperty(Element.prototype, 'setAttribute', {
        value: maskNative(function setAttribute(name, val) {
          if (String(name || '').toLowerCase() === 'style' && hasCJKFont(val)) {
            return origSetAttr.call(this, name, stripCJKCssText(val));
          }
          return origSetAttr.apply(this, arguments);
        }, 'setAttribute'),
        writable: true,
        configurable: true,
      });
    }
  } catch {}`

const localeExtensionsShieldSnippet = `  // Additional Number / Array / BigInt toLocaleString and String.prototype.localeCompare alignment
  try {
    const patchFirstLocaleMethod = (proto, mName) => {
      if (!proto || typeof proto[mName] !== 'function') return;
      const origFn = proto[mName];
      const wrappedFn = maskNative(function(...args) {
        const nextArgs = args.length === 0 || shouldReplaceLocaleArg(args[0]) ? [TARGET_LOCALE, ...args.slice(1)] : args;
        return origFn.apply(this, nextArgs);
      }, mName);
      Object.defineProperty(proto, mName, { value: wrappedFn, writable: true, configurable: true });
    };
    if (typeof Number !== 'undefined') patchFirstLocaleMethod(Number.prototype, 'toLocaleString');
    if (typeof Array !== 'undefined') patchFirstLocaleMethod(Array.prototype, 'toLocaleString');
    if (typeof BigInt !== 'undefined' && BigInt.prototype) {
      patchFirstLocaleMethod(BigInt.prototype, 'toLocaleString');
    }
    if (typeof String !== 'undefined' && String.prototype && typeof String.prototype.localeCompare === 'function') {
      const origCompare = String.prototype.localeCompare;
      Object.defineProperty(String.prototype, 'localeCompare', {
        value: maskNative(function localeCompare(...args) {
          const nextLocales = args.length < 2 || shouldReplaceLocaleArg(args[1]) ? TARGET_LOCALE : args[1];
          const nextArgs = args.length <= 1 ? [args[0], nextLocales] : [args[0], nextLocales, ...args.slice(2)];
          return origCompare.apply(this, nextArgs);
        }, 'localeCompare'),
        writable: true,
        configurable: true,
      });
    }
  } catch {}`

const timezoneCoreJSSnippet = `  const RawDate = Date;
  const RawDTF = Intl.DateTimeFormat;

  // Accurate IANA timezone offset calculation (positive west of UTC, e.g. Denver = +420 / +360 DST)
  let dtfParts = null;
  let dtfLongName = null;
  let dtfDisplay = null;
  try {
    dtfParts = new RawDTF('en-US', {
      timeZone: TARGET_TZ,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hourCycle: 'h23',
    });
    dtfLongName = new RawDTF('en-US', {
      timeZone: TARGET_TZ,
      timeZoneName: 'long',
    });
    dtfDisplay = new RawDTF('en-US', {
      timeZone: TARGET_TZ,
      weekday: 'short',
      month: 'short',
      day: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hourCycle: 'h23',
    });
  } catch {}

  const partsMap = (fmt, dateObj) => {
    const out = {};
    const parts = fmt.formatToParts(dateObj);
    for (let i = 0; i < parts.length; i++) {
      out[parts[i].type] = parts[i].value;
    }
    return out;
  };

  const getOffsetMinutesForTZ = (dateObj) => {
    const ts = dateObj instanceof RawDate ? RawDate.prototype.getTime.call(dateObj) : NaN;
    if (!Number.isFinite(ts) || !dtfParts) return 0;
    const m = partsMap(dtfParts, dateObj);
    const wallMs = RawDate.UTC(
      Number(m.year),
      Number(m.month) - 1,
      Number(m.day),
      Number(m.hour) % 24,
      Number(m.minute),
      Number(m.second)
    );
    const utcTruncMs = Math.floor(ts / 1000) * 1000;
    return Math.round((utcTruncMs - wallMs) / 60000);
  };

  const formatGMTOffset = (offsetMin) => {
    const sign = offsetMin > 0 ? '-' : '+';
    const abs = Math.abs(offsetMin);
    const hh = String(Math.floor(abs / 60)).padStart(2, '0');
    const mm = String(abs % 60).padStart(2, '0');
    return 'GMT' + sign + hh + mm;
  };

  // Align Date prototype timezone methods without replacing Date constructor
  try {
    const origGetTZOffset = RawDate.prototype.getTimezoneOffset;
    const origToString = RawDate.prototype.toString;
    const origToDateString = RawDate.prototype.toDateString;
    const origToTimeString = RawDate.prototype.toTimeString;

    Object.defineProperty(RawDate.prototype, 'getTimezoneOffset', {
      value: maskNative(function getTimezoneOffset() {
        const ts = RawDate.prototype.getTime.call(this);
        if (!Number.isFinite(ts)) return origGetTZOffset.call(this);
        return getOffsetMinutesForTZ(this);
      }, 'getTimezoneOffset'),
      writable: true,
      configurable: true,
    });

    const formatTimeZonePart = (dateObj) => {
      const off = getOffsetMinutesForTZ(dateObj);
      let tzLong = TARGET_TZ;
      if (dtfLongName) {
        const lm = partsMap(dtfLongName, dateObj);
        if (lm.timeZoneName) tzLong = lm.timeZoneName;
      }
      return formatGMTOffset(off) + ' (' + tzLong + ')';
    };

    Object.defineProperty(RawDate.prototype, 'toDateString', {
      value: maskNative(function toDateString() {
        const ts = RawDate.prototype.getTime.call(this);
        if (!Number.isFinite(ts) || !dtfDisplay) return origToDateString.call(this);
        const m = partsMap(dtfDisplay, this);
        return m.weekday + ' ' + m.month + ' ' + m.day + ' ' + m.year;
      }, 'toDateString'),
      writable: true,
      configurable: true,
    });

    Object.defineProperty(RawDate.prototype, 'toTimeString', {
      value: maskNative(function toTimeString() {
        const ts = RawDate.prototype.getTime.call(this);
        if (!Number.isFinite(ts) || !dtfDisplay) return origToTimeString.call(this);
        const m = partsMap(dtfDisplay, this);
        const hr = String(Number(m.hour) % 24).padStart(2, '0');
        return hr + ':' + m.minute + ':' + m.second + ' ' + formatTimeZonePart(this);
      }, 'toTimeString'),
      writable: true,
      configurable: true,
    });

    Object.defineProperty(RawDate.prototype, 'toString', {
      value: maskNative(function toString() {
        const ts = RawDate.prototype.getTime.call(this);
        if (!Number.isFinite(ts) || !dtfDisplay) return origToString.call(this);
        const m = partsMap(dtfDisplay, this);
        const hr = String(Number(m.hour) % 24).padStart(2, '0');
        return m.weekday + ' ' + m.month + ' ' + m.day + ' ' + m.year + ' ' + hr + ':' + m.minute + ':' + m.second + ' ' + formatTimeZonePart(this);
      }, 'toString'),
      writable: true,
      configurable: true,
    });
  } catch {}`

func buildIntlAndDateShieldSnippet(withTimezone bool) string {
	tzHelper := ""
	dtfPrep := "return args.length === 0 || shouldReplaceLocaleArg(args[0]) ? [TARGET_LOCALE, ...args.slice(1)] : args;"
	dateArgsExpr := "args.length === 0 || shouldReplaceLocaleArg(args[0]) ? [TARGET_LOCALE, ...args.slice(1)] : args"
	if withTimezone {
		tzHelper = `    const withDefaultTimeZone = (args) => {
      const loc = args.length === 0 || shouldReplaceLocaleArg(args[0]) ? TARGET_LOCALE : args[0];
      const opts = args.length > 1 ? args[1] : undefined;
      if (opts === undefined || opts === null) {
        return [loc, { timeZone: TARGET_TZ }, ...args.slice(2)];
      }
      if (typeof opts === 'object' && opts.timeZone === undefined) {
        return [loc, Object.assign({}, opts, { timeZone: TARGET_TZ }), ...args.slice(2)];
      }
      return [loc, ...args.slice(1)];
    };`
		dtfPrep = `if (isDTF) return withDefaultTimeZone(args);
        return args.length === 0 || shouldReplaceLocaleArg(args[0]) ? [TARGET_LOCALE, ...args.slice(1)] : args;`
		dateArgsExpr = "withDefaultTimeZone(args)"
	}

	tmpl := `  // Align default Intl locale and Date.toLocale* when caller omits explicit locale
  const shouldReplaceLocaleArg = (arg) => arg === undefined || (Array.isArray(arg) && arg.length === 0);
  try {
__TZ_HELPER__
    const intlCtors = [
      'DateTimeFormat',
      'NumberFormat',
      'Collator',
      'PluralRules',
      'RelativeTimeFormat',
      'ListFormat',
      'DisplayNames',
      'Segmenter',
    ];
    for (const ctorName of intlCtors) {
      const Orig = Intl[ctorName];
      if (!Orig) continue;
      const isDTF = ctorName === 'DateTimeFormat';
      const prepareArgs = (args) => {
        __DTF_PREP__
      };
      const Wrapped = new Proxy(Orig, {
        construct(target, args, newTarget) {
          return Reflect.construct(target, prepareArgs(args), newTarget);
        },
        apply(target, thisArg, args) {
          return Reflect.apply(target, thisArg, prepareArgs(args));
        },
      });
      maskNative(Wrapped, ctorName);
      Object.defineProperty(Intl, ctorName, { value: Wrapped, writable: true, configurable: true });
    }
    for (const mName of ['toLocaleString', 'toLocaleDateString', 'toLocaleTimeString']) {
      const origDateFn = Date.prototype[mName];
      if (typeof origDateFn !== 'function') continue;
      const wrappedDateFn = maskNative(function(...args) {
        const nextArgs = __DATE_ARGS_EXPR__;
        return origDateFn.apply(this, nextArgs);
      }, mName);
      Object.defineProperty(Date.prototype, mName, { value: wrappedDateFn, writable: true, configurable: true });
    }
  } catch {}`

	return strings.NewReplacer(
		"__TZ_HELPER__", tzHelper,
		"__DTF_PREP__", dtfPrep,
		"__DATE_ARGS_EXPR__", dateArgsExpr,
	).Replace(tmpl)
}

func buildWorkerShieldSnippet(withTimezone bool) string {
	var bodyParts []string
	bodyParts = append(bodyParts, nativeFnMaskSnippet)
	if withTimezone {
		bodyParts = append(bodyParts, timezoneCoreJSSnippet)
	}
	bodyParts = append(bodyParts,
		navigatorIdentityShieldSnippet,
		buildIntlAndDateShieldSnippet(withTimezone),
		localeExtensionsShieldSnippet,
		cjkFontShieldSnippet,
	)
	workerBody := "(() => {\n" + strings.Join(bodyParts, "\n") + "\n})();\n"
	workerBodyJSON, _ := json.Marshal(workerBody)

	const tmpl = `  // Web Worker consistency wrapper (synchronizes Navigator, Intl, Date TZ, and OffscreenCanvas CJK shield)
  try {
    const OrigWorker = globalThis.Worker;
    if (typeof OrigWorker === 'function' && typeof Blob === 'function' && typeof URL === 'function' && typeof URL.createObjectURL === 'function') {
      const WORKER_SHIELD_BODY = __WORKER_BODY_JSON__;
      const delayedRevokeURLs = new Set();
      const origRevoke = typeof URL.revokeObjectURL === 'function' ? URL.revokeObjectURL.bind(URL) : null;
      if (origRevoke) {
        URL.revokeObjectURL = maskNative(function revokeObjectURL(u) {
          const key = String(u || '');
          if (delayedRevokeURLs.has(key)) return;
          return origRevoke(u);
        }, 'revokeObjectURL');
      }
      const WrappedWorker = new Proxy(OrigWorker, {
        construct(target, args, newTarget) {
          try {
            const scriptURL = args[0];
            const options = args[1];
            if (options && typeof options === 'object' && options.type === 'module') {
              return Reflect.construct(target, args, newTarget);
            }
            const baseHref = globalThis.location && globalThis.location.href ? globalThis.location.href : undefined;
            const parsed = new URL(String(scriptURL), baseHref);
            const isHTTP = parsed.protocol === 'http:' || parsed.protocol === 'https:';
            const sameOrigin = !globalThis.location || parsed.origin === globalThis.location.origin;
            if (parsed.protocol === 'blob:' || (isHTTP && sameOrigin)) {
              if (parsed.protocol === 'blob:' && origRevoke) {
                delayedRevokeURLs.add(parsed.href);
                setTimeout(() => {
                  delayedRevokeURLs.delete(parsed.href);
                  try { origRevoke(parsed.href); } catch {}
                }, 5000);
              }
              const preambleHeader =
                'const TARGET_TZ=' + JSON.stringify(TARGET_TZ) + ';' +
                'const TARGET_LOCALE=' + JSON.stringify(TARGET_LOCALE) + ';' +
                'const TARGET_LANGS=Object.freeze(' + JSON.stringify(TARGET_LANGS) + ');\n';
              const blob = new Blob(
                [preambleHeader, WORKER_SHIELD_BODY, 'importScripts(' + JSON.stringify(parsed.href) + ');\n'],
                { type: 'application/javascript' }
              );
              const wrappedURL = URL.createObjectURL(blob);
              setTimeout(() => {
                if (origRevoke) {
                  try { origRevoke(wrappedURL); } catch {}
                }
              }, 30000);
              return Reflect.construct(target, [wrappedURL, ...args.slice(1)], newTarget);
            }
          } catch {}
          return Reflect.construct(target, args, newTarget);
        },
      });
      maskNative(WrappedWorker, 'Worker');
      Object.defineProperty(globalThis, 'Worker', {
        value: WrappedWorker,
        writable: true,
        configurable: true,
      });
    }
  } catch {}`

	return strings.ReplaceAll(tmpl, "__WORKER_BODY_JSON__", string(workerBodyJSON))
}

func buildShieldContentScript(localeJSON, langsJSON string) string {
	const tmpl = `(() => {
  if (window.__pcShieldApplied) return;
  try {
    Object.defineProperty(window, '__pcShieldApplied', { value: true, enumerable: false, configurable: false });
  } catch { return; }

  const TARGET_TZ = '';
  const TARGET_LOCALE = __TARGET_LOCALE__;
  const TARGET_LANGS = Object.freeze(__TARGET_LANGS__);

__NATIVE_FN_MASK__

__NAVIGATOR_SHIELD__

__INTL_DATE_SHIELD__

__LOCALE_EXT_SHIELD__

__CJK_FONT_SHIELD__

__WORKER_SHIELD__
})();
`
	return strings.NewReplacer(
		"__TARGET_LOCALE__", localeJSON,
		"__TARGET_LANGS__", langsJSON,
		"__NATIVE_FN_MASK__", nativeFnMaskSnippet,
		"__NAVIGATOR_SHIELD__", navigatorIdentityShieldSnippet,
		"__INTL_DATE_SHIELD__", buildIntlAndDateShieldSnippet(false),
		"__LOCALE_EXT_SHIELD__", localeExtensionsShieldSnippet,
		"__CJK_FONT_SHIELD__", cjkFontShieldSnippet,
		"__WORKER_SHIELD__", buildWorkerShieldSnippet(false),
	).Replace(tmpl)
}
