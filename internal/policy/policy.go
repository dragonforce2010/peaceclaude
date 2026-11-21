package policy

import (
	"net"
	"net/netip"
	"strings"
	"sync"

	"peaceclaude/internal/config"
)

type Category string

const (
	CategoryLoopback  Category = "loopback"
	CategoryCorporate Category = "corporate"
	CategoryTelemetry Category = "telemetry"
	CategoryAnthropic Category = "anthropic"
	CategoryAuth      Category = "auth"
	CategoryPrivate   Category = "private"
	CategoryPublic    Category = "public"
)

type Action string

const (
	ActionProxy            Action = "proxy"
	ActionDirectLocal      Action = "direct_local"
	ActionDirectCorporate  Action = "direct_corporate"
	ActionBlockFailClosed  Action = "block_failclosed"
	ActionBlockTelemetry   Action = "block_telemetry"
	ActionBlockPrivate     Action = "block_private"
	ActionBlockStrictSplit Action = "block_strict_split"
)

type Decision struct {
	Host           string   `json:"host"`
	Port           string   `json:"port"`
	Category       Category `json:"category"`
	Action         Action   `json:"action"`
	Reason         string   `json:"reason"`
	SplitIPWarning bool     `json:"split_ip_warning"`
	IsClaudeStream bool     `json:"is_claude_stream"`
}

var (
	AnthropicV4CIDRs = []string{
		"160.79.104.0/23",
		"160.79.106.0/24",
		"38.46.212.0/24",
		"38.84.152.0/24",
		"209.249.57.0/24",
	}
	AnthropicV6CIDRs = []string{
		"2607:6bc0::/32",
	}

	defaultClaudeDomains = []string{
		"anthropic.com",
		"claude.ai",
		"claude.com",
		"claudeusercontent.com",
	}

	defaultAuthDomains = []string{
		"accounts.google.com",
		"oauthaccountmanager.googleapis.com",
		"apis.google.com",
		"gstatic.com",
		"googleapis.com",
		"googleusercontent.com",
		"hcaptcha.com",
		"challenges.cloudflare.com",
		"turnstile.cloudflare.com",
		"appleid.apple.com",
		"stripe.com",
		"js.stripe.com",
		"m.stripe.network",
		"stripe.network",
		"stytch.com",
	}

	defaultTelemetryDomains = []string{
		"sentry.io",
		"statsigapi.net",
		"statsig.anthropic.com",
		"api.segment.io",
		"cdn.segment.com",
		"events.launchdarkly.com",
		"mobile.launchdarkly.com",
		"datadoghq.com",
		"browser-intake-datadoghq.com",
		"o1158394.ingest.us.sentry.io",
		"o1158394.ingest.sentry.io",
	}

	anthropicPrefixes []netip.Prefix
	reservedPrefixes  []netip.Prefix
)

// ClaudeDomains returns a defensive copy of built-in Claude/Anthropic domains.
func ClaudeDomains() []string {
	return append([]string(nil), defaultClaudeDomains...)
}

// AuthDomains returns a defensive copy of built-in OAuth/captcha/billing auth domains.
func AuthDomains() []string {
	return append([]string(nil), defaultAuthDomains...)
}

// TelemetryDomains returns a defensive copy of built-in telemetry sink domains.
func TelemetryDomains() []string {
	return append([]string(nil), defaultTelemetryDomains...)
}

func init() {
	for _, s := range append(append([]string{}, AnthropicV4CIDRs...), AnthropicV6CIDRs...) {
		anthropicPrefixes = append(anthropicPrefixes, netip.MustParsePrefix(s))
	}
	for _, s := range []string{
		"0.0.0.0/8",
		"100.64.0.0/10",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"240.0.0.0/4",
		"::/96",
		"100::/64",
		"2001:db8::/32",
		"fec0::/10",
		"64:ff9b:1::/48",
	} {
		reservedPrefixes = append(reservedPrefixes, netip.MustParsePrefix(s))
	}
}

type Engine struct {
	mu  sync.RWMutex
	cfg *config.Config
}

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{cfg: cfg}
}

func (e *Engine) UpdateConfig(cfg *config.Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
}

func (e *Engine) configSnapshot() *config.Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

func SplitHostPort(hostport string) (string, string) {
	h, p, err := net.SplitHostPort(hostport)
	if err == nil {
		return normalizeHost(h), p
	}
	return normalizeHost(hostport), ""
}

func normalizeHost(h string) string {
	h = strings.TrimSpace(strings.ToLower(h))
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	h = strings.TrimSuffix(h, ".")
	return h
}

func matchesDomain(host, rule string) bool {
	rule = strings.TrimSpace(strings.ToLower(rule))
	rule = strings.TrimPrefix(rule, "*.")
	rule = strings.TrimPrefix(rule, ".")
	rule = strings.TrimSuffix(rule, ".")
	if rule == "" {
		return false
	}
	return host == rule || strings.HasSuffix(host, "."+rule)
}

func IsAnthropicIP(ipStr string) bool {
	addr, err := netip.ParseAddr(normalizeHost(ipStr))
	if err != nil {
		return false
	}
	addr = addr.Unmap().WithZone("")
	for _, p := range anthropicPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func IsLoopbackHost(host string) bool {
	h := normalizeHost(host)
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	return addr.Unmap().IsLoopback()
}

func IsPrivateNonLoopbackIP(host string) bool {
	addr, err := netip.ParseAddr(normalizeHost(host))
	if err != nil {
		return false
	}
	addr = addr.Unmap().WithZone("")
	if addr.IsLoopback() {
		return false
	}
	if addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	for _, r := range reservedPrefixes {
		if r.Contains(addr) {
			return true
		}
	}
	return false
}

func IsClaudeStreamHost(host string) bool {
	h := normalizeHost(host)
	return h == "api.anthropic.com" ||
		h == "a-api.anthropic.com" ||
		h == "claude.ai" ||
		strings.HasSuffix(h, ".anthropic.com") ||
		strings.HasSuffix(h, ".claude.ai")
}

func (e *Engine) Classify(host string) Category {
	return classifyWithConfig(e.configSnapshot(), host)
}

func classifyWithConfig(cfg *config.Config, host string) Category {
	h := normalizeHost(host)
	if IsLoopbackHost(h) {
		return CategoryLoopback
	}
	if cfg != nil {
		for _, d := range cfg.Shield.ExtraBlockedDomains {
			if matchesDomain(h, d) {
				return CategoryTelemetry
			}
		}
	}
	for _, d := range defaultTelemetryDomains {
		if matchesDomain(h, d) {
			return CategoryTelemetry
		}
	}
	if IsAnthropicIP(h) {
		return CategoryAnthropic
	}
	for _, d := range defaultClaudeDomains {
		if matchesDomain(h, d) {
			return CategoryAnthropic
		}
	}
	if cfg != nil {
		for _, d := range cfg.Shield.ExtraClaudeDomains {
			if matchesDomain(h, d) {
				return CategoryAnthropic
			}
		}
	}
	for _, d := range defaultAuthDomains {
		if matchesDomain(h, d) {
			return CategoryAuth
		}
	}
	if cfg != nil {
		for _, d := range cfg.Shield.CorporateDomains {
			if matchesDomain(h, d) {
				return CategoryCorporate
			}
		}
	}
	if IsPrivateNonLoopbackIP(h) {
		return CategoryPrivate
	}
	return CategoryPublic
}

// Evaluate determines how the gateway handles a connection given current Egress Gate state.
func (e *Engine) Evaluate(hostport string, gateOpen bool, gateReason string) Decision {
	cfg := e.configSnapshot()
	host, port := SplitHostPort(hostport)
	cat := classifyWithConfig(cfg, host)
	dec := Decision{
		Host:           host,
		Port:           port,
		Category:       cat,
		IsClaudeStream: cat == CategoryAnthropic && IsClaudeStreamHost(host),
	}

	switch cat {
	case CategoryLoopback:
		dec.Action = ActionDirectLocal
		dec.Reason = "loopback/local service"
		return dec

	case CategoryCorporate:
		if cfg != nil && cfg.Shield.AllowCorporateDirect {
			dec.Action = ActionDirectCorporate
			dec.Reason = "corporate allowlist direct"
			return dec
		}

	case CategoryTelemetry:
		if cfg != nil && cfg.Shield.TelemetryShield {
			dec.Action = ActionBlockTelemetry
			dec.Reason = "blocked by telemetry shield"
			return dec
		}

	case CategoryPrivate:
		if cfg != nil && cfg.Shield.BlockPrivateTargets {
			dec.Action = ActionBlockPrivate
			dec.Reason = "blocked private/internal IP target"
			return dec
		}
		dec.Action = ActionDirectLocal
		dec.Reason = "private LAN direct"
		return dec
	}

	// Protected or public egress traffic.
	failClosed := cfg == nil || cfg.Egress.FailClosed
	if !gateOpen && failClosed {
		if gateReason == "" {
			gateReason = "egress gate locked"
		}
		dec.Action = ActionBlockFailClosed
		dec.Reason = "fail-closed: " + gateReason
		return dec
	}

	// Detect split-IP risk when riding on a rule-based local proxy (e.g. Clash Verge).
	if cat == CategoryPublic && cfg != nil && cfg.Upstream.Mode == "local-proxy" {
		dec.SplitIPWarning = true
		if cfg.Egress.StrictDomainIsolation {
			dec.Action = ActionBlockStrictSplit
			dec.Reason = "blocked non-Claude public host under local-proxy strict mode (prevents split-IP)"
			return dec
		}
		dec.Action = ActionProxy
		dec.Reason = "forwarded via local-proxy (split-IP risk: verify upstream routes this domain via static IP)"
		return dec
	}

	dec.Action = ActionProxy
	dec.Reason = "forwarded via verified static-IP upstream"
	return dec
}
