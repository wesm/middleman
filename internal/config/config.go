package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	defaultGitHubTokenEnv = "MIDDLEMAN_GITHUB_TOKEN"
	defaultSyncInterval   = "5m"
	defaultHost           = "127.0.0.1"
	defaultPort           = 8090
	defaultViewMode       = "threaded"
	defaultTimeRange      = "7d"
	defaultBasePath       = "/"
)

type Repo struct {
	Owner string `toml:"owner" json:"owner"`
	Name  string `toml:"name" json:"name"`
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

type Activity struct {
	ViewMode   string `toml:"view_mode" json:"view_mode"`
	TimeRange  string `toml:"time_range" json:"time_range"`
	HideClosed bool   `toml:"hide_closed" json:"hide_closed"`
	HideBots   bool   `toml:"hide_bots" json:"hide_bots"`
}

type Config struct {
	SyncInterval   string   `toml:"sync_interval"`
	GitHubTokenEnv string   `toml:"github_token_env"`
	Host           string   `toml:"host"`
	Port           int      `toml:"port"`
	BasePath       string   `toml:"base_path"`
	DataDir        string   `toml:"data_dir"`
	Repos          []Repo   `toml:"repos"`
	Activity       Activity `toml:"activity"`
}

func DefaultConfigPath() string {
	return filepath.Join(homeDir(), ".config", "middleman", "config.toml")
}

func DefaultDataDir() string {
	return filepath.Join(homeDir(), ".config", "middleman")
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// EnsureDefault creates a default config file at path if it does not exist.
// The file contains sensible defaults. Repos can be added later through the
// settings UI.
//
// Writes to a temp file first, then hard-links into place so the target
// path is never left empty or partially written.
func EnsureDefault(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		return fmt.Errorf("creating temp config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	const defaultConfig = `# middleman configuration
# See https://github.com/wesm/middleman for documentation.

sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8090

# Add repositories to monitor (or add them in the Settings UI).
# [[repos]]
# owner = "your-org"
# name = "your-repo"

[activity]
view_mode = "threaded"
time_range = "7d"
`
	if _, err := tmp.WriteString(defaultConfig); err != nil {
		tmp.Close()
		return fmt.Errorf("writing default config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("flushing default config: %w", err)
	}

	// Link fails atomically when path already exists, providing
	// both atomic install and race-free existence check.
	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		// Hard links may not be supported (FAT/exFAT, network
		// shares, cross-device). Fall back to atomic rename.
		// Check if another process already created the file.
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		// Rename is atomic on the same filesystem. The temp file
		// is in the same directory, so this is guaranteed.
		if renameErr := os.Rename(tmpPath, path); renameErr != nil {
			return fmt.Errorf("installing config %s: %w", path, renameErr)
		}
		return nil
	}
	return nil
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		SyncInterval:   defaultSyncInterval,
		GitHubTokenEnv: defaultGitHubTokenEnv,
		Host:           defaultHost,
		Port:           defaultPort,
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

	if cfg.Activity.ViewMode == "" {
		cfg.Activity.ViewMode = defaultViewMode
	}
	if cfg.Activity.TimeRange == "" {
		cfg.Activity.TimeRange = defaultTimeRange
	}

	if cfg.BasePath == "" {
		cfg.BasePath = defaultBasePath
	} else {
		bp := "/" + strings.Trim(cfg.BasePath, "/")
		if bp != "/" {
			bp += "/"
		}
		cfg.BasePath = bp
	}

	return cfg, cfg.Validate()
}

func (c *Config) Validate() error {
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
		return fmt.Errorf("config: host %q is not loopback; only loopback addresses are supported", c.Host)
	}

	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: invalid port %d", c.Port)
	}

	if !validBasePathRe.MatchString(c.BasePath) {
		return fmt.Errorf("config: invalid base_path %q: must be / or /path/ using only alphanumerics, hyphens, underscores, dots, and tildes", c.BasePath)
	}
	for seg := range strings.SplitSeq(strings.Trim(c.BasePath, "/"), "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("config: invalid base_path %q: dot segments are not allowed", c.BasePath)
		}
	}

	validViewModes := map[string]bool{
		"flat": true, "threaded": true,
	}
	if !validViewModes[c.Activity.ViewMode] {
		return fmt.Errorf(
			"config: invalid activity view_mode %q",
			c.Activity.ViewMode,
		)
	}
	validTimeRanges := map[string]bool{
		"24h": true, "7d": true, "30d": true, "90d": true,
	}
	if !validTimeRanges[c.Activity.TimeRange] {
		return fmt.Errorf(
			"config: invalid activity time_range %q",
			c.Activity.TimeRange,
		)
	}

	return nil
}

var validBasePathRe = regexp.MustCompile(`^/([a-zA-Z0-9._~-]+/)*$`)

func (c *Config) SyncDuration() time.Duration {
	d, _ := time.ParseDuration(c.SyncInterval)
	return d
}

func (c *Config) GitHubToken() string {
	if token := os.Getenv(c.GitHubTokenEnv); token != "" {
		return token
	}
	return ghAuthToken()
}

var execCommand = exec.Command

func ghAuthToken() string {
	out, err := execCommand("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "middleman.db")
}

// configFile is the subset of Config written to disk.
type configFile struct {
	SyncInterval   string   `toml:"sync_interval"`
	GitHubTokenEnv string   `toml:"github_token_env"`
	Host           string   `toml:"host"`
	Port           int      `toml:"port"`
	BasePath       string   `toml:"base_path,omitempty"`
	DataDir        string   `toml:"data_dir,omitempty"`
	Repos          []Repo   `toml:"repos"`
	Activity       Activity `toml:"activity"`
}

// Save writes the current config to the given path.
func (c *Config) Save(path string) error {
	f := configFile{
		SyncInterval:   c.SyncInterval,
		GitHubTokenEnv: c.GitHubTokenEnv,
		Host:           c.Host,
		Port:           c.Port,
		Repos:          c.Repos,
		Activity:       c.Activity,
	}
	if c.BasePath != defaultBasePath {
		f.BasePath = c.BasePath
	}
	if c.DataDir != DefaultDataDir() {
		f.DataDir = c.DataDir
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(f); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
