package launcher

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/host"
)

func TestEnsureNativeBrowserShieldArtifactsAndSyntax(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Shield.ExtraClaudeDomains = []string{"https://custom-claude.example.org/path", "*.claude.ai"}
	gate := egress.GateSnapshot{
		Open:              true,
		EffectiveTimezone: "America/Denver",
		EffectiveLanguage: "en-US",
	}

	extDir, err := EnsureNativeBrowserShield(cfg, gate)
	if err != nil {
		t.Fatalf("EnsureNativeBrowserShield: %v", err)
	}

	expectedFiles := []string{
		"manifest.json", "rules.json", "config.js", "shield.js",
		"background.js", "popup.html", "popup.js", "icon48.png", "icon128.png",
	}
	for _, fn := range expectedFiles {
		fi, err := os.Stat(filepath.Join(extDir, fn))
		if err != nil || fi.Size() == 0 {
			t.Fatalf("missing or empty extension artifact %s: err=%v", fn, err)
		}
	}

	rawManifest, _ := os.ReadFile(filepath.Join(extDir, "manifest.json"))
	var manifest struct {
		ManifestVersion int `json:"manifest_version"`
		ContentScripts  []struct {
			Matches []string `json:"matches"`
			JS      []string `json:"js"`
			World   string   `json:"world"`
		} `json:"content_scripts"`
	}
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	if manifest.ManifestVersion != 3 || len(manifest.ContentScripts) != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	cs := manifest.ContentScripts[0]
	if cs.World != "MAIN" || !slices.Equal(cs.JS, []string{"config.js", "shield.js"}) {
		t.Fatalf("unexpected content script spec: %+v", cs)
	}
	if slices.Contains(cs.Matches, "<all_urls>") || slices.Contains(cs.Matches, "*://*/*") {
		t.Fatalf("content_scripts must strictly target Claude/test domains, got %v", cs.Matches)
	}
	if !slices.Contains(cs.Matches, "https://claude.ai/*") || !slices.Contains(cs.Matches, "https://fuck-claude.app/*") || !slices.Contains(cs.Matches, "https://custom-claude.example.org/*") {
		t.Fatalf("content_scripts missing expected matches: %v", cs.Matches)
	}

	rawRules, _ := os.ReadFile(filepath.Join(extDir, "rules.json"))
	var rules []struct {
		Condition struct {
			RequestDomains []string `json:"requestDomains"`
			URLFilter      string   `json:"urlFilter"`
		} `json:"condition"`
	}
	if err := json.Unmarshal(rawRules, &rules); err != nil || len(rules) != 1 {
		t.Fatalf("rules json: %v", err)
	}
	if rules[0].Condition.URLFilter != "" || !slices.Contains(rules[0].Condition.RequestDomains, "fuck-claude.app") {
		t.Fatalf("DNR rule must scope via requestDomains only: %+v", rules[0].Condition)
	}

	shieldRaw, _ := os.ReadFile(filepath.Join(extDir, "shield.js"))
	if strings.Contains(string(shieldRaw), "navigator.connection") || strings.Contains(string(shieldRaw), "NetworkInformation") {
		t.Fatalf("native shield must keep network connection untouched per design decision")
	}

	if nodeBin, err := exec.LookPath("node"); err == nil {
		for _, jsFile := range []string{"config.js", "shield.js", "background.js", "popup.js"} {
			if out, err := exec.Command(nodeBin, "--check", filepath.Join(extDir, jsFile)).CombinedOutput(); err != nil {
				t.Fatalf("%s syntax check failed: %v (%s)", jsFile, err, string(out))
			}
		}

		// Simulate execution in an Asia/Shanghai host environment to verify Date, Intl, Navigator, and navigator.connection behavior.
		harness := `
const fs = require('fs');
const vm = require('vm');
function Nav() {}
Nav.prototype = {
  get language() { return 'zh-CN'; },
  get languages() { return ['zh-CN', 'zh']; },
  get doNotTrack() { return null; },
  get connection() { return { effectiveType: '4g', downlink: 10 }; },
};
const nav = new Nav();
function Canvas2D() { this._f = '12px Arial'; }
Object.defineProperty(Canvas2D.prototype, 'font', {
  get() { return this._f; },
  set(v) { this._f = String(v); },
  configurable: true,
  enumerable: true,
});
const ctx = {
  window: null,
  globalThis: null,
  Navigator: Nav,
  navigator: nav,
  CanvasRenderingContext2D: Canvas2D,
  Date,
  Intl,
  Object,
  Array,
  Number,
  String,
  Math,
  Proxy,
  Reflect,
  WeakMap,
  Function,
  JSON,
  console,
};
ctx.window = ctx;
ctx.globalThis = ctx;
vm.createContext(ctx);
vm.runInContext(fs.readFileSync(process.argv[1], 'utf8'), ctx);
vm.runInContext(fs.readFileSync(process.argv[2], 'utf8'), ctx);

const winter = new ctx.Date('2026-01-15T12:00:00Z');
const summer = new ctx.Date('2026-07-15T12:00:00Z');
const dtfOpts = ctx.Intl.DateTimeFormat().resolvedOptions();
const res = {
  tz: dtfOpts.timeZone,
  locale: dtfOpts.locale,
  winterOffset: winter.getTimezoneOffset(),
  summerOffset: summer.getTimezoneOffset(),
  toStr: winter.toString(),
  lang: ctx.navigator.language,
  langs: Array.from(ctx.navigator.languages),
  dnt: ctx.navigator.doNotTrack,
  gpc: ctx.navigator.globalPrivacyControl,
  connType: ctx.navigator.connection.effectiveType,
  isDateInstance: winter instanceof ctx.Date,
};
const canvas = new Canvas2D();
canvas.font = '16px "PingFang SC", sans-serif';
res.canvasFont = canvas.font;
console.log(JSON.stringify(res));
`
		cmd := exec.Command(nodeBin, "-e", harness, filepath.Join(extDir, "config.js"), filepath.Join(extDir, "shield.js"))
		cmd.Env = append(os.Environ(), "TZ=Asia/Shanghai")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("node shield simulation failed: %v (%s)", err, string(out))
		}
		var sim struct {
			TZ             string   `json:"tz"`
			Locale         string   `json:"locale"`
			WinterOffset   int      `json:"winterOffset"`
			SummerOffset   int      `json:"summerOffset"`
			ToStr          string   `json:"toStr"`
			Lang           string   `json:"lang"`
			Langs          []string `json:"langs"`
			DNT            string   `json:"dnt"`
			GPC            bool     `json:"gpc"`
			ConnType       string   `json:"connType"`
			IsDateInstance bool     `json:"isDateInstance"`
			CanvasFont     string   `json:"canvasFont"`
		}
		if err := json.Unmarshal(out, &sim); err != nil {
			t.Fatalf("parse simulation json: %v (%s)", err, string(out))
		}
		if sim.TZ != "America/Denver" || sim.Locale != "en-US" {
			t.Fatalf("expected America/Denver & en-US, got %+v", sim)
		}
		if sim.WinterOffset != 420 || sim.SummerOffset != 360 {
			t.Fatalf("expected Denver DST-aware offsets 420/360, got %d/%d", sim.WinterOffset, sim.SummerOffset)
		}
		if !strings.Contains(sim.ToStr, "GMT-0700") || strings.Contains(sim.ToStr, "China") {
			t.Fatalf("expected GMT-0700 US string, got %q", sim.ToStr)
		}
		if sim.Lang != "en-US" || !slices.Equal(sim.Langs, []string{"en-US", "en"}) || sim.DNT != "1" || !sim.GPC {
			t.Fatalf("unexpected navigator identity overrides: %+v", sim)
		}
		if sim.ConnType != "4g" || !sim.IsDateInstance {
			t.Fatalf("expected native connection & instanceof Date intact: %+v", sim)
		}
		if strings.Contains(sim.CanvasFont, "PingFang") {
			t.Fatalf("expected CJK font stripped from canvas font, got %q", sim.CanvasFont)
		}
	}
}

func TestInspectNativeBrowsersWithRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PEACECLAUDE_HOME", filepath.Join(home, ".peaceclaude"))
	apps := filepath.Join(home, "ApplicationsRoot")
	for _, app := range []string{"Arc.app", "Google Chrome.app", "Safari.app"} {
		if err := os.MkdirAll(filepath.Join(apps, app), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	extDir := config.NativeBrowserShieldDir()
	arcProf1 := filepath.Join(home, "Library/Application Support/Arc/User Data/Profile 1")
	chromeDef := filepath.Join(home, "Library/Application Support/Google/Chrome/Default")
	if err := os.MkdirAll(arcProf1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(chromeDef, 0o755); err != nil {
		t.Fatal(err)
	}
	arcPrefs := map[string]any{
		"extensions": map[string]any{
			"settings": map[string]any{
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": map[string]any{
					"location": 4,
					"path":     extDir,
					"state":    1,
					"manifest": map[string]any{"name": "PeaceClaude Native Shield"},
				},
			},
		},
	}
	b, _ := json.Marshal(arcPrefs)
	_ = os.WriteFile(filepath.Join(arcProf1, "Preferences"), b, 0o600)
	_ = os.WriteFile(filepath.Join(chromeDef, "Preferences"), []byte(`{"extensions":{"settings":{}}}`), 0o600)

	cfg := config.Default()
	cfg.Shield.ProtectedBrowsers = []string{"arc", "chrome"}
	pac := host.BrowserPACStatus{MatchesPeace: true}
	rep := inspectNativeBrowsersWithRoots(cfg, pac, egress.GateSnapshot{EffectiveTimezone: "America/Denver"}, apps, home)

	byID := map[string]NativeBrowserInfo{}
	for _, b := range rep.Browsers {
		byID[b.ID] = b
	}
	if !byID["arc"].Installed || !byID["arc"].Selected || !byID["arc"].ExtensionEnabled || byID["arc"].ProtectionLevel != "full" {
		t.Fatalf("expected arc=full protection, got %+v", byID["arc"])
	}
	if !byID["chrome"].Installed || !byID["chrome"].Selected || byID["chrome"].ExtensionLoaded || byID["chrome"].ProtectionLevel != "network_only" {
		t.Fatalf("expected chrome=network_only, got %+v", byID["chrome"])
	}
	if !byID["safari"].Installed || byID["safari"].SupportsExtension || byID["safari"].ProtectionLevel != "network_only" {
		t.Fatalf("expected safari=network_only without extension support, got %+v", byID["safari"])
	}
}


func TestInspectNativeBrowsersSecurePreferencesAndDisableReasons(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PEACECLAUDE_HOME", filepath.Join(home, ".peaceclaude"))
	apps := filepath.Join(home, "ApplicationsRoot")
	for _, app := range []string{"Arc.app", "Google Chrome.app"} {
		if err := os.MkdirAll(filepath.Join(apps, app), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	extDir := config.NativeBrowserShieldDir()
	arcUserData := filepath.Join(home, "Library/Application Support/Arc/User Data")
	chromeUserData := filepath.Join(home, "Library/Application Support/Google/Chrome")
	arcDef := filepath.Join(arcUserData, "Default")
	arcP1 := filepath.Join(arcUserData, "Profile 1")
	arcSys := filepath.Join(arcUserData, "Profile 2")
	chromeDef := filepath.Join(chromeUserData, "Default")
	for _, dir := range []string{arcDef, arcP1, arcSys, chromeDef} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	localState := `{
  "profile": {
    "info_cache": {
      "Default": {"name": "Work Space"},
      "Profile 1": {"name": "Personal"},
      "Profile 2": {"name": "__ARC_SYSTEM_PROFILE"}
    }
  }
}`
	_ = os.WriteFile(filepath.Join(arcUserData, "Local State"), []byte(localState), 0o600)

	// Arc Default: Preferences empty, Secure Preferences has enabled shield (omits state, disable_reasons=[]).
	_ = os.WriteFile(filepath.Join(arcDef, "Preferences"), []byte(`{"extensions":{"settings":{}}}`), 0o600)
	arcSecDef, _ := json.Marshal(map[string]any{
		"extensions": map[string]any{
			"settings": map[string]any{
				"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb": map[string]any{
					"location":        4,
					"path":            extDir,
					"disable_reasons": []int{},
					"manifest":        map[string]any{"name": "PeaceClaude Native Shield"},
				},
			},
		},
	})
	_ = os.WriteFile(filepath.Join(arcDef, "Secure Preferences"), arcSecDef, 0o600)
	// Arc Profile 1: Secure Preferences does not have shield yet.
	_ = os.WriteFile(filepath.Join(arcP1, "Secure Preferences"), []byte(`{"extensions":{"settings":{}}}`), 0o600)
	// Arc Profile 2 (__ARC_SYSTEM_PROFILE): must be excluded from totals & missing_profiles.
	_ = os.WriteFile(filepath.Join(arcSys, "Secure Preferences"), []byte(`{"extensions":{"settings":{}}}`), 0o600)

	// Chrome Default: loaded in Secure Preferences, but disabled (disable_reasons: [8192]).
	chromeSec, _ := json.Marshal(map[string]any{
		"extensions": map[string]any{
			"settings": map[string]any{
				"cccccccccccccccccccccccccccccccc": map[string]any{
					"location":        4,
					"path":            extDir,
					"disable_reasons": []int{8192},
					"manifest":        map[string]any{"name": "PeaceClaude Native Shield"},
				},
			},
		},
	})
	_ = os.WriteFile(filepath.Join(chromeDef, "Secure Preferences"), chromeSec, 0o600)

	cfg := config.Default()
	cfg.Shield.ProtectedBrowsers = []string{"arc", "chrome"}
	pac := host.BrowserPACStatus{MatchesPeace: true}
	rep := inspectNativeBrowsersWithRoots(cfg, pac, egress.GateSnapshot{EffectiveTimezone: "America/Denver"}, apps, home)

	byID := map[string]NativeBrowserInfo{}
	for _, b := range rep.Browsers {
		byID[b.ID] = b
	}
	arc := byID["arc"]
	if !arc.ExtensionLoaded || !arc.ExtensionEnabled || arc.ProtectionLevel != "partial" {
		t.Fatalf("expected arc partial protection, got %+v", arc)
	}
	if arc.TotalProfiles != 2 || len(arc.LoadedProfiles) != 1 || arc.LoadedProfiles[0] != "Default (Work Space)" {
		t.Fatalf("expected Arc 2 real profiles (1 loaded Default (Work Space)), got total=%d loaded=%v missing=%v", arc.TotalProfiles, arc.LoadedProfiles, arc.MissingProfiles)
	}
	if len(arc.MissingProfiles) != 1 || arc.MissingProfiles[0] != "Profile 1 (Personal)" {
		t.Fatalf("expected missing=[Profile 1 (Personal)] excluding __ARC_SYSTEM_PROFILE, got %v", arc.MissingProfiles)
	}

	chrome := byID["chrome"]
	if !chrome.ExtensionLoaded || chrome.ExtensionEnabled || chrome.ProtectionLevel != "network_only" {
		t.Fatalf("expected disabled chrome extension (disable_reasons=[8192]) to report loaded=true enabled=false level=network_only, got %+v", chrome)
	}
}

func TestDeepFontIntlLocaleCompareAndWorkerWrapperSimulation(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable for JS shield verification")
	}

	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	gate := egress.GateSnapshot{
		Open:              true,
		EffectiveTimezone: "America/Denver",
		EffectiveLanguage: "en-US",
	}
	extDir, err := EnsureNativeBrowserShield(cfg, gate)
	if err != nil {
		t.Fatal(err)
	}

	rawRules, _ := os.ReadFile(filepath.Join(extDir, "rules.json"))
	if !strings.Contains(string(rawRules), `"websocket"`) || !strings.Contains(string(rawRules), `"stylesheet"`) || !strings.Contains(string(rawRules), `"font"`) {
		t.Fatalf("expected DNR rules to cover websocket/stylesheet/font resourceTypes: %s", string(rawRules))
	}

	harness := `
const fs = require('fs');
const vm = require('vm');
function Nav() {}
Nav.prototype = {
  get language() { return 'zh-CN'; },
  get languages() { return ['zh-CN', 'zh']; },
  get doNotTrack() { return null; },
  get connection() { return { effectiveType: '4g', downlink: 10 }; },
};
function WorkerNav() {}
WorkerNav.prototype = {
  get language() { return 'zh-CN'; },
  get languages() { return ['zh-CN']; },
  get doNotTrack() { return null; },
  get connection() { return { effectiveType: '4g' }; },
};
function Canvas2D() { this._f = '12px Arial'; }
Object.defineProperty(Canvas2D.prototype, 'font', {
  get() { return this._f; },
  set(v) { this._f = String(v); },
  configurable: true,
  enumerable: true,
});
function Offscreen2D() { this._f = '12px Arial'; }
Object.defineProperty(Offscreen2D.prototype, 'font', {
  get() { return this._f; },
  set(v) { this._f = String(v); },
  configurable: true,
  enumerable: true,
});
function FakeFontFaceSet() {}
FakeFontFaceSet.prototype.check = function(font) {
  return true; // Simulates macOS system font match before shield
};
function FakeCSSStyle() { this._ff = ''; this._ct = ''; }
Object.defineProperty(FakeCSSStyle.prototype, 'fontFamily', {
  get() { return this._ff; },
  set(v) { this._ff = String(v); },
  configurable: true,
  enumerable: true,
});
Object.defineProperty(FakeCSSStyle.prototype, 'cssText', {
  get() { return this._ct; },
  set(v) { this._ct = String(v); },
  configurable: true,
  enumerable: true,
});
FakeCSSStyle.prototype.setProperty = function(prop, val) {
  if (String(prop).toLowerCase() === 'font-family') this._ff = String(val);
};

const createdBlobs = new Map();
let seq = 0;
class FakeBlob {
  constructor(parts, opts) {
    this.text = parts.join('');
    this.type = opts && opts.type;
  }
}
class FakeURL extends URL {}
FakeURL.createObjectURL = (blob) => {
  const u = 'blob:https://fuck-claude.app/' + (++seq);
  createdBlobs.set(u, blob.text);
  return u;
};
FakeURL.revokeObjectURL = (u) => { createdBlobs.delete(u); };

let lastWorkerScriptURL = '';
class OrigWorker {
  constructor(url) { lastWorkerScriptURL = String(url); }
}

const ctx = {
  window: null,
  globalThis: null,
  location: new URL('https://fuck-claude.app/'),
  Navigator: Nav,
  navigator: new Nav(),
  CanvasRenderingContext2D: Canvas2D,
  OffscreenCanvasRenderingContext2D: Offscreen2D,
  FontFaceSet: FakeFontFaceSet,
  CSSStyleDeclaration: FakeCSSStyle,
  Worker: OrigWorker,
  Blob: FakeBlob,
  URL: FakeURL,
  Date, Intl, Object, Array, Number, String, BigInt, Math, Proxy, Reflect, WeakMap, Set, Map, Function, JSON, setTimeout, console,
};
ctx.window = ctx;
ctx.globalThis = ctx;
vm.createContext(ctx);
vm.runInContext(fs.readFileSync(process.argv[1], 'utf8'), ctx);
vm.runInContext(fs.readFileSync(process.argv[2], 'utf8'), ctx);

const ffs = new FakeFontFaceSet();
const css = new FakeCSSStyle();
css.fontFamily = '"PingFang SC", monospace';
const cssByProp = new FakeCSSStyle();
cssByProp.setProperty('font-family', '"Hiragino Sans GB", serif');
const cssByText = new FakeCSSStyle();
cssByText.cssText = 'color: red; font-family: "Microsoft YaHei", monospace;';

let compareCalledWith = null;
const origCompare = String.prototype.localeCompare;
// Verify localeCompare wraps missing/empty locales while honoring explicit ones cleanly
const cmpDefault = ctx.String.prototype.localeCompare.call('a', 'b');
const cmpExplicit = ctx.String.prototype.localeCompare.call('a', 'b', 'de-DE');

const w = new ctx.Worker('/turnstile-worker.js');
const blobScript = createdBlobs.get(lastWorkerScriptURL) || '';

// Run captured Worker blob script inside an isolated Worker VM context (with WorkerNavigator & mocked importScripts)
let importedHref = '';
const wCtx = {
  globalThis: null,
  self: null,
  WorkerNavigator: WorkerNav,
  navigator: new WorkerNav(),
  OffscreenCanvasRenderingContext2D: Offscreen2D,
  Date, Intl, Object, Array, Number, String, BigInt, Math, Proxy, Reflect, WeakMap, Set, Function, JSON, console,
  importScripts(href) { importedHref = href; },
};
wCtx.globalThis = wCtx;
wCtx.self = wCtx;
vm.createContext(wCtx);
vm.runInContext(blobScript, wCtx);

const wOffscreen = new Offscreen2D();
wOffscreen.font = '18px "PingFang SC", sans-serif';

console.log(JSON.stringify({
  fontCheckCJK: ffs.check('12px "PingFang SC"'),
  fontCheckLatin: ffs.check('12px Arial'),
  cssFontFamily: css.fontFamily,
  cssPropFamily: cssByProp.fontFamily,
  cssText: cssByText.cssText,
  cmpOk: typeof cmpDefault === 'number' && typeof cmpExplicit === 'number',
  workerInstance: w instanceof OrigWorker,
  workerLang: wCtx.navigator.language,
  workerLangs: Array.from(wCtx.navigator.languages),
  workerGPC: wCtx.navigator.globalPrivacyControl,
  workerLocale: wCtx.Intl.DateTimeFormat().resolvedOptions().locale,
  workerTZ: wCtx.Intl.DateTimeFormat().resolvedOptions().timeZone,
  workerConn: wCtx.navigator.connection.effectiveType,
  workerCanvasFont: wOffscreen.font,
  importedHref,
}));
`
	cmd := exec.Command(nodeBin, "-e", harness, filepath.Join(extDir, "config.js"), filepath.Join(extDir, "shield.js"))
	cmd.Env = append(os.Environ(), "TZ=Asia/Shanghai")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("deep shield simulation failed: %v (%s)", err, string(out))
	}
	var sim struct {
		FontCheckCJK     bool     `json:"fontCheckCJK"`
		FontCheckLatin   bool     `json:"fontCheckLatin"`
		CSSFontFamily    string   `json:"cssFontFamily"`
		CSSPropFamily    string   `json:"cssPropFamily"`
		CSSText          string   `json:"cssText"`
		CmpOk            bool     `json:"cmpOk"`
		WorkerInstance   bool     `json:"workerInstance"`
		WorkerLang       string   `json:"workerLang"`
		WorkerLangs      []string `json:"workerLangs"`
		WorkerGPC        bool     `json:"workerGPC"`
		WorkerLocale     string   `json:"workerLocale"`
		WorkerTZ         string   `json:"workerTZ"`
		WorkerConn       string   `json:"workerConn"`
		WorkerCanvasFont string   `json:"workerCanvasFont"`
		ImportedHref     string   `json:"importedHref"`
	}
	if err := json.Unmarshal(out, &sim); err != nil {
		t.Fatalf("parse deep sim json: %v (%s)", err, string(out))
	}
	if sim.FontCheckCJK || !sim.FontCheckLatin {
		t.Fatalf("expected CJK FontFaceSet.check=false and Latin=true, got %+v", sim)
	}
	if strings.Contains(sim.CSSFontFamily, "PingFang") || strings.Contains(sim.CSSPropFamily, "Hiragino") || strings.Contains(sim.CSSText, "Microsoft YaHei") {
		t.Fatalf("expected CSSStyleDeclaration setters to strip CJK fonts, got %+v", sim)
	}
	if !strings.Contains(sim.CSSText, "color: red;") {
		t.Fatalf("expected unrelated cssText styles preserved, got %q", sim.CSSText)
	}
	if !sim.CmpOk || !sim.WorkerInstance || sim.WorkerLang != "en-US" || sim.WorkerLocale != "en-US" || sim.WorkerTZ != "America/Denver" || !sim.WorkerGPC || sim.WorkerConn != "4g" {
		t.Fatalf("unexpected worker/locale simulation state: %+v", sim)
	}
	if strings.Contains(sim.WorkerCanvasFont, "PingFang") || sim.ImportedHref != "https://fuck-claude.app/turnstile-worker.js" {
		t.Fatalf("unexpected worker font/importHref: %+v", sim)
	}
}
