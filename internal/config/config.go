package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Repo struct {
	Owner string `toml:"owner"`
	Name  string `toml:"name"`
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

type Config struct {
	SyncInterval   string `toml:"sync_interval"`
	GitHubTokenEnv string `toml:"github_token_env"`
	Host           string `toml:"host"`
	Port           int    `toml:"port"`
	DataDir        string `toml:"data_dir"`
	Repos          []Repo `toml:"repos"`
}

func DefaultConfigPath() string {
	return filepath.Join(homeDir(), ".config", "ghboard", "config.toml")
}

func DefaultDataDir() string {
	return filepath.Join(homeDir(), ".config", "ghboard")
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		SyncInterval:   "5m",
		GitHubTokenEnv: "GHBOARD_GITHUB_TOKEN",
		Host:           "127.0.0.1",
		Port:           8090,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}

	return cfg, cfg.Validate()
}

func (c *Config) Validate() error {
	if len(c.Repos) == 0 {
		return errors.New("config: at least one [[repos]] entry required")
	}

	for i, r := range c.Repos {
		if r.Owner == "" || r.Name == "" {
			return fmt.Errorf("config: repos[%d] must have owner and name", i)
		}
	}

	if _, err := time.ParseDuration(c.SyncInterval); err != nil {
		return fmt.Errorf("config: invalid sync_interval %q: %w", c.SyncInterval, err)
	}

	if ip := net.ParseIP(c.Host); ip == nil {
		return fmt.Errorf("config: invalid host %q", c.Host)
	} else if !ip.IsLoopback() {
		return fmt.Errorf("config: host %q is not loopback; ghboard v1 only supports loopback addresses", c.Host)
	}

	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: invalid port %d", c.Port)
	}

	return nil
}

func (c *Config) SyncDuration() time.Duration {
	d, _ := time.ParseDuration(c.SyncInterval)
	return d
}

func (c *Config) GitHubToken() string {
	return os.Getenv(c.GitHubTokenEnv)
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "ghboard.db")
}
