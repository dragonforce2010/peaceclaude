package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"peaceclaude/internal/config"
)

func TestProfileIsolationRulesSyncAndClean(t *testing.T) {
	t.Setenv("PEACECLAUDE_HOME", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	// Create two isolated profiles
	p1, err := Ensure(cfg, "work-us", "Primary US account")
	if err != nil {
		t.Fatalf("Ensure work-us: %v", err)
	}
	p2, err := Ensure(cfg, "personal-us", "Backup US account")
	if err != nil {
		t.Fatalf("Ensure personal-us: %v", err)
	}
	if p1.Fingerprint.DeviceUUID == p2.Fingerprint.DeviceUUID {
		t.Fatal("profiles must have distinct device fingerprints")
	}

	// Shared rules sync
	sharedMD := filepath.Join(config.SharedRulesDir(), "CLAUDE.md")
	if err := os.WriteFile(sharedMD, []byte("# Global rule\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := SyncAllRules(cfg, true)
	if err != nil {
		t.Fatalf("SyncAllRules on: %v", err)
	}
	if st.Profiles["work-us"] != "synced" || st.Profiles["personal-us"] != "synced" {
		t.Fatalf("expected both profiles synced, got %+v", st.Profiles)
	}

	// Detach keeps local copies
	st, err = SyncAllRules(cfg, false)
	if err != nil {
		t.Fatalf("SyncAllRules off: %v", err)
	}
	if st.Profiles["work-us"] != "detached" {
		t.Fatalf("expected detached, got %+v", st.Profiles)
	}
	if _, err := os.Stat(filepath.Join(p1.CLIConfigDir, "CLAUDE.md")); err != nil {
		t.Fatalf("detached CLAUDE.md should still exist: %v", err)
	}

	// Seed fake credentials/session/desktop fingerprint in work-us and verify clean preserves settings & CLAUDE.md
	_ = os.WriteFile(filepath.Join(p1.CLIConfigDir, ".credentials.json"), []byte(`{"token":"old"}`), 0o600)
	_ = os.WriteFile(filepath.Join(p1.CLIConfigDir, ".claude.json"), []byte(`{"oauthAccount":"old@x.com","theme":"dark"}`), 0o600)
	_ = os.WriteFile(filepath.Join(p1.DesktopDir, "ant-did"), []byte("old-device-id"), 0o600)

	// Dry-run must NOT mutate files
	dry, err := CleanProfile(cfg, "work-us", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Removed) == 0 || len(dry.Scrubbed) == 0 {
		t.Fatalf("dry-run expected findings, got %+v", dry)
	}
	if _, err := os.Stat(filepath.Join(p1.DesktopDir, "ant-did")); err != nil {
		t.Fatal("dry-run must not remove ant-did")
	}

	// Real clean wipes fingerprints, backs up, preserves settings.json & CLAUDE.md, keeps non-identity keys
	oldUUID := p1.Fingerprint.DeviceUUID
	rep, err := CleanProfile(cfg, "work-us", false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.NewDevice == oldUUID {
		t.Fatal("expected regenerated device UUID after clean")
	}
	if _, err := os.Stat(filepath.Join(p1.DesktopDir, "ant-did")); !os.IsNotExist(err) {
		t.Fatal("ant-did should be removed after clean")
	}
	if _, err := os.Stat(filepath.Join(rep.BackupDir, "desktop-data", "ant-did")); err != nil {
		t.Fatalf("ant-did should exist in backup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p1.CLIConfigDir, "settings.json")); err != nil {
		t.Fatal("settings.json must be preserved")
	}
	rawJSON, _ := os.ReadFile(filepath.Join(p1.CLIConfigDir, ".claude.json"))
	var parsed map[string]any
	_ = json.Unmarshal(rawJSON, &parsed)
	if _, exists := parsed["oauthAccount"]; exists {
		t.Fatal("oauthAccount should be scrubbed")
	}
	if parsed["theme"] != "dark" {
		t.Fatalf("non-identity preference theme should be preserved, got %+v", parsed)
	}
}
