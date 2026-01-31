package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Config represents the server configuration
type Config struct {
	Server   ServerConfig   `json:"server"`
	Storage  StorageConfig  `json:"storage"`
	Auth     AuthConfig     `json:"auth"`
	Security SecurityConfig `json:"security"`
	Database DatabaseConfig `json:"database"`
	AutoRestart AutoRestartConfig `json:"auto_restart"`
}

type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type StorageConfig struct {
	ImagesDir        string `json:"images_dir"`
	MaxFileSize      int64  `json:"max_file_size"`
	CleanupInterval  int    `json:"cleanup_interval"`
	DefaultTTL       int    `json:"default_ttl"`
	MaxTTL           int    `json:"max_ttl"`
}

type AuthConfig struct {
	APIKey        string `json:"api_key"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
	ListPassword  string `json:"list_password"`
}

type SecurityConfig struct {
	IPWhitelist          []string `json:"ip_whitelist"`
	RateLimitPerMinute   int      `json:"rate_limit_per_minute"`
	SessionTimeout       int      `json:"session_timeout"`
}

type DatabaseConfig struct {
	Path string `json:"path"`
}

type AutoRestartConfig struct {
	Enabled         bool `json:"enabled"`
	MaxRestartCount int  `json:"max_restart_count"`
}

var globalConfig *Config

// Load loads the configuration from file or creates default
func Load(configPath string) (*Config, error) {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Try to load existing config
	cfg := &Config{}
	data, err := os.ReadFile(configPath)
	if err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
		globalConfig = cfg
		return cfg, nil
	}

	// Create default config if not exists
	if os.IsNotExist(err) {
		cfg = getDefaultConfig()
		if err := Save(cfg, configPath); err != nil {
			return nil, fmt.Errorf("failed to save default config: %w", err)
		}
		globalConfig = cfg
		return cfg, nil
	}

	return nil, err
}

// Save saves the configuration to file
func Save(cfg *Config, configPath string) error {
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	globalConfig = cfg
	return nil
}

// GetGlobalConfig returns the global configuration
func GetGlobalConfig() *Config {
	return globalConfig
}

// UpdatePort updates the port in config and saves it
func UpdatePort(configPath string, port int) error {
	cfg := GetGlobalConfig()
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	cfg.Server.Port = port
	return Save(cfg, configPath)
}

// getDefaultConfig returns the default configuration
func getDefaultConfig() *Config {
	dataDir := getDataDir()
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Storage: StorageConfig{
			ImagesDir:       filepath.Join(dataDir, "Images"),
			MaxFileSize:     100 * 1024 * 1024, // 100MB
			CleanupInterval: 60,
			DefaultTTL:      1,
			MaxTTL:          8760, // 365 days
		},
		Auth: AuthConfig{
			APIKey:        "change-me-api-key",
			AdminUsername: "276793422",
			AdminPassword: "490003219",
			ListPassword:  "490003219",
		},
		Security: SecurityConfig{
			IPWhitelist:        []string{},
			RateLimitPerMinute: 60,
			SessionTimeout:     300, // 5 minutes
		},
		Database: DatabaseConfig{
			Path: filepath.Join(dataDir, "metadata.db"),
		},
		AutoRestart: AutoRestartConfig{
			Enabled:         true,
			MaxRestartCount: 10,
		},
	}
}

// getDataDir returns the data directory based on platform
func getDataDir() string {
	if runtime.GOOS == "windows" {
		// Windows: use current working directory (preferred)
		// Avoid using temp directories from go run
		if cwd, err := os.Getwd(); err == nil {
			// Check if it looks like a temp directory (from go run)
			if !strings.Contains(strings.ToLower(cwd), "\\temp\\") &&
			   !strings.Contains(strings.ToLower(cwd), "\\appdata\\local\\temp\\") {
				// Not a temp directory, use current working directory
				return cwd
			}
		}
		// Fallback: try user home directory
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, "HttpServer")
		}
		return "."
	}
	// Linux/Unix: use ~/HttpServer
	home, err := os.UserHomeDir()
	if err != nil {
		return "./HttpServer"
	}
	return filepath.Join(home, "HttpServer")
}

// EnsureDirectories ensures all required directories exist
func EnsureDirectories(cfg *Config) error {
	dirs := []string{
		cfg.Storage.ImagesDir,
		filepath.Dir(cfg.Database.Path),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}
