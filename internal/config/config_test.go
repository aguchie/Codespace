package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigLoadAndDefaults(t *testing.T) {
	tempDir := t.TempDir()
	yamlContent := `
app:
  port: 4500
  visibility: public
  allow_crawling: true

commands:
  dev: "nub watch custom.ts"

sub_users:
  - id: "tester"
    password: "pass"
`
	cfgFile := filepath.Join(tempDir, "workspace.yml")
	if err := os.WriteFile(cfgFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test yaml: %v", err)
	}

	sysCfg := SystemConfig{
		WorkspaceDir: tempDir,
	}

	cm, err := NewConfigManager(sysCfg, nil)
	if err != nil {
		t.Fatalf("NewConfigManager error: %v", err)
	}

	ws := cm.GetWorkspace()
	if ws.App.Port != 4500 {
		t.Errorf("expected port 4500, got %d", ws.App.Port)
	}
	if ws.App.Visibility != "public" {
		t.Errorf("expected visibility public, got %s", ws.App.Visibility)
	}
	if !ws.App.AllowCrawling {
		t.Errorf("expected allow_crawling true, got false")
	}
	if ws.Commands.Dev != "nub watch custom.ts" {
		t.Errorf("expected custom dev command, got %s", ws.Commands.Dev)
	}
	if len(ws.SubUsers) != 1 || ws.SubUsers[0].ID != "tester" {
		t.Errorf("sub_users not parsed correctly: %+v", ws.SubUsers)
	}
}

func TestConfigMissingFallback(t *testing.T) {
	tempDir := t.TempDir()
	sysCfg := SystemConfig{
		WorkspaceDir: tempDir,
	}

	cm, err := NewConfigManager(sysCfg, nil)
	if err != nil {
		t.Fatalf("unexpected error on missing workspace.yml: %v", err)
	}

	ws := cm.GetWorkspace()
	if ws.App.Port != 3000 {
		t.Errorf("expected default port 3000, got %d", ws.App.Port)
	}
	if ws.App.Visibility != "private" {
		t.Errorf("expected default visibility private, got %s", ws.App.Visibility)
	}
}
