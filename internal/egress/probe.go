package egress

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/upstream"
)

type GeoInfo struct {
	IP        string    `json:"ip"`
	Country   string    `json:"country"`
	Region    string    `json:"region"`
	City      string    `json:"city"`
	Timezone  string    `json:"timezone"`
	ASN       int       `json:"asn"`
	Org       string    `json:"org"`
	ISP       string    `json:"isp"`
	FetchedAt time.Time `json:"fetched_at"`
}

type ProbeResult struct {
	CheckedAt       time.Time `json:"checked_at"`
	OK              bool      `json:"ok"`
	EgressIP        string    `json:"egress_ip,omitempty"`
	Country         string    `json:"country,omitempty"`
	Region          string    `json:"region,omitempty"`
	City            string    `json:"city,omitempty"`
	Timezone        string    `json:"timezone,omitempty"`
	ASN             int       `json:"asn,omitempty"`
	Org             string    `json:"org,omitempty"`
	ISP             string    `json:"isp,omitempty"`
	Colo            string    `json:"colo,omitempty"`
	LatencyMs       int64     `json:"latency_ms"`
	Source          string    `json:"source,omitempty"`
	PublicSplitIP   string    `json:"public_split_ip,omitempty"`
	SplitIPDetected bool      `json:"split_ip_detected"`
	FailureReason   string    `json:"failure_reason,omitempty"`
	SkippedReason   string    `json:"skipped_reason,omitempty"`
}

type GateSnapshot struct {
	Open               bool        `json:"open"`
	ManualLock         bool        `json:"manual_lock"`
	LockReason         string      `json:"lock_reason,omitempty"`
	ActiveStreams      int64       `json:"active_streams"`
	EffectiveTimezone  string      `json:"effective_timezone"`
	EffectiveLanguage  string      `json:"effective_language"`
	EffectiveLocale    string      `json:"effective_locale"`
	LastProbe          ProbeResult `json:"last_probe"`
	LastGood           ProbeResult `json:"last_good"`
	ExpectedIP         string      `json:"expected_ip,omitempty"`
	AllowedCountries   []string    `json:"allowed_countries,omitempty"`
	ConsecutiveFails   int         `json:"consecutive_fails"`
}

type ProbeFunc func(ctx context.Context, cfg *config.Config) ProbeResult

type Gate struct {
	mu               sync.RWMutex
	cfg              *config.Config
	open             bool
	manualLock       bool
	lockReason       string
	lastProbe        ProbeResult
	lastGood         ProbeResult
	consecutiveFails int
	activeStreams    atomic.Int64
	geoCache         map[string]GeoInfo
	probeFn          ProbeFunc
	onStateChange    func(open bool, reason string, res ProbeResult)
	initialDone      chan struct{}
	initialOnce      sync.Once
}

func NewGate(cfg *config.Config) *Gate {
	g := &Gate{
		cfg:         cfg,
		open:        false,
		lockReason:  "initializing egress check",
		geoCache:    loadGeoCache(),
		initialDone: make(chan struct{}),
	}
	g.probeFn = g.defaultProbe
	return g
}

// WaitInitialProbe blocks until the first egress probe finishes or ctx expires.
func (g *Gate) WaitInitialProbe(ctx context.Context) {
	select {
	case <-g.initialDone:
	case <-ctx.Done():
	}
}

func (g *Gate) SetProbeFunc(fn ProbeFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.probeFn = fn
}

func (g *Gate) SetOnStateChange(fn func(open bool, reason string, res ProbeResult)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onStateChange = fn
}

func (g *Gate) UpdateConfig(cfg *config.Config) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cfg = cfg
}

func (g *Gate) BeginStream() { g.activeStreams.Add(1) }
func (g *Gate) EndStream() {
	if g.activeStreams.Add(-1) < 0 {
		g.activeStreams.Store(0)
	}
}
func (g *Gate) ActiveStreams() int64 { return g.activeStreams.Load() }

func (g *Gate) AllowTraffic() (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.manualLock {
		return false, "manual fail-closed lock active"
	}
	if !g.open {
		if g.lockReason != "" {
			return false, g.lockReason
		}
		return false, "egress gate locked"
	}
	return true, ""
}

func (g *Gate) SetManualLock(locked bool, reason string) {
	g.mu.Lock()
	g.manualLock = locked
	if locked {
		if reason == "" {
			reason = "manual emergency lock"
		}
		g.lockReason = reason
		g.open = false
	}
	cb := g.onStateChange
	res := g.lastProbe
	g.mu.Unlock()
	if cb != nil && locked {
		cb(false, reason, res)
	}
}

func (g *Gate) Snapshot() GateSnapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	cfg := g.cfg
	tz := g.effectiveTimezoneLocked()
	lang := "en-US"
	locale := "en_US.UTF-8"
	var expectedIP string
	var allowed []string
	if cfg != nil {
		lang = cfg.Identity.Language
		locale = cfg.Identity.Locale
		expectedIP = cfg.Egress.ExpectedIP
		allowed = append([]string(nil), cfg.Egress.AllowedCountries...)
	}
	return GateSnapshot{
		Open:              g.open && !g.manualLock,
		ManualLock:        g.manualLock,
		LockReason:        g.lockReason,
		ActiveStreams:     g.activeStreams.Load(),
		EffectiveTimezone: tz,
		EffectiveLanguage: lang,
		EffectiveLocale:   locale,
		LastProbe:         g.lastProbe,
		LastGood:          g.lastGood,
		ExpectedIP:        expectedIP,
		AllowedCountries:  allowed,
		ConsecutiveFails:  g.consecutiveFails,
	}
}

func (g *Gate) effectiveTimezoneLocked() string {
	if g.cfg != nil && strings.TrimSpace(g.cfg.Identity.Timezone) != "" {
		return strings.TrimSpace(g.cfg.Identity.Timezone)
	}
	if g.cfg == nil || g.cfg.Identity.SyncTimezone {
		if g.lastGood.Timezone != "" {
			return g.lastGood.Timezone
		}
		if g.lastProbe.Timezone != "" {
			return g.lastProbe.Timezone
		}
		country := strings.ToUpper(g.lastGood.Country)
		if country == "" {
			country = strings.ToUpper(g.lastProbe.Country)
		}
		if tz := countryDefaultTimezone(country); tz != "" {
			return tz
		}
	}
	return "America/New_York"
}

func countryDefaultTimezone(cc string) string {
	switch cc {
	case "US":
		return "America/New_York"
	case "GB", "UK":
		return "Europe/London"
	case "JP":
		return "Asia/Tokyo"
	case "DE":
		return "Europe/Berlin"
	case "FR":
		return "Europe/Paris"
	case "NL":
		return "Europe/Amsterdam"
	case "CA":
		return "America/Toronto"
	case "AU":
		return "Australia/Sydney"
	case "SG":
		return "Asia/Singapore"
	default:
		return ""
	}
}

// Check runs an egress probe and updates the Fail-Closed Gate state.
// If respectSSE is true and a Claude stream is active while the gate is already open,
// it preserves the healthy state without hitting the residential proxy with extra handshakes.
func (g *Gate) Check(ctx context.Context, respectSSE bool) ProbeResult {
	g.mu.RLock()
	cfg := g.cfg
	probeFn := g.probeFn
	currentlyOpen := g.open
	manualLock := g.manualLock
	lastGood := g.lastGood
	g.mu.RUnlock()

	if respectSSE && cfg != nil && cfg.Egress.SkipProbeDuringSSE && currentlyOpen && !manualLock && g.activeStreams.Load() > 0 {
		if time.Since(lastGood.CheckedAt) < 10*time.Minute && lastGood.OK {
			skipped := lastGood
			skipped.CheckedAt = time.Now().UTC()
			skipped.SkippedReason = "active_sse_stream_protected"
			g.mu.Lock()
			g.lastProbe = skipped
			g.mu.Unlock()
			return skipped
		}
	}

	res := probeFn(ctx, cfg)
	if res.CheckedAt.IsZero() {
		res.CheckedAt = time.Now().UTC()
	}

	// Validate against expected IP and allowed countries.
	if res.OK {
		if errMsg := ValidateEgressPolicy(cfg, res); errMsg != "" {
			res.OK = false
			res.FailureReason = errMsg
		}
	}

	g.mu.Lock()
	prevOpen := g.open && !g.manualLock
	g.lastProbe = res
	if res.OK {
		g.lastGood = res
		g.consecutiveFails = 0
		if !g.manualLock {
			g.open = true
			g.lockReason = ""
		}
	} else {
		g.consecutiveFails++
		// On proven IP/country mismatch, lock immediately.
		// On transient timeout when already open, require 2 consecutive failures if streams are active,
		// otherwise lock immediately for strict fail-closed protection.
		mismatch := strings.Contains(res.FailureReason, "mismatch") || strings.Contains(res.FailureReason, "country")
		if mismatch || !currentlyOpen || g.activeStreams.Load() == 0 || g.consecutiveFails >= 2 {
			g.open = false
			g.lockReason = res.FailureReason
		}
	}
	nowOpen := g.open && !g.manualLock
	lockReason := g.lockReason
	cb := g.onStateChange
	g.mu.Unlock()
	g.initialOnce.Do(func() { close(g.initialDone) })

	if cb != nil && prevOpen != nowOpen {
		cb(nowOpen, lockReason, res)
	}
	return res
}

func ValidateEgressPolicy(cfg *config.Config, res ProbeResult) string {
	ip, err := netip.ParseAddr(strings.TrimSpace(res.EgressIP))
	if err != nil {
		return fmt.Sprintf("invalid egress IP %q", res.EgressIP)
	}
	if !ip.Is4() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return fmt.Sprintf("non-public IPv4 egress %q rejected", res.EgressIP)
	}
	if cfg == nil {
		return ""
	}
	expected := strings.TrimSpace(cfg.Egress.ExpectedIP)
	if expected != "" && res.EgressIP != expected {
		return fmt.Sprintf("egress IP mismatch: actual=%s expected=%s", res.EgressIP, expected)
	}
	if len(cfg.Egress.AllowedCountries) > 0 && res.Country != "" {
		matched := false
		for _, cc := range cfg.Egress.AllowedCountries {
			if strings.EqualFold(strings.TrimSpace(cc), strings.TrimSpace(res.Country)) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Sprintf("egress country %q not in allowed list %v", res.Country, cfg.Egress.AllowedCountries)
		}
	}
	return ""
}

func (g *Gate) defaultProbe(ctx context.Context, cfg *config.Config) ProbeResult {
	res := ProbeResult{CheckedAt: time.Now().UTC()}
	if cfg == nil {
		res.FailureReason = "missing config"
		return res
	}
	timeout := time.Duration(cfg.Egress.ProbeTimeoutSec) * time.Second
	if timeout < 10*time.Second {
		timeout = 12 * time.Second
	}
	dialer := upstream.FromConfig(cfg.Upstream)
	client := dialer.NewHTTPClient(timeout)

	type probeSource struct {
		name string
		url  string
		kind string
	}
	var sources []probeSource
	if cfg.Upstream.Mode == "local-proxy" {
		// In rule-based local-proxy mode (e.g. Clash Verge), only probe Claude's own domain
		// (with one retry) so non-Claude fallback URLs never produce false Split-IP mismatches.
		sources = []probeSource{
			{"claude.ai/trace", "https://claude.ai/cdn-cgi/trace", "trace"},
			{"claude.ai/trace(retry)", "https://claude.ai/cdn-cgi/trace", "trace"},
		}
	} else {
		sources = []probeSource{
			{"claude.ai/trace", "https://claude.ai/cdn-cgi/trace", "trace"},
			{"1.1.1.1/trace", "https://1.1.1.1/cdn-cgi/trace", "trace"},
			{"icanhazip", "https://ipv4.icanhazip.com/", "plain"},
			{"ipify", "https://api.ipify.org/", "plain"},
		}
	}

	var lastErr string
	for _, src := range sources {
		start := time.Now()
		sCtx, sCancel := context.WithTimeout(ctx, timeout)
		ip, loc, colo, err := fetchEgressEndpoint(sCtx, client, src.url, src.kind)
		sCancel()
		if err == nil && ip != "" {
			res.OK = true
			res.EgressIP = ip
			res.Country = loc
			res.Colo = colo
			res.Source = src.name
			res.LatencyMs = time.Since(start).Milliseconds()
			break
		}
		lastErr = fmt.Sprintf("%s: %v", src.name, err)
	}

	if !res.OK {
		res.FailureReason = "upstream probe failed: " + lastErr
		return res
	}

	// Enrich GeoIP & Split-IP in parallel with tight timeouts so probes stay fast.
	var wg sync.WaitGroup
	var geo GeoInfo
	var hasGeo bool
	wg.Add(1)
	go func() {
		defer wg.Done()
		geo, hasGeo = g.lookupGeo(ctx, res.EgressIP)
	}()

	var pubIP string
	var splitErr error
	if cfg.Upstream.Mode == "local-proxy" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			splitCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			pubIP, _, _, splitErr = fetchEgressEndpoint(splitCtx, client, "https://1.1.1.1/cdn-cgi/trace", "trace")
		}()
	}
	wg.Wait()

	if hasGeo {
		if geo.Country != "" {
			res.Country = geo.Country
		}
		res.Region = geo.Region
		res.City = geo.City
		res.Timezone = geo.Timezone
		res.ASN = geo.ASN
		res.Org = geo.Org
		res.ISP = geo.ISP
	}
	if cfg.Upstream.Mode == "local-proxy" && splitErr == nil && pubIP != "" {
		res.PublicSplitIP = pubIP
		if pubIP != res.EgressIP {
			res.SplitIPDetected = true
		}
	}
	return res
}

func fetchEgressEndpoint(ctx context.Context, client *http.Client, targetURL, kind string) (ip, loc, colo string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("User-Agent", "Go-http-client/1.1")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return "", "", "", err
	}
	text := strings.TrimSpace(string(body))
	if kind == "trace" {
		for _, line := range strings.Split(text, "\n") {
			k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			switch k {
			case "ip":
				ip = strings.TrimSpace(v)
			case "loc":
				loc = strings.TrimSpace(v)
			case "colo":
				colo = strings.TrimSpace(v)
			}
		}
	} else {
		ip = strings.Fields(text)[0]
	}
	if _, parseErr := netip.ParseAddr(ip); parseErr != nil {
		return "", "", "", fmt.Errorf("invalid IP in response: %q", ip)
	}
	return ip, loc, colo, nil
}

func (g *Gate) lookupGeo(ctx context.Context, ip string) (GeoInfo, bool) {
	g.mu.RLock()
	if cached, ok := g.geoCache[ip]; ok && time.Since(cached.FetchedAt) < 24*time.Hour {
		g.mu.RUnlock()
		return cached, true
	}
	g.mu.RUnlock()

	geoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Query public GeoIP metadata for the already-discovered egress IP string.
	req, err := http.NewRequestWithContext(geoCtx, http.MethodGet, "https://ipwho.is/"+ip, nil)
	if err != nil {
		return GeoInfo{}, false
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return GeoInfo{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return GeoInfo{}, false
	}
	var raw struct {
		Success     bool   `json:"success"`
		IP          string `json:"ip"`
		CountryCode string `json:"country_code"`
		Region      string `json:"region"`
		City        string `json:"city"`
		Connection  struct {
			ASN int    `json:"asn"`
			Org string `json:"org"`
			ISP string `json:"isp"`
		} `json:"connection"`
		Timezone struct {
			ID string `json:"id"`
		} `json:"timezone"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&raw); err != nil || !raw.Success {
		return GeoInfo{}, false
	}
	info := GeoInfo{
		IP:        ip,
		Country:   raw.CountryCode,
		Region:    raw.Region,
		City:      raw.City,
		Timezone:  raw.Timezone.ID,
		ASN:       raw.Connection.ASN,
		Org:       raw.Connection.Org,
		ISP:       raw.Connection.ISP,
		FetchedAt: time.Now().UTC(),
	}
	g.mu.Lock()
	if g.geoCache == nil {
		g.geoCache = map[string]GeoInfo{}
	}
	g.geoCache[ip] = info
	saveGeoCache(g.geoCache)
	g.mu.Unlock()
	return info, true
}

func loadGeoCache() map[string]GeoInfo {
	out := map[string]GeoInfo{}
	raw, err := os.ReadFile(config.GeoCachePath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

func saveGeoCache(cache map[string]GeoInfo) {
	_ = config.EnsureDirs()
	raw, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(config.GeoCachePath(), raw, 0o600)
}
