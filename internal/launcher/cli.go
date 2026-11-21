package launcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"peaceclaude/internal/config"
	"peaceclaude/internal/egress"
	"peaceclaude/internal/host"
	"peaceclaude/internal/profile"
)

var hostPollutingEnvPrefixes = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_CUSTOM_HEADERS",
	"ANTHROPIC_MODEL",
	"ANTHROPIC_SMALL_FAST_MODEL",
	"ANTHROPIC_DEFAULT_",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"CLAUDE_CODE_AUTO_MODE_SERVER",
	"CLAUDE_CODE_MODEL_CAPABILITIES",
}

func BuildIsolatedEnv(cfg *config.Config, prof *profile.Profile, gate egress.GateSnapshot, baseEnv []string) []string {
	dropKeys := map[string]bool{
		"HTTP_PROXY": true, "HTTPS_PROXY": true, "ALL_PROXY": true, "NO_PROXY": true,
		"http_proxy": true, "https_proxy": true, "all_proxy": true, "no_proxy": true,
		"SOCKS_PROXY": true, "socks_proxy": true,
		"CLAUDE_CONFIG_DIR": true,
		"TZ": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
		"LANGUAGE": true, "LC_TIME": true, "LC_NUMERIC": true, "LC_MONETARY": true, "LC_MESSAGES": true,
		"DISABLE_TELEMETRY": true, "DISABLE_ERROR_REPORTING": true,
	}

	out := make([]string, 0, len(baseEnv)+20)
	for _, kv := range baseEnv {
		k, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if dropKeys[k] {
			continue
		}
		if cfg.Identity.StripHostAnthropicEnv && isPollutingAnthropicEnv(k) {
			continue
		}
		out = append(out, kv)
	}

	tz := gate.EffectiveTimezone
	if prof.Timezone != "" {
		tz = prof.Timezone
	}
	if tz == "" {
		tz = "America/New_York"
	}
	locale := cfg.Identity.Locale
	if locale == "" {
		locale = "en_US.UTF-8"
	}
	noProxy := profile.BuildNoProxy(cfg)

	overrides := map[string]string{
		"CLAUDE_CONFIG_DIR":    prof.CLIConfigDir,
		"HTTP_PROXY":           cfg.HTTPProxyURL(),
		"HTTPS_PROXY":          cfg.HTTPProxyURL(),
		"ALL_PROXY":            cfg.SOCKS5ProxyURL(),
		"NO_PROXY":             noProxy,
		"http_proxy":           cfg.HTTPProxyURL(),
		"https_proxy":          cfg.HTTPProxyURL(),
		"all_proxy":            cfg.SOCKS5ProxyURL(),
		"no_proxy":             noProxy,
		"TZ":                   tz,
		"LANG":                 locale,
		"LC_ALL":               locale,
		"LC_CTYPE":             locale,
		"LANGUAGE":             "en_US:en",
		"PEACECLAUDE_ACTIVE":    "1",
		"PEACECLAUDE_PROFILE":   prof.Name,
		"PEACECLAUDE_EGRESS_IP": gate.LastGood.EgressIP,
	}
	if cfg.Shield.TelemetryShield {
		overrides["DISABLE_TELEMETRY"] = "1"
		overrides["DISABLE_ERROR_REPORTING"] = "1"
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func isPollutingAnthropicEnv(key string) bool {
	upper := strings.ToUpper(key)
	for _, p := range hostPollutingEnvPrefixes {
		if strings.HasSuffix(p, "_") {
			if strings.HasPrefix(upper, p) {
				return true
			}
		} else if upper == p {
			return true
		}
	}
	return false
}

// LaunchCLI executes upstream Claude Code CLI inside the isolated environment after verifying Egress Gate.
func LaunchCLI(ctx context.Context, cfg *config.Config, selfBin, profileName string, args []string, printOnly bool) error {
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	prof, err := profile.Ensure(cfg, profileName, "")
	if err != nil {
		return err
	}
	gate, err := Preflight(ctx, cfg, selfBin, printOnly)
	if err != nil {
		return err
	}
	cliBin, _ := host.FindRealClaudeCLI(cfg)
	if cliBin == "" {
		return fmt.Errorf("未找到原生 Claude Code CLI 二进制。请先安装 Claude Code，或通过 `peaceclaude config set-cli <path>` 指定路径")
	}

	env := BuildIsolatedEnv(cfg, prof, gate, os.Environ())
	if printOnly {
		fmt.Printf("binary:  %s\n", cliBin)
		fmt.Printf("profile: %s (%s)\n", prof.Name, prof.CLIConfigDir)
		fmt.Printf("proxy:   %s (socks5: %s)\n", cfg.HTTPProxyURL(), cfg.SOCKS5ProxyURL())
		fmt.Printf("egress:  %s (%s · TZ=%s · LANG=%s)\n", gate.LastGood.EgressIP, gate.LastGood.Country, gate.EffectiveTimezone, cfg.Identity.Language)
		fmt.Printf("args:    %v\n", args)
		return nil
	}

	// Replace current process with Claude CLI so TTY, signals, and resize behave 100% natively.
	argv := append([]string{cliBin}, args...)
	if err := syscall.Exec(cliBin, argv, env); err != nil {
		// Fallback to subprocess if exec fails for any reason.
		cmd := exec.Command(cliBin, args...)
		cmd.Env = env
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return nil
}
