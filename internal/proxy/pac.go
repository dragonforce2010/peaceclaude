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
  host = host.toLowerCase();
  if (isPlainHostName(host) || host === "localhost" || host === "127.0.0.1" || host === "::1" || dnsDomainIs(host, ".local")) return "DIRECT";
%s
  return "%s";
}
`, strings.Join(bypassChecks, "\n"), proxyLine)
	}

	domains := []string{
		"anthropic.com",
		"claude.ai",
		"claude.com",
		"claudeusercontent.com",
		"accounts.google.com",
		"oauthaccountmanager.googleapis.com",
		"hcaptcha.com",
		"challenges.cloudflare.com",
		"turnstile.cloudflare.com",
		"sentry.io",
		"statsigapi.net",
	}
	for _, extra := range cfg.Shield.ExtraClaudeDomains {
		clean := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(extra)), "*."), ".")
		if clean != "" {
			domains = append(domains, clean)
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

	return fmt.Sprintf(`// PeaceClaude PAC (mode=claude) — fail-closed Claude/Anthropic routing, everything else DIRECT
function FindProxyForURL(url, host) {
  var PROXY = "%s";
  host = host.toLowerCase();
%s
  if (/^\d+\.\d+\.\d+\.\d+$/.test(host)) {
%s
  }
  if (host.indexOf("2607:6bc0:") === 0 || host.indexOf("[2607:6bc0:") === 0) return PROXY;
  return "DIRECT";
}
`, proxyLine, strings.Join(domainChecks, "\n"), strings.Join(cidrChecks, "\n"))
}
