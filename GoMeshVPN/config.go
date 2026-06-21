package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	ServerAddr string `json:"server_addr"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	// AutoConnect bool   `json:"auto_connect"` // TODO for future
}

func GetConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "gomeshvpn_config.json")
}

func LoadConfig() (*Config, error) {
	b, err := os.ReadFile(GetConfigPath())
	if err != nil {
		return nil, err
	}
	var cfg Config
	err = json.Unmarshal(b, &cfg)
	if err != nil {
		return nil, err
	}
	// Defaults
	if cfg.ServerAddr == "" {
		cfg.ServerAddr = "127.0.0.1"
	}
	return &cfg, nil
}

func SaveConfig(cfg *Config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(GetConfigPath(), b, 0600)
}
