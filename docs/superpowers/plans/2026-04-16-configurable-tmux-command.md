# Configurable tmux Launch Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow operators to prefix every tmux invocation middleman makes with a configured command (e.g. `systemd-run --user --scope tmux`) so the tmux session can run under different permissions than a hardened middleman systemd service.

**Architecture:** Add an optional `[tmux] command = [...]` TOML section loaded into `*config.Config`. A nil-safe `TmuxCommand()` helper returns the prefix (default `["tmux"]`). The prefix is threaded into `workspace.Manager` and `terminal.Handler` via `newServer`. The four existing tmux call sites (`new-session`, `has-session`, `kill-session`, `attach-session`) all route through the prefix. Default behavior is preserved bit-for-bit when the setting is unset.

**Tech Stack:** Go 1.26, BurntSushi/toml, testify, coder/websocket, creack/pty.

**Spec:** `docs/superpowers/specs/2026-04-16-configurable-tmux-command-design.md`

---

## File Structure

**Create:**
- `internal/server/tmux_wrapper_test.go` — e2e tests that wire a recording script into `tmux.command` and assert argv for new-session and attach-session paths.

**Modify:**
- `internal/config/config.go` — add `Tmux` type, `Config.Tmux` field, `TmuxCommand()` method, validation case.
- `internal/config/config_test.go` — add parsing, defensive-copy, and validation tests.
- `internal/workspace/manager.go` — add `tmuxCmd []string` field + `SetTmuxCommand` setter; convert `EnsureTmux`, `TmuxSessionExists`, `newTmuxSession` to methods on `*Manager`; update `Setup` and `Delete` call sites to use the prefix.
- `internal/workspace/manager_test.go` — recording-script unit test for all three workspace-side tmux call sites.
- `internal/terminal/handler.go` — add `TmuxCommand []string` field; call `h.Workspaces.EnsureTmux` instead of the package function; build the attach-session `exec.Cmd` from the prefix.
- `internal/server/server.go` — in `newServer`, populate `workspaces.SetTmuxCommand(cfg.TmuxCommand())` and the terminal handler's `TmuxCommand` from `cfg.TmuxCommand()`.

**Field naming convention (used throughout this plan):**
- `workspace.Manager.tmuxCmd` (unexported, set via `SetTmuxCommand`)
- `terminal.Handler.TmuxCommand` (exported, set at struct-literal construction)

Both are `[]string`. The asymmetry (exported vs unexported) mirrors existing patterns: `Manager` uses setter methods (`SetClones`), while `Handler` uses struct-literal construction (`&terminal.Handler{Workspaces: s.workspaces}`).

---

## Task 1: Add Tmux config type, helper, and validation

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1.1: Write failing parsing test**

Append to `internal/config/config_test.go`:

```go
func TestLoadTmuxCommand(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[tmux]
command = ["systemd-run", "--user", "--scope", "tmux"]
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(
		[]string{"systemd-run", "--user", "--scope", "tmux"},
		cfg.Tmux.Command,
	)
}

func TestLoadTmuxCommandOmitted(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, ``)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Empty(cfg.Tmux.Command)
	assert.Equal([]string{"tmux"}, cfg.TmuxCommand())
}

func TestLoadTmuxCommandEmptyArray(t *testing.T) {
	assert := Assert.New(t)
	path := writeConfig(t, `
[tmux]
command = []
`)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal([]string{"tmux"}, cfg.TmuxCommand())
}
```

- [ ] **Step 1.2: Run parsing tests to verify they fail**

Run: `go test ./internal/config -run TestLoadTmux -shuffle=on`
Expected: FAIL — `cfg.Tmux.Command` and `cfg.TmuxCommand()` do not exist yet.

- [ ] **Step 1.3: Add the `Tmux` struct, `Config.Tmux` field, and `TmuxCommand()` method**

Edit `internal/config/config.go`. Add near the other config sub-structs (`Activity`, `Roborev`):

```go
type Tmux struct {
	Command []string `toml:"command,omitempty"`
}
```

Add to the `Config` struct (alongside `Activity` and `Roborev`):

```go
type Config struct {
	// ... existing fields ...
	Activity          Activity `toml:"activity"`
	Roborev           Roborev  `toml:"roborev"`
	Tmux              Tmux     `toml:"tmux"`
}
```

Add the helper method below the existing `Config` methods (next to `RoborevEndpoint`):

```go
// TmuxCommand returns the command + argv prefix used to invoke
// tmux. Defaults to ["tmux"] when c is nil or the setting is
// unconfigured. The returned slice is a copy, safe to append to.
func (c *Config) TmuxCommand() []string {
	if c == nil || len(c.Tmux.Command) == 0 {
		return []string{"tmux"}
	}
	return append([]string(nil), c.Tmux.Command...)
}
```

- [ ] **Step 1.4: Run parsing tests to verify they pass**

Run: `go test ./internal/config -run TestLoadTmux -shuffle=on`
Expected: PASS (3 tests).

- [ ] **Step 1.5: Write failing defensive-copy test**

Append to `internal/config/config_test.go`:

```go
func TestTmuxCommandDefensiveCopy(t *testing.T) {
	assert := Assert.New(t)
	cfg := &Config{Tmux: Tmux{
		Command: []string{"tmux"},
	}}
	first := cfg.TmuxCommand()
	first[0] = "hacked"
	second := cfg.TmuxCommand()
	assert.Equal([]string{"tmux"}, second)
}

func TestTmuxCommandNilReceiver(t *testing.T) {
	assert := Assert.New(t)
	var cfg *Config
	assert.Equal([]string{"tmux"}, cfg.TmuxCommand())
}
```

- [ ] **Step 1.6: Run the two new tests to verify they pass**

Run: `go test ./internal/config -run "TestTmuxCommand" -shuffle=on`
Expected: PASS (2 tests). The `TmuxCommand()` implementation added in step 1.3 already satisfies both (the nil-guard handles `TestTmuxCommandNilReceiver`, the `append([]string(nil), ...)` handles `TestTmuxCommandDefensiveCopy`).

- [ ] **Step 1.7: Write failing validation tests**

The rule: the first element must contain non-whitespace after `strings.TrimSpace`. An empty string fails (TOML lets you write `[""]`), and a whitespace-only string fails too (`exec("   ")` would otherwise produce a confusing shell-level error at first workspace create instead of a clean config-load validation message). Append to `internal/config/config_test.go`:

```go
func TestLoadTmuxCommandRejectsEmptyFirstElement(t *testing.T) {
	path := writeConfig(t, `
[tmux]
command = ["", "extra"]
`)
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(
		t, err.Error(),
		`config: invalid tmux.command`,
	)
}

func TestLoadTmuxCommandRejectsWhitespaceFirstElement(t *testing.T) {
	path := writeConfig(t, `
[tmux]
command = ["   ", "extra"]
`)
	_, err := Load(path)
	require.Error(t, err)
	require.Contains(
		t, err.Error(),
		`config: invalid tmux.command`,
	)
}
```

- [ ] **Step 1.8: Run validation tests to verify they fail**

Run: `go test ./internal/config -run TestLoadTmuxCommandRejects -shuffle=on`
Expected: FAIL — no validation exists; the config loads without error for both cases.

- [ ] **Step 1.9: Add validation in `Config.Validate`**

Edit `internal/config/config.go`. In the `Validate` method, add a new block alongside the other validation checks (e.g. just before `return nil`):

```go
if len(c.Tmux.Command) > 0 &&
	strings.TrimSpace(c.Tmux.Command[0]) == "" {
	return fmt.Errorf(
		"config: invalid tmux.command: first element must be non-empty",
	)
}
```

`strings` is already imported in `config.go`.

- [ ] **Step 1.10: Run validation test to verify it passes**

Run: `go test ./internal/config -run TestLoadTmuxCommandRejects -shuffle=on`
Expected: PASS.

- [ ] **Step 1.11: Write failing round-trip test for `Save()` preserving `[tmux]`**

`(*Config).Save` serializes via an internal `configFile` struct that enumerates a subset of fields. Without adding `Tmux` to it, every settings-UI action (`PUT /settings`, `POST /repos`, `DELETE /repos`) silently strips the operator's wrapper configuration from disk. Guard this with a test that saves and reloads:

Append to `internal/config/config_test.go`:

```go
func TestSavePreservesTmuxCommand(t *testing.T) {
	assert := Assert.New(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := &Config{
		SyncInterval:   "5m",
		GitHubTokenEnv: "MIDDLEMAN_GITHUB_TOKEN",
		Host:           "127.0.0.1",
		Port:           8091,
		DataDir:        dir,
		Activity:       Activity{ViewMode: "threaded", TimeRange: "7d"},
		Tmux: Tmux{
			Command: []string{"systemd-run", "--user", "--scope", "tmux"},
		},
	}
	require.NoError(t, cfg.Save(path))

	reloaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(
		[]string{"systemd-run", "--user", "--scope", "tmux"},
		reloaded.Tmux.Command,
	)
}
```

- [ ] **Step 1.12: Run the round-trip test to verify it fails**

Run: `go test ./internal/config -run TestSavePreservesTmux -shuffle=on`
Expected: FAIL — `configFile` does not include `Tmux`, so `Save()` drops the section and `reloaded.Tmux.Command` is empty.

- [ ] **Step 1.13: Extend `configFile` and `Save()` to round-trip the Tmux section**

Edit `internal/config/config.go`. Update the `configFile` struct to include the new field:

```go
// configFile is the subset of Config written to disk.
type configFile struct {
	SyncInterval      string   `toml:"sync_interval"`
	GitHubTokenEnv    string   `toml:"github_token_env"`
	Host              string   `toml:"host"`
	Port              int      `toml:"port"`
	SyncBudgetPerHour int      `toml:"sync_budget_per_hour,omitempty"`
	BasePath          string   `toml:"base_path,omitempty"`
	DataDir           string   `toml:"data_dir,omitempty"`
	Repos             []Repo   `toml:"repos"`
	Activity          Activity `toml:"activity"`
	Roborev           Roborev  `toml:"roborev,omitempty"`
	Tmux              Tmux     `toml:"tmux,omitempty"`
}
```

In `(*Config).Save`, populate the new field by adding one assignment inside the `configFile{...}` literal, right after `Roborev: c.Roborev,`:

```go
	f := configFile{
		SyncInterval:   c.SyncInterval,
		GitHubTokenEnv: c.GitHubTokenEnv,
		Host:           c.Host,
		Port:           c.Port,
		Repos:          c.Repos,
		Activity:       c.Activity,
		Roborev:        c.Roborev,
		Tmux:           c.Tmux,
	}
```

The existing conditional assignments below (`SyncBudgetPerHour`, `BasePath`, `DataDir`) stay unchanged.

- [ ] **Step 1.14: Run the round-trip test to verify it passes**

Run: `go test ./internal/config -run TestSavePreservesTmux -shuffle=on`
Expected: PASS.

- [ ] **Step 1.15: Run the full config package tests to confirm no regressions**

Run: `go test ./internal/config -shuffle=on`
Expected: PASS (all tests).

- [ ] **Step 1.16: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add tmux.command prefix with nil-safe helper"
```

---

## Task 2: Thread tmuxCmd through `workspace.Manager`

**Files:**
- Modify: `internal/workspace/manager.go`
- Test: `internal/workspace/manager_test.go`

**Summary of code changes in this task:**
- Add a `tmuxCmd []string` field to `Manager`.
- Add a `SetTmuxCommand([]string)` setter mirroring `SetClones`.
- Add a method `Manager.tmuxExec(ctx, extra ...string) *exec.Cmd` that builds the full argv (prefix + extra) and returns an `*exec.Cmd`, defaulting to `["tmux"]` when `m.tmuxCmd` is nil/empty. Returning `*exec.Cmd` (rather than a `[]string` callers index) keeps the first-argument access inside the helper so NilAway can prove safety.
- Add a package helper `runBuiltCmd(*exec.Cmd) error` that runs a pre-built command and wraps failures with the combined output (replaces the previous `runCmd` path, which is no longer needed).
- Add method versions of `EnsureTmux`, `tmuxSessionExists`, `newTmuxSession`, and `killTmuxSession` on `*Manager` that use the prefix. `tmuxSessionExists` returns `(bool, error)`, and a package helper `isTmuxSessionAbsent(stderr []byte, err error) bool` encodes the "session absent" contract: exit code 1 **and** stderr containing `can't find session` or `no server running`. Every other failure propagates via `EnsureTmux` as `"tmux has-session: %w"`.
- `tmuxSessionExists` must capture stdout and stderr into **separate** `bytes.Buffer`s (not `CombinedOutput`) so stdout content cannot spoof the tmux-absent signal.
- Update `Setup` (internal call site of `newTmuxSession`) and `Delete` (kill-session call) to use the new methods.
- **Keep the existing package-level `EnsureTmux`, `TmuxSessionExists`, and `newTmuxSession` functions untouched** in Task 2 — they are still called by `internal/terminal/handler.go:77`. Task 3 switches the terminal handler to the method form and removes the package functions at that point, so every commit on this branch builds cleanly and tests pass.

- [ ] **Step 2.1: Add the recording-script helper and write the failing test**

Append to `internal/workspace/manager_test.go`:

```go
// writeRecorderScript creates an executable shell script at a
// fresh path under t.TempDir() that appends the count and each
// argument, NUL-delimited, to TMUX_RECORD. Returns the script path
// and the record file path.
func writeRecorderScript(t *testing.T) (scriptPath, recordPath string) {
	t.Helper()
	dir := t.TempDir()
	recordPath = filepath.Join(dir, "record")
	scriptPath = filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", recordPath)
	return scriptPath, recordPath
}

// readRecorderArgv reads the NUL-delimited record file and returns
// each recorded invocation as a []string. Each invocation is stored
// as "<argc>\0<arg0>\0<arg1>...\0".
//
// The parser must be robust against two realities:
//   - Interior empty args (argc > 0 but one of the args is "") are
//     meaningful — the NUL framing exists precisely to preserve
//     them. Don't skip them.
//   - Async tests poll this file while the recorder is still
//     writing; if the last record is mid-write (argc parsed but
//     args not all flushed yet, or argc itself not yet a valid
//     integer), stop cleanly rather than panic. The next poll will
//     see a complete record.
func readRecorderArgv(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	// Split without TrimRight — TrimRight would nuke trailing empty
	// args. A flushed stream always ends with a trailing \0 so
	// Split produces one trailing empty element; strip exactly one.
	parts := strings.Split(string(data), "\x00")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	var out [][]string
	for i := 0; i < len(parts); {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			// Mid-write; next poll will see it.
			break
		}
		if i+1+n > len(parts) {
			// argc parsed but args not all on disk yet.
			break
		}
		i++
		argv := parts[i : i+n]
		out = append(out, argv)
		i += n
	}
	return out
}

func TestManagerEnsureTmuxHasSessionPrefix(t *testing.T) {
	assert := Assert.New(t)

	script, record := writeRecorderScript(t)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script, "wrap"})

	ctx := context.Background()

	// Script exits 0 for every invocation, so EnsureTmux observes
	// "session exists" after the has-session call and returns
	// without running new-session.
	require.NoError(t, mgr.EnsureTmux(ctx, "sess-A", t.TempDir()))

	argvs := readRecorderArgv(t, record)
	require.Len(t, argvs, 1)
	assert.Equal(
		[]string{"wrap", "has-session", "-t", "sess-A"},
		argvs[0],
	)
}

func TestManagerDeleteUsesTmuxPrefix(t *testing.T) {
	assert := Assert.New(t)

	script, record := writeRecorderScript(t)

	d := openTestDB(t)
	repoID := seedRepo(t, d, "github.com", "acme", "widget")
	seedMR(t, d, repoID, 42, "feature/thing")

	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script, "wrap"})

	ctx := context.Background()
	ws, err := mgr.Create(ctx, "github.com", "acme", "widget", 42)
	require.NoError(t, err)

	// force=true skips the dirty-files check. m.clones is nil, so
	// Delete takes the clones==nil short-circuit after killing the
	// tmux session — no git operations are required.
	_, err = mgr.Delete(ctx, ws.ID, true)
	require.NoError(t, err)

	// Delete invokes exactly one tmux command on this path
	// (kill-session). It ignores the exit code because the session
	// may not exist, but our script exits 0 so the invocation is
	// still recorded.
	argvs := readRecorderArgv(t, record)
	require.Len(t, argvs, 1)
	assert.Equal(
		[]string{"wrap", "kill-session", "-t", ws.TmuxSession},
		argvs[0],
	)
}

func TestManagerEnsureTmuxCreatesSessionOnMiss(t *testing.T) {
	assert := Assert.New(t)

	// Script: "has-session" emits tmux's canonical "can't find
	// session" stderr and exits 1 (so isTmuxSessionAbsent classifies
	// it as session-missing rather than wrapper failure); everything
	// else succeeds, so EnsureTmux calls newTmuxSession.
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	script := filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then` + "\n" +
		`    echo "can't find session: sim" >&2` + "\n" +
		`    exit 1` + "\n" +
		`  fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)

	d := openTestDB(t)
	mgr := NewManager(d, t.TempDir())
	mgr.SetTmuxCommand([]string{script})

	ctx := context.Background()
	require.NoError(t, mgr.EnsureTmux(ctx, "sess-B", "/tmp/cwd"))

	argvs := readRecorderArgv(t, record)
	require.Len(t, argvs, 2)
	assert.Equal(
		[]string{"has-session", "-t", "sess-B"},
		argvs[0],
	)
	// new-session argv: "new-session -d -s sess-B -c /tmp/cwd <shell> -l"
	// We check the prefix up to the shell; the shell resolves per
	// runtime so just assert it is non-empty and ends with "-l".
	require.GreaterOrEqual(t, len(argvs[1]), 8)
	assert.Equal("new-session", argvs[1][0])
	assert.Equal("-d", argvs[1][1])
	assert.Equal("-s", argvs[1][2])
	assert.Equal("sess-B", argvs[1][3])
	assert.Equal("-c", argvs[1][4])
	assert.Equal("/tmp/cwd", argvs[1][5])
	assert.NotEmpty(argvs[1][6])
	assert.Equal("-l", argvs[1][7])
}
```

Add these imports to `internal/workspace/manager_test.go` if not present:

```go
"os"
"strconv"
"strings"
```

- [ ] **Step 2.2: Run the new tests to verify they fail**

Run: `go test ./internal/workspace -run 'TestManager(EnsureTmux|DeleteUsesTmux)' -shuffle=on`
Expected: FAIL — `mgr.SetTmuxCommand` does not exist yet, and the method forms of `EnsureTmux` / the internal `killTmuxSession` have not been added, so `TestManagerDeleteUsesTmuxPrefix` cannot reach the prefix via the Delete code path.

- [ ] **Step 2.3: Add the `tmuxCmd` field and `SetTmuxCommand` setter**

Edit `internal/workspace/manager.go`. Add to the `Manager` struct:

```go
type Manager struct {
	db          *db.DB
	worktreeDir string
	clones      *gitclone.Manager
	tmuxCmd     []string
}
```

Add below `SetClones`:

```go
// SetTmuxCommand sets the command + argv prefix for every tmux
// invocation the manager issues. When nil/empty, the manager uses
// ["tmux"] — preserving today's behavior.
func (m *Manager) SetTmuxCommand(cmd []string) {
	m.tmuxCmd = append([]string(nil), cmd...)
}

// tmuxExec builds an *exec.Cmd for a tmux invocation: the
// configured prefix + extra args. Defaults to ["tmux"] when
// unconfigured. Returning the *exec.Cmd directly (rather than a
// []string that callers index) keeps the first-element access
// inside this function where the branch structure makes it
// statically safe — NilAway cannot prove safety through an indexed
// slice return.
func (m *Manager) tmuxExec(
	ctx context.Context, extra ...string,
) *exec.Cmd {
	if len(m.tmuxCmd) == 0 {
		return exec.CommandContext(ctx, "tmux", extra...)
	}
	args := make([]string, 0, len(m.tmuxCmd)-1+len(extra))
	args = append(args, m.tmuxCmd[1:]...)
	args = append(args, extra...)
	return exec.CommandContext(ctx, m.tmuxCmd[0], args...)
}
```

Also add a package-level helper for running the pre-built command
(replaces the old `runCmd(ctx, dir, name, args...)` path; the two
callers that used it in this file now route through `tmuxExec`):

```go
// runBuiltCmd runs a pre-built exec.Cmd and wraps any failure with
// the combined output. Used for tmux invocations whose *exec.Cmd is
// assembled by tmuxExec so argv[0] access stays inside that helper.
func runBuiltCmd(cmd *exec.Cmd) error {
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"%w: %s", err, strings.TrimSpace(string(out)),
		)
	}
	return nil
}
```

- [ ] **Step 2.4: Add method forms, keep the package functions, and migrate internal callers**

Edit `internal/workspace/manager.go`. **Do not delete** the package-level `EnsureTmux`, `TmuxSessionExists`, or `newTmuxSession`. They stay until Task 3, when the last external caller (`internal/terminal/handler.go`) migrates and Task 3 removes them.

**Add** a method form of `EnsureTmux` on `*Manager`. Method and package function coexist; both remain exported. The method uses the configured prefix and propagates non-absent errors:

```go
// EnsureTmux creates a tmux session if it does not already exist,
// using the manager's configured tmux command prefix. Errors from
// has-session that are not tmux's canonical "session missing"
// signal propagate — a broken wrapper should surface here rather
// than be masked by a subsequent new-session through the same
// wrapper.
func (m *Manager) EnsureTmux(
	ctx context.Context, session, cwd string,
) error {
	exists, err := m.tmuxSessionExists(ctx, session)
	if err != nil {
		return fmt.Errorf("tmux has-session: %w", err)
	}
	if exists {
		return nil
	}
	return m.newTmuxSession(ctx, session, cwd)
}
```

**Add** a method form of `newTmuxSession` on `*Manager`. The existing package-level `newTmuxSession` stays; the method lives alongside it with a different (method) signature so there is no name clash:

```go
func (m *Manager) newTmuxSession(
	ctx context.Context, session, cwd string,
) error {
	shell := userLoginShell()
	cmd := m.tmuxExec(
		ctx,
		"new-session", "-d",
		"-s", session,
		"-c", cwd,
		shell, "-l",
	)
	return runBuiltCmd(cmd)
}
```

**Add** an unexported method `tmuxSessionExists` on `*Manager`. It returns `(bool, error)` — the boolean answers "is the session there" only when the command actually told us, and any other failure becomes the error. Stdout and stderr are captured **separately** so the absent-session stderr-only match is not fooled by a wrapper emitting the phrase on stdout. The existing exported package function `TmuxSessionExists` stays:

```go
// tmuxSessionExists runs `tmux has-session` and distinguishes a
// genuine "session absent" signal from a wrapper/binary failure.
// See isTmuxSessionAbsent for the exact contract.
func (m *Manager) tmuxSessionExists(
	ctx context.Context, session string,
) (bool, error) {
	cmd := m.tmuxExec(ctx, "has-session", "-t", session)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if isTmuxSessionAbsent(stderr.Bytes(), err) {
		return false, nil
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = strings.TrimSpace(stdout.String())
	}
	return false, fmt.Errorf("%w: %s", err, msg)
}

// isTmuxSessionAbsent reports whether a has-session failure is
// tmux's documented "session does not exist" signal. Must be both
// exit code 1 AND one of tmux's specific stderr phrases. Plain
// exit 1 is a common generic wrapper/shell failure code, and
// stdout content is not load-bearing — a wrapper could emit
// anything there for unrelated reasons.
func isTmuxSessionAbsent(stderr []byte, err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		return false
	}
	msg := string(stderr)
	return strings.Contains(msg, "can't find session") ||
		strings.Contains(msg, "no server running")
}
```

Add `"bytes"` and `"errors"` to `internal/workspace/manager.go` imports if not already present.

**Add** an unexported method `killTmuxSession` on `*Manager` (no existing package equivalent):

```go
// killTmuxSession kills a tmux session via the manager's prefix.
// Errors are returned rather than logged — callers decide whether
// to ignore them (Delete ignores; tests assert).
func (m *Manager) killTmuxSession(
	ctx context.Context, session string,
) error {
	return runBuiltCmd(m.tmuxExec(ctx, "kill-session", "-t", session))
}
```

**Update** `Setup` — switch its internal call from the package-level `newTmuxSession` to the method. Change:

```go
	err = newTmuxSession(ctx, ws.TmuxSession, ws.WorktreePath)
```

to:

```go
	err = m.newTmuxSession(ctx, ws.TmuxSession, ws.WorktreePath)
```

**Update** `Delete` — switch its inline `runCmd(..., "tmux", "kill-session", ...)` to the method. Change:

```go
	// Kill tmux session (ignore errors -- session may not exist).
	_ = runCmd(
		ctx, "",
		"tmux", "kill-session", "-t", ws.TmuxSession,
	)
```

to:

```go
	// Kill tmux session (ignore errors -- session may not exist).
	_ = m.killTmuxSession(ctx, ws.TmuxSession)
```

After this step, both forms coexist:
- Methods on `*Manager` (prefix-aware) — used by `Setup`, `Delete`, and the new tests.
- Package-level `EnsureTmux`, `TmuxSessionExists`, `newTmuxSession` — still hard-coded to `"tmux"`, still called only by `internal/terminal/handler.go:77` until Task 3 migrates it.

- [ ] **Step 2.5: Run workspace tests to verify the new tests pass**

Run: `go test ./internal/workspace -run 'TestManager(EnsureTmux|DeleteUsesTmux)' -shuffle=on`
Expected: PASS (3 tests: `TestManagerEnsureTmuxHasSessionPrefix`, `TestManagerEnsureTmuxCreatesSessionOnMiss`, `TestManagerDeleteUsesTmuxPrefix`).

- [ ] **Step 2.6: Run full workspace package tests to confirm no regressions**

Run: `go test ./internal/workspace -shuffle=on`
Expected: PASS — the package-level functions still exist and existing tests (which don't call them directly) continue to work.

- [ ] **Step 2.7: Build the whole module to confirm no cross-package break**

Run: `go build ./...`
Expected: success. `terminal/handler.go` still compiles because `workspace.EnsureTmux` (the package function) is untouched in this task.

- [ ] **Step 2.8: Commit**

```bash
git add internal/workspace/manager.go internal/workspace/manager_test.go
git commit -m "feat(workspace): thread tmux command prefix through Manager"
```

---

## Task 3: Thread tmux command prefix through `terminal.Handler` and retire package-level tmux helpers

**Files:**
- Modify: `internal/terminal/handler.go`
- Modify: `internal/workspace/manager.go`

No new test file — the attach-session path is covered by the server-level e2e in Task 5. Existing `handler_test.go` cases (workspace-not-found, workspace-not-ready) do not reach the `attach-session` exec and remain passing unchanged.

This task also removes the now-unused package-level `EnsureTmux`, `TmuxSessionExists`, and `newTmuxSession` functions that Task 2 intentionally left in place for bisectability. Once the terminal handler switches to `h.Workspaces.EnsureTmux`, nothing outside the `workspace` package references them, and `Manager.newTmuxSession` (method) replaces the internal package function.

- [ ] **Step 3.1: Add the `TmuxCommand` field to `Handler`**

Edit `internal/terminal/handler.go`. Update the `Handler` struct:

```go
// Handler serves WebSocket connections that bridge a
// PTY-attached tmux session to the browser.
type Handler struct {
	Workspaces  *workspace.Manager
	TmuxCommand []string
}
```

- [ ] **Step 3.2: Switch to `h.Workspaces.EnsureTmux` and build attach from the prefix**

Edit `internal/terminal/handler.go`. Locate the block:

```go
	if tmuxErr := workspace.EnsureTmux(
		ctx, ws.TmuxSession, ws.WorktreePath,
	); tmuxErr != nil {
		slog.Error("ensure tmux", "err", tmuxErr)
		conn.Close(
			websocket.StatusInternalError,
			"failed to start tmux",
		)
		return
	}

	cmd := exec.CommandContext(
		ctx, "tmux", "attach-session", "-t", ws.TmuxSession,
	)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
```

Replace with:

```go
	if tmuxErr := h.Workspaces.EnsureTmux(
		ctx, ws.TmuxSession, ws.WorktreePath,
	); tmuxErr != nil {
		slog.Error("ensure tmux", "err", tmuxErr)
		conn.Close(
			websocket.StatusInternalError,
			"failed to start tmux",
		)
		return
	}

	prefix := h.TmuxCommand
	if len(prefix) == 0 {
		prefix = []string{"tmux"}
	}
	argv := make([]string, 0, len(prefix)+3)
	argv = append(argv, prefix...)
	argv = append(argv, "attach-session", "-t", ws.TmuxSession)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
```

- [ ] **Step 3.3: Remove the now-unused package-level tmux functions**

Edit `internal/workspace/manager.go`. Delete the package-level `EnsureTmux`, `TmuxSessionExists`, and `newTmuxSession` functions that Task 2 preserved. With the terminal handler now calling `h.Workspaces.EnsureTmux`, these have no remaining callers. The method forms on `*Manager` stay.

- [ ] **Step 3.4: Run full terminal and workspace package tests to confirm no regressions**

Run: `go test ./internal/terminal ./internal/workspace -shuffle=on`
Expected: PASS — existing handler tests (not-found, not-ready) short-circuit before `EnsureTmux` and `attach-session`; workspace tests use only the exported method API.

- [ ] **Step 3.5: Build the whole module to confirm nothing else referenced the removed functions**

Run: `go build ./...`
Expected: success. If build fails with "undefined: workspace.EnsureTmux" (or similar), search for any missed caller with `grep -rn "workspace\.EnsureTmux\|workspace\.TmuxSessionExists" internal/` and migrate it to the method form.

- [ ] **Step 3.6: Commit**

```bash
git add internal/terminal/handler.go internal/workspace/manager.go
git commit -m "feat(terminal): use workspace.Manager.EnsureTmux and configurable attach prefix"
```

---

## Task 4: Wire `cfg.TmuxCommand()` in `newServer`

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 4.1: Populate both the manager and the handler from `cfg`**

Edit `internal/server/server.go`. Locate the block around line 290-303 (inside `newServer`):

```go
	if options.WorktreeDir != "" {
		s.workspaces = workspace.NewManager(database, options.WorktreeDir)
		if clones != nil {
			s.workspaces.SetClones(clones)
		}
	}

	if s.workspaces != nil {
		termHandler := &terminal.Handler{Workspaces: s.workspaces}
		mux.Handle(
			"GET /api/v1/workspaces/{id}/terminal",
			termHandler,
		)
	}
```

Replace with:

```go
	if options.WorktreeDir != "" {
		s.workspaces = workspace.NewManager(database, options.WorktreeDir)
		s.workspaces.SetTmuxCommand(cfg.TmuxCommand())
		if clones != nil {
			s.workspaces.SetClones(clones)
		}
	}

	if s.workspaces != nil {
		termHandler := &terminal.Handler{
			Workspaces:  s.workspaces,
			TmuxCommand: cfg.TmuxCommand(),
		}
		mux.Handle(
			"GET /api/v1/workspaces/{id}/terminal",
			termHandler,
		)
	}
```

`cfg.TmuxCommand()` is nil-safe (from Task 1), so this works whether `cfg` is nil or populated. Two calls intentionally produce two independent copies so the manager and handler each own their slice.

- [ ] **Step 4.2: Run server tests to confirm no regressions**

Run: `go test ./internal/server -shuffle=on`
Expected: PASS — all existing tests pass with the default `["tmux"]` prefix because `cfg.TmuxCommand()` returns `["tmux"]` for nil cfg and for cfg without a `[tmux]` section.

- [ ] **Step 4.3: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(server): wire tmux.command config into workspace and terminal"
```

---

## Task 5: Server-level e2e — happy-path tmux verbs

**Files:**
- Create: `internal/server/tmux_wrapper_test.go`

Cover the three happy-path tmux verbs that cross the HTTP boundary with a recording script standing in for the real `tmux` binary:

- `new-session` via `POST /api/v1/workspaces`
- `attach-session` via the terminal WebSocket
  (`GET /api/v1/workspaces/{id}/terminal`)
- `kill-session` via `DELETE /api/v1/workspaces/{id}`

The non-GET routes need a `Content-Type: application/json` header even when the body is absent (the server's CSRF guard rejects bare `DELETE` otherwise). The e2e helper wraps `http.DefaultTransport` with a `roundTripFunc` that adds the header on every non-GET request.

Task 6 extends this file with the wrapper-failure surface tests; Task 7 adds the Save-round-trip test in `internal/server/settings_test.go`. Splitting into three commits keeps each reviewable and lets a bisect land on the exact stage that broke.

- [ ] **Step 5.1: Add the new-session e2e test**

Task 5 follows Tasks 1-4 which have already wired the prefix end-to-end, so this test is expected to PASS on first run. We add it not for TDD but as a regression gate: if a future edit breaks the wiring at any of the four layers, this test fails loudly at the HTTP boundary.

Create `internal/server/tmux_wrapper_test.go`:

```go
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/middleman/internal/apiclient"
	"github.com/wesm/middleman/internal/apiclient/generated"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/gitclone"
)

// writeTmuxRecorder creates an executable fake-tmux script at a
// fresh temp path. The script appends NUL-delimited argv to
// record. It exits 1 when invoked as "has-session" (so EnsureTmux
// proceeds to new-session), 0 otherwise. Returns the script path
// and the record path.
func writeTmuxRecorder(t *testing.T) (script, record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record")
	script = filepath.Join(dir, "fake-tmux")
	body := "#!/bin/sh\n" +
		`printf '%s\0' "$#" "$@" >> "$TMUX_RECORD"` + "\n" +
		`for a in "$@"; do` + "\n" +
		`  if [ "$a" = "has-session" ]; then exit 1; fi` + "\n" +
		"done\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o755))
	t.Setenv("TMUX_RECORD", record)
	return script, record
}

func readTmuxRecord(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	var out [][]string
	for i := 0; i < len(parts); {
		if parts[i] == "" {
			i++
			continue
		}
		n, err := strconv.Atoi(parts[i])
		require.NoError(t, err)
		i++
		argv := parts[i : i+n]
		out = append(out, argv)
		i += n
	}
	return out
}

// setupWrapperServer constructs a full server wired with a
// recording-script tmux command, a bare repo, and a seeded PR.
// Returns a generated API client pointed at the httptest server,
// the httptest baseURL (needed for WebSocket dialing), and the
// record-file path.
func setupWrapperServer(t *testing.T) (client *apiclient.Client, baseURL, record string) {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e tests skipped in short mode")
	}

	script, record := writeTmuxRecorder(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bareDir := filepath.Join(dir, "clones")
	require.NoError(t, os.MkdirAll(bareDir, 0o755))
	bare := filepath.Join(
		bareDir, "github.com", "acme", "widget.git",
	)
	tmpWork := filepath.Join(dir, "work")
	runGit(t, dir, "init", "--bare", "--initial-branch=main", bare)
	runGit(t, dir, "clone", bare, tmpWork)
	runGit(t, tmpWork, "config", "user.email", "test@test.com")
	runGit(t, tmpWork, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpWork, "base.txt"),
		[]byte("base\n"), 0o644,
	))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "base commit")
	runGit(t, tmpWork, "push", "origin", "main")
	runGit(t, tmpWork, "checkout", "-b", "feature")
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpWork, "new.txt"),
		[]byte("new\n"), 0o644,
	))
	runGit(t, tmpWork, "add", ".")
	runGit(t, tmpWork, "commit", "-m", "feature commit")
	runGit(t, tmpWork, "push", "origin", "feature")

	clones := gitclone.New(bareDir, nil)
	worktreeDir := filepath.Join(dir, "worktrees")

	repos := []ghclient.RepoRef{
		{Owner: "acme", Name: "widget", PlatformHost: "github.com"},
	}
	mock := &mockGH{}
	syncer := ghclient.NewSyncer(
		map[string]ghclient.Client{"github.com": mock},
		database, nil, repos, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)

	cfg := &config.Config{
		Tmux: config.Tmux{
			Command: []string{script, "wrap"},
		},
	}
	srv := New(database, syncer, nil, "/", cfg, ServerOptions{
		Clones:      clones,
		WorktreeDir: worktreeDir,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(
			context.Background(), 5*time.Second,
		)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	seedPR(t, database, "acme", "widget", 1)

	// Real listener — WebSocket Dial needs a real TCP endpoint.
	// The generated API client also points at this URL rather than
	// the in-process roundtripper used elsewhere, because we cannot
	// split HTTP and WebSocket transports per-request.
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	client, err = apiclient.NewWithHTTPClient(ts.URL, http.DefaultClient)
	require.NoError(t, err)

	return client, ts.URL, record
}

func TestTmuxWrapperNewSession(t *testing.T) {
	assert := Assert.New(t)
	client, _, record := setupWrapperServer(t)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, createResp.StatusCode())
	require.NotNil(t, createResp.JSON202)

	// Workspace setup runs asynchronously. Poll the record file
	// until the new-session invocation shows up, up to ~5s.
	var argvs [][]string
	require.Eventually(
		t,
		func() bool {
			argvs = readTmuxRecord(t, record)
			for _, argv := range argvs {
				if len(argv) >= 2 && argv[1] == "new-session" {
					return true
				}
			}
			return false
		},
		5*time.Second, 50*time.Millisecond,
		"new-session argv not recorded",
	)

	var newSession []string
	for _, argv := range argvs {
		if len(argv) >= 2 && argv[1] == "new-session" {
			newSession = argv
			break
		}
	}

	// "wrap" prefix, then "new-session -d -s <id> -c <path> <shell> -l"
	require.GreaterOrEqual(t, len(newSession), 9)
	assert.Equal("wrap", newSession[0])
	assert.Equal("new-session", newSession[1])
	assert.Equal("-d", newSession[2])
	assert.Equal("-s", newSession[3])
	assert.NotEmpty(newSession[4])
	assert.Equal("-c", newSession[5])
	assert.NotEmpty(newSession[6])
	assert.NotEmpty(newSession[7])
	assert.Equal("-l", newSession[8])
}
```

- [ ] **Step 5.2: Run new-session e2e and confirm it passes**

Run: `go test ./internal/server -run TestTmuxWrapperNewSession -shuffle=on`
Expected: PASS — Tasks 1-4 wired the prefix end-to-end; this e2e confirms the wiring.

If it FAILS, inspect the recorded argv via `cat $record` (where `$record` is in a `t.TempDir()` — use `t.Logf` in the test if needed). Common causes: missing `SetTmuxCommand` call in Task 4, prefix not reaching `newTmuxSession` in Task 2, `cfg` arriving as a non-nil with empty `Tmux.Command` (still works, just yields `["tmux"]`).

- [ ] **Step 5.3: Add the attach-session e2e test**

Same caveat as step 5.1: this test guards the attach-session wiring rather than driving TDD. It is expected to PASS on first run.

Append to `internal/server/tmux_wrapper_test.go`:

```go
func TestTmuxWrapperAttachSession(t *testing.T) {
	assert := Assert.New(t)
	client, baseURL, record := setupWrapperServer(t)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, createResp.StatusCode())
	require.NotNil(t, createResp.JSON202)
	wsID := createResp.JSON202.Id

	// Poll for status == "ready".
	require.Eventually(
		t,
		func() bool {
			getResp, getErr := client.HTTP.GetWorkspacesByIdWithResponse(
				ctx, wsID,
			)
			if getErr != nil || getResp.JSON200 == nil {
				return false
			}
			return getResp.JSON200.Status == "ready"
		},
		5*time.Second, 50*time.Millisecond,
	)

	// Connect to the WebSocket terminal endpoint using the
	// httptest baseURL (the generated client cannot upgrade to
	// WebSocket, so we dial directly with coder/websocket).
	wsURL := strings.Replace(baseURL, "http://", "ws://", 1) +
		"/api/v1/workspaces/" + wsID + "/terminal"
	dialCtx, dialCancel := context.WithTimeout(
		ctx, 3*time.Second,
	)
	defer dialCancel()
	u, err := url.Parse(wsURL)
	require.NoError(t, err)
	conn, httpResp, err := websocket.Dial(
		dialCtx, u.String(), nil,
	)
	require.NoError(t, err)
	if httpResp != nil && httpResp.Body != nil {
		httpResp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// The recording script exits 0 immediately, so the PTY
	// closes and the handler sends an "exited" message. Read
	// until the connection closes or 3s elapses.
	readCtx, readCancel := context.WithTimeout(
		ctx, 3*time.Second,
	)
	defer readCancel()
	for {
		_, _, readErr := conn.Read(readCtx)
		if readErr != nil {
			break
		}
	}

	// The recorded argv should contain an attach-session invocation
	// with our "wrap" prefix.
	var attach []string
	for _, argv := range readTmuxRecord(t, record) {
		if len(argv) >= 2 && argv[1] == "attach-session" {
			attach = argv
			break
		}
	}
	require.NotNil(t, attach, "attach-session argv not recorded")
	require.Len(t, attach, 4)
	assert.Equal("wrap", attach[0])
	assert.Equal("attach-session", attach[1])
	assert.Equal("-t", attach[2])
	assert.NotEmpty(attach[3])
}
```

- [ ] **Step 5.4: Run attach-session e2e and confirm it passes**

Run: `go test ./internal/server -run TestTmuxWrapperAttachSession -shuffle=on`
Expected: PASS — the attach path uses `h.TmuxCommand` wired in Task 4.

If it FAILS with a timeout waiting for `status == "ready"`, the likely cause is that workspace Setup is erroring out — inspect the recorded argv with `t.Logf("%#v", readTmuxRecord(t, record))` and check whether the recording script is being called as expected. If it FAILS on the WebSocket dial, confirm the httptest server URL and the ws:// scheme conversion.

- [ ] **Step 5.5: Add the kill-session e2e test**

DELETE without a body is rejected by the server's CSRF guard as 415 Unsupported Media Type unless the client sends `Content-Type: application/json`. Wrap the HTTP transport used by the generated client with a `roundTripFunc` that injects the header on non-GET requests, and add a test that creates a workspace, waits for ready, then DELETEs it and asserts the wrapper reached `kill-session`:

```go
func TestTmuxWrapperKillSession(t *testing.T) {
	assert := Assert.New(t)
	client, _, record := setupWrapperServer(t)
	ctx := context.Background()

	createResp, err := client.HTTP.CreateWorkspaceWithResponse(
		ctx,
		generated.CreateWorkspaceInputBody{
			PlatformHost: "github.com",
			Owner:        "acme",
			Name:         "widget",
			MrNumber:     1,
		},
	)
	require.NoError(t, err)
	wsID := createResp.JSON202.Id

	require.Eventually(t, func() bool {
		r, _ := client.HTTP.GetWorkspacesByIdWithResponse(ctx, wsID)
		return r != nil && r.JSON200 != nil && r.JSON200.Status == "ready"
	}, 5*time.Second, 50*time.Millisecond)

	force := true
	delResp, err := client.HTTP.DeleteWorkspaceWithResponse(
		ctx, wsID, &generated.DeleteWorkspaceParams{Force: &force},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, delResp.StatusCode())

	var kill []string
	for _, argv := range readTmuxRecord(t, record) {
		if len(argv) >= 2 && argv[1] == "kill-session" {
			kill = argv
			break
		}
	}
	require.NotNil(t, kill, "kill-session argv not recorded")
	require.Len(t, kill, 4)
	assert.Equal("wrap", kill[0])
	assert.Equal("kill-session", kill[1])
}
```

`setupWrapperServer` must set up the `Content-Type`-injecting transport when building the `apiclient`. Example wrapper:

```go
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

httpClient := &http.Client{
	Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
		return http.DefaultTransport.RoundTrip(req)
	}),
}
client, err := apiclient.NewWithHTTPClient(ts.URL, httpClient)
```

Run: `go test ./internal/server -run TestTmuxWrapperKillSession -shuffle=on`
Expected: PASS.

- [ ] **Step 5.6: Run full server package tests to confirm no regressions**

Run: `go test ./internal/server -shuffle=on`
Expected: PASS.

- [ ] **Step 5.7: Commit**

```bash
git add internal/server/tmux_wrapper_test.go
git commit -m "test(server): happy-path e2e for wrapped tmux verbs"
```

---

## Task 6: Server-level e2e — has-session wrapper-failure surface

**Files:**
- Modify: `internal/server/tmux_wrapper_test.go`

Task 5 proved the happy-path prefix reaches every verb. This task proves the has-session failure contract holds at the HTTP layer: a wrapper failing has-session in any of the three non-absent shapes must close the attach WebSocket with `websocket.StatusInternalError` rather than silently proceeding to `new-session` through the same broken wrapper.

- [ ] **Step 6.1: Add the shared attach-failure helper**

Append to `internal/server/tmux_wrapper_test.go`:

```go
// attachWebsocketAndExpectInternalError writes scriptBody to a
// fake-tmux in t.TempDir(), sets $TMUX_RECORD, wires the server
// via setupWrapperServerWithScript, creates a workspace, waits for
// ready, dials the terminal WebSocket, and asserts conn.Read
// returns a CloseError with websocket.StatusInternalError.
func attachWebsocketAndExpectInternalError(t *testing.T, scriptBody string) {
	t.Helper()
	// ... write script, env, server setup, Create, wait for ready,
	// Dial ws://.../terminal, conn.Read — expect
	// websocket.CloseStatus(err) == websocket.StatusInternalError.
}
```

- [ ] **Step 6.2: Add one test per failure shape**

Each test body supplies a `#!/bin/sh` script that succeeds on new-session (so Setup finishes) but fails has-session in exactly one of the three shapes the contract must surface:

- `TestTmuxWrapperAttachSurfacesWrapperFailure` — has-session exits 127 (non-1 ExitCode, wrapper ENOEXEC-style).
- `TestTmuxWrapperAttachSurfacesExit1Failure` — has-session prints `"wrapper failed"` to stderr and exits 1 (exit 1 without tmux's canonical stderr).
- `TestTmuxWrapperAttachIgnoresAbsencePhraseOnStdout` — has-session prints `"can't find session: sim"` to stdout, prints an unrelated line to stderr, exits 1 (tmux phrase routed to stdout — must not trigger the absent-session branch).

Each test calls `attachWebsocketAndExpectInternalError(t, body)`.

Run: `go test ./internal/server -run "TestTmuxWrapperAttach(SurfacesWrapperFailure|SurfacesExit1Failure|IgnoresAbsencePhraseOnStdout)" -shuffle=on`
Expected: PASS (3 tests).

- [ ] **Step 6.3: Run full server package tests**

Run: `go test ./internal/server -shuffle=on`
Expected: PASS.

- [ ] **Step 6.4: Commit**

```bash
git add internal/server/tmux_wrapper_test.go
git commit -m "test(server): wrapper-failure surface at attach WebSocket"
```

---

## Task 7: Server-level e2e — `[tmux]` preserved across settings save

**Files:**
- Modify: `internal/server/settings_test.go`

A single mutation route regression-gates the entire Save path — all three mutation handlers (`PUT /api/v1/settings`, `POST /api/v1/repos`, `DELETE /api/v1/repos`) call the same `(*Config).Save`. `PUT /api/v1/settings` has the smallest request body and is the cheapest canary.

- [ ] **Step 7.1: Add the preservation test**

Append to `internal/server/settings_test.go`:

```go
func TestHandleUpdateSettingsPreservesTmuxCommand(t *testing.T) {
	srv, _, cfgPath := setupTestServerWithConfigContent(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091
[[repos]]
owner = "acme"
name = "widget"
[tmux]
command = ["systemd-run", "--user", "--scope", "tmux"]
`, &mockGH{})

	body := updateSettingsRequest{Activity: config.Activity{
		ViewMode:  "threaded",
		TimeRange: "30d",
	}}
	rr := doJSON(t, srv, http.MethodPut, "/api/v1/settings", body)
	require.Equal(t, http.StatusOK, rr.Code)

	reloaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Equal(t,
		[]string{"systemd-run", "--user", "--scope", "tmux"},
		reloaded.Tmux.Command,
	)
}
```

Run: `go test ./internal/server -run TestHandleUpdateSettingsPreservesTmuxCommand -shuffle=on`
Expected: PASS.

- [ ] **Step 7.2: Run the whole suite as a final gate**

Run: `make test`
Expected: PASS.

- [ ] **Step 7.3: Commit**

```bash
git add internal/server/settings_test.go
git commit -m "test(server): settings save preserves [tmux] section"
```

---

## Completion checklist

After Task 7, the branch should:
- Add a `[tmux] command = [...]` config section parsed by BurntSushi/toml, with validation rejecting an empty or whitespace-only first element.
- Round-trip `[tmux]` through `(*Config).Save` — every settings mutation route (`PUT /api/v1/settings`, `POST /api/v1/repos`, `DELETE /api/v1/repos`) shares this path, so `[tmux]` is not silently erased by any of them.
- Thread the prefix through `workspace.Manager` and `terminal.Handler` via `newServer`.
- Change every one of the four tmux exec call sites to use the prefix.
- Enforce the has-session failure contract: only `exit 1 + stderr containing "can't find session" or "no server running"` is treated as session-absent. Wrapper misconfiguration (exec errors, other exit codes, exit 1 without the stderr match, or the tmux phrase only on stdout) propagates through `EnsureTmux` as `"tmux has-session: %w"`.
- Leave default behavior (`["tmux"]`) unchanged when the config is omitted.
- Cover the new behavior with unit tests (`internal/config`, `internal/workspace`) and full-stack e2e tests (`internal/server`) including the wrapper-failure surface at the terminal WebSocket layer.

Run `make test` as the final verification. Once green, hand back to the user for review.
