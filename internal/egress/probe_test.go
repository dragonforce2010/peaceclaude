package egress

import (
	"context"
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
