package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/host"
)

func TestDaemonControlAPICSRFAndRedaction(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	t.Setenv("PEACECLAUDE_NO_NOTIFY", "1")
	t.Setenv("PEACECLAUDE_DISABLE_AUTO_PEACE", "1")
	t.Setenv("PEACECLAUDE_DISABLE_KEYCHAIN", "1")

	cfg := config.Default()
	cfg.Listen.HTTPPort = 28897
	cfg.Listen.SOCKS5Port = 28898
	cfg.Listen.ControlPort = 28899
	cfg.Remote.MobilePort = 28900
	cfg.Upstream.Password = "top-secret-ws-pass-777"
	cfg.Egress.ExpectedIP = "198.51.100.10"

	srv := New(cfg, "/bin/echo")
	srv.Gate().SetProbeFunc(func(_ context.Context, _ *config.Config) egress.ProbeResult {
		return egress.ProbeResult{
			CheckedAt: time.Now().UTC(),
			OK:        true,
			EgressIP:  "198.51.100.10",
			Country:   "US",
			Timezone:  "America/Denver",
			Source:    "mock",
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start daemon: %v", err)
	}
	defer srv.Shutdown(context.Background())

	base := "http://" + srv.ControlAddr()

	// Wait briefly for async initial probe so gate becomes open cleanly.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 3*time.Second)
	srv.Gate().WaitInitialProbe(waitCtx)
	waitCancel()

	// 1. /api/status must succeed and never leak upstream password
	resp, err := http.Get(base + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status HTTP %d", resp.StatusCode)
	}
	var statusObj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&statusObj); err != nil {
		t.Fatal(err)
	}
	rawBytes, _ := json.Marshal(statusObj)
	if strings.Contains(string(rawBytes), "top-secret-ws-pass-777") {
		t.Fatal("/api/status leaked upstream password!")
	}

	// 2. Cross-origin POST MUST be rejected by CSRF guard!
	req, _ := http.NewRequest(http.MethodPost, base+"/api/lock", bytes.NewBufferString(`{"locked":true}`))
	req.Header.Set("Origin", "https://evil.example.com")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin POST, got %d", resp2.StatusCode)
	}

	// 3. Same-origin POST /api/lock MUST lock the gate
	req3, _ := http.NewRequest(http.MethodPost, base+"/api/lock", bytes.NewBufferString(`{"locked":true,"reason":"test lock"}`))
	req3.Header.Set("Origin", base)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("same-origin lock returned HTTP %d", resp3.StatusCode)
	}
	if ok, _ := srv.Gate().AllowTraffic(); ok {
		t.Fatal("gate should be locked after POST /api/lock")
	}

	// 4. Web UI index must render
	resp4, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("GET / returned HTTP %d", resp4.StatusCode)
	}

	// 5. PAC endpoint must serve valid PAC
	resp5, err := http.Get(base + "/peaceclaude.pac")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp5.Body.Close()
	if resp5.StatusCode != http.StatusOK {
		t.Fatalf("GET /peaceclaude.pac returned HTTP %d", resp5.StatusCode)
	}

	// 5b. Multi-account CRUD + detail + usage endpoint + CSRF check
	reqCreate, _ := http.NewRequest(http.MethodPost, base+"/api/profiles/create",
		bytes.NewBufferString(`{"name":"team-us","display_name":"US Max 主号","note":"backup","manual_plan":"Max 5x","manual_registered_at":"2026-06-15"}`))
	reqCreate.Header.Set("Origin", base)
	respC, err := http.DefaultClient.Do(reqCreate)
	if err != nil || respC.StatusCode != http.StatusOK {
		t.Fatalf("create profile failed: err=%v resp=%v", err, respC)
	}
	_ = respC.Body.Close()

	respAccts, err := http.Get(base + "/api/accounts")
	if err != nil || respAccts.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/accounts failed: err=%v resp=%v", err, respAccts)
	}
	var acctsObj map[string]any
	_ = json.NewDecoder(respAccts.Body).Decode(&acctsObj)
	_ = respAccts.Body.Close()
	acctsBytes, _ := json.Marshal(acctsObj)
	if !strings.Contains(string(acctsBytes), `"Max 5x"`) || !strings.Contains(string(acctsBytes), `"US Max 主号"`) {
		t.Fatalf("unexpected /api/accounts payload: %s", string(acctsBytes))
	}

	respDet, err := http.Get(base + "/api/accounts/detail?name=team-us")
	if err != nil || respDet.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/accounts/detail failed: err=%v resp=%v", err, respDet)
	}
	_ = respDet.Body.Close()

	reqUpd, _ := http.NewRequest(http.MethodPost, base+"/api/accounts/update",
		bytes.NewBufferString(`{"name":"team-us","manual_plan":"Ultra"}`))
	reqUpd.Header.Set("Origin", base)
	respU, err := http.DefaultClient.Do(reqUpd)
	if err != nil || respU.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/accounts/update failed: err=%v resp=%v", err, respU)
	}
	_ = respU.Body.Close()

	// Cross-origin delete MUST fail 403
	reqDelEvil, _ := http.NewRequest(http.MethodPost, base+"/api/accounts/delete", bytes.NewBufferString(`{"name":"team-us"}`))
	reqDelEvil.Header.Set("Origin", "https://evil.example.com")
	respDelEvil, err := http.DefaultClient.Do(reqDelEvil)
	if err != nil || respDelEvil.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 on cross-origin account delete, got err=%v resp=%v", err, respDelEvil)
	}
	_ = respDelEvil.Body.Close()

	reqDelOK, _ := http.NewRequest(http.MethodPost, base+"/api/accounts/delete", bytes.NewBufferString(`{"name":"team-us"}`))
	reqDelOK.Header.Set("Origin", base)
	respDelOK, err := http.DefaultClient.Do(reqDelOK)
	if err != nil || respDelOK.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/accounts/delete failed: err=%v resp=%v", err, respDelOK)
	}
	_ = respDelOK.Body.Close()

	// 5c. Appearance update, workbench lock, and remote QR pair -> redeem -> auth -> revoke
	reqApp, _ := http.NewRequest(http.MethodPost, base+"/api/appearance/update",
		bytes.NewBufferString(`{"theme_mode":"light","preset_light":"codex","preset_dark":"ayu","ui_font_size":15,"code_font_size":12}`))
	reqApp.Header.Set("Origin", base)
	respApp, err := http.DefaultClient.Do(reqApp)
	if err != nil || respApp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/appearance/update failed: err=%v resp=%v", err, respApp)
	}
	_ = respApp.Body.Close()

	reqLockUI, _ := http.NewRequest(http.MethodPost, base+"/api/workbench/lock", bytes.NewBufferString(`{"locked":true}`))
	reqLockUI.Header.Set("Origin", base)
	respLockUI, err := http.DefaultClient.Do(reqLockUI)
	if err != nil || respLockUI.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/workbench/lock failed: err=%v resp=%v", err, respLockUI)
	}
	_ = respLockUI.Body.Close()

	reqPair, _ := http.NewRequest(http.MethodPost, base+"/api/remote/pair", bytes.NewBufferString(`{"host":"127.0.0.1"}`))
	reqPair.Header.Set("Origin", base)
	respPair, err := http.DefaultClient.Do(reqPair)
	if err != nil || respPair.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/remote/pair failed: err=%v resp=%v", err, respPair)
	}
	var pairObj struct {
		Ticket struct {
			Code string `json:"code"`
			SVG  string `json:"svg"`
		} `json:"ticket"`
	}
	_ = json.NewDecoder(respPair.Body).Decode(&pairObj)
	_ = respPair.Body.Close()
	if !strings.HasPrefix(pairObj.Ticket.Code, "PEACE-") || !strings.Contains(pairObj.Ticket.SVG, "<svg ") {
		t.Fatalf("invalid remote pair ticket: %+v", pairObj)
	}

	// Redeem & call authenticated mobile API via standalone mobile listener port 28900
	mobileBase := "http://127.0.0.1:28900"
	respRedeem, err := http.Post(mobileBase+"/mobile/api/redeem", "application/json",
		bytes.NewBufferString(`{"code":"`+pairObj.Ticket.Code+`","device_name":"QA Phone","os":"iOS"}`))
	if err != nil || respRedeem.StatusCode != http.StatusOK {
		t.Fatalf("POST /mobile/api/redeem failed: err=%v resp=%v", err, respRedeem)
	}
	var redeemRes struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(respRedeem.Body).Decode(&redeemRes)
	_ = respRedeem.Body.Close()
	if redeemRes.Token == "" {
		t.Fatal("empty bearer token after mobile redeem")
	}

	reqMobState, _ := http.NewRequest(http.MethodGet, mobileBase+"/mobile/api/state", nil)
	reqMobState.Header.Set("Authorization", "Bearer "+redeemRes.Token)
	respMobState, err := http.DefaultClient.Do(reqMobState)
	if err != nil || respMobState.StatusCode != http.StatusOK {
		t.Fatalf("GET /mobile/api/state failed: err=%v resp=%v", err, respMobState)
	}
	_ = respMobState.Body.Close()

	// Revoke mobile devices -> mobile state MUST return 401
	reqRev, _ := http.NewRequest(http.MethodPost, base+"/api/remote/revoke", bytes.NewBufferString(`{}`))
	reqRev.Header.Set("Origin", base)
	respRev, err := http.DefaultClient.Do(reqRev)
	if err != nil || respRev.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/remote/revoke failed: err=%v resp=%v", err, respRev)
	}
	_ = respRev.Body.Close()

	reqMobDenied, _ := http.NewRequest(http.MethodGet, base+"/mobile/api/state", nil)
	reqMobDenied.Header.Set("Authorization", "Bearer "+redeemRes.Token)
	respMobDenied, err := http.DefaultClient.Do(reqMobDenied)
	if err != nil || respMobDenied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after remote revoke, got err=%v resp=%v", err, respMobDenied)
	}
	_ = respMobDenied.Body.Close()

	// 6. Regression guard: POST /api/shutdown MUST close srv.Done() so daemon exits cleanly (no zombie processes).
	reqShut, _ := http.NewRequest(http.MethodPost, base+"/api/shutdown", nil)
	reqShut.Header.Set("Origin", base)
	respShut, err := http.DefaultClient.Do(reqShut)
	if err != nil {
		t.Fatalf("POST /api/shutdown: %v", err)
	}
	_ = respShut.Body.Close()
	select {
	case <-srv.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("srv.Done() was not closed after POST /api/shutdown (zombie daemon regression)")
	}
}

func TestAutoPeaceSafetyGuards(t *testing.T) {
	t.Setenv("PEACECLAUDE_NO_NOTIFY", "1")
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	t.Setenv("PEACECLAUDE_DISABLE_AUTO_PEACE", "")
	t.Setenv("PEACECLAUDE_FORCE_AUTO_PEACE", "")

	// 1. Custom PEACECLAUDE_HOME sandbox MUST disable Auto-Peace automatically unless forced.
	if dis, _ := shouldDisableAutoPeace(); !dis {
		t.Fatal("expected Auto-Peace to auto-disable when PEACECLAUDE_HOME sandbox is set")
	}
	t.Setenv("PEACECLAUDE_HOME", "")
	t.Setenv("PEACECLAUDE_DISABLE_AUTO_PEACE", "1")
	if dis, _ := shouldDisableAutoPeace(); !dis {
		t.Fatal("expected Auto-Peace disabled when PEACECLAUDE_DISABLE_AUTO_PEACE=1")
	}

	// 2. isAutoPeaceTarget MUST ONLY intercept bare native Claude.app with NO --user-data-dir=.
	nativeBare := host.DesktopProcAudit{
		PID:        "101",
		Isolated:   false,
		RawCommand: "/Applications/Claude.app/Contents/MacOS/Claude",
	}
	if !isAutoPeaceTarget(nativeBare) {
		t.Fatal("expected bare native Claude.app without --user-data-dir to be Auto-Peace target")
	}
	otherSandboxOrIsolated := host.DesktopProcAudit{
		PID:        "102",
		Isolated:   false,
		RawCommand: "/Applications/Claude.app/Contents/MacOS/Claude --user-data-dir=/tmp/other-sandbox/profiles/default/desktop-data --proxy-server=http://127.0.0.1:29897",
	}
	if isAutoPeaceTarget(otherSandboxOrIsolated) {
		t.Fatal("Auto-Peace MUST NOT kill another isolated/sandbox Claude.app carrying --user-data-dir=")
	}
	alreadyIsolated := host.DesktopProcAudit{
		PID:        "103",
		Isolated:   true,
		RawCommand: "/Applications/Claude.app/Contents/MacOS/Claude --user-data-dir=/Users/demo/.peaceclaude/profiles/default/desktop-data",
	}
	if isAutoPeaceTarget(alreadyIsolated) {
		t.Fatal("already isolated instance must not trigger Auto-Peace")
	}

	// 3. Frequency circuit breaker: >= 3 triggers inside 60s MUST pause for 10m.
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	srv := New(config.Default(), "/usr/bin/true")
	base := time.Now()
	for i := 0; i < 3; i++ {
		if !srv.allowAutoPeaceTrigger(base.Add(time.Duration(i) * 5 * time.Second)) {
			t.Fatalf("trigger %d should be allowed before circuit breaker trips", i+1)
		}
	}
	if srv.allowAutoPeaceTrigger(base.Add(20 * time.Second)) {
		t.Fatal("4th trigger inside 60s MUST trip Auto-Peace circuit breaker")
	}
	if srv.autoPeacePausedUntil.IsZero() || srv.autoPeacePausedUntil.Before(base.Add(9*time.Minute)) {
		t.Fatalf("expected 10m cooldown, got %v", srv.autoPeacePausedUntil)
	}

	// 4. Consecutive failure circuit breaker: >= 2 takeover failures MUST pause Auto-Peace.
	srv2 := New(config.Default(), "/usr/bin/true")
	srv2.recordAutoPeaceOutcome(base, true)
	if !srv2.autoPeacePausedUntil.IsZero() {
		t.Fatal("single failure should not trip pause yet")
	}
	srv2.recordAutoPeaceOutcome(base.Add(5*time.Second), true)
	if srv2.autoPeacePausedUntil.IsZero() {
		t.Fatal("2 consecutive takeover failures MUST trip Auto-Peace circuit breaker")
	}

	// 5. Foreign sandbox isolation guard in isOwnedUnisolatedTarget & BrowserPAC missing field -> 400
	ownProf := config.ProfilesDir()
	foreignSandbox := host.DesktopProcAudit{
		PID:        "777",
		Isolated:   false,
		RawCommand: "/Applications/Claude.app/Contents/MacOS/Claude --user-data-dir=/tmp/peaceclaude-regression.abc/profiles/default/desktop-data",
	}
	if isOwnedUnisolatedTarget(foreignSandbox, ownProf) {
		t.Fatal("TakeoverAllExposed MUST skip foreign sandbox desktop processes")
	}

	reqBadPAC, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/browser-pac", bytes.NewBufferString(`{}`))
	reqBadPAC.Header.Set("Origin", "http://127.0.0.1")
	rr := newResponseRecorder()
	srv2.withCSRF(srv2.handleBrowserPAC)(rr, reqBadPAC)
	if rr.code != http.StatusBadRequest {
		t.Fatalf("expected 400 when enabled boolean omitted in /api/browser-pac, got %d", rr.code)
	}
}

type simpleRecorder struct {
	code   int
	header http.Header
	body   bytes.Buffer
}

func newResponseRecorder() *simpleRecorder {
	return &simpleRecorder{code: http.StatusOK, header: http.Header{}}
}

func (r *simpleRecorder) Header() http.Header         { return r.header }
func (r *simpleRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *simpleRecorder) WriteHeader(statusCode int)  { r.code = statusCode }
