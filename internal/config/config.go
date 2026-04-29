package config

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	defaultGitHubTokenEnv    = "MIDDLEMAN_GITHUB_TOKEN"
	defaultSyncInterval      = "5m"
	defaultHost              = "127.0.0.1"
	defaultPort              = 8091
	defaultViewMode          = "threaded"
	defaultTimeRange         = "7d"
	defaultBasePath          = "/"
	defaultSyncBudgetPerHour = 500
	defaultPlatformHost      = "github.com"
)

type Repo struct {
	Owner        string `toml:"owner" json:"owner"`
	Name         string `toml:"name" json:"name"`
	PlatformHost string `toml:"platform_host,omitempty" json:"platform_host,omitempty"`
	TokenEnv     string `toml:"token_env,omitempty" json:"token_env,omitempty"`
}

func (r Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

func (r Repo) HasNameGlob() bool {
	return strings.ContainsAny(r.Name, "*?[")
}

// PlatformHostOrDefault returns the configured platform host,
// defaulting to "github.com" when empty.
func (r Repo) PlatformHostOrDefault() string {
	if r.PlatformHost == "" {
		return "github.com"
	}
	return r.PlatformHost
}

// ResolveToken returns the token for this repo. When TokenEnv is
// set, it reads from that env var. Falls back to globalToken if
// the env var is empty or TokenEnv is not set.
func (r Repo) ResolveToken(globalToken string) string {
	if r.TokenEnv != "" {
		if tok := os.Getenv(r.TokenEnv); tok != "" {
			return tok
		}
	}
	return globalToken
}

// normalize cleans up a Repo entry, extracting owner/name from
// GitHub URLs or SSH addresses if the user pasted one into either
// field. It also strips a trailing .git suffix.
func (r *Repo) normalize() error {
	// Check if either field contains a full GitHub URL or SSH
	// address. If so, extract owner/name from it.
	for _, raw := range []string{r.Owner, r.Name} {
		owner, name, err := parseGitHubRef(raw)
		if err != nil {
			return err
		}
		if owner != "" {
			r.Owner = owner
			r.Name = name
			break
		}
	}

	r.Name = strings.TrimSuffix(r.Name, ".git")
	if r.Owner == "" || r.Name == "" {
		return errors.New("must have owner and name")
	}
	r.Owner = strings.ToLower(r.Owner)
	r.Name = strings.ToLower(r.Name)
	r.PlatformHost = strings.ToLower(r.PlatformHost)
	return nil
}

func (r Repo) ownerHasGlob() bool {
	return strings.ContainsAny(r.Owner, "*?[")
}

// parseGitHubRef extracts owner and repo name from a GitHub URL or
// SSH address. Returns ("", "", nil) when the input is not a GitHub
// ref at all, or a non-nil error when it looks like a GitHub ref but
// is malformed (e.g. missing the repo name).
func parseGitHubRef(raw string) (owner, name string, err error) {
	raw = strings.TrimSpace(raw)
	var path string
	switch {
	case strings.HasPrefix(raw, "ssh://"):
		// URI-style SSH: ssh://git@github.com[:port]/owner/repo
		p, isGitHub, err := parseSSHURI(raw)
		if err != nil {
			return "", "", err
		}
		if !isGitHub {
			return "", "", nil
		}
		path = p
	default:
		if m := ghSCPRe.FindStringSubmatch(raw); m != nil {
			path = m[1]
		} else if m := ghSchemeRe.FindStringSubmatch(raw); m != nil {
			path = m[1]
		} else if m := ghBareRe.FindStringSubmatch(raw); m != nil {
			path = m[1]
		} else {
			return "", "", nil
		}
	}
	path = cleanPath(path)
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf(
			"incomplete GitHub reference %q: expected owner/repo", raw,
		)
	}
	return parts[0], parts[1], nil
}

// parseSSHURI parses ssh:// URIs. Returns (path, true, nil) when
// the host is github.com, or ("", false, nil) when it is not.
func parseSSHURI(raw string) (string, bool, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false, fmt.Errorf("invalid SSH URI %q: %w", raw, err)
	}
	if u.Hostname() != "github.com" {
		return "", false, nil
	}
	return strings.TrimPrefix(u.Path, "/"), true, nil
}

// cleanPath strips query strings, fragments, trailing slashes,
// and an optional .git suffix from a GitHub ref path.
func cleanPath(path string) string {
	if idx := strings.IndexAny(path, "?#"); idx != -1 {
		path = path[:idx]
	}
	path = strings.TrimRight(path, "/")
	path = strings.TrimSuffix(path, ".git")
	return path
}

type Activity struct {
	ViewMode   string `toml:"view_mode" json:"view_mode"`
	TimeRange  string `toml:"time_range" json:"time_range"`
	HideClosed bool   `toml:"hide_closed" json:"hide_closed"`
	HideBots   bool   `toml:"hide_bots" json:"hide_bots"`
}

type Terminal struct {
	FontFamily string `toml:"font_family,omitempty" json:"font_family"`
}

type Agent struct {
	Key     string   `toml:"key" json:"key"`
	Label   string   `toml:"label,omitempty" json:"label"`
	Command []string `toml:"command,omitempty" json:"command"`
	Enabled *bool    `toml:"enabled,omitempty" json:"enabled,omitempty"`
}

func (a Agent) EnabledOrDefault() bool {
	return a.Enabled == nil || *a.Enabled
}

type Roborev struct {
	Endpoint string `toml:"endpoint,omitempty"`
}

type Tmux struct {
	Command       []string `toml:"command,omitempty"`
	AgentSessions *bool    `toml:"agent_sessions,omitempty"`
}

type Config struct {
	SyncInterval        string   `toml:"sync_interval"`
	GitHubTokenEnv      string   `toml:"github_token_env"`
	DefaultPlatformHost string   `toml:"default_platform_host"`
	Host                string   `toml:"host"`
	Port                int      `toml:"port"`
	BasePath            string   `toml:"base_path"`
	DataDir             string   `toml:"data_dir"`
	SyncBudgetPerHour   int      `toml:"sync_budget_per_hour"`
	Repos               []Repo   `toml:"repos"`
	Activity            Activity `toml:"activity"`
	Terminal            Terminal `toml:"terminal"`
	Agents              []Agent  `toml:"agents"`
	Roborev             Roborev  `toml:"roborev"`
	Tmux                Tmux     `toml:"tmux"`
}

func DefaultConfigPath() string {
	return filepath.Join(baseDir(), "config.toml")
}

func DefaultDataDir() string {
	return baseDir()
}

func baseDir() string {
	if d := os.Getenv("MIDDLEMAN_HOME"); d != "" {
		return d
	}
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
default_platform_host = "github.com"
host = "127.0.0.1"
port = 8091

# Add repositories to monitor (or add them in the Settings UI).
# [[repos]]
# owner = "your-org"
# name = "your-repo"

[activity]
view_mode = "threaded"
time_range = "7d"

[tmux]
agent_sessions = true
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
		// shares, cross-device). Fall back to O_EXCL create +
		// write with cleanup on failure.
		return writeExclusive(tmpPath, path)
	}
	return nil
}

// writeExclusive creates dst with O_EXCL (fails if it exists) and
// copies the content from src. Partial files are removed on failure.
func writeExclusive(src, dst string) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("reading temp config: %w", err)
	}

	f, err := os.OpenFile(
		dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600,
	)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("creating config %s: %w", dst, err)
	}

	if _, err := f.Write(content); err != nil {
		f.Close()
		os.Remove(dst)
		return fmt.Errorf("writing config %s: %w", dst, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(dst)
		return fmt.Errorf("flushing config %s: %w", dst, err)
	}
	return nil
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		SyncInterval:        defaultSyncInterval,
		GitHubTokenEnv:      defaultGitHubTokenEnv,
		DefaultPlatformHost: defaultPlatformHost,
		Host:                defaultHost,
		Port:                defaultPort,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if cfg.Repos == nil {
		cfg.Repos = []Repo{}
	}
	if cfg.Agents == nil {
		cfg.Agents = []Agent{}
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

	if cfg.SyncBudgetPerHour == 0 {
		cfg.SyncBudgetPerHour = defaultSyncBudgetPerHour
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
	c.DefaultPlatformHost = strings.ToLower(
		strings.TrimSpace(c.DefaultPlatformHost),
	)
	if c.DefaultPlatformHost == "" {
		c.DefaultPlatformHost = defaultPlatformHost
	}

	for i := range c.Repos {
		if c.Repos[i].ownerHasGlob() {
			return fmt.Errorf(
				"config: repos[%d]: glob syntax in owner is not supported", i,
			)
		}
		if err := c.Repos[i].normalize(); err != nil {
			return fmt.Errorf("config: repos[%d]: %w", i, err)
		}
	}

	// Reject duplicate owner+name pairs.
	seen := make(map[string]bool, len(c.Repos))
	for _, r := range c.Repos {
		key := r.Owner + "/" + r.Name
		if seen[key] {
			return fmt.Errorf(
				"config: duplicate repo %q", key,
			)
		}
		seen[key] = true
	}

	// Reject conflicting token_env for the same host. Compare
	// effective env name: empty TokenEnv means "use global
	// github_token_env", so treat "" as equivalent to the global.
	hostToken := make(map[string]string, len(c.Repos))
	for _, r := range c.Repos {
		host := r.PlatformHostOrDefault()
		effective := r.TokenEnv
		if effective == "" {
			effective = c.GitHubTokenEnv
		}
		if prev, ok := hostToken[host]; ok {
			if prev != effective {
				return fmt.Errorf(
					"config: conflicting token_env for host %q: %q vs %q",
					host, prev, effective,
				)
			}
		} else {
			hostToken[host] = effective
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

	if c.SyncBudgetPerHour != 0 && c.SyncBudgetPerHour < 50 {
		return fmt.Errorf(
			"config: sync_budget_per_hour must be >= 50 or omitted, got %d",
			c.SyncBudgetPerHour,
		)
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

	c.Terminal.FontFamily = strings.TrimSpace(c.Terminal.FontFamily)

	if err := c.validateAgents(); err != nil {
		return err
	}

	if len(c.Tmux.Command) > 0 &&
		strings.TrimSpace(c.Tmux.Command[0]) == "" {
		return fmt.Errorf(
			"config: invalid tmux.command: first element must be non-empty",
		)
	}

	return nil
}

func (c *Config) validateAgents() error {
	seen := make(map[string]struct{}, len(c.Agents))
	for i := range c.Agents {
		agent := &c.Agents[i]
		agent.Key = strings.ToLower(strings.TrimSpace(agent.Key))
		agent.Label = strings.TrimSpace(agent.Label)
		if agent.Key == "" {
			return fmt.Errorf("config: agents[%d]: key is required", i)
		}
		if agent.Label == "" {
			agent.Label = agent.Key
		}
		if reservedSystemLaunchTargetKeys[agent.Key] {
			return fmt.Errorf(
				"config: agents[%d]: key %q is a reserved system launch target",
				i, agent.Key,
			)
		}
		if _, ok := seen[agent.Key]; ok {
			return fmt.Errorf(
				"config: duplicate agent %q", agent.Key,
			)
		}
		seen[agent.Key] = struct{}{}

		if !agent.EnabledOrDefault() {
			continue
		}
		if len(agent.Command) == 0 {
			return fmt.Errorf(
				"config: agents[%d]: command is required when enabled", i,
			)
		}
		if strings.TrimSpace(agent.Command[0]) == "" {
			return fmt.Errorf(
				"config: agents[%d]: command first element must be non-empty", i,
			)
		}
	}
	return nil
}

var reservedSystemLaunchTargetKeys = map[string]bool{
	"tmux":        true,
	"plain_shell": true,
}

var (
	validBasePathRe = regexp.MustCompile(`^/([a-zA-Z0-9._~-]+/)*$`)
	// With scheme: optional path so https://github.com is caught.
	ghSchemeRe = regexp.MustCompile(`^https?://github\.com(?:/(.*))?$`)
	// Without scheme: require / so bare "github.com" (a valid repo
	// name) is not falsely matched.
	ghBareRe = regexp.MustCompile(`^github\.com/(.*)$`)
	// SCP-style only (git@github.com:path); ssh:// URIs use net/url.
	ghSCPRe = regexp.MustCompile(`^[^@]+@github\.com:(.*)$`)
)

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

// TokenEnvNames returns every env var name that may hold a GitHub
// token according to this config: the global github_token_env plus
// any per-repo token_env overrides. Used by the runtime sanitizer
// to strip them from launched session environments so a non-default
// token name is not exposed to PR-controlled processes.
func (c *Config) TokenEnvNames() []string {
	if c == nil {
		return nil
	}
	names := make([]string, 0, 1+len(c.Repos))
	if c.GitHubTokenEnv != "" {
		names = append(names, c.GitHubTokenEnv)
	}
	for _, r := range c.Repos {
		if r.TokenEnv != "" {
			names = append(names, r.TokenEnv)
		}
	}
	return names
}

var execCommand = exec.Command

func ghAuthToken() string {
	out, err := execCommand("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *Config) BudgetPerHour() int {
	return c.SyncBudgetPerHour
}

func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "middleman.db")
}

// RoborevEndpoint returns the configured roborev daemon endpoint,
// falling back to the default localhost address.
func (c *Config) RoborevEndpoint() string {
	if c.Roborev.Endpoint != "" {
		return c.Roborev.Endpoint
	}
	return "http://127.0.0.1:7373"
}

// TmuxCommand returns the command + argv prefix used to invoke
// tmux. Defaults to ["tmux"] when c is nil or the setting is
// unconfigured. The returned slice is a copy, safe to append to.
func (c *Config) TmuxCommand() []string {
	if c == nil || len(c.Tmux.Command) == 0 {
		return []string{"tmux"}
	}
	return append([]string(nil), c.Tmux.Command...)
}

// TmuxAgentSessionsEnabled reports whether runtime agent launches
// should prefer tmux-backed sessions. Defaults to true so agent
// activity is visible to tmux-based workspace fingerprinting.
func (c *Config) TmuxAgentSessionsEnabled() bool {
	return c == nil ||
		c.Tmux.AgentSessions == nil ||
		*c.Tmux.AgentSessions
}

// configFile is the subset of Config written to disk.
type configFile struct {
	SyncInterval        string   `toml:"sync_interval"`
	GitHubTokenEnv      string   `toml:"github_token_env"`
	DefaultPlatformHost string   `toml:"default_platform_host,omitempty"`
	Host                string   `toml:"host"`
	Port                int      `toml:"port"`
	SyncBudgetPerHour   int      `toml:"sync_budget_per_hour,omitempty"`
	BasePath            string   `toml:"base_path,omitempty"`
	DataDir             string   `toml:"data_dir,omitempty"`
	Repos               []Repo   `toml:"repos"`
	Activity            Activity `toml:"activity"`
	Terminal            Terminal `toml:"terminal,omitempty"`
	Agents              []Agent  `toml:"agents,omitempty"`
	Roborev             Roborev  `toml:"roborev,omitempty"`
	Tmux                Tmux     `toml:"tmux,omitempty"`
}

// Save writes the current config to the given path.
func (c *Config) Save(path string) error {
	f := configFile{
		SyncInterval:        c.SyncInterval,
		GitHubTokenEnv:      c.GitHubTokenEnv,
		DefaultPlatformHost: c.DefaultPlatformHost,
		Host:                c.Host,
		Port:                c.Port,
		Repos:               c.Repos,
		Activity:            c.Activity,
		Terminal:            c.Terminal,
		Agents:              c.Agents,
		Roborev:             c.Roborev,
		Tmux:                c.Tmux,
	}
	if c.DefaultPlatformHost == defaultPlatformHost {
		f.DefaultPlatformHost = ""
	}
	if c.SyncBudgetPerHour != defaultSyncBudgetPerHour {
		f.SyncBudgetPerHour = c.SyncBudgetPerHour
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
