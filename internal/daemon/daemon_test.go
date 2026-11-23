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
	"peaceclaude/internal/profile"
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

	// 6. /api/doctor/fix contract: missing action -> 400, enable_hardening -> 200 and fixes config flags.
	reqBadFix, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/doctor/fix", bytes.NewBufferString(`{}`))
	reqBadFix.Header.Set("Origin", "http://127.0.0.1")
	rrBadFix := newResponseRecorder()
	srv2.withCSRF(srv2.handleDoctorFix)(rrBadFix, reqBadFix)
	if rrBadFix.code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty /api/doctor/fix, got %d", rrBadFix.code)
	}

	srv2.mu.Lock()
	srv2.cfg.Egress.FailClosed = false
	srv2.cfg.Shield.TelemetryShield = false
	srv2.mu.Unlock()
	reqGoodFix, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/doctor/fix", bytes.NewBufferString(`{"action":"enable_hardening"}`))
	reqGoodFix.Header.Set("Origin", "http://127.0.0.1")
	rrGoodFix := newResponseRecorder()
	srv2.withCSRF(srv2.handleDoctorFix)(rrGoodFix, reqGoodFix)
	if rrGoodFix.code != http.StatusOK {
		t.Fatalf("expected 200 for enable_hardening fix, got %d: %s", rrGoodFix.code, rrGoodFix.body.String())
	}
	snapAfterFix := srv2.configSnapshot()
	if !snapAfterFix.Egress.FailClosed || !snapAfterFix.Shield.TelemetryShield {
		t.Fatalf("expected FailClosed and TelemetryShield enabled after fix, got %+v %+v", snapAfterFix.Egress.FailClosed, snapAfterFix.Shield.TelemetryShield)
	}

	// 7. Native browser shield inspection & selection persistence (with enable_pac=false to stay 100% sandboxed).
	reqNBGet, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/api/native-browsers", nil)
	rrNBGet := newResponseRecorder()
	srv2.handleNativeBrowsers(rrNBGet, reqNBGet)
	if rrNBGet.code != http.StatusOK || !strings.Contains(rrNBGet.body.String(), `"extension_ready":true`) {
		t.Fatalf("expected /api/native-browsers ok + extension_ready=true, got %d: %s", rrNBGet.code, rrNBGet.body.String())
	}

	reqNBProtect, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/native-browsers/protect", bytes.NewBufferString(`{"browsers":["arc","chrome","invalid-browser"],"enable_pac":false}`))
	reqNBProtect.Header.Set("Origin", "http://127.0.0.1")
	rrNBProtect := newResponseRecorder()
	srv2.withCSRF(srv2.handleNativeBrowsersProtect)(rrNBProtect, reqNBProtect)
	if rrNBProtect.code != http.StatusOK || !strings.Contains(rrNBProtect.body.String(), `"ok":true`) {
		t.Fatalf("expected /api/native-browsers/protect ok, got %d: %s", rrNBProtect.code, rrNBProtect.body.String())
	}
	nbCfg := srv2.configSnapshot()
	if len(nbCfg.Shield.ProtectedBrowsers) != 2 || nbCfg.Shield.ProtectedBrowsers[0] != "arc" || nbCfg.Shield.ProtectedBrowsers[1] != "chrome" {
		t.Fatalf("expected normalized ProtectedBrowsers [arc chrome], got %v", nbCfg.Shield.ProtectedBrowsers)
	}
}

func TestHandleAccountUpdatePreservesManualLockAndStatusReflectsIdentity(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	t.Setenv("PEACECLAUDE_NO_NOTIFY", "1")
	t.Setenv("PEACECLAUDE_DISABLE_AUTO_PEACE", "1")
	t.Setenv("PEACECLAUDE_DISABLE_KEYCHAIN", "1")

	srv := New(config.Default(), "/usr/bin/true")

	// 1. Lock manual_plan first on default profile.
	reqLock, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/accounts/update", bytes.NewBufferString(`{"name":"default","manual_plan":"Max 5x"}`))
	reqLock.Header.Set("Origin", "http://127.0.0.1")
	rrLock := newResponseRecorder()
	srv.withCSRF(srv.handleAccountUpdate)(rrLock, reqLock)
	if rrLock.code != http.StatusOK {
		t.Fatalf("seed manual_plan failed (%d): %s", rrLock.code, rrLock.body.String())
	}

	// 2. Identity-only update (display_name + avatar_id) MUST preserve manual_locked and manual_plan.
	reqIdent, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/accounts/update", bytes.NewBufferString(`{"name":"default","display_name":"金狮主力","avatar_id":"lion"}`))
	reqIdent.Header.Set("Origin", "http://127.0.0.1")
	rrIdent := newResponseRecorder()
	srv.withCSRF(srv.handleAccountUpdate)(rrIdent, reqIdent)
	if rrIdent.code != http.StatusOK {
		t.Fatalf("identity update failed (%d): %s", rrIdent.code, rrIdent.body.String())
	}
	var updated struct {
		DisplayName  string `json:"display_name"`
		AvatarID     string `json:"avatar_id"`
		ManualPlan   string `json:"manual_plan"`
		ManualLocked bool   `json:"manual_locked"`
	}
	if err := json.Unmarshal(rrIdent.body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "金狮主力" || updated.AvatarID != "lion" || updated.ManualPlan != "Max 5x" || !updated.ManualLocked {
		t.Fatalf("identity update corrupted manual lock or identity: %+v", updated)
	}

	// 3. /api/status must expose active_display_name & active_avatar_id for instant sidebar hydration.
	reqSt, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/api/status", nil)
	rrSt := newResponseRecorder()
	srv.handleStatus(rrSt, reqSt)
	if rrSt.code != http.StatusOK {
		t.Fatalf("handleStatus failed: %d", rrSt.code)
	}
	var st struct {
		ActiveDisplayName string `json:"active_display_name"`
		ActiveAvatarID    string `json:"active_avatar_id"`
	}
	if err := json.Unmarshal(rrSt.body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.ActiveDisplayName != "金狮主力" || st.ActiveAvatarID != "lion" {
		t.Fatalf("expected status identity 金狮主力/lion, got %+v", st)
	}
}

func TestDeleteActiveProfileSyncsMemoryConfigAndAccountsExposeSecurityScore(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	t.Setenv("PEACECLAUDE_NO_NOTIFY", "1")
	t.Setenv("PEACECLAUDE_DISABLE_AUTO_PEACE", "1")
	t.Setenv("PEACECLAUDE_DISABLE_KEYCHAIN", "1")

	srv := New(config.Default(), "/usr/bin/true")

	// 1. Create a temporary profile and activate it.
	reqCreate, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/profiles/create", bytes.NewBufferString(`{"name":"temp-acct","display_name":"临时账号","use_now":true}`))
	reqCreate.Header.Set("Origin", "http://127.0.0.1")
	rrCreate := newResponseRecorder()
	srv.withCSRF(srv.handleProfileCreate)(rrCreate, reqCreate)
	if rrCreate.code != http.StatusOK {
		t.Fatalf("create failed: %d %s", rrCreate.code, rrCreate.body.String())
	}
	if got := srv.configSnapshot().ActiveProfile; got != "temp-acct" {
		t.Fatalf("expected active temp-acct, got %s", got)
	}

	// 2. Verify /api/accounts populates security score/grade/summary and reflects FailClosed toggle.
	reqList, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/api/accounts?with_usage=0", nil)
	rrList := newResponseRecorder()
	srv.handleAccounts(rrList, reqList)
	if rrList.code != http.StatusOK {
		t.Fatalf("handleAccounts failed: %d", rrList.code)
	}
	var listResp struct {
		Accounts []struct {
			Name     string          `json:"name"`
			Security AccountSecurity `json:"security"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rrList.body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Accounts) < 2 {
		t.Fatalf("expected >=2 accounts, got %d", len(listResp.Accounts))
	}
	scoreBefore := listResp.Accounts[0].Security.Score
	if scoreBefore <= 0 || scoreBefore > 100 || listResp.Accounts[0].Security.Summary == "" {
		t.Fatalf("unexpected account security snapshot: %+v", listResp.Accounts[0].Security)
	}

	srv.mu.Lock()
	next := *srv.cfg
	next.Egress.FailClosed = false
	srv.swapConfigLocked(next)
	srv.mu.Unlock()

	rrList2 := newResponseRecorder()
	srv.handleAccounts(rrList2, reqList)
	var listResp2 struct {
		Accounts []struct {
			Security AccountSecurity `json:"security"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rrList2.body.Bytes(), &listResp2); err != nil {
		t.Fatal(err)
	}
	if listResp2.Accounts[0].Security.Score >= scoreBefore {
		t.Fatalf("expected disabling FailClosed to drop account security score (< %d), got %d", scoreBefore, listResp2.Accounts[0].Security.Score)
	}

	// 3. Delete currently active temp-acct; server's in-memory ActiveProfile MUST fall back to "default".
	reqDel, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/accounts/delete", bytes.NewBufferString(`{"name":"temp-acct"}`))
	reqDel.Header.Set("Origin", "http://127.0.0.1")
	rrDel := newResponseRecorder()
	srv.withCSRF(srv.handleAccountDelete)(rrDel, reqDel)
	if rrDel.code != http.StatusOK {
		t.Fatalf("handleAccountDelete failed: %d %s", rrDel.code, rrDel.body.String())
	}
	if got := srv.configSnapshot().ActiveProfile; got != "default" {
		t.Fatalf("expected ActiveProfile to fall back to default after deleting active profile, got %q", got)
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

func TestParseBootTimeAndBootGraceExemption(t *testing.T) {
	sec, ok := parseBootTimeSec("{ sec = 1795279619, usec = 805208 } Sun Nov 22 00:46:59 2026\n")
	if !ok || sec != 1795279619 {
		t.Fatalf("parseBootTimeSec failed: sec=%d ok=%t", sec, ok)
	}
	if _, badOK := parseBootTimeSec("invalid output"); badOK {
		t.Fatal("expected parseBootTimeSec to reject malformed string")
	}

	cases := []struct {
		name     string
		auto     bool
		uptime   time.Duration
		known    bool
		wantEx   bool
	}{
		{"disabled_auto_peace_always_exempt", false, 30 * time.Second, true, true},
		{"just_booted_within_grace_never_exempt", true, 45 * time.Second, true, false},
		{"edge_3m_still_takes_over", true, 3 * time.Minute, true, false},
		{"mid_session_long_uptime_exempt", true, 2 * time.Hour, true, true},
		{"unknown_uptime_falls_back_to_safe_exempt", true, 0, false, true},
	}
	for _, tc := range cases {
		if got := shouldExemptPreExistingDesktop(tc.auto, tc.uptime, tc.known); got != tc.wantEx {
			t.Fatalf("%s: shouldExemptPreExistingDesktop(%t, %v, %t) = %t, want %t",
				tc.name, tc.auto, tc.uptime, tc.known, got, tc.wantEx)
		}
	}
}

func TestServiceAutostartAPI(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	t.Setenv("PEACECLAUDE_NO_LAUNCHD", "1")
	srv := New(config.Default(), "/usr/bin/true")

	// 1. /api/status must include service status object with auto_start_enabled=true by default.
	reqStatus, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/api/status", nil)
	rrStatus := newResponseRecorder()
	srv.handleStatus(rrStatus, reqStatus)
	if rrStatus.code != http.StatusOK {
		t.Fatalf("/api/status HTTP %d: %s", rrStatus.code, rrStatus.body.String())
	}
	var stBody struct {
		Service struct {
			Running          bool `json:"running"`
			AutoStartEnabled bool `json:"auto_start_enabled"`
		} `json:"service"`
	}
	if err := json.Unmarshal(rrStatus.body.Bytes(), &stBody); err != nil {
		t.Fatal(err)
	}
	if !stBody.Service.Running || !stBody.Service.AutoStartEnabled {
		t.Fatalf("unexpected service status in /api/status: %+v", stBody.Service)
	}

	// 2. Missing enabled boolean -> 400.
	reqBad, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/service/autostart", bytes.NewBufferString(`{}`))
	reqBad.Header.Set("Origin", "http://127.0.0.1")
	rrBad := newResponseRecorder()
	srv.withCSRF(srv.handleServiceAutostart)(rrBad, reqBad)
	if rrBad.code != http.StatusBadRequest {
		t.Fatalf("expected 400 when enabled omitted, got %d", rrBad.code)
	}

	// 3. Toggle disabled -> persists false in config without touching host LaunchAgent in sandbox.
	reqOff, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/service/autostart", bytes.NewBufferString(`{"enabled":false}`))
	reqOff.Header.Set("Origin", "http://127.0.0.1")
	rrOff := newResponseRecorder()
	srv.withCSRF(srv.handleServiceAutostart)(rrOff, reqOff)
	if rrOff.code != http.StatusOK {
		t.Fatalf("disable autostart HTTP %d: %s", rrOff.code, rrOff.body.String())
	}
	snapOff := srv.configSnapshot()
	if snapOff.ShouldAutoStartLaunchAgent() {
		t.Fatal("expected ShouldAutoStartLaunchAgent=false after disabling via API")
	}

	// 4. Toggle enabled back -> persists true.
	reqOn, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/api/service/autostart", bytes.NewBufferString(`{"enabled":true}`))
	reqOn.Header.Set("Origin", "http://127.0.0.1")
	rrOn := newResponseRecorder()
	srv.withCSRF(srv.handleServiceAutostart)(rrOn, reqOn)
	if rrOn.code != http.StatusOK {
		t.Fatalf("enable autostart HTTP %d: %s", rrOn.code, rrOn.body.String())
	}
	snapOn := srv.configSnapshot()
	if !snapOn.ShouldAutoStartLaunchAgent() {
		t.Fatal("expected ShouldAutoStartLaunchAgent=true after enabling via API")
	}
}

func TestMobileExtendedStateAndActions(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	t.Setenv("PEACECLAUDE_NO_LAUNCHD", "1")
	t.Setenv("PEACECLAUDE_NO_SYS_PAC", "1")
	srv := New(config.Default(), "/usr/bin/true")

	// Pair a mobile client via internal RemoteManager
	snapCfg := srv.configSnapshot()
	qr, err := srv.remoteMgr.CreatePairTicket(context.Background(), &snapCfg, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	tok, _, err := srv.remoteMgr.RedeemPair(qr.Code, "iPhone-Test", "iOS")
	if err != nil {
		t.Fatal(err)
	}

	handler := srv.mobileHandler()

	// 1. /mobile/api/state includes version, platform, upstream_route, browser_pac, doctor_summary.
	reqState, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/mobile/api/state", nil)
	reqState.Header.Set("Authorization", "Bearer "+tok)
	rrState := newResponseRecorder()
	handler.ServeHTTP(rrState, reqState)
	if rrState.code != http.StatusOK {
		t.Fatalf("/mobile/api/state HTTP %d: %s", rrState.code, rrState.body.String())
	}
	var st struct {
		Version       string `json:"version"`
		Platform      string `json:"platform"`
		UpstreamRoute struct {
			BypassEnabled bool `json:"bypass_enabled"`
		} `json:"upstream_route"`
		BrowserPAC struct {
			PACURL string `json:"pac_url"`
		} `json:"browser_pac"`
		DoctorSummary struct {
			CriticalTotal int `json:"critical_total"`
		} `json:"doctor_summary"`
	}
	if err := json.Unmarshal(rrState.body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.Platform == "" || st.BrowserPAC.PACURL == "" || st.DoctorSummary.CriticalTotal < 10 {
		t.Fatalf("unexpected extended /mobile/api/state payload: %+v", st)
	}

	// 2. lock_gate -> lock_ui -> toggle_pac -> unlock_gate
	postMobile := func(payload string) (int, map[string]any) {
		req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/mobile/api/action", bytes.NewBufferString(payload))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		rr := newResponseRecorder()
		handler.ServeHTTP(rr, req)
		var m map[string]any
		_ = json.Unmarshal(rr.body.Bytes(), &m)
		return rr.code, m
	}

	c1, m1 := postMobile(`{"action":"lock_gate","reason":"test lock"}`)
	if c1 != http.StatusOK || m1["ok"] != true {
		t.Fatalf("lock_gate failed: %d %v", c1, m1)
	}
	if open, _ := srv.gate.AllowTraffic(); open {
		t.Fatal("expected gate locked after mobile lock_gate")
	}

	c2, m2 := postMobile(`{"action":"lock_ui","locked":true}`)
	if c2 != http.StatusOK || m2["ui_locked"] != true || !srv.uiLocked.Load() {
		t.Fatalf("lock_ui failed: %d %v", c2, m2)
	}
	_, _ = postMobile(`{"action":"lock_ui","locked":false}`)

	c3, m3 := postMobile(`{"action":"toggle_pac","enabled":false}`)
	if c3 != http.StatusOK || m3["enabled"] != false {
		t.Fatalf("toggle_pac=false failed: %d %v", c3, m3)
	}

	c4, m4 := postMobile(`{"action":"unlock_gate"}`)
	if c4 != http.StatusOK || m4["ok"] != true {
		t.Fatalf("unlock_gate failed: %d %v", c4, m4)
	}

	// 3. Concurrency check on multiple profiles ensuring /mobile/api/state finishes within tight budget
	for _, extra := range []string{"extra-one", "extra-two", "extra-three"} {
		_, _ = profile.Ensure(srv.cfg, extra, "Extra "+extra)
	}
	start := time.Now()
	reqMulti, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/mobile/api/state", nil)
	reqMulti.Header.Set("Authorization", "Bearer "+tok)
	rrMulti := newResponseRecorder()
	handler.ServeHTTP(rrMulti, reqMulti)
	elapsed := time.Since(start)
	if rrMulti.code != http.StatusOK || elapsed > 5*time.Second {
		t.Fatalf("multi-profile /mobile/api/state code=%d elapsed=%v body=%s", rrMulti.code, elapsed, rrMulti.body.String())
	}
	var multiRes struct {
		Accounts []map[string]any `json:"accounts"`
	}
	if err := json.Unmarshal(rrMulti.body.Bytes(), &multiRes); err != nil || len(multiRes.Accounts) < 4 {
		t.Fatalf("expected >=4 concurrent accounts in mobile state, got %d err=%v", len(multiRes.Accounts), err)
	}
}
