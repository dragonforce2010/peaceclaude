package launcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/host"
)

const nativeShieldExtName = "PeaceClaude Native Shield"

type NativeBrowserInfo struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	AppPath           string   `json:"app_path"`
	Installed         bool     `json:"installed"`
	SupportsExtension bool     `json:"supports_extension"`
	ExtensionsURL     string   `json:"extensions_url,omitempty"`
	Selected          bool     `json:"selected"`
	ExtensionLoaded   bool     `json:"extension_loaded"`
	ExtensionEnabled  bool     `json:"extension_enabled"`
	LoadedProfiles    []string `json:"loaded_profiles,omitempty"`
	MissingProfiles   []string `json:"missing_profiles,omitempty"`
	TotalProfiles     int      `json:"total_profiles,omitempty"`
	ProtectionLevel   string   `json:"protection_level"` // "full" | "partial" | "network_only" | "unprotected"
	Note              string   `json:"note"`
}

type NativeBrowserReport struct {
	ExtensionDir     string              `json:"extension_dir"`
	ExtensionReady   bool                `json:"extension_ready"`
	PACEnabled       bool                `json:"pac_enabled"`
	TargetTimezone   string              `json:"target_timezone"`
	TargetLocale     string              `json:"target_locale"`
	ProtectedDomains []string            `json:"protected_domains"`
	Browsers         []NativeBrowserInfo `json:"browsers"`
}

type GuideResult struct {
	OK            bool     `json:"ok"`
	BrowserID     string   `json:"browser_id"`
	BrowserName   string   `json:"browser_name"`
	ExtensionsURL string   `json:"extensions_url,omitempty"`
	ExtensionDir  string   `json:"extension_dir"`
	Steps         []string `json:"steps"`
	Message       string   `json:"message"`
}

type nativeBrowserSpec struct {
	ID                string
	Name              string
	AppBundle         string
	SupportsExtension bool
	ExtensionsURL     string
	UserDataRelPath   string
}

var nativeBrowserCatalog = []nativeBrowserSpec{
	{
		ID:                "arc",
		Name:              "Arc",
		AppBundle:         "Arc.app",
		SupportsExtension: true,
		ExtensionsURL:     "arc://extensions",
		UserDataRelPath:   "Library/Application Support/Arc/User Data",
	},
	{
		ID:                "chrome",
		Name:              "Google Chrome",
		AppBundle:         "Google Chrome.app",
		SupportsExtension: true,
		ExtensionsURL:     "chrome://extensions",
		UserDataRelPath:   "Library/Application Support/Google/Chrome",
	},
	{
		ID:                "brave",
		Name:              "Brave Browser",
		AppBundle:         "Brave Browser.app",
		SupportsExtension: true,
		ExtensionsURL:     "brave://extensions",
		UserDataRelPath:   "Library/Application Support/BraveSoftware/Brave-Browser",
	},
	{
		ID:                "edge",
		Name:              "Microsoft Edge",
		AppBundle:         "Microsoft Edge.app",
		SupportsExtension: true,
		ExtensionsURL:     "edge://extensions",
		UserDataRelPath:   "Library/Application Support/Microsoft Edge",
	},
	{
		ID:                "chromium",
		Name:              "Chromium",
		AppBundle:         "Chromium.app",
		SupportsExtension: true,
		ExtensionsURL:     "chrome://extensions",
		UserDataRelPath:   "Library/Application Support/Chromium",
	},
	{
		ID:                "vivaldi",
		Name:              "Vivaldi",
		AppBundle:         "Vivaldi.app",
		SupportsExtension: true,
		ExtensionsURL:     "vivaldi://extensions",
		UserDataRelPath:   "Library/Application Support/Vivaldi",
	},
	{
		ID:                "safari",
		Name:              "Safari",
		AppBundle:         "Safari.app",
		SupportsExtension: false,
	},
}

// NormalizeProtectedBrowsers validates, lowercases, and deduplicates browser IDs against the catalog.
func NormalizeProtectedBrowsers(ids []string) []string {
	valid := make(map[string]struct{}, len(nativeBrowserCatalog))
	for _, spec := range nativeBrowserCatalog {
		valid[spec.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		clean := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := valid[clean]; !ok {
			continue
		}
		if _, dup := seen[clean]; dup {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

// NativeShieldProtectedDomains returns the deduplicated allowlist of domains
// where the native browser extension applies identity & header shielding.
func NativeShieldProtectedDomains(cfg *config.Config) []string {
	base := []string{
		"claude.ai",
		"claude.com",
		"anthropic.com",
		"claudeusercontent.com",
		"fuck-claude.app",
	}
	seen := make(map[string]struct{}, len(base))
	out := make([]string, 0, len(base))
	add := func(raw string) {
		clean := strings.ToLower(strings.TrimSpace(raw))
		clean = strings.TrimPrefix(clean, "https://")
		clean = strings.TrimPrefix(clean, "http://")
		if h, _, ok := strings.Cut(clean, "/"); ok {
			clean = h
		}
		if h, _, ok := strings.Cut(clean, ":"); ok {
			clean = h
		}
		clean = strings.TrimPrefix(strings.TrimPrefix(clean, "*."), ".")
		clean = strings.TrimSuffix(clean, ".")
		if clean == "" || clean == "*" || !strings.Contains(clean, ".") {
			return
		}
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	for _, d := range base {
		add(d)
	}
	if cfg != nil {
		for _, d := range cfg.Shield.ExtraClaudeDomains {
			add(d)
		}
	}
	return out
}

func resolveNativeShieldTarget(cfg *config.Config, gate egress.GateSnapshot) (tz, locale string) {
	tz = strings.TrimSpace(gate.EffectiveTimezone)
	if tz == "" && cfg != nil {
		tz = strings.TrimSpace(cfg.Identity.Timezone)
	}
	if tz == "" {
		tz = "America/Denver"
	}

	locale = strings.TrimSpace(gate.EffectiveLanguage)
	if locale == "" && cfg != nil {
		locale = strings.TrimSpace(cfg.Identity.Language)
	}
	if locale == "" {
		locale = "en-US"
	}
	return tz, locale
}

func writeFileIfChanged(path string, content []byte, perm os.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		_ = os.Chmod(path, perm)
		return nil
	}
	return os.WriteFile(path, content, perm)
}

func renderIconPNGBytes(size int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, drawPeaceIcon(size)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// EnsureNativeBrowserShield generates or hot-updates `~/.peaceclaude/extensions/native-browser-shield/`
// with a domain-scoped MV3 extension that protects only Claude & diagnostic sites.
func EnsureNativeBrowserShield(cfg *config.Config, gate egress.GateSnapshot) (string, error) {
	if cfg == nil {
		cfg = config.Default()
	}
	if err := config.EnsureDirs(); err != nil {
		return "", err
	}
	extDir := config.NativeBrowserShieldDir()
	if err := os.MkdirAll(extDir, 0o700); err != nil {
		return "", err
	}

	domains := NativeShieldProtectedDomains(cfg)
	tz, rawLang := resolveNativeShieldTarget(cfg, gate)
	primary, langsSlice, headerAccept := buildAcceptLangSpec(rawLang)
	ctrlURL := strings.TrimRight(cfg.ControlURL(), "/")

	var matchPatterns []string
	for _, d := range domains {
		matchPatterns = append(matchPatterns, "https://"+d+"/*", "https://*."+d+"/*")
	}
	hostPerms := append(append([]string{}, matchPatterns...), "http://127.0.0.1/*")

	manifest := map[string]any{
		"manifest_version": 3,
		"name":             nativeShieldExtName,
		"description":      "Domain-scoped timezone, language, CJK font, and WebRTC shield for Claude & Anthropic on native macOS browsers.",
		"version":          "1.0.0",
		"permissions":      []string{"declarativeNetRequest", "privacy", "alarms", "storage"},
		"host_permissions": hostPerms,
		"icons": map[string]string{
			"48":  "icon48.png",
			"128": "icon128.png",
		},
		"action": map[string]any{
			"default_title": "PeaceClaude Native Shield",
			"default_popup": "popup.html",
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
			"matches":    matchPatterns,
			"js":         []string{"config.js", "shield.js"},
			"run_at":     "document_start",
			"all_frames": true,
			"world":      "MAIN",
		}},
	}
	mRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
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
			"requestDomains": domains,
			"resourceTypes":  sharedDNRResourceTypes,
		},
	}}
	rRaw, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		return "", err
	}

	cfgPayload := map[string]any{
		"timezone":         tz,
		"locale":           primary,
		"langs":            langsSlice,
		"acceptHeader":     headerAccept,
		"controlURL":       ctrlURL,
		"protectedDomains": domains,
	}
	cfgJSON, err := json.Marshal(cfgPayload)
	if err != nil {
		return "", err
	}
	configJS := fmt.Sprintf(`(() => {
  try {
    Object.defineProperty(globalThis, '__PC_NATIVE_SHIELD_CFG__', {
      value: Object.freeze(%s),
      enumerable: false,
      configurable: true,
    });
  } catch {}
})();
`, string(cfgJSON))

	ctrlURLJSON, _ := json.Marshal(ctrlURL)
	domainsJSON, _ := json.Marshal(domains)
	tzJSON, _ := json.Marshal(tz)
	locJSON, _ := json.Marshal(primary)

	files := []struct {
		name string
		data []byte
	}{
		{"manifest.json", append(mRaw, '\n')},
		{"rules.json", append(rRaw, '\n')},
		{"config.js", []byte(configJS)},
		{"shield.js", []byte(buildNativeShieldContentScript())},
		{"background.js", []byte(buildNativeShieldBackgroundJS(string(ctrlURLJSON), string(tzJSON), string(locJSON)))},
		{"popup.html", []byte(nativeShieldPopupHTML)},
		{"popup.js", []byte(buildNativeShieldPopupJS(string(ctrlURLJSON), string(tzJSON), string(locJSON), string(domainsJSON)))},
	}

	for _, f := range files {
		if err := writeFileIfChanged(filepath.Join(extDir, f.name), f.data, 0o600); err != nil {
			return "", err
		}
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

func buildNativeShieldContentScript() string {
	const tmpl = `(() => {
  if (window.__pcShieldApplied) return;
  try {
    Object.defineProperty(window, '__pcShieldApplied', { value: true, enumerable: false, configurable: false });
  } catch { return; }

  const cfg = globalThis.__PC_NATIVE_SHIELD_CFG__ || {};
  const TARGET_TZ = typeof cfg.timezone === 'string' && cfg.timezone ? cfg.timezone : 'America/Denver';
  const TARGET_LOCALE = typeof cfg.locale === 'string' && cfg.locale ? cfg.locale : 'en-US';
  const TARGET_LANGS = Object.freeze(
    Array.isArray(cfg.langs) && cfg.langs.length > 0 ? cfg.langs.slice() : [TARGET_LOCALE, 'en']
  );

__NATIVE_FN_MASK__

__TIMEZONE_CORE__

__NAVIGATOR_SHIELD__

__INTL_DATE_SHIELD__

__LOCALE_EXT_SHIELD__

__CJK_FONT_SHIELD__

__WORKER_SHIELD__
})();
`
	return strings.NewReplacer(
		"__NATIVE_FN_MASK__", nativeFnMaskSnippet,
		"__TIMEZONE_CORE__", timezoneCoreJSSnippet,
		"__NAVIGATOR_SHIELD__", navigatorIdentityShieldSnippet,
		"__INTL_DATE_SHIELD__", buildIntlAndDateShieldSnippet(true),
		"__LOCALE_EXT_SHIELD__", localeExtensionsShieldSnippet,
		"__CJK_FONT_SHIELD__", cjkFontShieldSnippet,
		"__WORKER_SHIELD__", buildWorkerShieldSnippet(true),
	).Replace(tmpl)
}

func buildNativeShieldBackgroundJS(ctrlURLJSON, tzJSON, locJSON string) string {
	const tmpl = `const CONTROL_URL = __CONTROL_URL__;
const LOADED_TZ = __LOADED_TZ__;
const LOADED_LOCALE = __LOADED_LOCALE__;
const ALARM_NAME = 'pc-native-shield-sync';

function enforceWebRTCPolicy() {
  try {
    if (chrome.privacy && chrome.privacy.network && chrome.privacy.network.webRTCIPHandlingPolicy) {
      chrome.privacy.network.webRTCIPHandlingPolicy.set(
        { value: 'disable_non_proxied_udp' },
        () => { void chrome.runtime.lastError; }
      );
    }
  } catch {}
}

function setBadge(text, color) {
  try {
    chrome.action.setBadgeText({ text });
    chrome.action.setBadgeBackgroundColor({ color });
  } catch {}
}

async function pollStatus() {
  try {
    const resp = await fetch(CONTROL_URL + '/api/status', { cache: 'no-store' });
    if (!resp.ok) throw new Error('HTTP ' + resp.status);
    const data = await resp.json();
    const gate = data.gate || {};
    const open = !!gate.open;
    const ip = (gate.last_good && gate.last_good.egress_ip) || (gate.last_probe && gate.last_probe.egress_ip) || '';
    const tz = gate.effective_timezone || LOADED_TZ;
    const loc = gate.effective_language || LOADED_LOCALE;
    const pacMatches = !!(data.browser_pac && data.browser_pac.matches_peaceclaude);

    setBadge(open ? 'ON' : 'LOCK', open ? '#16A34A' : '#E5484D');

    await chrome.storage.local.set({
      pcStatus: {
        reachable: true,
        open,
        ip,
        timezone: tz,
        locale: loc,
        pacMatches,
        updatedAt: Date.now(),
      },
    });

    if ((tz && tz !== LOADED_TZ) || (loc && loc !== LOADED_LOCALE)) {
      const store = await chrome.storage.local.get(['pcLastReloadAt']);
      const last = Number(store.pcLastReloadAt || 0);
      if (Date.now() - last > 30000) {
        await chrome.storage.local.set({ pcLastReloadAt: Date.now() });
        chrome.runtime.reload();
      }
    }
  } catch {
    setBadge('OFF', '#6B7280');
    try {
      await chrome.storage.local.set({
        pcStatus: {
          reachable: false,
          open: false,
          ip: '',
          timezone: LOADED_TZ,
          locale: LOADED_LOCALE,
          pacMatches: false,
          updatedAt: Date.now(),
        },
      });
    } catch {}
  }
}

chrome.runtime.onInstalled.addListener(() => {
  enforceWebRTCPolicy();
  chrome.alarms.create(ALARM_NAME, { periodInMinutes: 1 });
  pollStatus();
});

chrome.runtime.onStartup.addListener(() => {
  enforceWebRTCPolicy();
  chrome.alarms.create(ALARM_NAME, { periodInMinutes: 1 });
  pollStatus();
});

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm && alarm.name === ALARM_NAME) {
    pollStatus();
  }
});
`
	return strings.NewReplacer(
		"__CONTROL_URL__", ctrlURLJSON,
		"__LOADED_TZ__", tzJSON,
		"__LOADED_LOCALE__", locJSON,
	).Replace(tmpl)
}

const nativeShieldPopupHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8" />
  <title>PeaceClaude Native Shield</title>
  <style>
    :root { color-scheme: light dark; }
    body {
      margin: 0; padding: 14px; width: 310px;
      font-family: -apple-system, BlinkMacSystemFont, "SF Pro Text", sans-serif;
      background: #14181e; color: #eceff4; font-size: 12.5px;
    }
    .head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
    .brand { display: flex; align-items: center; gap: 8px; font-weight: 600; font-size: 13.5px; }
    .dot { width: 8px; height: 8px; border-radius: 50%; background: #6b7280; }
    .dot.ok { background: #22c55e; }
    .dot.bad { background: #ef4444; }
    .pill { font-size: 11px; padding: 2px 8px; border-radius: 999px; border: 1px solid #303846; color: #9ca3af; }
    .pill.ok { color: #4ade80; border-color: rgba(74,222,128,.35); background: rgba(34,197,94,.12); }
    .pill.warn { color: #fbbf24; border-color: rgba(251,191,36,.35); background: rgba(245,158,11,.12); }
    .card { background: #1c222b; border: 1px solid #2b3441; border-radius: 10px; padding: 10px 12px; margin-bottom: 10px; }
    .row { display: flex; justify-content: space-between; gap: 8px; padding: 4px 0; }
    .k { color: #9ca3af; }
    .v { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; text-align: right; color: #e5e7eb; }
    .note { font-size: 11.5px; color: #9ca3af; line-height: 1.5; margin-bottom: 10px; }
    button {
      width: 100%; padding: 8px 12px; border: none; border-radius: 8px; cursor: pointer;
      background: #d97757; color: #fff; font-weight: 500; font-size: 12.5px;
    }
    button:hover { filter: brightness(1.08); }
  </style>
</head>
<body>
  <div class="head">
    <div class="brand"><span class="dot" id="gate-dot"></span><span>PeaceClaude Native Shield</span></div>
    <span class="pill" id="tab-pill">检测中</span>
  </div>
  <div class="card">
    <div class="row"><span class="k">出口门禁</span><span class="v" id="v-gate">—</span></div>
    <div class="row"><span class="k">静态出口 IP</span><span class="v" id="v-ip">—</span></div>
    <div class="row"><span class="k">PAC 分流</span><span class="v" id="v-pac">—</span></div>
    <div class="row"><span class="k">护航时区 / 语言</span><span class="v" id="v-fp">—</span></div>
  </div>
  <div class="note" id="tab-note">仅对 Claude / Anthropic 与诊断域名注入时区、语言、字体与 WebRTC 防护，其余日常网站 100% 零干预直连。</div>
  <button id="btn-open-ui" type="button">打开 PeaceClaude 控制台</button>
  <script src="popup.js"></script>
</body>
</html>
`

func buildNativeShieldPopupJS(ctrlURLJSON, tzJSON, locJSON, domainsJSON string) string {
	const tmpl = `const CONTROL_URL = __CONTROL_URL__;
const DEFAULT_TZ = __DEFAULT_TZ__;
const DEFAULT_LOC = __DEFAULT_LOC__;
const PROTECTED_DOMAINS = __DOMAINS__;

function isProtectedHost(host) {
  const h = String(host || '').toLowerCase().replace(/\.$/, '');
  return PROTECTED_DOMAINS.some((d) => h === d || h.endsWith('.' + d));
}

async function initPopup() {
  const dot = document.getElementById('gate-dot');
  const pill = document.getElementById('tab-pill');
  const vGate = document.getElementById('v-gate');
  const vIP = document.getElementById('v-ip');
  const vPac = document.getElementById('v-pac');
  const vFP = document.getElementById('v-fp');

  try {
    if (chrome.tabs && chrome.tabs.query) {
      chrome.tabs.query({ active: true, currentWindow: true }, (tabs) => {
        const rawUrl = (tabs && tabs[0] && tabs[0].url) || '';
        try {
          const u = new URL(rawUrl);
          if (isProtectedHost(u.hostname)) {
            pill.className = 'pill ok';
            pill.textContent = '已护航域';
          } else {
            pill.className = 'pill warn';
            pill.textContent = '日常直连域';
          }
        } catch {
          pill.textContent = '本地页';
        }
      });
    }
  } catch {}

  let status = null;
  try {
    const resp = await fetch(CONTROL_URL + '/api/status', { cache: 'no-store' });
    if (resp.ok) {
      const data = await resp.json();
      const gate = data.gate || {};
      status = {
        reachable: true,
        open: !!gate.open,
        ip: (gate.last_good && gate.last_good.egress_ip) || (gate.last_probe && gate.last_probe.egress_ip) || '—',
        pacMatches: !!(data.browser_pac && data.browser_pac.matches_peaceclaude),
        timezone: gate.effective_timezone || DEFAULT_TZ,
        locale: gate.effective_language || DEFAULT_LOC,
      };
    }
  } catch {}

  if (!status && chrome.storage && chrome.storage.local) {
    const saved = await chrome.storage.local.get(['pcStatus']);
    status = saved.pcStatus || null;
  }

  if (!status || !status.reachable) {
    dot.className = 'dot bad';
    vGate.textContent = '网关未连接';
    vIP.textContent = '—';
    vPac.textContent = '未知';
    vFP.textContent = DEFAULT_TZ + ' · ' + DEFAULT_LOC;
  } else {
    dot.className = 'dot ' + (status.open ? 'ok' : 'bad');
    vGate.textContent = status.open ? 'OPEN · 安全放行' : 'LOCKED · 已阻断';
    vIP.textContent = status.ip || '—';
    vPac.textContent = status.pacMatches ? '已接管 Claude' : '未开启 PAC';
    vFP.textContent = (status.timezone || DEFAULT_TZ) + ' · ' + (status.locale || DEFAULT_LOC);
  }

  document.getElementById('btn-open-ui').addEventListener('click', () => {
    if (chrome.tabs && chrome.tabs.create) {
      chrome.tabs.create({ url: CONTROL_URL });
    } else {
      window.open(CONTROL_URL, '_blank');
    }
  });
}

initPopup();
`
	return strings.NewReplacer(
		"__CONTROL_URL__", ctrlURLJSON,
		"__DEFAULT_TZ__", tzJSON,
		"__DEFAULT_LOC__", locJSON,
		"__DOMAINS__", domainsJSON,
	).Replace(tmpl)
}

// InspectNativeBrowsers inspects installed macOS browsers and checks whether each Chromium
// browser currently has PeaceClaude Native Shield loaded and enabled, alongside system PAC state.
func InspectNativeBrowsers(cfg *config.Config, pac host.BrowserPACStatus, gate ...egress.GateSnapshot) NativeBrowserReport {
	home, _ := os.UserHomeDir()
	var g egress.GateSnapshot
	if len(gate) > 0 {
		g = gate[0]
	}
	return inspectNativeBrowsersWithRoots(cfg, pac, g, "/Applications", home)
}

func inspectNativeBrowsersWithRoots(cfg *config.Config, pac host.BrowserPACStatus, gate egress.GateSnapshot, appsDir, homeDir string) NativeBrowserReport {
	extDir := config.NativeBrowserShieldDir()
	_, extErr := os.Stat(filepath.Join(extDir, "manifest.json"))
	tz, locale := resolveNativeShieldTarget(cfg, gate)

	var selectedSet map[string]bool
	if cfg != nil && len(cfg.Shield.ProtectedBrowsers) > 0 {
		selectedSet = make(map[string]bool, len(cfg.Shield.ProtectedBrowsers))
		for _, id := range cfg.Shield.ProtectedBrowsers {
			selectedSet[strings.ToLower(strings.TrimSpace(id))] = true
		}
	}

	out := NativeBrowserReport{
		ExtensionDir:     extDir,
		ExtensionReady:   extErr == nil,
		PACEnabled:       pac.MatchesPeace,
		TargetTimezone:   tz,
		TargetLocale:     locale,
		ProtectedDomains: NativeShieldProtectedDomains(cfg),
		Browsers:         make([]NativeBrowserInfo, 0, len(nativeBrowserCatalog)),
	}

	for _, spec := range nativeBrowserCatalog {
		appPath := findBrowserAppPath(appsDir, homeDir, spec.AppBundle)
		installed := appPath != ""
		selected := selectedSet[spec.ID]

		info := NativeBrowserInfo{
			ID:                spec.ID,
			Name:              spec.Name,
			AppPath:           appPath,
			Installed:         installed,
			SupportsExtension: spec.SupportsExtension,
			ExtensionsURL:     spec.ExtensionsURL,
			Selected:          selected,
		}

		if spec.SupportsExtension && installed {
			userDataDir := filepath.Join(homeDir, spec.UserDataRelPath)
			loaded, enabled, loadedProfs, missingProfs, totalProfs := scanChromiumNativeShieldProfiles(userDataDir, extDir)
			info.ExtensionLoaded = loaded
			info.ExtensionEnabled = enabled
			info.LoadedProfiles = loadedProfs
			info.MissingProfiles = missingProfs
			info.TotalProfiles = totalProfs
		}

		switch {
		case !installed:
			info.ProtectionLevel = "unprotected"
			info.Note = "本机未检测到此浏览器"
		case !spec.SupportsExtension:
			if pac.MatchesPeace {
				info.ProtectionLevel = "network_only"
				info.Note = "Safari 仅支持网络层 PAC 静态 IP 分流（不支持 Chromium 扩展改写时区/语言/字体，安全测试请用 Arc/Chrome）"
			} else {
				info.ProtectionLevel = "unprotected"
				info.Note = "未开启系统 PAC；Safari 不支持扩展级指纹护航"
			}
		case pac.MatchesPeace && info.ExtensionEnabled && len(info.MissingProfiles) == 0:
			info.ProtectionLevel = "full"
			info.Note = fmt.Sprintf("全链路已护航（PAC + 定向扩展已启用于全部 %d 个配置档：%s）", max(1, len(info.LoadedProfiles)), strings.Join(info.LoadedProfiles, ", "))
		case pac.MatchesPeace && info.ExtensionEnabled && len(info.MissingProfiles) > 0:
			info.ProtectionLevel = "partial"
			info.Note = fmt.Sprintf("已启用于 %d/%d 个配置档；Arc/Chrome 扩展按 Profile/Space 隔离，如常用 Space 未覆盖需切换到该 Space 补载", len(info.LoadedProfiles), info.TotalProfiles)
		case pac.MatchesPeace && info.ExtensionLoaded:
			info.ProtectionLevel = "network_only"
			info.Note = fmt.Sprintf("网络层 PAC 已护航；扩展已在 %s 载入但处于禁用状态，请在扩展页启用", strings.Join(info.LoadedProfiles, ", "))
		case pac.MatchesPeace:
			info.ProtectionLevel = "network_only"
			info.Note = "仅网络层 PAC 已护航（静态 IP）；受 Chromium 安全限制，单纯重启浏览器不会自动加载扩展，请点下方按钮并把目录拖进扩展页（10 秒完成，永久生效）"
		case info.ExtensionEnabled:
			info.ProtectionLevel = "unprotected"
			info.Note = "扩展已载入，但系统浏览器 PAC 未开启（存在直连风险），请勾选并开启护航"
		default:
			info.ProtectionLevel = "unprotected"
			info.Note = "尚未开启 PAC 与指纹护航扩展"
		}
		out.Browsers = append(out.Browsers, info)
	}
	return out
}

func findBrowserAppPath(appsDir, homeDir, appBundle string) string {
	for _, root := range []string{appsDir, filepath.Join(homeDir, "Applications")} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		p := filepath.Join(root, appBundle)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return ""
}

// isChromiumExtensionEntryEnabled interprets Chromium's extensions.settings entry across
// legacy (`state: 1, disable_reasons: 0`) and Chromium 149+/Arc 158+ (`state` omitted, `disable_reasons: []` or `[8192]`).
func isChromiumExtensionEntryEnabled(entry map[string]any) bool {
	if entry == nil {
		return false
	}
	if stateNum, hasState := entry["state"].(float64); hasState {
		if int(stateNum) == 0 {
			return false
		}
	}
	switch reasons := entry["disable_reasons"].(type) {
	case []any:
		if len(reasons) > 0 {
			return false
		}
	case float64:
		if int(reasons) != 0 {
			return false
		}
	}
	return true
}

func readChromiumProfileDisplayNames(userDataDir string) map[string]string {
	out := map[string]string{}
	raw, err := os.ReadFile(filepath.Join(userDataDir, "Local State"))
	if err != nil || len(raw) == 0 {
		return out
	}
	var doc struct {
		Profile struct {
			InfoCache map[string]struct {
				Name string `json:"name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return out
	}
	for dirName, meta := range doc.Profile.InfoCache {
		if strings.TrimSpace(meta.Name) != "" {
			out[dirName] = strings.TrimSpace(meta.Name)
		}
	}
	return out
}

func formatChromiumProfileLabel(dirName string, displayNames map[string]string) string {
	disp := strings.TrimSpace(displayNames[dirName])
	if disp == "" || strings.EqualFold(disp, dirName) {
		return dirName
	}
	return fmt.Sprintf("%s (%s)", dirName, disp)
}

func inspectProfileExtensionFiles(profDir, cleanExt string) (foundLoaded, foundEnabled bool) {
	// Chromium 149+ & Arc 158+ store extension settings primarily in Secure Preferences.
	for _, fn := range []string{"Secure Preferences", "Preferences"} {
		raw, err := os.ReadFile(filepath.Join(profDir, fn))
		if err != nil || len(raw) == 0 {
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
			matchPath := extPath != "" && (cleanPath == cleanExt || strings.HasSuffix(cleanPath, string(filepath.Separator)+"native-browser-shield"))
			matchName := strings.EqualFold(strings.TrimSpace(mName), nativeShieldExtName)
			if !matchPath && !matchName {
				continue
			}
			foundLoaded = true
			if isChromiumExtensionEntryEnabled(entry) {
				foundEnabled = true
			}
		}
	}
	return foundLoaded, foundEnabled
}

func scanChromiumNativeShieldProfiles(userDataDir, extDir string) (loaded, enabled bool, loadedProfs, missingProfs []string, totalProfs int) {
	entries, err := os.ReadDir(userDataDir)
	if err != nil {
		return false, false, nil, nil, 0
	}
	displayNames := readChromiumProfileDisplayNames(userDataDir)
	cleanExt := filepath.Clean(extDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name != "Default" && !strings.HasPrefix(name, "Profile ") {
			continue
		}
		// Skip Arc internal hidden system profile (__ARC_SYSTEM_PROFILE).
		if strings.HasPrefix(displayNames[name], "__ARC_") {
			continue
		}
		profDir := filepath.Join(userDataDir, name)
		hasSec := false
		if _, err := os.Stat(filepath.Join(profDir, "Secure Preferences")); err == nil {
			hasSec = true
		}
		hasPref := false
		if _, err := os.Stat(filepath.Join(profDir, "Preferences")); err == nil {
			hasPref = true
		}
		if !hasSec && !hasPref {
			continue
		}
		totalProfs++
		label := formatChromiumProfileLabel(name, displayNames)
		profLoaded, profEnabled := inspectProfileExtensionFiles(profDir, cleanExt)
		if profLoaded {
			loaded = true
			if !slices.Contains(loadedProfs, label) {
				loadedProfs = append(loadedProfs, label)
			}
		}
		if profEnabled {
			enabled = true
		} else if !slices.Contains(missingProfs, label) {
			missingProfs = append(missingProfs, label)
		}
	}
	sort.Strings(loadedProfs)
	sort.Strings(missingProfs)
	return loaded, enabled, loadedProfs, missingProfs, totalProfs
}

// OpenNativeBrowserExtensionGuide ensures extension files exist, reveals the unpacked folder in Finder,
// activates the target macOS browser if installed, and returns step-by-step instructions.
func OpenNativeBrowserExtensionGuide(browserID, extDir string) (GuideResult, error) {
	id := strings.ToLower(strings.TrimSpace(browserID))
	var spec *nativeBrowserSpec
	for i := range nativeBrowserCatalog {
		if nativeBrowserCatalog[i].ID == id {
			spec = &nativeBrowserCatalog[i]
			break
		}
	}
	if spec == nil {
		return GuideResult{}, fmt.Errorf("未知浏览器 ID %q", browserID)
	}
	res := GuideResult{
		OK:            true,
		BrowserID:     spec.ID,
		BrowserName:   spec.Name,
		ExtensionsURL: spec.ExtensionsURL,
		ExtensionDir:  extDir,
	}
	if !spec.SupportsExtension {
		res.Steps = []string{
			"Safari 不支持 Chromium MV3 扩展，已通过系统 PAC 保护其静态 IP 出口",
			"如需让网页通过 fuck-claude.app 时区/语言/字体全部检测，请选用 Arc 或 Google Chrome",
		}
		res.Message = "Safari 仅支持 PAC 网络层静态 IP 护航，不支持扩展级指纹伪装"
		return res, nil
	}

	home, _ := os.UserHomeDir()
	appPath := findBrowserAppPath("/Applications", home, spec.AppBundle)
	if appPath != "" {
		_ = openAppTarget(appPath)
	} else if spec.ID == "chrome" || spec.ID == "edge" || spec.ID == "brave" {
		if runtime.GOOS != "darwin" {
			_ = openExternalTarget(spec.ExtensionsURL)
		}
	}
	if _, err := os.Stat(extDir); err == nil {
		_ = revealPathInFileManager(extDir)
	}

	openHint := "直接在地址栏粘贴并打开"
	dragHint := "直接把文件管理器已选中的 native-browser-shield 文件夹拖入扩展页空白处即载入"
	if runtime.GOOS == "darwin" {
		openHint = "在地址栏按 Cmd+L 粘贴并打开"
		dragHint = fmt.Sprintf("最省事方式：直接把 Finder 已高亮选中的 native-browser-shield 文件夹拖入扩展页空白处即载入（因 .peaceclaude 默认隐藏，若点「加载已解压的扩展程序」，请在文件弹窗按 Cmd+Shift+G 粘贴：%s）", extDir)
	} else {
		dragHint = fmt.Sprintf("%s（或点击「加载已解压的扩展程序」选择目录：%s）", dragHint, extDir)
	}

	res.Steps = []string{
		fmt.Sprintf("若使用 %s 多 Space / 多配置档，请先切到你平时访问 Claude 与 fuck-claude.app 的那个 Space（不同 Space 扩展互不共享）", spec.Name),
		fmt.Sprintf("%s %s（已为你自动复制），开启右上角「开发者模式 (Developer mode)」", openHint, spec.ExtensionsURL),
		dragHint,
		"切回本控制台点击「刷新检测状态」变成「全链路已护航」，随后回到 fuck-claude.app 点 RESCAN 即可达成 2/100 LOW RISK",
	}
	res.Message = fmt.Sprintf("已打开扩展目录 %s；请将该文件夹拖入 %s 的「开发者模式」页面（单纯重启浏览器不会自动加载）", extDir, spec.ExtensionsURL)
	return res, nil
}
