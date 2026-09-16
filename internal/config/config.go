// Package config provides system configuration and workspace.yml management.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// AppConfig represents application preview settings.
type AppConfig struct {
	Port          int    `yaml:"port"`
	Visibility    string `yaml:"visibility"`
	AllowCrawling bool   `yaml:"allow_crawling"`
}

// SubUser represents a secondary user credential for client previews.
type SubUser struct {
	ID       string `yaml:"id"`
	Password string `yaml:"password"`
}

// CommandsConfig represents user application commands configured in workspace.yml.
type CommandsConfig struct {
	Dev string `yaml:"dev"`
}

// WorkspaceConfig represents the parsed configuration of workspace.yml.
type WorkspaceConfig struct {
	App      AppConfig      `yaml:"app"`
	Commands CommandsConfig `yaml:"commands"`
	SubUsers []SubUser      `yaml:"sub_users"`
}

// SystemConfig represents runtime environment variables and gateway system settings.
type SystemConfig struct {
	AdminUser      string
	AdminPass      string
	Port           int
	CodeServerPort int
	WorkspaceDir   string
	CookieSecret   string
	DisableCodeSrv bool
}

// ConfigManager holds the current system and workspace configuration thread-safely.
type ConfigManager struct {
	mu           sync.RWMutex
	system       SystemConfig
	workspace    WorkspaceConfig
	configPath   string
	onConfigLoad func(WorkspaceConfig)
}

// LoadSystemConfig loads environment variables with sensible defaults.
func LoadSystemConfig() SystemConfig {
	port := 80
	if p := os.Getenv("PORT"); p != "" {
		if val, err := strconv.Atoi(p); err == nil {
			port = val
		}
	}

	csPort := 8080
	if p := os.Getenv("CODE_SERVER_PORT"); p != "" {
		if val, err := strconv.Atoi(p); err == nil {
			csPort = val
		}
	}

	adminUser := os.Getenv("ADMIN_USER")
	if adminUser == "" {
		adminUser = "admin"
	}

	adminPass := os.Getenv("ADMIN_PASS")
	if adminPass == "" {
		adminPass = "admin123"
	}

	workspaceDir := os.Getenv("WORKSPACE_DIR")
	if workspaceDir == "" {
		workspaceDir = "."
	}
	absWorkspaceDir, err := filepath.Abs(workspaceDir)
	if err == nil {
		workspaceDir = absWorkspaceDir
	}

	cookieSecret := os.Getenv("COOKIE_SECRET")
	if cookieSecret == "" {
		cookieSecret = "codespace-default-secret-change-in-production"
	}

	disableCodeSrv := false
	if os.Getenv("DISABLE_CODE_SERVER") == "1" || os.Getenv("DISABLE_CODE_SERVER") == "true" {
		disableCodeSrv = true
	}

	return SystemConfig{
		AdminUser:      adminUser,
		AdminPass:      adminPass,
		Port:           port,
		CodeServerPort: csPort,
		WorkspaceDir:   workspaceDir,
		CookieSecret:   cookieSecret,
		DisableCodeSrv: disableCodeSrv,
	}
}

// DefaultWorkspaceConfig returns default settings if workspace.yml is absent.
func DefaultWorkspaceConfig() WorkspaceConfig {
	return WorkspaceConfig{
		App: AppConfig{
			Port:          3000,
			Visibility:    "private",
			AllowCrawling: false,
		},
		Commands: CommandsConfig{
			Dev: "nub sample_app/server.ts",
		},
		SubUsers: []SubUser{},
	}
}

// NewConfigManager initializes configuration and loads workspace.yml.
func NewConfigManager(sys SystemConfig, onUpdate func(WorkspaceConfig)) (*ConfigManager, error) {
	cfgPath := filepath.Join(sys.WorkspaceDir, "workspace.yml")
	cm := &ConfigManager{
		system:       sys,
		configPath:   cfgPath,
		onConfigLoad: onUpdate,
		workspace:    DefaultWorkspaceConfig(),
	}

	if err := cm.Reload(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load workspace.yml: %w", err)
	}

	return cm, nil
}

// Reload reads and parses the workspace.yml file.
func (cm *ConfigManager) Reload() error {
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return err
	}

	var wCfg WorkspaceConfig
	if err := yaml.Unmarshal(data, &wCfg); err != nil {
		return fmt.Errorf("yaml parse error: %w", err)
	}

	if wCfg.App.Port == 0 {
		wCfg.App.Port = 3000
	}
	if wCfg.App.Visibility == "" {
		wCfg.App.Visibility = "private"
	}
	wCfg.App.Visibility = strings.ToLower(wCfg.App.Visibility)

	cm.mu.Lock()
	cm.workspace = wCfg
	cm.mu.Unlock()

	if cm.onConfigLoad != nil {
		cm.onConfigLoad(wCfg)
	}

	return nil
}

// GetWorkspace returns a copy of the current workspace configuration.
func (cm *ConfigManager) GetWorkspace() WorkspaceConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.workspace
}

// GetSystem returns the current system configuration.
func (cm *ConfigManager) GetSystem() SystemConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.system
}

// GetConfigPath returns the absolute path of workspace.yml.
func (cm *ConfigManager) GetConfigPath() string {
	return cm.configPath
}
