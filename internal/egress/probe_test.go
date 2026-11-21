package egress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"peaceclaude/internal/config"
)

func TestGateFailClosedAndSSEProtection(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Egress.ExpectedIP = "198.51.100.10"
	cfg.Egress.AllowedCountries = []string{"US"}
	cfg.Egress.SkipProbeDuringSSE = true

	gate := NewGate(cfg)
	// Initially locked before first probe.
	if ok, _ := gate.AllowTraffic(); ok {
		t.Fatal("gate must start fail-closed before first probe")
	}

	probeCalls := 0
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) ProbeResult {
		probeCalls++
		return ProbeResult{
			CheckedAt: time.Now().UTC(),
			OK:        true,
			EgressIP:  "198.51.100.10",
			Country:   "US",
			Timezone:  "America/Denver",
			Source:    "mock",
		}
	})

	res := gate.Check(context.Background(), false)
	if !res.OK {
		t.Fatalf("expected OK probe, got %+v", res)
	}
	if ok, reason := gate.AllowTraffic(); !ok {
		t.Fatalf("expected gate open, got locked: %s", reason)
	}
	if snap := gate.Snapshot(); snap.EffectiveTimezone != "America/Denver" {
		t.Fatalf("expected synced timezone America/Denver, got %s", snap.EffectiveTimezone)
	}

	// While SSE stream is active, respectSSE=true skips extra probes.
	gate.BeginStream()
	skipped := gate.Check(context.Background(), true)
	if skipped.SkippedReason != "active_sse_stream_protected" || probeCalls != 1 {
		t.Fatalf("expected SSE probe skip, calls=%d skipped=%+v", probeCalls, skipped)
	}
	gate.EndStream()

	// Wrong IP MUST lock immediately even if previously open.
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) ProbeResult {
		return ProbeResult{
			CheckedAt: time.Now().UTC(),
			OK:        true,
			EgressIP:  "203.0.113.99",
			Country:   "US",
			Source:    "mock",
		}
	})
	bad := gate.Check(context.Background(), false)
	if bad.OK {
		t.Fatalf("expected mismatch failure, got %+v", bad)
	}
	if ok, _ := gate.AllowTraffic(); ok {
		t.Fatal("gate must fail-closed on IP mismatch")
	}

	// Manual lock overrides even valid probes.
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) ProbeResult {
		return ProbeResult{CheckedAt: time.Now().UTC(), OK: true, EgressIP: "198.51.100.10", Country: "US"}
	})
	gate.SetManualLock(true, "emergency stop")
	gate.Check(context.Background(), false)
	if ok, _ := gate.AllowTraffic(); ok {
		t.Fatal("manual lock must keep gate closed")
	}
	gate.SetManualLock(false, "")
	gate.Check(context.Background(), false)
	if ok, _ := gate.AllowTraffic(); !ok {
		t.Fatal("gate should reopen after manual unlock and good probe")
	}
}

func TestGateTransientTimeoutToleranceAndImmediateMismatchLock(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg := config.Default()
	cfg.Egress.ExpectedIP = "198.51.100.10"
	cfg.Egress.AllowedCountries = []string{"US"}

	gate := NewGate(cfg)
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) ProbeResult {
		return ProbeResult{CheckedAt: time.Now().UTC(), OK: true, EgressIP: "198.51.100.10", Country: "US"}
	})
	if res := gate.Check(context.Background(), false); !res.OK {
		t.Fatalf("expected initial probe OK, got %+v", res)
	}

	// Single transient timeout on an already-open gate MUST NOT sever active login/auth sessions.
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) ProbeResult {
		return ProbeResult{CheckedAt: time.Now().UTC(), OK: false, FailureReason: "upstream probe failed: context deadline exceeded"}
	})
	gate.Check(context.Background(), false)
	if ok, reason := gate.AllowTraffic(); !ok {
		t.Fatalf("expected gate to tolerate single transient timeout while open, got locked: %s", reason)
	}
	if snap := gate.Snapshot(); snap.ConsecutiveFails != 1 {
		t.Fatalf("expected ConsecutiveFails=1, got %d", snap.ConsecutiveFails)
	}

	// Second consecutive timeout MUST fail-closed immediately.
	gate.Check(context.Background(), false)
	if ok, _ := gate.AllowTraffic(); ok {
		t.Fatal("expected gate to fail-closed after 2 consecutive probe timeouts")
	}

	// Re-open, then verify a single IP mismatch STILL locks immediately with zero tolerance.
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) ProbeResult {
		return ProbeResult{CheckedAt: time.Now().UTC(), OK: true, EgressIP: "198.51.100.10", Country: "US"}
	})
	gate.Check(context.Background(), false)
	if ok, _ := gate.AllowTraffic(); !ok {
		t.Fatal("expected gate reopened")
	}
	gate.SetProbeFunc(func(_ context.Context, _ *config.Config) ProbeResult {
		return ProbeResult{CheckedAt: time.Now().UTC(), OK: true, EgressIP: "203.0.113.55", Country: "US"}
	})
	gate.Check(context.Background(), false)
	if ok, _ := gate.AllowTraffic(); ok {
		t.Fatal("expected immediate zero-tolerance lock on IP mismatch")
	}
}

func TestParallelProbeFastestWins(t *testing.T) {
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(4 * time.Second):
			_, _ = w.Write([]byte("198.51.100.99"))
		}
	}))
	defer slowSrv.Close()

	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer errSrv.Close()

	fastSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ip=198.51.100.10\nloc=US\ncolo=BOS\n"))
	}))
	defer fastSrv.Close()

	start := time.Now()
	res := probeSourcesParallel(context.Background(), &http.Client{Timeout: 5 * time.Second}, 5*time.Second, []probeSource{
		{name: "slow1", url: slowSrv.URL, kind: "plain"},
		{name: "err2", url: errSrv.URL, kind: "plain"},
		{name: "slow3", url: slowSrv.URL, kind: "plain"},
		{name: "fast4", url: fastSrv.URL, kind: "trace"},
	})
	elapsed := time.Since(start)
	if !res.OK || res.EgressIP != "198.51.100.10" || res.Source != "fast4" {
		t.Fatalf("unexpected parallel probe result: %+v", res)
	}
	if elapsed > 1200*time.Millisecond {
		t.Fatalf("parallel probe took too long (%v), expected fast source win", elapsed)
	}
}

func TestFetchEgressEndpointEmptyBodyNoPanic(t *testing.T) {
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer emptySrv.Close()

	if _, _, _, err := fetchEgressEndpoint(context.Background(), emptySrv.Client(), emptySrv.URL, "plain"); err == nil {
		t.Fatal("expected error for empty plain egress response body, not panic or success")
	}
}

func TestValidateEgressPolicyRejectsUnknownCountryWhenAllowedListSet(t *testing.T) {
	cfg := config.Default()
	cfg.Egress.ExpectedIP = ""
	cfg.Egress.AllowedCountries = []string{"US"}

	res := ProbeResult{
		OK:       true,
		EgressIP: "198.51.100.10",
		Country:  "",
	}
	if msg := ValidateEgressPolicy(cfg, res); msg == "" {
		t.Fatal("expected policy rejection when AllowedCountries is set, ExpectedIP is empty, and Country is unknown")
	}
}
