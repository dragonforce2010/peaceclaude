package proxy

import (
	"fmt"
	"net"
	"strings"

	"peaceclaude/internal/config"
	"peaceclaude/internal/policy"
)

// RenderPAC generates a PAC script routing Claude/Anthropic/Auth domains & CIDRs
// through PeaceClaude's local fail-closed gateway. When mode=="all", all public traffic
// (everything except loopback and corporate allowlist) routes through PeaceClaude.
func RenderPAC(cfg *config.Config, mode string) string {
	proxyLine := fmt.Sprintf("PROXY %s:%d; SOCKS5 %s:%d",
		cfg.Listen.BindAddr, cfg.Listen.HTTPPort,
		cfg.Listen.BindAddr, cfg.Listen.SOCKS5Port,
	)

	if strings.EqualFold(mode, "all") {
		var bypassChecks []string
		if cfg.Shield.AllowCorporateDirect {
			for _, d := range cfg.Shield.CorporateDomains {
				clean := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d)), "*."), ".")
				if clean == "" {
					continue
				}
				bypassChecks = append(bypassChecks, fmt.Sprintf(`  if (dnsDomainIs(host, ".%s") || host === "%s") return "DIRECT";`, clean, clean))
			}
		}
		return fmt.Sprintf(`// PeaceClaude PAC (mode=all) — routes all non-local traffic via PeaceClaude Fail-Closed Gateway
function FindProxyForURL(url, host) {
  host = host.toLowerCase().replace(/\.+$/, "");
  if (isPlainHostName(host) || host === "localhost" || host === "127.0.0.1" || host === "::1" || dnsDomainIs(host, ".local")) return "DIRECT";
%s
  return "%s";
}
`, strings.Join(bypassChecks, "\n"), proxyLine)
	}

	seen := make(map[string]struct{})
	var domains []string
	addDomain := func(raw string) {
		clean := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "*."), ".")
		clean = strings.TrimSuffix(clean, ".")
		if clean == "" {
			return
		}
		if _, ok := seen[clean]; ok {
			return
		}
		seen[clean] = struct{}{}
		domains = append(domains, clean)
	}

	for _, d := range policy.ClaudeDomains() {
		addDomain(d)
	}
	// Only route third-party OAuth/verification domains (Google/Apple/Cloudflare) through
	// PeaceClaude when explicitly requested; by default keep PAC strictly Claude-only so
	// Google login and regular browsing always use the normal host network (DIRECT).
	if cfg != nil && cfg.Shield.ProxyAuthDomains {
		for _, d := range policy.AuthDomains() {
			addDomain(d)
		}
	}
	// Route only Anthropic-specific telemetry sinks into PeaceClaude's telemetry shield
	// so third-party websites using sentry.io or statsigapi.net stay DIRECT for host browsers.
	for _, d := range []string{
		"o1158394.ingest.us.sentry.io",
		"o1158394.ingest.sentry.io",
		"o298668.ingest.sentry.io",
		"statsig.anthropic.com",
	} {
		addDomain(d)
	}
	isDesktop := strings.EqualFold(strings.TrimSpace(mode), "desktop")
	if isDesktop && cfg != nil && cfg.Shield.TelemetryShield {
		for _, d := range policy.TelemetryDomains() {
			addDomain(d)
		}
	}
	if cfg != nil {
		for _, extra := range cfg.Shield.ExtraClaudeDomains {
			addDomain(extra)
		}
		if isDesktop {
			for _, extra := range cfg.Shield.ExtraBlockedDomains {
				addDomain(extra)
			}
		}
	}

	var domainChecks []string
	for _, d := range domains {
		domainChecks = append(domainChecks, fmt.Sprintf(`  if (dnsDomainIs(host, ".%s") || host === "%s") return PROXY;`, d, d))
	}

	var cidrChecks []string
	for _, c := range policy.AnthropicV4CIDRs {
		_, ipNet, err := net.ParseCIDR(c)
		if err != nil {
			continue
		}
		mask := net.IP(ipNet.Mask).String()
		cidrChecks = append(cidrChecks, fmt.Sprintf(`  if (isInNet(host, "%s", "%s")) return PROXY;`, ipNet.IP.String(), mask))
	}

	modeLabel := "claude"
	if isDesktop {
		modeLabel = "desktop"
	}

	return fmt.Sprintf(`// PeaceClaude PAC (mode=%s) — fail-closed Claude/Anthropic routing, everything else DIRECT
function FindProxyForURL(url, host) {
  var PROXY = "%s";
  host = host.toLowerCase().replace(/\.+$/, "");
%s
  if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) {
%s
  }
  if (host.indexOf("2607:6bc0:") === 0 || host.indexOf("[2607:6bc0:") === 0) return PROXY;
  return "DIRECT";
}
`, modeLabel, proxyLine, strings.Join(domainChecks, "\n"), strings.Join(cidrChecks, "\n"))
}
