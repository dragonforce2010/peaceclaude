package profile

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/upstream"
)

func TestProfileIsolationRulesSyncAndClean(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Create two isolated profiles
	p1, err := Ensure(cfg, "work-us", "Primary US account")
	if err != nil {
		t.Fatalf("Ensure work-us: %v", err)
	}
	p2, err := Ensure(cfg, "personal-us", "Backup US account")
	if err != nil {
		t.Fatalf("Ensure personal-us: %v", err)
	}
	if p1.Fingerprint.DeviceUUID == p2.Fingerprint.DeviceUUID {
		t.Fatal("profiles must have distinct device fingerprints")
	}

	// Shared rules sync
	sharedMD := filepath.Join(config.SharedRulesDir(), "CLAUDE.md")
	if err := os.WriteFile(sharedMD, []byte("# Global rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := SyncAllRules(cfg, true)
	if err != nil {
		t.Fatalf("SyncAllRules on: %v", err)
	}
	if st.Profiles["work-us"] != "synced" || st.Profiles["personal-us"] != "synced" {
		t.Fatalf("expected both profiles synced, got %+v", st.Profiles)
	}

	// Detach keeps local copies
	st, err = SyncAllRules(cfg, false)
	if err != nil {
		t.Fatalf("SyncAllRules off: %v", err)
	}
	if st.Profiles["work-us"] != "detached" {
		t.Fatalf("expected detached, got %+v", st.Profiles)
	}
	if _, err := os.Stat(filepath.Join(p1.CLIConfigDir, "CLAUDE.md")); err != nil {
		t.Fatalf("detached CLAUDE.md should still exist: %v", err)
	}

	// Seed fake credentials/session/desktop fingerprint in work-us and verify clean preserves settings & CLAUDE.md
	_ = os.WriteFile(filepath.Join(p1.CLIConfigDir, ".credentials.json"), []byte(`{"token":"old"}`), 0o600)
	_ = os.WriteFile(filepath.Join(p1.CLIConfigDir, ".claude.json"), []byte(`{"oauthAccount":"old@x.com","theme":"dark"}`), 0o600)
	_ = os.WriteFile(filepath.Join(p1.DesktopDir, "ant-did"), []byte("old-device-id"), 0o600)

	// Dry-run must NOT mutate files
	dry, err := CleanProfile(cfg, "work-us", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Removed) == 0 || len(dry.Scrubbed) == 0 {
		t.Fatalf("dry-run expected findings, got %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(p1.DesktopDir, "ant-did")); err != nil {
		t.Fatal("dry-run must not remove ant-did")
	}

	// Real clean wipes fingerprints, backs up, preserves settings.json & CLAUDE.md, keeps non-identity keys
	oldUUID := p1.Fingerprint.DeviceUUID
	rep, err := CleanProfile(cfg, "work-us", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.NewDevice == oldUUID {
		t.Fatal("expected regenerated device UUID after clean")
	}
	if _, err := os.Stat(filepath.Join(p1.DesktopDir, "ant-did")); !os.IsNotExist(err) {
		t.Fatal("ant-did should be removed after clean")
	}
	if _, err := os.Stat(filepath.Join(rep.BackupDir, "desktop-data", "ant-did")); err != nil {
		t.Fatalf("ant-did should exist in backup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p1.CLIConfigDir, "settings.json")); err != nil {
		t.Fatal("settings.json must be preserved")
	}
	rawJSON, _ := os.ReadFile(filepath.Join(p1.CLIConfigDir, ".claude.json"))
	var parsed map[string]any
	_ = json.Unmarshal(rawJSON, &parsed)
	if _, exists := parsed["oauthAccount"]; exists {
		t.Fatal("oauthAccount should be scrubbed")
	}
	if parsed["theme"] != "dark" {
		t.Fatalf("non-identity preference theme should be preserved, got %+v", parsed)
	}
}

func TestNormalizePlanLabelAndAgeDuration(t *testing.T) {
	cases := []struct {
		orgType, orgTier, userTier, seatTier, manual, want string
	}{
		{"claude_pro", "default_claude_ai", "", "", "", "Pro"},
		{"claude_max", "default_claude_max_5x", "", "", "", "Max 5x"},
		{"claude_max", "default_claude_max_20x", "", "", "", "Max 20x"},
		{"claude_ultra", "", "", "", "", "Ultra"},
		{"claude_team", "", "", "", "", "Team"},
		{"claude_free", "", "", "", "", "Free"},
		{"claude_pro", "", "", "", "Max 20x", "Max 20x"},
	}
	for _, tc := range cases {
		if got := NormalizePlanLabel(tc.orgType, tc.orgTier, tc.userTier, tc.seatTier, tc.manual); got != tc.want {
			t.Fatalf("NormalizePlanLabel(%q,%q,%q) = %q, want %q", tc.orgType, tc.orgTier, tc.manual, got, tc.want)
		}
	}

	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	if got := FormatAgeDuration(now.Add(-19*24*time.Hour), now); got != "19 天" {
		t.Fatalf("19d age = %q", got)
	}
	if got := FormatAgeDuration(now.Add(-150*24*time.Hour), now); got != "5 个月" {
		t.Fatalf("5m age = %q", got)
	}
}

func TestAccountIdentityUpdateMetaAndOAuthUsage(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	t.Setenv("PEACECLAUDE_DISABLE_KEYCHAIN", "1")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	prof, err := Ensure(cfg, "acct-alpha", "主力美区号")
	if err != nil {
		t.Fatal(err)
	}

	// Write simulated .claude.json and .credentials.json inside isolated profile
	claudeJSON := `{
		"oauthAccount": {
			"accountUuid": "acc-1111-2222-3333",
			"emailAddress": "alpha@example.test",
			"displayName": "Alpha User",
			"organizationUuid": "org-aaaa-bbbb",
			"organizationName": "Alpha Org",
			"organizationType": "claude_max",
			"organizationRateLimitTier": "default_claude_max_5x",
			"billingType": "stripe_subscription",
			"accountCreatedAt": "2026-06-15T08:00:00Z",
			"subscriptionCreatedAt": "2026-07-01T08:00:00Z",
			"hasExtraUsageEnabled": true
		}
	}`
	_ = os.WriteFile(filepath.Join(prof.CLIConfigDir, ".claude.json"), []byte(claudeJSON), 0o600)
	_ = os.WriteFile(filepath.Join(prof.CLIConfigDir, ".credentials.json"), []byte(`{"claudeAiOauth":{"accessToken":"secret-test-access-token-999","subscriptionType":"max"}}`), 0o600)

	loaded, err := Load(cfg, "acct-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Account.PlanLabel != "Max 5x" || loaded.Account.Email != "alpha@example.test" || loaded.Account.AccountAgeText == "" {
		t.Fatalf("unexpected account identity: %+v", loaded.Account)
	}
	if !loaded.HasCLICreds {
		t.Fatal("expected HasCLICreds=true")
	}

	// Mock Anthropic usage endpoint behind a local HTTP CONNECT proxy to test full Dialer flow.
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/usage" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret-test-access-token-999" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"five_hour": {"utilization": 28.5, "resets_at": "2099-01-01T03:00:00Z"},
			"seven_day": {"utilization": 42.0, "resets_at": "2099-01-05T00:00:00Z"},
			"seven_day_sonnet": {"utilization": 15.0, "resets_at": "2099-01-05T00:00:00Z"},
			"extra_usage": {"is_enabled": true, "monthly_limit": 100, "used_credits": 12.5, "utilization": 12.5, "currency": "USD"}
		}`)
	}))
	defer apiSrv.Close()

	// Spin up minimal HTTP CONNECT proxy forwarding to apiSrv
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxyLn.Close()
	go func() {
		for {
			c, err := proxyLn.Accept()
			if err != nil {
				return
			}
			go func(client net.Conn) {
				defer client.Close()
				br := bufio.NewReader(client)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				targetConn, err := net.Dial("tcp", req.Host)
				if err != nil {
					return
				}
				defer targetConn.Close()
				_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
				go io.Copy(targetConn, br)
				io.Copy(client, targetConn)
			}(c)
		}
	}()

	origBase := usageEndpointBase
	usageEndpointBase = apiSrv.URL
	defer func() { usageEndpointBase = origBase }()

	dialer := &upstream.Dialer{Mode: "direct-http", Address: proxyLn.Addr().String(), Timeout: 5 * time.Second}
	snap := FetchUsage(context.Background(), cfg, loaded, true, dialer)
	if snap.Status != "ok" || snap.Source != "oauth_api" {
		t.Fatalf("unexpected usage status: %+v", snap)
	}
	if snap.FiveHour == nil || snap.FiveHour.Utilization != 28.5 {
		t.Fatalf("unexpected five_hour: %+v", snap.FiveHour)
	}
	if snap.Weekly == nil || snap.Weekly.Utilization != 42.0 {
		t.Fatalf("unexpected weekly: %+v", snap.Weekly)
	}
	if snap.ExtraUsage == nil || !snap.ExtraUsage.Enabled || snap.ExtraUsage.UsedCredits != 12.5 {
		t.Fatalf("unexpected extra usage: %+v", snap.ExtraUsage)
	}
	rawSnap, _ := json.Marshal(snap)
	if strings.Contains(string(rawSnap), "secret-test-access-token-999") {
		t.Fatal("UsageSnapshot MUST NEVER contain accessToken!")
	}

	// Test Desktop history fallback when no token exists
	profHist, _ := Ensure(cfg, "acct-desktop-only", "")
	_ = os.WriteFile(filepath.Join(profHist.DesktopDir, "plan-usage-history.json"),
		[]byte(`{"version":2,"samples":[{"t":1795252770885,"org":"org-desk-1","u":{"fh":18,"sd":63,"sn":12}}]}`), 0o600)
	histLoaded, _ := Load(cfg, "acct-desktop-only")
	histSnap := FetchUsage(context.Background(), cfg, histLoaded, true, dialer)
	if histSnap.Status != "history" || histSnap.FiveHour == nil || histSnap.FiveHour.Utilization != 18 || histSnap.Weekly == nil || histSnap.Weekly.Utilization != 63 {
		t.Fatalf("unexpected desktop history fallback: %+v", histSnap)
	}

	// Test UpdateMeta & manual override
	newDisp := "备战 Max 号"
	newPlan := "Ultra"
	newReg := "2026-06-01"
	updated, err := UpdateMeta(cfg, "acct-desktop-only", MetaPatch{
		DisplayName:        &newDisp,
		ManualPlan:         &newPlan,
		ManualRegisteredAt: &newReg,
	})
	if err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}
	if updated.DisplayName != newDisp || updated.Account.PlanLabel != "Ultra" || updated.Account.AccountAgeText == "" {
		t.Fatalf("unexpected updated profile: %+v", updated)
	}

	// Verify creation default (ManualLocked=false) defers to Anthropic .claude.json once logged in
	defPlan := "Pro"
	lockedFalse := false
	synced, err := UpdateMeta(cfg, "acct-alpha", MetaPatch{ManualPlan: &defPlan, ManualLocked: &lockedFalse})
	if err != nil || synced.Account.PlanLabel != "Max 5x" {
		t.Fatalf("expected logged-in Anthropic plan Max 5x to take precedence when unlocked, got %+v err=%v", synced, err)
	}
}
