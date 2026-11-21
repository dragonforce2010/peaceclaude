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
)

func TestDaemonControlAPICSRFAndRedaction(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	t.Setenv("PEACECLAUDE_NO_NOTIFY", "1")
	t.Setenv("PEACECLAUDE_DISABLE_AUTO_PEACE", "1")

	cfg := config.Default()
	cfg.Listen.HTTPPort = 28897
	cfg.Listen.SOCKS5Port = 28898
	cfg.Listen.ControlPort = 28899
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
