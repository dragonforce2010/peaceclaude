package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSaveAndRedaction(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("PEACECLAUDE_HOME", tmp)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load default: %v", err)
	}
	if cfg.Listen.HTTPPort != 18897 || cfg.Listen.SOCKS5Port != 18898 || cfg.Listen.ControlPort != 18899 {
		t.Fatalf("unexpected default ports: %+v", cfg.Listen)
	}

	cfg.Upstream.Mode = "direct-socks5"
	cfg.Upstream.Address = "198.51.100.10:6331"
	cfg.Upstream.Username = "secret-user"
	cfg.Upstream.Password = "super-secret-pass-999"
	cfg.Egress.ExpectedIP = "198.51.100.10"

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fi, err := os.Stat(ConfigPath())
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("config file mode = %o, want 0600", fi.Mode().Perm())
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if reloaded.Upstream.Password != "super-secret-pass-999" {
		t.Fatalf("password did not persist")
	}

	red := reloaded.Redacted()
	raw, _ := json.Marshal(red)
	if strings.Contains(string(raw), "super-secret-pass-999") || strings.Contains(string(raw), "secret-user") {
		t.Fatalf("redacted config leaked credentials: %s", raw)
	}
	if !red.Upstream.HasCredentials {
		t.Fatalf("expected HasCredentials=true")
	}
}

func TestParseClashProxyNode(t *testing.T) {
	sample := `
mixed-port: 7897
proxies:
  - name: "Claude-US-1"
    type: socks5
    server: 198.51.100.10
    port: 6331
    username: "ws-user"
    password: "ws-secret-value"
  - { name: Claude-US-Chain, type: socks5, server: 198.51.100.10, port: 6331, username: 'u2', password: 'p2', dialer-proxy: Hop-1 }
proxy-groups:
  - name: Anthropic
    type: select
`
	n1, err := ParseClashProxyNode(sample, "Claude-US-1")
	if err != nil {
		t.Fatalf("parse block node: %v", err)
	}
	if n1.Type != "socks5" || n1.Server != "198.51.100.10" || n1.Port != "6331" || n1.Username != "ws-user" || n1.Password != "ws-secret-value" {
		t.Fatalf("unexpected block node: %+v", n1)
	}
	if strings.Contains(n1.Summary(), "ws-secret-value") {
		t.Fatalf("summary leaked password: %s", n1.Summary())
	}

	n2, err := ParseClashProxyNode(sample, "Claude-US-Chain")
	if err != nil {
		t.Fatalf("parse inline node: %v", err)
	}
	if n2.DialerProxy != "Hop-1" || n2.Password != "p2" {
		t.Fatalf("unexpected inline node: %+v", n2)
	}

	tmp := filepath.Join(t.TempDir(), "clash.yaml")
	if err := os.WriteFile(tmp, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportClashNodeFromFile(tmp, "Claude-US-1"); err != nil {
		t.Fatalf("ImportClashNodeFromFile: %v", err)
	}

	// Regression test: zero-indented `- name:` list items directly under `proxies:` (Clash Verge runtime format).
	zeroIndent := `
proxies:
- name: Claude-US-1
  type: socks5
  server: 198.51.100.10
  port: 6331
  username: ws-u
  password: ws-p
  dialer-proxy: Hop-1
proxy-groups:
- name: Anthropic
`
	n3, err := ParseClashProxyNode(zeroIndent, "Claude-US-1")
	if err != nil {
		t.Fatalf("parse zero-indent node: %v", err)
	}
	if n3.Server != "198.51.100.10" || n3.Port != "6331" || n3.DialerProxy != "Hop-1" {
		t.Fatalf("unexpected zero-indent node: %+v", n3)
	}
}

func TestValidateRejectsInvalidConfig(t *testing.T) {
	c := Default()
	c.Upstream.Mode = "bogus"
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for invalid upstream mode")
	}
	c = Default()
	c.Listen.HTTPPort = c.Listen.SOCKS5Port
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for colliding ports")
	}
}
