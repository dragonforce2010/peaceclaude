package policy

import (
	"sync"
	"testing"

	"peaceclaude/internal/config"
)

func TestPolicyDecisions(t *testing.T) {
	cfg := config.Default()
	cfg.Upstream.Mode = "local-proxy"
	cfg.Shield.TelemetryShield = true
	cfg.Shield.CorporateDomains = []string{".corp.internal", "git.corp.internal"}
	cfg.Egress.FailClosed = true
	eng := NewEngine(cfg)

	// 1. Anthropic allowed when gate is open
	d := eng.Evaluate("api.anthropic.com:443", true, "")
	if d.Category != CategoryAnthropic || d.Action != ActionProxy || !d.IsClaudeStream {
		t.Fatalf("unexpected open anthropic decision: %+v", d)
	}

	// 2. Anthropic MUST fail-closed when gate is locked
	d = eng.Evaluate("api.anthropic.com:443", false, "ip mismatch")
	if d.Action != ActionBlockFailClosed {
		t.Fatalf("expected fail-closed block, got: %+v", d)
	}

	// 3. Literal Anthropic IPv4 & IPv6 CIDRs must be classified as Anthropic
	for _, target := range []string{"160.79.104.10:443", "[2607:6bc0::10]:443"} {
		d = eng.Evaluate(target, false, "locked")
		if d.Category != CategoryAnthropic || d.Action != ActionBlockFailClosed {
			t.Fatalf("target %s expected anthropic fail-closed, got %+v", target, d)
		}
	}

	// 4. Telemetry blocked regardless of gate state
	for _, host := range []string{"o1158394.ingest.us.sentry.io:443", "statsig.anthropic.com:443", "statsigapi.net:443"} {
		d = eng.Evaluate(host, true, "")
		if d.Category != CategoryTelemetry || d.Action != ActionBlockTelemetry {
			t.Fatalf("telemetry %s expected block, got %+v", host, d)
		}
	}

	// 5. Loopback & corporate bypass allowed even when gate is locked
	d = eng.Evaluate("127.0.0.1:3000", false, "locked")
	if d.Action != ActionDirectLocal {
		t.Fatalf("loopback expected direct_local, got %+v", d)
	}
	d = eng.Evaluate("git.corp.internal:443", false, "locked")
	if d.Action != ActionDirectCorporate {
		t.Fatalf("corporate expected direct_corporate, got %+v", d)
	}

	// 6. Split-IP warning on non-Claude public domain in local-proxy mode
	d = eng.Evaluate("ipwho.is:443", true, "")
	if d.Action != ActionProxy || !d.SplitIPWarning {
		t.Fatalf("expected split-IP warning under local-proxy, got %+v", d)
	}

	// 7. Strict domain isolation blocks non-Claude public host under local-proxy
	cfg.Egress.StrictDomainIsolation = true
	d = eng.Evaluate("ipwho.is:443", true, "")
	if d.Action != ActionBlockStrictSplit {
		t.Fatalf("expected strict split block, got %+v", d)
	}

	// 8. Under direct-socks5 (standalone Webshare), public domains have NO split-IP warning
	cfg.Upstream.Mode = "direct-socks5"
	d = eng.Evaluate("ipwho.is:443", true, "")
	if d.Action != ActionProxy || d.SplitIPWarning {
		t.Fatalf("direct-socks5 should proxy without split-IP warning, got %+v", d)
	}
}

func TestAuthDomainsContainGoogleOAuthAssetsAndExcludeWWWGoogle(t *testing.T) {
	cfg := config.Default()
	eng := NewEngine(cfg)

	for _, authHost := range []string{
		"accounts.google.com:443",
		"oauthaccountmanager.googleapis.com:443",
		"www.googleapis.com:443",
		"fonts.gstatic.com:443",
		"lh3.googleusercontent.com:443",
		"newassets.hcaptcha.com:443",
		"b.stripe.network:443",
	} {
		if cat := eng.Classify(authHost[:len(authHost)-4]); cat != CategoryAuth {
			t.Fatalf("expected %s to classify as auth, got %s", authHost, cat)
		}
	}

	// Regular Google Search MUST NOT be forced into auth/proxy list so PAC stays DIRECT for daily search.
	for _, pubHost := range []string{"www.google.com", "google.com", "mail.google.com"} {
		if cat := eng.Classify(pubHost); cat != CategoryPublic {
			t.Fatalf("expected %s to remain public/direct, got %s", pubHost, cat)
		}
	}
}

func TestEngineConcurrentUpdateAndEvaluate(t *testing.T) {
	cfg := config.Default()
	eng := NewEngine(cfg)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if idx%2 == 0 {
					next := config.Default()
					next.Shield.AllowCorporateDirect = (j%2 == 0)
					eng.UpdateConfig(next)
				} else {
					_ = eng.Evaluate("api.anthropic.com:443", true, "")
					_ = eng.Classify("accounts.google.com")
				}
			}
		}(i)
	}
	wg.Wait()
}
