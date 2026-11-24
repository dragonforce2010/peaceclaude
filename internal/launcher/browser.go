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

// LaunchLoginBrowser opens an isolated Chromium/Chrome browser profile routed strictly
// through PeaceClaude's static-IP proxy with matching TZ & en-US identity shielding for clean OAuth login.
func LaunchLoginBrowser(ctx context.Context, cfg *config.Config, selfBin, profileName, targetURL string, dryRun bool) (string, []string, error) {
	browserBin, _, args, err := LaunchLoginBrowserWithPID(ctx, cfg, selfBin, profileName, targetURL, dryRun)
	return browserBin, args, err
}

// LaunchLoginBrowserWithPID opens an isolated login browser with hardened language/privacy preferences,
// loads the ephemeral Identity Shield MV3 extension over loopback CDP on Chromium browsers,
// navigates directly to targetURL without attaching Runtime hooks, and focuses its window.
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

	rawLang := cfg.Identity.Language
	if prof.Language != "" {
		rawLang = prof.Language
	}
	lang, _, acceptLangs := normalizeBrowserLanguage(rawLang)
	useCDPShield := supportsCDPShieldBrowser(browserBin)

	runningPID := 0
	hasShield := false
	if !dryRun {
		runningPID, hasShield = inspectRunningBrowser(prof.BrowserDir)
		if runningPID > 0 && !hasShield && useCDPShield {
			// Stop unhardened instance BEFORE rewriting Preferences so Chrome shutdown does not flush stale zh-CN prefs back to disk.
			TerminatePIDs([]string{strconv.Itoa(runningPID)}, 3*time.Second)
			runningPID = 0
		}
	}

	if err := ensureBrowserPrivacyPrefs(prof.BrowserDir, lang); err != nil {
		return "", 0, nil, fmt.Errorf("写入隔离浏览器隐私首选项失败: %w", err)
	}
	extDir, err := ensureBrowserShieldExtension(prof.BrowserDir, lang)
	if err != nil {
		return "", 0, nil, fmt.Errorf("生成隔离浏览器指纹防护扩展失败: %w", err)
	}

	baseArgs := []string{
		"--user-data-dir=" + prof.BrowserDir,
	}
	baseArgs = append(baseArgs, BuildChromiumProxyArgs(cfg)...)
	baseArgs = append(baseArgs,
		"--lang="+lang,
		"--accept-lang="+acceptLangs,
		"--disable-quic",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--no-first-run",
		"--no-default-browser-check",
	)

	if dryRun {
		dryArgs := append([]string{}, baseArgs...)
		if useCDPShield {
			dryArgs = append(dryArgs, "--enable-unsafe-extension-debugging")
		}
		dryArgs = append(dryArgs, targetURL)
		return browserBin, 0, dryArgs, nil
	}

	if runningPID > 0 && hasShield {
		// Reuse already-hardened isolated browser instance and open targetURL in a new tab.
		openArgs := append(append([]string{}, baseArgs...), targetURL)
		cmd := exec.Command(browserBin, openArgs...)
		cmd.Env = BuildIsolatedEnv(cfg, prof, gate, os.Environ())
		_ = cmd.Start()
		focusProcessWindow(runningPID)
		return browserBin, runningPID, openArgs, nil
	}

	_ = config.EnsureDirs()
	logPath := filepath.Join(config.LogsDir(), "browser-launcher.log")
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
		return browserBin, 0, launchArgs, fmt.Errorf("launch isolated browser (%s): %w", filepath.Base(browserBin), err)
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
		select {
		case exitErr := <-waitCh:
			detail := readLogTailFrom(logPath, startOffset, 4096)
			if exitErr == nil {
				if existing, _ := inspectRunningBrowser(prof.BrowserDir); existing > 0 {
					focusProcessWindow(existing)
					return browserBin, existing, launchArgs, nil
				}
				return browserBin, pid, launchArgs, nil
			}
			return browserBin, 0, launchArgs, fmt.Errorf("隔离浏览器 (%s) 启动后退出 (%v): %s", filepath.Base(browserBin), exitErr, detail)
		case <-time.After(700 * time.Millisecond):
			focusProcessWindow(pid)
			return browserBin, pid, launchArgs, nil
		}
	}

	cdpCtx, cdpCancel := context.WithTimeout(ctx, 6*time.Second)
	defer cdpCancel()

	// Cancel CDP polling immediately if the spawned browser exits unexpectedly.
	doneWatch := make(chan struct{})
	defer close(doneWatch)
	go func() {
		select {
		case err := <-waitCh:
			if err != nil {
				cdpCancel()
			}
		case <-doneWatch:
		}
	}()

	if _, extErr := cdpLoadUnpackedExtension(cdpCtx, cdpPort, extDir); extErr != nil {
		appendBrowserLog(logPath, fmt.Sprintf("CDP loadUnpacked fallback (%s): %v", extDir, extErr))
	}
	if navErr := cdpNavigateFirstPage(cdpCtx, cdpPort, targetURL); navErr != nil {
		appendBrowserLog(logPath, fmt.Sprintf("CDP navigate fallback (%s): %v", targetURL, navErr))
		fallbackArgs := append(append([]string{}, baseArgs...), targetURL)
		fbCmd := exec.Command(browserBin, fallbackArgs...)
		fbCmd.Env = BuildIsolatedEnv(cfg, prof, gate, os.Environ())
		_ = fbCmd.Start()
	}

	if curPID, _ := inspectRunningBrowser(prof.BrowserDir); curPID > 0 {
		pid = curPID
	}
	focusProcessWindow(pid)
	return browserBin, pid, launchArgs, nil
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
	want := filepath.Clean(userDataDir)
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return 0, false
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
				return pid, strings.Contains(cmdLine, "--enable-unsafe-extension-debugging")
			}
		}
	}
	return 0, false
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
	if err := os.WriteFile(prefsPath, out, 0o600); err != nil {
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
	return os.WriteFile(localStatePath, lsOut, 0o600)
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
		"name":             "PeaceClaude Identity Shield",
		"version":          "1.0.0",
		"permissions":      []string{"declarativeNetRequest"},
		"host_permissions": []string{"<all_urls>"},
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
	if err := os.WriteFile(filepath.Join(extDir, "manifest.json"), append(mRaw, '\n'), 0o600); err != nil {
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
			"resourceTypes": []string{"main_frame", "sub_frame", "xmlhttprequest", "script", "other"},
		},
	}}
	rRaw, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(extDir, "rules.json"), append(rRaw, '\n'), 0o600); err != nil {
		return "", err
	}

	localeJSON, _ := json.Marshal(primary)
	langsJSON, _ := json.Marshal(langsSlice)
	shieldJS := buildShieldContentScript(string(localeJSON), string(langsJSON))
	if err := os.WriteFile(filepath.Join(extDir, "shield.js"), []byte(shieldJS), 0o600); err != nil {
		return "", err
	}
	return extDir, nil
}

const canvasCJKFontShieldSnippet = `  // Canvas CJK font-probing shield (neutralizes measureText enumeration without altering DOM CSS rendering)
  try {
    const CJK_FONT_RE = /(?:["']?)(?:Microsoft YaHei(?: UI)?|Microsoft JhengHei(?: UI)?|SimSun|NSimSun|SimHei|KaiTi|FangSong|DengXian|PingFang\s*(?:SC|TC|HK)|Hiragino Sans GB|STHeiti|STSong|Songti\s*(?:SC|TC)|Source Han (?:Sans|Serif)\s*(?:CN|SC|TW|HK)|Noto (?:Sans|Serif) CJK\s*(?:SC|TC|HK)?|WenQuanYi (?:Micro|Zen) Hei|PMingLiU|MingLiU|DFKai-SB)(?:["']?)\s*,?\s*/gi;
    const patchFontProto = (proto) => {
      if (!proto) return;
      const desc = Object.getOwnPropertyDescriptor(proto, 'font');
      if (!desc || !desc.set || !desc.get) return;
      const origGet = desc.get;
      const origSet = desc.set;
      Object.defineProperty(proto, 'font', {
        get: maskNative(function font() { return origGet.call(this); }, 'get font'),
        set: maskNative(function font(val) {
          let next = String(val);
          if (CJK_FONT_RE.test(next)) {
            CJK_FONT_RE.lastIndex = 0;
            next = next.replace(CJK_FONT_RE, '')
              .replace(/,\s*,+/g, ',')
              .replace(/(\d+(?:\.\d+)?(?:px|pt|em|rem|ex|ch|vw|vh|%)\s*),+\s*/i, '$1 ')
              .replace(/,\s*$/g, '')
              .replace(/\s{2,}/g, ' ')
              .trim();
            if (/^\d+(?:\.\d+)?(?:px|pt|em|rem|ex|ch|vw|vh|%)$/i.test(next)) {
              next = next + ' sans-serif';
            }
          }
          return origSet.call(this, next);
        }, 'set font'),
        configurable: true,
        enumerable: true,
      });
    };
    if (typeof CanvasRenderingContext2D !== 'undefined') patchFontProto(CanvasRenderingContext2D.prototype);
    if (typeof OffscreenCanvasRenderingContext2D !== 'undefined') patchFontProto(OffscreenCanvasRenderingContext2D.prototype);
  } catch {}`

func buildShieldContentScript(localeJSON, langsJSON string) string {
	const tmpl = `(() => {
  if (window.__pcShieldApplied) return;
  try {
    Object.defineProperty(window, '__pcShieldApplied', { value: true, enumerable: false, configurable: false });
  } catch { return; }

  const TARGET_LOCALE = __TARGET_LOCALE__;
  const TARGET_LANGS = Object.freeze(__TARGET_LANGS__);

  const nativeFnMap = new WeakMap();
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
  };

  // 1. Browser Language & Privacy Signals (Navigator.prototype)
  try {
    Object.defineProperty(Navigator.prototype, 'language', {
      get: maskNative(function language() { return TARGET_LOCALE; }, 'get language'),
      configurable: true,
    });
    Object.defineProperty(Navigator.prototype, 'languages', {
      get: maskNative(function languages() { return TARGET_LANGS; }, 'get languages'),
      configurable: true,
    });
    Object.defineProperty(Navigator.prototype, 'globalPrivacyControl', {
      get: maskNative(function globalPrivacyControl() { return true; }, 'get globalPrivacyControl'),
      configurable: true,
    });
    Object.defineProperty(Navigator.prototype, 'doNotTrack', {
      get: maskNative(function doNotTrack() { return '1'; }, 'get doNotTrack'),
      configurable: true,
    });
  } catch {}

  // 2. Align default Intl locale and Date.toLocale* when caller omits explicit locale
  try {
    const shouldReplaceLocaleArg = (arg) => arg === undefined || (Array.isArray(arg) && arg.length === 0);
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
      const Wrapped = new Proxy(Orig, {
        construct(target, args, newTarget) {
          const nextArgs = args.length === 0 || shouldReplaceLocaleArg(args[0]) ? [TARGET_LOCALE, ...args.slice(1)] : args;
          return Reflect.construct(target, nextArgs, newTarget);
        },
        apply(target, thisArg, args) {
          const nextArgs = args.length === 0 || shouldReplaceLocaleArg(args[0]) ? [TARGET_LOCALE, ...args.slice(1)] : args;
          return Reflect.apply(target, thisArg, nextArgs);
        },
      });
      maskNative(Wrapped, ctorName);
      Object.defineProperty(Intl, ctorName, { value: Wrapped, writable: true, configurable: true });
    }
    for (const mName of ['toLocaleString', 'toLocaleDateString', 'toLocaleTimeString']) {
      const origDateFn = Date.prototype[mName];
      if (typeof origDateFn !== 'function') continue;
      const wrappedDateFn = maskNative(function(...args) {
        const nextArgs = args.length === 0 || shouldReplaceLocaleArg(args[0]) ? [TARGET_LOCALE, ...args.slice(1)] : args;
        return origDateFn.apply(this, nextArgs);
      }, mName);
      Object.defineProperty(Date.prototype, mName, { value: wrappedDateFn, writable: true, configurable: true });
    }
  } catch {}

__CANVAS_CJK_SHIELD__
})();
`
	return strings.NewReplacer(
		"__TARGET_LOCALE__", localeJSON,
		"__TARGET_LANGS__", langsJSON,
		"__CANVAS_CJK_SHIELD__", canvasCJKFontShieldSnippet,
	).Replace(tmpl)
}
