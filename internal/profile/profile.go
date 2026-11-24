package profile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"peaceclaude/internal/config"
)

var validNameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,47}$`)

type Fingerprint struct {
	DeviceUUID string    `json:"device_uuid"`
	MachineID  string    `json:"machine_id"`
	StableID   string    `json:"stable_id"`
	Hostname   string    `json:"hostname"`
	CreatedAt  time.Time `json:"created_at"`
}

type AccountIdentity struct {
	AccountUUID          string     `json:"account_uuid,omitempty"`
	Email                string     `json:"email,omitempty"`
	DisplayName          string     `json:"display_name,omitempty"`
	OrgUUID              string     `json:"org_uuid,omitempty"`
	OrgName              string     `json:"org_name,omitempty"`
	OrgType              string     `json:"org_type,omitempty"`
	PlanLabel            string     `json:"plan_label"`
	RateLimitTier        string     `json:"rate_limit_tier,omitempty"`
	BillingType          string     `json:"billing_type,omitempty"`
	AccountCreatedAt     *time.Time `json:"account_created_at,omitempty"`
	SubscriptionAt       *time.Time `json:"subscription_created_at,omitempty"`
	AccountAgeText       string     `json:"account_age_text,omitempty"`
	SubscriptionAgeText  string     `json:"subscription_age_text,omitempty"`
	HasExtraUsageEnabled bool       `json:"has_extra_usage_enabled"`
	Source               string     `json:"source"` // claude_json | manual | none
}

type MetaPatch struct {
	DisplayName        *string `json:"display_name"`
	AvatarID           *string `json:"avatar_id"`
	Note               *string `json:"note"`
	ManualPlan         *string `json:"manual_plan"`
	ManualRegisteredAt *string `json:"manual_registered_at"`
	ManualLocked       *bool   `json:"manual_locked"`
	Timezone           *string `json:"timezone"`
	Language           *string `json:"language"`
}

type Profile struct {
	Name               string          `json:"name"`
	DisplayName        string          `json:"display_name,omitempty"`
	AvatarID           string          `json:"avatar_id,omitempty"`
	Note               string          `json:"note,omitempty"`
	ExpectedIP         string          `json:"expected_ip,omitempty"`
	Timezone           string          `json:"timezone,omitempty"`
	Language           string          `json:"language,omitempty"`
	ManualPlan         string          `json:"manual_plan,omitempty"`
	ManualRegisteredAt string          `json:"manual_registered_at,omitempty"`
	ManualLocked       bool            `json:"manual_locked,omitempty"`
	Fingerprint        Fingerprint     `json:"fingerprint"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	LastUsedAt         *time.Time      `json:"last_used_at,omitempty"`
	Dir                string          `json:"dir"`
	CLIConfigDir       string          `json:"cli_config_dir"`
	DesktopDir         string          `json:"desktop_data_dir"`
	BrowserDir         string          `json:"browser_profile_dir"`
	Active             bool            `json:"active"`
	HasCLICreds        bool            `json:"has_cli_creds"`
	HasDesktopDat      bool            `json:"has_desktop_data"`
	HasDesktopAuth     bool            `json:"has_desktop_auth"`
	Account            AccountIdentity `json:"account"`
}

type RulesStatus struct {
	Enabled   bool              `json:"enabled"`
	SharedDir string            `json:"shared_dir"`
	Items     []string          `json:"items"`
	Existing  map[string]bool   `json:"existing"`
	Profiles  map[string]string `json:"profiles"` // profile -> synced | detached | partial
}

type CleanReport struct {
	Profile    string   `json:"profile"`
	DryRun     bool     `json:"dry_run"`
	BackupDir  string   `json:"backup_dir"`
	Removed    []string `json:"removed"`
	Scrubbed   []string `json:"scrubbed"`
	Preserved  []string `json:"preserved"`
	NewDevice  string   `json:"new_device_uuid"`
	Notes      []string `json:"notes,omitempty"`
}

var PresetAvatarIDs = []string{
	"tiger", "lion", "eagle", "fox", "panda", "shiba", "owl", "koala", "wolf",
}

func IsPresetAvatarID(id string) bool {
	for _, v := range PresetAvatarIDs {
		if v == id {
			return true
		}
	}
	return false
}

func ValidateName(name string) error {
	if !validNameRe.MatchString(name) {
		return fmt.Errorf("invalid profile name %q (use letters, digits, ., _, -, max 48 chars)", name)
	}
	return nil
}

func GenerateFingerprint() Fingerprint {
	uuidBytes := randBytes(16)
	uuidBytes[6] = (uuidBytes[6] & 0x0f) | 0x40
	uuidBytes[8] = (uuidBytes[8] & 0x3f) | 0x80
	uuidStr := strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x",
		uuidBytes[0:4], uuidBytes[4:6], uuidBytes[6:8], uuidBytes[8:10], uuidBytes[10:16]))

	machineID := hex.EncodeToString(randBytes(16))
	sum := sha256.Sum256(randBytes(32))
	stableID := hex.EncodeToString(sum[:16])
	hostSuffix := hex.EncodeToString(randBytes(3))

	return Fingerprint{
		DeviceUUID: uuidStr,
		MachineID:  machineID,
		StableID:   stableID,
		Hostname:   fmt.Sprintf("MacBook-Pro-%s.local", hostSuffix),
		CreatedAt:  time.Now().UTC(),
	}
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func Dir(name string) string {
	return filepath.Join(config.ProfilesDir(), name)
}

func MetaPath(name string) string {
	return filepath.Join(Dir(name), "profile.json")
}

// Ensure creates a profile if missing and ensures isolated CLI/Desktop/Browser directories exist.
func Ensure(cfg *config.Config, name, note string) (*Profile, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	if err := config.EnsureDirs(); err != nil {
		return nil, err
	}
	dir := Dir(name)
	cliDir := filepath.Join(dir, "claude-config")
	desktopDir := filepath.Join(dir, "desktop-data")
	browserDir := filepath.Join(dir, "browser-profile")
	for _, d := range []string{dir, cliDir, desktopDir, browserDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("create profile dir %s: %w", d, err)
		}
		_ = os.Chmod(d, 0o700)
	}

	var p Profile
	metaPath := MetaPath(name)
	if raw, err := os.ReadFile(metaPath); err == nil {
		_ = json.Unmarshal(raw, &p)
	}
	now := time.Now().UTC()
	if p.Name == "" {
		p.Name = name
		p.Note = note
		p.CreatedAt = now
		p.Fingerprint = GenerateFingerprint()
	} else if note != "" {
		p.Note = note
	}
	if p.Fingerprint.DeviceUUID == "" {
		p.Fingerprint = GenerateFingerprint()
	}
	p.UpdatedAt = now
	if err := writeProfileMeta(&p); err != nil {
		return nil, err
	}
	if err := EnsureCLISettings(cfg, cliDir); err != nil {
		return nil, err
	}
	if cfg != nil && cfg.Rules.SharedEnabled {
		_ = syncProfileRules(cfg, name, true)
	}
	return Load(cfg, name)
}

// EnsureCLISettings writes/updates the isolated CLAUDE_CONFIG_DIR/settings.json
// to pin proxy settings to PeaceClaude's local fail-closed gateway and never leak host relays.
func EnsureCLISettings(cfg *config.Config, cliDir string) error {
	settingsPath := filepath.Join(cliDir, "settings.json")
	data := map[string]any{}
	if raw, err := os.ReadFile(settingsPath); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	envMap, ok := data["env"].(map[string]any)
	if !ok || envMap == nil {
		envMap = map[string]any{}
	}
	if cfg != nil {
		envMap["HTTP_PROXY"] = cfg.HTTPProxyURL()
		envMap["HTTPS_PROXY"] = cfg.HTTPProxyURL()
		envMap["ALL_PROXY"] = cfg.SOCKS5ProxyURL()
		envMap["NO_PROXY"] = BuildNoProxy(cfg)
		if cfg.Shield.TelemetryShield {
			envMap["DISABLE_TELEMETRY"] = "1"
			envMap["DISABLE_ERROR_REPORTING"] = "1"
		}
	}
	data["env"] = envMap
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(b, '\n'), 0o600)
}

func BuildNoProxy(cfg *config.Config) string {
	items := []string{"127.0.0.1", "localhost", "::1", ".local"}
	if cfg != nil && cfg.Shield.AllowCorporateDirect {
		items = append(items, cfg.Shield.CorporateDomains...)
	}
	return strings.Join(items, ",")
}

func writeProfileMeta(p *Profile) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(MetaPath(p.Name), append(raw, '\n'), 0o600)
}

func Load(cfg *config.Config, name string) (*Profile, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(MetaPath(name))
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	p.Dir = Dir(name)
	p.CLIConfigDir = filepath.Join(p.Dir, "claude-config")
	p.DesktopDir = filepath.Join(p.Dir, "desktop-data")
	p.BrowserDir = filepath.Join(p.Dir, "browser-profile")
	if cfg != nil {
		p.Active = (cfg.ActiveProfile == name)
	}
	p.HasCLICreds = fileExists(filepath.Join(p.CLIConfigDir, ".credentials.json")) ||
		hasClaudeJSONAccount(filepath.Join(p.CLIConfigDir, ".claude.json"))
	if !p.HasCLICreds {
		if tok, _ := LookupOAuthAccessToken(p.CLIConfigDir); tok != "" {
			p.HasCLICreds = true
		}
	}
	p.HasDesktopDat = fileExists(filepath.Join(p.DesktopDir, "Cookies")) ||
		fileExists(filepath.Join(p.DesktopDir, "Local State"))
	p.HasDesktopAuth = hasDesktopLoggedIn(p.DesktopDir)
	p.Account = readAccountIdentity(&p)
	if strings.TrimSpace(p.DisplayName) == "" {
		switch {
		case p.Account.DisplayName != "":
			p.DisplayName = p.Account.DisplayName
		case p.Account.Email != "":
			p.DisplayName = p.Account.Email
		default:
			p.DisplayName = p.Name
		}
	}
	return &p, nil
}

// UpdateMeta updates editable user metadata (display name, note, manual plan/registration date, timezone, language).
func UpdateMeta(cfg *config.Config, name string, patch MetaPatch) (*Profile, error) {
	if _, err := Ensure(cfg, name, ""); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(MetaPath(name))
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if patch.DisplayName != nil {
		p.DisplayName = strings.TrimSpace(*patch.DisplayName)
	}
	if patch.AvatarID != nil {
		av := strings.ToLower(strings.TrimSpace(*patch.AvatarID))
		if av != "" && !IsPresetAvatarID(av) {
			return nil, fmt.Errorf("无效的预设头像 %q", av)
		}
		p.AvatarID = av
	}
	if patch.Note != nil {
		p.Note = strings.TrimSpace(*patch.Note)
	}
	if patch.ManualPlan != nil {
		p.ManualPlan = strings.TrimSpace(*patch.ManualPlan)
	}
	if patch.ManualRegisteredAt != nil {
		val := strings.TrimSpace(*patch.ManualRegisteredAt)
		if val != "" {
			if _, parseErr := parseFlexibleDate(val); parseErr != nil {
				return nil, fmt.Errorf("注册日期格式错误 %q，请使用 YYYY-MM-DD", val)
			}
		}
		p.ManualRegisteredAt = val
	}
	if patch.ManualLocked != nil {
		p.ManualLocked = *patch.ManualLocked
	}
	if patch.Timezone != nil {
		p.Timezone = strings.TrimSpace(*patch.Timezone)
	}
	if patch.Language != nil {
		p.Language = strings.TrimSpace(*patch.Language)
	}
	p.UpdatedAt = time.Now().UTC()
	if err := writeProfileMeta(&p); err != nil {
		return nil, err
	}
	return Load(cfg, name)
}

// TouchLastUsed records the most recent launch timestamp on a profile.
func TouchLastUsed(name string) {
	if ValidateName(name) != nil {
		return
	}
	raw, err := os.ReadFile(MetaPath(name))
	if err != nil {
		return
	}
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}
	now := time.Now().UTC()
	p.LastUsedAt = &now
	_ = writeProfileMeta(&p)
}

func List(cfg *config.Config) ([]Profile, error) {
	if err := config.EnsureDirs(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(config.ProfilesDir())
	if err != nil {
		return nil, err
	}
	var out []Profile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if p, err := Load(cfg, e.Name()); err == nil {
			out = append(out, *p)
		}
	}
	// Seed a default profile only when no profiles exist yet, so deleting
	// "default" when other profiles exist will not resurrect it.
	if len(out) == 0 {
		if p, err := Ensure(cfg, "default", "Default isolated profile"); err == nil && p != nil {
			out = append(out, *p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func Delete(cfg *config.Config, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	all, err := List(cfg)
	if err != nil {
		return err
	}
	var remaining []Profile
	found := false
	for _, p := range all {
		if p.Name == name {
			found = true
			continue
		}
		remaining = append(remaining, p)
	}
	if !found {
		return fmt.Errorf("profile %q does not exist", name)
	}
	if len(remaining) == 0 {
		return errors.New("cannot delete the last remaining profile; create another profile first or use peaceclaude clean")
	}
	sort.Slice(remaining, func(i, j int) bool {
		return remaining[i].Name < remaining[j].Name
	})
	if cfg != nil && cfg.ActiveProfile == name {
		fallback := remaining[0].Name
		for _, p := range remaining {
			if p.Name == "default" {
				fallback = "default"
				break
			}
		}
		cfg.ActiveProfile = fallback
		if err := config.Save(cfg); err != nil {
			return err
		}
	}
	return os.RemoveAll(Dir(name))
}

func SeedSharedRulesFromHost() ([]string, error) {
	if err := config.EnsureDirs(); err != nil {
		return nil, err
	}
	shared := config.SharedRulesDir()
	var linked []string

	// In sandboxed test runs (PEACECLAUDE_HOME set without explicit host dir), avoid touching host ~/.claude.
	hostClaude := strings.TrimSpace(os.Getenv("PEACECLAUDE_HOST_CLAUDE_DIR"))
	if hostClaude == "" && strings.TrimSpace(os.Getenv("PEACECLAUDE_HOME")) == "" {
		if home, err := os.UserHomeDir(); err == nil {
			hostClaude = filepath.Join(home, ".claude")
		}
	}

	if hostClaude != "" {
		for _, item := range []string{"CLAUDE.md", "skills", "agents", "hooks"} {
			dst := filepath.Join(shared, item)
			if _, err := os.Lstat(dst); err == nil {
				continue
			}
			src := filepath.Join(hostClaude, item)
			fi, err := os.Stat(src)
			if err != nil {
				continue
			}
			if fi.IsDir() {
				// Symlink directories (skills/agents/hooks) directly so updates to ~/.agents/skills stay live & instant.
				if err := os.Symlink(src, dst); err == nil {
					linked = append(linked, item)
				}
			} else {
				if err := copyFile(src, dst); err == nil {
					linked = append(linked, item)
				}
			}
		}
	}
	// Ensure baseline CLAUDE.md exists in shared-rules if nothing was imported.
	readmePath := filepath.Join(shared, "CLAUDE.md")
	if _, err := os.Stat(readmePath); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(readmePath, []byte("# Shared Global Claude Rules (managed by peaceclaude rules)\n"), 0o600)
	}
	return linked, nil
}

func SyncAllRules(cfg *config.Config, enable bool) (RulesStatus, error) {
	if err := config.EnsureDirs(); err != nil {
		return RulesStatus{}, err
	}
	if enable {
		_, _ = SeedSharedRulesFromHost()
	}
	cfg.Rules.SharedEnabled = enable
	if err := config.Save(cfg); err != nil {
		return RulesStatus{}, err
	}
	profiles, err := List(cfg)
	if err != nil {
		return RulesStatus{}, err
	}
	for _, p := range profiles {
		if err := syncProfileRules(cfg, p.Name, enable); err != nil {
			return RulesStatus{}, err
		}
	}
	return GetRulesStatus(cfg), nil
}

func syncProfileRules(cfg *config.Config, profileName string, enable bool) error {
	shared := config.SharedRulesDir()
	cliDir := filepath.Join(Dir(profileName), "claude-config")
	_ = os.MkdirAll(cliDir, 0o700)

	for _, item := range cfg.Rules.SyncItems {
		src := filepath.Join(shared, item)
		dst := filepath.Join(cliDir, item)
		if enable {
			if _, err := os.Stat(src); err != nil {
				continue
			}
			// If dst is already a symlink to src, keep it.
			if target, err := os.Readlink(dst); err == nil && target == src {
				continue
			}
			// If dst exists as real file/dir and src doesn't conflict, back it up before symlinking.
			if _, err := os.Lstat(dst); err == nil {
				_ = os.RemoveAll(dst)
			}
			if err := os.Symlink(src, dst); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
			}
		} else {
			// Detach: materialize symlink into independent local copy so disabling rules never loses user content.
			target, err := os.Readlink(dst)
			if err != nil {
				continue
			}
			_ = os.Remove(dst)
			if fi, statErr := os.Stat(target); statErr == nil {
				if fi.IsDir() {
					_ = copyDir(target, dst)
				} else {
					_ = copyFile(target, dst)
				}
			}
		}
	}
	return nil
}

func GetRulesStatus(cfg *config.Config) RulesStatus {
	shared := config.SharedRulesDir()
	st := RulesStatus{
		Enabled:   cfg.Rules.SharedEnabled,
		SharedDir: shared,
		Items:     append([]string(nil), cfg.Rules.SyncItems...),
		Existing:  make(map[string]bool),
		Profiles:  make(map[string]string),
	}
	for _, item := range cfg.Rules.SyncItems {
		_, err := os.Stat(filepath.Join(shared, item))
		st.Existing[item] = (err == nil)
	}
	profiles, _ := List(cfg)
	for _, p := range profiles {
		linked := 0
		total := 0
		for _, item := range cfg.Rules.SyncItems {
			if !st.Existing[item] {
				continue
			}
			total++
			dst := filepath.Join(p.CLIConfigDir, item)
			if target, err := os.Readlink(dst); err == nil && target == filepath.Join(shared, item) {
				linked++
			}
		}
		switch {
		case total == 0 || linked == 0:
			st.Profiles[p.Name] = "detached"
		case linked == total:
			st.Profiles[p.Name] = "synced"
		default:
			st.Profiles[p.Name] = "partial"
		}
	}
	return st
}

var (
	cliSessionDirs = []string{
		"sessions", "projects", "history", "diagnostics", "cache", "file-history",
		"tasks", "paste-cache", "shell-snapshots", "telemetry", "todos", "debug",
		"logs", ".cc-writes", "session-env",
	}
	cliSessionFiles = []string{
		"history.jsonl", "stats-cache.json", ".last-update-result.json",
		"daemon-auth-status.json", "daemon-auth-cooldown", ".credentials.json",
		"credentials.json", "auth.json",
	}
	identityJSONKeys = []string{
		"oauthAccount", "userID", "machineID", "claudeAiOauth", "organizationUuid",
		"accountUuid", "claudeCodeFirstTokenDate", "hasCompletedOnboarding",
		"subscriptionNoticeCount", "cachedGrowthBookFeatures", "cachedDynamicConfigs",
		"cachedStatsigGates", "isQualifiedForDataSharing", "profileFetchedAt",
	}
	desktopWipeNames = []string{
		"ant-did", "ant-device-registry.json", "buddy-tokens.json",
		"Cookies", "Cookies-journal", "Session Storage", "Local Storage",
		"IndexedDB", "Cache", "Code Cache", "GPUCache", "Shared Dictionary",
		"Network Persistent State", "Preferences", "TransportSecurity",
		"Trust Tokens", "Trust Tokens-journal", "shared_proto_db", "blob_storage",
		"File System", "Service Worker", "DIPS", "DIPS-wal", "InterestGroups",
		"WebStorage", "CacheStorage", "Local State", "Partitions",
	}
)

// CleanProfile wipes identity/session fingerprints inside an isolated PeaceClaude profile,
// automatically archiving a timestamped backup and preserving CLAUDE.md/skills/agents/hooks/settings.json.
func CleanProfile(cfg *config.Config, name string, dryRun bool) (*CleanReport, error) {
	p, err := Load(cfg, name)
	if err != nil {
		return nil, fmt.Errorf("load profile %q: %w", name, err)
	}
	ts := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(config.BackupsDir(), fmt.Sprintf("%s-%s", name, ts))
	rep := &CleanReport{
		Profile:   name,
		DryRun:    dryRun,
		BackupDir: backupDir,
		Preserved: []string{"settings.json", "CLAUDE.md", "skills", "agents", "hooks"},
	}

	if !dryRun {
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			return nil, err
		}
	}

	moveTarget := func(src, rel string) {
		if _, err := os.Lstat(src); err != nil {
			return
		}
		rep.Removed = append(rep.Removed, rel)
		if dryRun {
			return
		}
		dst := filepath.Join(backupDir, rel)
		_ = os.MkdirAll(filepath.Dir(dst), 0o700)
		if err := os.Rename(src, dst); err != nil {
			fi, statErr := os.Stat(src)
			if statErr == nil && fi.IsDir() {
				_ = copyDir(src, dst)
			} else {
				_ = copyFile(src, dst)
			}
			_ = os.RemoveAll(src)
		}
	}

	// 1. CLI session trees & credentials
	for _, d := range cliSessionDirs {
		moveTarget(filepath.Join(p.CLIConfigDir, d), filepath.Join("claude-config", d))
	}
	for _, f := range cliSessionFiles {
		moveTarget(filepath.Join(p.CLIConfigDir, f), filepath.Join("claude-config", f))
	}

	// 2. Scrub identity keys from .claude.json / claude.json inside profile
	for _, cand := range []string{
		filepath.Join(p.CLIConfigDir, ".claude.json"),
		filepath.Join(p.CLIConfigDir, "claude.json"),
	} {
		if scrubJSONIdentity(cand, filepath.Join(backupDir, "claude-json", filepath.Base(cand)), dryRun) {
			rep.Scrubbed = append(rep.Scrubbed, cand)
		}
	}

	// 3. Desktop isolated data wipe
	for _, item := range desktopWipeNames {
		moveTarget(filepath.Join(p.DesktopDir, item), filepath.Join("desktop-data", item))
	}

	// 4. Isolated login browser profile wipe
	if entries, err := os.ReadDir(p.BrowserDir); err == nil && len(entries) > 0 {
		moveTarget(p.BrowserDir, "browser-profile")
		if !dryRun {
			_ = os.MkdirAll(p.BrowserDir, 0o700)
		}
	}

	// 5. Regenerate profile fingerprint metadata
	newFP := GenerateFingerprint()
	rep.NewDevice = newFP.DeviceUUID
	if !dryRun {
		p.Fingerprint = newFP
		p.UpdatedAt = time.Now().UTC()
		_ = writeProfileMeta(p)
		_ = EnsureCLISettings(cfg, p.CLIConfigDir)
	}

	rep.Notes = append(rep.Notes,
		"本地隔离指纹已清理并备份；宿主机原生 ~/.claude 未被触碰。",
		"注意：本地清理不能消除服务端 IP 或支付方式关联，请继续配合固定 Webshare 静态 IP。",
	)
	return rep, nil
}

func scrubJSONIdentity(path, backupDst string, dryRun bool) bool {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return false
	}
	data := map[string]any{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return false
	}
	found := false
	for _, k := range identityJSONKeys {
		if _, ok := data[k]; ok {
			delete(data, k)
			found = true
		}
	}
	if !found {
		return false
	}
	if dryRun {
		return true
	}
	_ = os.MkdirAll(filepath.Dir(backupDst), 0o700)
	_ = os.WriteFile(backupDst, raw, 0o600)
	out, err := json.MarshalIndent(data, "", "  ")
	if err == nil {
		_ = os.WriteFile(path, append(out, '\n'), 0o600)
	}
	return true
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func hasClaudeJSONAccount(p string) bool {
	raw, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	return m["oauthAccount"] != nil || m["claudeAiOauth"] != nil
}

// hasDesktopLoggedIn distinguishes an initialized-but-signed-out Electron desktop-data directory
// from a profile where Claude Desktop actually completed OAuth authentication.
func hasDesktopLoggedIn(desktopDir string) bool {
	raw, err := os.ReadFile(filepath.Join(desktopDir, "config.json"))
	if err != nil || len(raw) == 0 {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	if v, _ := m["oauth:tokenCache"].(string); v != "" {
		return true
	}
	if v, _ := m["oauth:tokenCacheV2"].(string); v != "" {
		return true
	}
	if v, _ := m["lastKnownAccountUuid"].(string); strings.TrimSpace(v) != "" {
		return true
	}
	if b, _ := m["windowSizeWasSignedIn"].(bool); b {
		return true
	}
	if side, ok := m["bootSideSessions"].(map[string]any); ok {
		if uuid, _ := side["accountUuid"].(string); strings.TrimSpace(uuid) != "" {
			return true
		}
	}
	return false
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return nil
			}
			_ = os.MkdirAll(filepath.Dir(target), 0o700)
			_ = os.Remove(target)
			return os.Symlink(linkTarget, target)
		}
		return copyFile(path, target)
	})
}
