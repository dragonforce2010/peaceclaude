package profile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"peaceclaude/internal/config"
	"peaceclaude/internal/upstream"
)

type UsageWindow struct {
	Key         string     `json:"key"`
	Label       string     `json:"label"`
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at,omitempty"`
	ResetInText string     `json:"reset_in_text,omitempty"`
}

type ExtraUsageInfo struct {
	Enabled      bool    `json:"enabled"`
	MonthlyLimit float64 `json:"monthly_limit,omitempty"`
	UsedCredits  float64 `json:"used_credits,omitempty"`
	Utilization  float64 `json:"utilization,omitempty"`
	Currency     string  `json:"currency,omitempty"`
}

type UsageSnapshot struct {
	Profile          string          `json:"profile"`
	Status           string          `json:"status"` // ok | history | no_token | gate_locked | error
	Source           string          `json:"source"` // oauth_api | desktop_history | none
	SubscriptionType string          `json:"subscription_type,omitempty"`
	FetchedAt        time.Time       `json:"fetched_at"`
	FiveHour         *UsageWindow    `json:"five_hour,omitempty"`
	Weekly           *UsageWindow    `json:"weekly,omitempty"`
	WeeklySonnet     *UsageWindow    `json:"weekly_sonnet,omitempty"`
	WeeklyOpus       *UsageWindow    `json:"weekly_opus,omitempty"`
	ExtraUsage       *ExtraUsageInfo `json:"extra_usage,omitempty"`
	Error            string          `json:"error,omitempty"`
	Hint             string          `json:"hint,omitempty"`
}

// NormalizePlanLabel turns Anthropic organization/rate-limit tier identifiers into readable badges.
func NormalizePlanLabel(orgType, orgRateTier, userRateTier, seatTier, manualPlan string) string {
	if strings.TrimSpace(manualPlan) != "" {
		return strings.TrimSpace(manualPlan)
	}
	joined := strings.ToLower(strings.Join([]string{orgType, orgRateTier, userRateTier, seatTier}, " "))
	switch {
	case strings.Contains(joined, "ultra"):
		return "Ultra"
	case strings.Contains(joined, "max_20x") || strings.Contains(joined, "max20x"):
		return "Max 20x"
	case strings.Contains(joined, "max_5x") || strings.Contains(joined, "max5x"):
		return "Max 5x"
	case strings.Contains(joined, "max"):
		return "Max"
	case strings.Contains(joined, "enterprise"):
		return "Enterprise"
	case strings.Contains(joined, "team"):
		return "Team"
	case strings.Contains(joined, "pro"):
		return "Pro"
	case strings.Contains(joined, "free"):
		return "Free"
	case strings.TrimSpace(orgType) != "":
		return orgType
	default:
		return "未识别"
	}
}

// FormatAgeDuration formats elapsed time since createdAt in readable Chinese ("19 天", "3 个月 12 天", "1 年 2 个月").
func FormatAgeDuration(createdAt time.Time, now time.Time) string {
	if createdAt.IsZero() || now.Before(createdAt) {
		return ""
	}
	days := int(now.Sub(createdAt).Hours() / 24)
	if days < 1 {
		hours := int(now.Sub(createdAt).Hours())
		if hours < 1 {
			return "不到 1 小时"
		}
		return fmt.Sprintf("%d 小时", hours)
	}
	if days < 30 {
		return fmt.Sprintf("%d 天", days)
	}
	months := days / 30
	remDays := days % 30
	if months < 12 {
		if remDays > 0 {
			return fmt.Sprintf("%d 个月 %d 天", months, remDays)
		}
		return fmt.Sprintf("%d 个月", months)
	}
	years := months / 12
	remMonths := months % 12
	if remMonths > 0 {
		return fmt.Sprintf("%d 年 %d 个月", years, remMonths)
	}
	return fmt.Sprintf("%d 年", years)
}

func FormatResetCountdown(resetsAt *time.Time, now time.Time) string {
	if resetsAt == nil || resetsAt.IsZero() {
		return ""
	}
	d := resetsAt.Sub(now)
	if d <= 0 {
		return "即将重置"
	}
	totalMins := int(d.Minutes())
	if totalMins < 1 {
		return "1 分钟内重置"
	}
	days := totalMins / (24 * 60)
	hours := (totalMins % (24 * 60)) / 60
	mins := totalMins % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%d 天 %d 小时后重置", days, hours)
	case hours > 0:
		return fmt.Sprintf("%d 小时 %d 分钟后重置", hours, mins)
	default:
		return fmt.Sprintf("%d 分钟后重置", mins)
	}
}

func parseFlexibleDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q", s)
}

func readAccountIdentity(p *Profile) AccountIdentity {
	id := AccountIdentity{
		PlanLabel: "未识别",
		Source:    "none",
	}
	now := time.Now().UTC()

	// 1. Read non-secret metadata from isolated CLIConfigDir/.claude.json
	claudeJSONPath := filepath.Join(p.CLIConfigDir, ".claude.json")
	if raw, err := os.ReadFile(claudeJSONPath); err == nil && len(raw) > 0 {
		var doc struct {
			OAuthAccount *struct {
				AccountUUID               string `json:"accountUuid"`
				EmailAddress              string `json:"emailAddress"`
				OrganizationUUID          string `json:"organizationUuid"`
				HasExtraUsageEnabled      bool   `json:"hasExtraUsageEnabled"`
				BillingType               string `json:"billingType"`
				AccountCreatedAt          string `json:"accountCreatedAt"`
				SubscriptionCreatedAt     string `json:"subscriptionCreatedAt"`
				SeatTier                  string `json:"seatTier"`
				DisplayName               string `json:"displayName"`
				FullName                  string `json:"fullName"`
				OrganizationType          string `json:"organizationType"`
				OrganizationRateLimitTier string `json:"organizationRateLimitTier"`
				UserRateLimitTier         string `json:"userRateLimitTier"`
				OrganizationName          string `json:"organizationName"`
			} `json:"oauthAccount"`
		}
		if json.Unmarshal(raw, &doc) == nil && doc.OAuthAccount != nil {
			oa := doc.OAuthAccount
			id.AccountUUID = oa.AccountUUID
			id.Email = oa.EmailAddress
			id.DisplayName = firstNonEmpty(oa.FullName, oa.DisplayName, oa.EmailAddress)
			id.OrgUUID = oa.OrganizationUUID
			id.OrgName = oa.OrganizationName
			id.OrgType = oa.OrganizationType
			id.RateLimitTier = firstNonEmpty(oa.UserRateLimitTier, oa.OrganizationRateLimitTier, oa.SeatTier)
			id.BillingType = oa.BillingType
			id.HasExtraUsageEnabled = oa.HasExtraUsageEnabled
			id.Source = "claude_json"
			if t, err := parseFlexibleDate(oa.AccountCreatedAt); err == nil {
				id.AccountCreatedAt = &t
				id.AccountAgeText = FormatAgeDuration(t, now)
			}
			if t, err := parseFlexibleDate(oa.SubscriptionCreatedAt); err == nil {
				id.SubscriptionAt = &t
				id.SubscriptionAgeText = FormatAgeDuration(t, now)
			}
			id.PlanLabel = NormalizePlanLabel(oa.OrganizationType, oa.OrganizationRateLimitTier, oa.UserRateLimitTier, oa.SeatTier, "")
		}
	}

	// 2. Fallback: enrich org UUID from desktop-data/plan-usage-history.json if CLI wasn't used yet
	if id.OrgUUID == "" {
		if hist := readLatestDesktopUsageSample(p.DesktopDir); hist != nil && hist.Org != "" {
			id.OrgUUID = hist.Org
		}
	}

	// 3. Apply credential subscription metadata if present without ever exposing secrets
	if subType, tier := lookupCredentialPlanMetadata(p.CLIConfigDir); subType != "" || tier != "" {
		if id.RateLimitTier == "" {
			id.RateLimitTier = tier
		}
		if id.PlanLabel == "未识别" || id.PlanLabel == "" {
			id.PlanLabel = NormalizePlanLabel(subType, tier, "", "", "")
		}
	}

	// 3b. Enrich account/org UUID from Desktop config metadata (non-secret fields only).
	if deskAcctUUID, deskOrgUUID := readDesktopAccountMetadata(p.DesktopDir); deskAcctUUID != "" || deskOrgUUID != "" {
		if id.AccountUUID == "" {
			id.AccountUUID = deskAcctUUID
		}
		if id.OrgUUID == "" {
			id.OrgUUID = deskOrgUUID
		}
		if id.Source == "none" && p.HasDesktopAuth {
			id.Source = "desktop_config"
		}
	} else if id.Source == "none" && p.HasDesktopAuth {
		id.Source = "desktop_config"
	}

	// 4. Apply manual values: when ManualLocked=true (edited in detail modal), override even after login;
	// otherwise treat creation defaults as fallback before real Anthropic metadata is synced.
	if strings.TrimSpace(p.ManualPlan) != "" && (p.ManualLocked || id.PlanLabel == "未识别" || id.PlanLabel == "") {
		id.PlanLabel = strings.TrimSpace(p.ManualPlan)
		if id.Source == "none" {
			id.Source = "manual"
		}
	}
	if strings.TrimSpace(p.ManualRegisteredAt) != "" && (p.ManualLocked || id.AccountCreatedAt == nil) {
		if t, err := parseFlexibleDate(p.ManualRegisteredAt); err == nil {
			id.AccountCreatedAt = &t
			id.AccountAgeText = FormatAgeDuration(t, now)
			if id.Source == "none" {
				id.Source = "manual"
			}
		}
	}

	// 5. Infer Free vs Pro for activated Desktop accounts when Anthropic tier keys aren't present locally.
	if (id.PlanLabel == "未识别" || id.PlanLabel == "") && (p.HasDesktopAuth || p.HasCLICreds) {
		if _, hasPaidQuota := hasDesktopPaidUsageSample(p.DesktopDir); hasPaidQuota {
			id.PlanLabel = "Pro"
		} else {
			id.PlanLabel = "Free"
		}
	}

	return id
}

func KeychainServiceName(cliConfigDir string) string {
	clean := filepath.Clean(cliConfigDir)
	sum := sha256.Sum256([]byte(clean))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

// LookupOAuthAccessToken strictly inspects ONLY this isolated profile's credential file
// or its hashed Keychain entry (never reading host ~/.claude credentials).
func LookupOAuthAccessToken(cliConfigDir string) (token string, subType string) {
	credFile := filepath.Join(cliConfigDir, ".credentials.json")
	if raw, err := os.ReadFile(credFile); err == nil && len(raw) > 0 {
		if t, s, _ := parseOAuthCredJSON(raw); t != "" {
			return t, s
		}
	}
	if runtime.GOOS == "darwin" && os.Getenv("PEACECLAUDE_DISABLE_KEYCHAIN") != "1" {
		svc := KeychainServiceName(cliConfigDir)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", svc, "-w").Output()
		if err == nil && len(out) > 0 {
			if t, s, _ := parseOAuthCredJSON(out); t != "" {
				return t, s
			}
		}
	}
	return "", ""
}

func lookupCredentialPlanMetadata(cliConfigDir string) (subType string, rateTier string) {
	credFile := filepath.Join(cliConfigDir, ".credentials.json")
	if raw, err := os.ReadFile(credFile); err == nil && len(raw) > 0 {
		_, s, tier := parseOAuthCredJSON(raw)
		return s, tier
	}
	if runtime.GOOS == "darwin" && os.Getenv("PEACECLAUDE_DISABLE_KEYCHAIN") != "1" {
		svc := KeychainServiceName(cliConfigDir)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", svc, "-w").Output()
		if err == nil && len(out) > 0 {
			_, s, tier := parseOAuthCredJSON(out)
			return s, tier
		}
	}
	return "", ""
}

func parseOAuthCredJSON(raw []byte) (accessToken, subscriptionType, rateLimitTier string) {
	var doc struct {
		ClaudeAiOauth *struct {
			AccessToken      string `json:"accessToken"`
			SubscriptionType string `json:"subscriptionType"`
			RateLimitTier    string `json:"rateLimitTier"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.ClaudeAiOauth == nil {
		return "", "", ""
	}
	return strings.TrimSpace(doc.ClaudeAiOauth.AccessToken),
		strings.TrimSpace(doc.ClaudeAiOauth.SubscriptionType),
		strings.TrimSpace(doc.ClaudeAiOauth.RateLimitTier)
}

var usageEndpointBase = "https://api.anthropic.com"

// FetchUsage queries Anthropic's OAuth usage endpoint strictly via PeaceClaude's static-IP
// upstream dialer (never direct), falling back to Desktop local plan-usage-history.json if needed.
func FetchUsage(ctx context.Context, cfg *config.Config, prof *Profile, gateOpen bool, dialer *upstream.Dialer) UsageSnapshot {
	now := time.Now().UTC()
	snap := UsageSnapshot{
		Profile:   prof.Name,
		FetchedAt: now,
		Source:    "none",
	}

	token, subType := LookupOAuthAccessToken(prof.CLIConfigDir)
	snap.SubscriptionType = subType

	if token != "" && gateOpen && dialer != nil {
		apiSnap, err := fetchOAuthUsageHTTP(ctx, dialer, token, prof.Name, subType, now)
		if err == nil {
			return apiSnap
		}
		snap.Error = err.Error()
	} else if token != "" && !gateOpen {
		snap.Status = "gate_locked"
		snap.Hint = "出口门禁当前未放行，已禁止直连查询实时用量"
	}

	// Fallback: inspect Claude Desktop's local usage history if present
	if histSnap, ok := buildUsageFromDesktopHistory(prof.Name, prof.DesktopDir, now); ok {
		if snap.Error != "" {
			histSnap.Error = snap.Error
		}
		return histSnap
	}

	if snap.Status == "" {
		if token == "" && strings.EqualFold(strings.TrimSpace(prof.Account.PlanLabel), "Free") {
			snap.Status = "free_no_quota"
			snap.Source = "free_tier"
			snap.Hint = "免费账号（Free）不含 5-Hour / Weekly 付费配额接口，无法获取实时用量进度；升级 Pro / Max 后将自动采集"
		} else if token == "" {
			snap.Status = "no_token"
			snap.Hint = fmt.Sprintf("该档案尚未绑定 Claude Code OAuth 凭据；运行 `peaceclaude cli --profile %s` 登录一次即可自动采集实时 5h 与 Weekly 用量，也可先查看/标注账号套餐与注册时间", prof.Name)
		} else {
			snap.Status = "error"
			snap.Hint = "实时用量接口暂不可达，请检查静态 IP 代理连通性或点“刷新用量”重试"
		}
	}
	return snap
}

type rawOAuthWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

func fetchOAuthUsageHTTP(ctx context.Context, dialer *upstream.Dialer, token, profName, subType string, now time.Time) (UsageSnapshot, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client := dialer.NewHTTPClient(10 * time.Second)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(usageEndpointBase, "/")+"/api/oauth/usage", nil)
	if err != nil {
		return UsageSnapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "claude-code/2.1")

	resp, err := client.Do(req)
	if err != nil {
		return UsageSnapshot{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if err != nil {
		return UsageSnapshot{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return UsageSnapshot{}, fmt.Errorf("oauth usage HTTP %d", resp.StatusCode)
	}

	var raw struct {
		FiveHour       *rawOAuthWindow `json:"five_hour"`
		SevenDay       *rawOAuthWindow `json:"seven_day"`
		SevenDaySonnet *rawOAuthWindow `json:"seven_day_sonnet"`
		SevenDayOpus   *rawOAuthWindow `json:"seven_day_opus"`
		ExtraUsage     *struct {
			IsEnabled    bool     `json:"is_enabled"`
			MonthlyLimit *float64 `json:"monthly_limit"`
			UsedCredits  *float64 `json:"used_credits"`
			Utilization  *float64 `json:"utilization"`
			Currency     string   `json:"currency"`
		} `json:"extra_usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return UsageSnapshot{}, fmt.Errorf("parse oauth usage: %w", err)
	}

	snap := UsageSnapshot{
		Profile:          profName,
		Status:           "ok",
		Source:           "oauth_api",
		SubscriptionType: subType,
		FetchedAt:        now,
		FiveHour:         toUsageWindow("five_hour", "5-Hour Limit", raw.FiveHour, now),
		Weekly:           toUsageWindow("seven_day", "Weekly · All Models", raw.SevenDay, now),
		WeeklySonnet:     toUsageWindow("seven_day_sonnet", "Weekly · Sonnet", raw.SevenDaySonnet, now),
		WeeklyOpus:       toUsageWindow("seven_day_opus", "Weekly · Opus", raw.SevenDayOpus, now),
	}
	if raw.ExtraUsage != nil {
		eu := &ExtraUsageInfo{
			Enabled:  raw.ExtraUsage.IsEnabled,
			Currency: raw.ExtraUsage.Currency,
		}
		if raw.ExtraUsage.MonthlyLimit != nil {
			eu.MonthlyLimit = *raw.ExtraUsage.MonthlyLimit
		}
		if raw.ExtraUsage.UsedCredits != nil {
			eu.UsedCredits = *raw.ExtraUsage.UsedCredits
		}
		if raw.ExtraUsage.Utilization != nil {
			eu.Utilization = *raw.ExtraUsage.Utilization
		}
		snap.ExtraUsage = eu
	}
	return snap, nil
}

func toUsageWindow(key, label string, w *rawOAuthWindow, now time.Time) *UsageWindow {
	if w == nil || w.Utilization == nil {
		return nil
	}
	out := &UsageWindow{
		Key:         key,
		Label:       label,
		Utilization: *w.Utilization,
	}
	if w.ResetsAt != nil && strings.TrimSpace(*w.ResetsAt) != "" {
		if t, err := parseFlexibleDate(*w.ResetsAt); err == nil {
			out.ResetsAt = &t
			out.ResetInText = FormatResetCountdown(&t, now)
		}
	}
	return out
}

type desktopUsageSample struct {
	T   int64              `json:"t"`
	Org string             `json:"org"`
	U   map[string]float64 `json:"u"`
}

func readLatestDesktopUsageSample(desktopDir string) *desktopUsageSample {
	raw, err := os.ReadFile(filepath.Join(desktopDir, "plan-usage-history.json"))
	if err != nil || len(raw) == 0 {
		return nil
	}
	var hist struct {
		Version int                  `json:"version"`
		Samples []desktopUsageSample `json:"samples"`
	}
	if err := json.Unmarshal(raw, &hist); err != nil || len(hist.Samples) == 0 {
		return nil
	}
	for i := len(hist.Samples) - 1; i >= 0; i-- {
		s := hist.Samples[i]
		if len(s.U) > 0 || s.Org != "" {
			return &s
		}
	}
	return nil
}

func buildUsageFromDesktopHistory(profName, desktopDir string, now time.Time) (UsageSnapshot, bool) {
	raw, err := os.ReadFile(filepath.Join(desktopDir, "plan-usage-history.json"))
	if err != nil || len(raw) == 0 {
		return UsageSnapshot{}, false
	}
	var hist struct {
		Samples []desktopUsageSample `json:"samples"`
	}
	if err := json.Unmarshal(raw, &hist); err != nil {
		return UsageSnapshot{}, false
	}
	for i := len(hist.Samples) - 1; i >= 0; i-- {
		s := hist.Samples[i]
		if len(s.U) == 0 {
			continue
		}
		ts := now
		if s.T > 0 {
			ts = time.UnixMilli(s.T).UTC()
		}
		snap := UsageSnapshot{
			Profile:   profName,
			Status:    "history",
			Source:    "desktop_history",
			FetchedAt: ts,
			Hint:      "来自 Claude Desktop 最近一次本地记录样本；完成一次 `peaceclaude cli` 登录可获取带重置倒计时的实时用量",
		}
		if v, ok := s.U["fh"]; ok {
			snap.FiveHour = &UsageWindow{Key: "five_hour", Label: "5-Hour Limit", Utilization: v}
		}
		if v, ok := s.U["sd"]; ok {
			snap.Weekly = &UsageWindow{Key: "seven_day", Label: "Weekly · All Models", Utilization: v}
		}
		if v, ok := s.U["sn"]; ok {
			snap.WeeklySonnet = &UsageWindow{Key: "seven_day_sonnet", Label: "Weekly · Sonnet", Utilization: v}
		}
		if v, ok := s.U["so"]; ok {
			snap.WeeklyOpus = &UsageWindow{Key: "seven_day_opus", Label: "Weekly · Opus", Utilization: v}
		}
		if snap.FiveHour != nil || snap.Weekly != nil {
			return snap, true
		}
	}
	return UsageSnapshot{}, false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// readDesktopAccountMetadata reads ONLY non-secret account/org UUIDs from Electron desktop-data.
func readDesktopAccountMetadata(desktopDir string) (accountUUID, orgUUID string) {
	if strings.TrimSpace(desktopDir) == "" {
		return "", ""
	}
	if raw, err := os.ReadFile(filepath.Join(desktopDir, "config.json")); err == nil && len(raw) > 0 {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			if v, _ := m["lastKnownAccountUuid"].(string); strings.TrimSpace(v) != "" {
				accountUUID = strings.TrimSpace(v)
			}
			if ssh, ok := m["sshTransportFlagLastAnswer"].(map[string]any); ok {
				if accountUUID == "" {
					if v, _ := ssh["accountUuid"].(string); strings.TrimSpace(v) != "" {
						accountUUID = strings.TrimSpace(v)
					}
				}
				if v, _ := ssh["orgUuid"].(string); strings.TrimSpace(v) != "" {
					orgUUID = strings.TrimSpace(v)
				}
			}
			if orgUUID == "" {
				for k := range m {
					if strings.HasPrefix(k, "dxt:allowlistEnabled:") {
						cand := strings.TrimSpace(strings.TrimPrefix(k, "dxt:allowlistEnabled:"))
						if cand != "" {
							orgUUID = cand
							break
						}
					}
				}
			}
		}
	}
	if accountUUID == "" {
		if raw, err := os.ReadFile(filepath.Join(desktopDir, "cowork-enabled-cli-ops.json")); err == nil && len(raw) > 0 {
			var doc struct {
				OwnerAccountID string `json:"ownerAccountId"`
			}
			if json.Unmarshal(raw, &doc) == nil {
				accountUUID = strings.TrimSpace(doc.OwnerAccountID)
			}
		}
	}
	return accountUUID, orgUUID
}

func hasDesktopPaidUsageSample(desktopDir string) (hasFile bool, hasQuotaValues bool) {
	raw, err := os.ReadFile(filepath.Join(desktopDir, "plan-usage-history.json"))
	if err != nil || len(raw) == 0 {
		return false, false
	}
	var hist struct {
		Samples []desktopUsageSample `json:"samples"`
	}
	if json.Unmarshal(raw, &hist) != nil {
		return true, false
	}
	for _, s := range hist.Samples {
		if len(s.U) > 0 {
			return true, true
		}
	}
	return len(hist.Samples) > 0, false
}
