package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const CurrentVersion = 1

type ListenConfig struct {
	BindAddr             string   `json:"bind_addr"`
	HTTPPort             int      `json:"http_port"`
	SOCKS5Port           int      `json:"socks5_port"`
	ControlPort          int      `json:"control_port"`
	AutoEnableBrowserPAC *bool    `json:"auto_enable_browser_pac,omitempty"`
	BrowserPACPaused     bool     `json:"browser_pac_paused,omitempty"`
	VMProxyEnabled       bool     `json:"vm_proxy_enabled"`
	VMProxyBind          string   `json:"vm_proxy_bind,omitempty"`
	VMProxySubnets       []string `json:"vm_proxy_subnets,omitempty"`
}

func (c *Config) ShouldAutoEnableBrowserPAC() bool {
	if c == nil || c.Listen.BrowserPACPaused {
		return false
	}
	if c.Listen.AutoEnableBrowserPAC != nil {
		return *c.Listen.AutoEnableBrowserPAC
	}
	return true
}

type UpstreamConfig struct {
	// Mode: "local-proxy" | "direct-socks5" | "direct-http"
	Mode           string `json:"mode"`
	Address        string `json:"address"`
	Username       string `json:"username,omitempty"`
	Password       string `json:"password,omitempty"`
	BackupMode     string `json:"backup_mode,omitempty"`
	BackupAddress  string `json:"backup_address,omitempty"`
	BackupUsername string `json:"backup_username,omitempty"`
	BackupPassword string `json:"backup_password,omitempty"`
	ClashSocket    string `json:"clash_socket,omitempty"`
	Label          string `json:"label,omitempty"`
}

type EgressConfig struct {
	ExpectedIP            string   `json:"expected_ip,omitempty"`
	AllowedCountries      []string `json:"allowed_countries,omitempty"`
	ProbeIntervalSec      int      `json:"probe_interval_sec"`
	BadIntervalSec        int      `json:"bad_interval_sec"`
	ProbeTimeoutSec       int      `json:"probe_timeout_sec"`
	FailClosed            bool     `json:"fail_closed"`
	SkipProbeDuringSSE    bool     `json:"skip_probe_during_sse"`
	StrictDomainIsolation bool     `json:"strict_domain_isolation"`
}

type IdentityConfig struct {
	SyncTimezone          bool   `json:"sync_timezone"`
	Timezone              string `json:"timezone,omitempty"`
	Language              string `json:"language"`
	Locale                string `json:"locale"`
	StripHostAnthropicEnv bool   `json:"strip_host_anthropic_env"`
	DisableDesktopQUIC    bool   `json:"disable_desktop_quic"`
	BlockDesktopLocalDNS  bool   `json:"block_desktop_local_dns"`
	AutoPeaceDesktop       bool   `json:"auto_peace_desktop"`
	SingleActiveDesktop   bool   `json:"single_active_desktop"`
}

type ShieldConfig struct {
	TelemetryShield      bool     `json:"telemetry_shield"`
	BlockPrivateTargets  bool     `json:"block_private_targets"`
	AllowCorporateDirect bool     `json:"allow_corporate_direct"`
	ProxyAuthDomains     bool     `json:"proxy_auth_domains"`
	ProtectedBrowsers    []string `json:"protected_browsers,omitempty"`
	CorporateDomains     []string `json:"corporate_domains,omitempty"`
	ExtraBlockedDomains  []string `json:"extra_blocked_domains,omitempty"`
	ExtraClaudeDomains   []string `json:"extra_claude_domains,omitempty"`
}

type RulesConfig struct {
	SharedEnabled bool     `json:"shared_enabled"`
	SyncItems     []string `json:"sync_items,omitempty"`
}

type CaptureConfig struct {
	Enabled    bool `json:"enabled"`
	MaxEntries int  `json:"max_entries"`
}

type BinariesConfig struct {
	ClaudeCLIPath    string `json:"claude_cli_path,omitempty"`
	ClaudeDesktopApp string `json:"claude_desktop_app"`
	LoginBrowserApp  string `json:"login_browser_app,omitempty"`
}

type ThemeOverride struct {
	Accent string `json:"accent,omitempty"`
	Bg     string `json:"bg,omitempty"`
	Text   string `json:"text,omitempty"`
}

type SavedTheme struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	ThemeMode   string        `json:"theme_mode,omitempty"`
	PresetLight string        `json:"preset_light,omitempty"`
	PresetDark  string        `json:"preset_dark,omitempty"`
	Light       ThemeOverride `json:"light"`
	Dark        ThemeOverride `json:"dark"`
	UIFont      string        `json:"ui_font,omitempty"`
	CodeFont    string        `json:"code_font,omitempty"`
}

type AppearanceConfig struct {
	ThemeMode    string         `json:"theme_mode"`
	PresetLight  string         `json:"preset_light"`
	PresetDark   string         `json:"preset_dark"`
	CustomLight  *ThemeOverride `json:"custom_light,omitempty"`
	CustomDark   *ThemeOverride `json:"custom_dark,omitempty"`
	SavedThemes  []SavedTheme   `json:"saved_themes,omitempty"`
	ActiveSaved  string         `json:"active_saved_theme,omitempty"`
	UIFont       string         `json:"ui_font,omitempty"`
	CodeFont     string         `json:"code_font,omitempty"`
	UIFontSize   int            `json:"ui_font_size,omitempty"`
	CodeFontSize int            `json:"code_font_size,omitempty"`
}

type PairedDevice struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OS         string    `json:"os"`
	TokenHash  string    `json:"-"`
	PairedAt   time.Time `json:"paired_at"`
	LastSeenAt time.Time `json:"last_seen_at,omitempty"`
}

type RemoteConfig struct {
	Enabled         bool           `json:"enabled"`
	RelayMode       string         `json:"relay_mode"` // "lan" | "custom" | "official"
	CustomRelayURL  string         `json:"custom_relay_url,omitempty"`
	RelayAdminToken string         `json:"relay_admin_token,omitempty"`
	MobilePort      int            `json:"mobile_port"`
	PairedDevices   []PairedDevice `json:"paired_devices,omitempty"`
}

type RedactedRemote struct {
	Enabled        bool           `json:"enabled"`
	RelayMode      string         `json:"relay_mode"`
	CustomRelayURL string         `json:"custom_relay_url,omitempty"`
	HasAdminToken  bool           `json:"has_admin_token"`
	MobilePort     int            `json:"mobile_port"`
	PairedDevices  []PairedDevice `json:"paired_devices,omitempty"`
}

type Config struct {
	Version       int              `json:"version"`
	ActiveProfile string           `json:"active_profile"`
	Listen        ListenConfig     `json:"listen"`
	Upstream      UpstreamConfig   `json:"upstream"`
	Egress        EgressConfig     `json:"egress"`
	Identity      IdentityConfig   `json:"identity"`
	Shield        ShieldConfig     `json:"shield"`
	Rules         RulesConfig      `json:"rules"`
	Capture       CaptureConfig    `json:"capture"`
	Binaries      BinariesConfig   `json:"binaries"`
	Appearance    AppearanceConfig `json:"appearance"`
	Remote        RemoteConfig     `json:"remote"`
}

// RedactedUpstream hides secrets while preserving diagnostic visibility.
type RedactedUpstream struct {
	Mode           string `json:"mode"`
	Address        string `json:"address"`
	HasCredentials bool   `json:"has_credentials"`
	BackupMode     string `json:"backup_mode,omitempty"`
	BackupAddress  string `json:"backup_address,omitempty"`
	HasBackupCreds bool   `json:"has_backup_credentials"`
	ClashSocket    string `json:"clash_socket,omitempty"`
	Label          string `json:"label,omitempty"`
}

type RedactedConfig struct {
	Version       int              `json:"version"`
	ActiveProfile string           `json:"active_profile"`
	Listen        ListenConfig     `json:"listen"`
	Upstream      RedactedUpstream `json:"upstream"`
	Egress        EgressConfig     `json:"egress"`
	Identity      IdentityConfig   `json:"identity"`
	Shield        ShieldConfig     `json:"shield"`
	Rules         RulesConfig      `json:"rules"`
	Capture       CaptureConfig    `json:"capture"`
	Binaries      BinariesConfig   `json:"binaries"`
	Appearance    AppearanceConfig `json:"appearance"`
	Remote        RedactedRemote   `json:"remote"`
}

func (c *Config) Redacted() RedactedConfig {
	return RedactedConfig{
		Version:       c.Version,
		ActiveProfile: c.ActiveProfile,
		Listen:        c.Listen,
		Upstream: RedactedUpstream{
			Mode:           c.Upstream.Mode,
			Address:        c.Upstream.Address,
			HasCredentials: c.Upstream.Username != "" || c.Upstream.Password != "",
			BackupMode:     c.Upstream.BackupMode,
			BackupAddress:  c.Upstream.BackupAddress,
			HasBackupCreds: c.Upstream.BackupUsername != "" || c.Upstream.BackupPassword != "",
			ClashSocket:    c.Upstream.ClashSocket,
			Label:          c.Upstream.Label,
		},
		Egress:     c.Egress,
		Identity:   c.Identity,
		Shield:     c.Shield,
		Rules:      c.Rules,
		Capture:    c.Capture,
		Binaries:   c.Binaries,
		Appearance: c.Appearance,
		Remote: RedactedRemote{
			Enabled:        c.Remote.Enabled,
			RelayMode:      c.Remote.RelayMode,
			CustomRelayURL: c.Remote.CustomRelayURL,
			HasAdminToken:  c.Remote.RelayAdminToken != "",
			MobilePort:     c.Remote.MobilePort,
			PairedDevices:  c.Remote.PairedDevices,
		},
	}
}

func BaseDir() string {
	if env := strings.TrimSpace(os.Getenv("PEACECLAUDE_HOME")); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".peaceclaude"
	}
	return filepath.Join(home, ".peaceclaude")
}

func ConfigPath() string       { return filepath.Join(BaseDir(), "config.json") }
func ProfilesDir() string      { return filepath.Join(BaseDir(), "profiles") }
func SharedRulesDir() string   { return filepath.Join(BaseDir(), "shared-rules") }
func LogsDir() string          { return filepath.Join(BaseDir(), "logs") }
func BackupsDir() string       { return filepath.Join(BaseDir(), "backups") }
func StateDir() string         { return filepath.Join(BaseDir(), "state") }
func RunDir() string           { return filepath.Join(BaseDir(), "run") }
func DaemonLogPath() string    { return filepath.Join(LogsDir(), "daemon.log") }
func LeaksLogPath() string     { return filepath.Join(LogsDir(), "leaks.jsonl") }
func CaptureLogPath() string   { return filepath.Join(LogsDir(), "capture.jsonl") }
func AppLauncherLog() string   { return filepath.Join(LogsDir(), "launcher-app.log") }
func DaemonPIDPath() string    { return filepath.Join(RunDir(), "daemon.pid") }
func GeoCachePath() string     { return filepath.Join(StateDir(), "geo-cache.json") }
func PFDir() string                     { return filepath.Join(StateDir(), "pf") }
func ExtensionsDir() string             { return filepath.Join(BaseDir(), "extensions") }
func NativeBrowserShieldDir() string    { return filepath.Join(ExtensionsDir(), "native-browser-shield") }

func EnsureDirs() error {
	dirs := []string{
		BaseDir(),
		ProfilesDir(),
		SharedRulesDir(),
		LogsDir(),
		BackupsDir(),
		StateDir(),
		RunDir(),
		PFDir(),
		ExtensionsDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
		_ = os.Chmod(d, 0o700)
	}
	return nil
}

func DetectClashSocket() string {
	uid := strconv.Itoa(os.Getuid())
	candidates := []string{
		filepath.Join("/var/run/clash-verge-service/users", uid, "verge-mihomo.sock"),
		"/tmp/verge/verge-mihomo.sock",
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && (fi.Mode()&os.ModeSocket != 0) {
			return p
		}
	}
	return candidates[0]
}

func detectExpectedIPFromHost() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".local/share/claude-egress/expected_ip"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func Default() *Config {
	expectedIP := detectExpectedIPFromHost()
	return &Config{
		Version:       CurrentVersion,
		ActiveProfile: "default",
		Listen: ListenConfig{
			BindAddr:       "127.0.0.1",
			HTTPPort:       18897,
			SOCKS5Port:     18898,
			ControlPort:    18899,
			VMProxyEnabled: false,
			VMProxyBind:    "192.168.64.1",
			VMProxySubnets: []string{"192.168.64.0/24"},
		},
		Upstream: UpstreamConfig{
			Mode:        "local-proxy",
			Address:     "127.0.0.1:7897",
			ClashSocket: DetectClashSocket(),
			Label:       "Local Clash Verge / Webshare Static IP",
		},
		Egress: EgressConfig{
			ExpectedIP:            expectedIP,
			AllowedCountries:      []string{"US"},
			ProbeIntervalSec:      45,
			BadIntervalSec:        5,
			ProbeTimeoutSec:       12,
			FailClosed:            true,
			SkipProbeDuringSSE:    true,
			StrictDomainIsolation: false,
		},
		Identity: IdentityConfig{
			SyncTimezone:          true,
			Timezone:              "",
			Language:              "en-US",
			Locale:                "en_US.UTF-8",
			StripHostAnthropicEnv: true,
			DisableDesktopQUIC:    true,
			BlockDesktopLocalDNS:  true,
			AutoPeaceDesktop:       true,
			SingleActiveDesktop:   true,
		},
		Shield: ShieldConfig{
			TelemetryShield:      true,
			BlockPrivateTargets:  false,
			AllowCorporateDirect: true,
			CorporateDomains:     []string{},
			ExtraBlockedDomains:  []string{},
			ExtraClaudeDomains:   []string{},
		},
		Rules: RulesConfig{
			SharedEnabled: true,
			SyncItems:     []string{"CLAUDE.md", "skills", "agents", "hooks"},
		},
		Capture: CaptureConfig{
			Enabled:    false,
			MaxEntries: 500,
		},
		Binaries: BinariesConfig{
			ClaudeCLIPath:    "",
			ClaudeDesktopApp: "/Applications/Claude.app",
			LoginBrowserApp:  "",
		},
		Appearance: AppearanceConfig{
			ThemeMode:    "dark",
			PresetLight:  "paper",
			PresetDark:   "obsidian",
			UIFontSize:   14,
			CodeFontSize: 12,
		},
		Remote: RemoteConfig{
			Enabled:    false,
			RelayMode:  "lan",
			MobilePort: 18900,
		},
	}
}

func (c *Config) Normalize() {
	def := Default()
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	if strings.TrimSpace(c.ActiveProfile) == "" {
		c.ActiveProfile = "default"
	}
	if c.Listen.BindAddr == "" {
		c.Listen.BindAddr = def.Listen.BindAddr
	}
	if c.Listen.HTTPPort <= 0 {
		c.Listen.HTTPPort = def.Listen.HTTPPort
	}
	if c.Listen.SOCKS5Port <= 0 {
		c.Listen.SOCKS5Port = def.Listen.SOCKS5Port
	}
	if c.Listen.ControlPort <= 0 {
		c.Listen.ControlPort = def.Listen.ControlPort
	}
	if len(c.Listen.VMProxySubnets) == 0 {
		c.Listen.VMProxySubnets = def.Listen.VMProxySubnets
	}
	if c.Upstream.Mode == "" {
		c.Upstream.Mode = def.Upstream.Mode
	}
	if c.Upstream.Address == "" {
		c.Upstream.Address = def.Upstream.Address
	}
	if c.Upstream.ClashSocket == "" {
		c.Upstream.ClashSocket = DetectClashSocket()
	}
	if c.Egress.ProbeIntervalSec <= 0 {
		c.Egress.ProbeIntervalSec = def.Egress.ProbeIntervalSec
	}
	if c.Egress.BadIntervalSec <= 0 {
		c.Egress.BadIntervalSec = def.Egress.BadIntervalSec
	}
	if c.Egress.ProbeTimeoutSec <= 0 {
		c.Egress.ProbeTimeoutSec = def.Egress.ProbeTimeoutSec
	}
	if c.Identity.Language == "" {
		c.Identity.Language = def.Identity.Language
	}
	if c.Identity.Locale == "" {
		c.Identity.Locale = def.Identity.Locale
	}
	if len(c.Rules.SyncItems) == 0 {
		c.Rules.SyncItems = def.Rules.SyncItems
	}
	if c.Capture.MaxEntries <= 0 {
		c.Capture.MaxEntries = def.Capture.MaxEntries
	}
	if c.Binaries.ClaudeDesktopApp == "" {
		c.Binaries.ClaudeDesktopApp = def.Binaries.ClaudeDesktopApp
	}
	if c.Appearance.ThemeMode != "light" && c.Appearance.ThemeMode != "dark" && c.Appearance.ThemeMode != "system" {
		c.Appearance.ThemeMode = def.Appearance.ThemeMode
	}
	if c.Appearance.PresetLight == "" {
		c.Appearance.PresetLight = def.Appearance.PresetLight
	}
	if c.Appearance.PresetDark == "" {
		c.Appearance.PresetDark = def.Appearance.PresetDark
	}
	if c.Appearance.UIFontSize < 12 || c.Appearance.UIFontSize > 20 {
		c.Appearance.UIFontSize = def.Appearance.UIFontSize
	}
	if c.Appearance.CodeFontSize < 11 || c.Appearance.CodeFontSize > 18 {
		c.Appearance.CodeFontSize = def.Appearance.CodeFontSize
	}
	switch c.Remote.RelayMode {
	case "lan", "custom", "official":
	default:
		c.Remote.RelayMode = def.Remote.RelayMode
	}
	if c.Remote.MobilePort < 1024 || c.Remote.MobilePort > 65535 {
		c.Remote.MobilePort = def.Remote.MobilePort
	}
}

func (c *Config) Validate() error {
	switch c.Upstream.Mode {
	case "local-proxy", "direct-socks5", "direct-http":
	default:
		return fmt.Errorf("unsupported upstream.mode %q (want local-proxy|direct-socks5|direct-http)", c.Upstream.Mode)
	}
	if strings.TrimSpace(c.Upstream.Address) == "" {
		return errors.New("upstream.address must not be empty")
	}
	for name, port := range map[string]int{
		"listen.http_port":    c.Listen.HTTPPort,
		"listen.socks5_port":  c.Listen.SOCKS5Port,
		"listen.control_port": c.Listen.ControlPort,
	} {
		if port < 1024 || port > 65535 {
			return fmt.Errorf("%s out of range [1024,65535]: %d", name, port)
		}
	}
	if c.Listen.HTTPPort == c.Listen.SOCKS5Port || c.Listen.HTTPPort == c.Listen.ControlPort || c.Listen.SOCKS5Port == c.Listen.ControlPort {
		return errors.New("listen ports (http, socks5, control) must be distinct")
	}
	return nil
}

func (c *Config) HTTPProxyAddr() string {
	return fmt.Sprintf("%s:%d", c.Listen.BindAddr, c.Listen.HTTPPort)
}

func (c *Config) SOCKS5ProxyAddr() string {
	return fmt.Sprintf("%s:%d", c.Listen.BindAddr, c.Listen.SOCKS5Port)
}

func (c *Config) ControlAddr() string {
	return fmt.Sprintf("%s:%d", c.Listen.BindAddr, c.Listen.ControlPort)
}

func (c *Config) ControlURL() string {
	return fmt.Sprintf("http://%s", c.ControlAddr())
}

func (c *Config) HTTPProxyURL() string {
	return fmt.Sprintf("http://%s", c.HTTPProxyAddr())
}

func (c *Config) SOCKS5ProxyURL() string {
	return fmt.Sprintf("socks5h://%s", c.SOCKS5ProxyAddr())
}

func Load() (*Config, error) {
	if err := EnsureDirs(); err != nil {
		return nil, err
	}
	p := ConfigPath()
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		if err := Save(cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", p, err)
	}
	cfg := Default()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", p, err)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func Save(cfg *Config) error {
	if err := EnsureDirs(); err != nil {
		return err
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	p := ConfigPath()
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
