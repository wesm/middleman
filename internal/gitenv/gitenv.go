// Package gitenv centralizes stripping of inherited GIT_* environment
// variables that bind a child git process to a parent git context.
//
// When middleman code (production helpers or test fixtures) shells out
// to git from inside a git hook, a worktree tooling wrapper, or a
// `go test` run invoked by a commit hook, the outer git exports
// variables like GIT_DIR, GIT_WORK_TREE, and GIT_CONFIG_* into the
// child environment. Those variables take precedence over cmd.Dir and
// override any local config paths. A child `git config user.email foo`
// will then write to the parent repo's .git/config rather than the
// intended target, silently contaminating the hosting repository.
//
// Callers must use [StripInherited] before spawning git and re-append
// the explicit variables they need on top.
package gitenv

import "strings"

// StripInherited returns env with every GIT_* variable that could
// rebind a child git process to an inherited parent context removed.
// It filters four categories:
//
//   - Repo context: GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE,
//     GIT_OBJECT_DIRECTORY, GIT_ALTERNATE_OBJECT_DIRECTORIES,
//     GIT_COMMON_DIR, GIT_NAMESPACE, GIT_PREFIX.
//   - Config injection: every variable with a GIT_CONFIG prefix
//     (GIT_CONFIG, GIT_CONFIG_COUNT, GIT_CONFIG_PARAMETERS,
//     GIT_CONFIG_GLOBAL, GIT_CONFIG_SYSTEM, GIT_CONFIG_NOSYSTEM, ...).
//   - Author/committer identity: GIT_AUTHOR_*, GIT_COMMITTER_*.
//   - Interactive/credential helpers: GIT_ASKPASS, GIT_SSH_COMMAND,
//     SSH_ASKPASS.
//
// GIT_TRACE* and other diagnostic or transport variables (proxy,
// SSL, HTTP) are preserved so that developers can still observe
// child git behavior without losing visibility.
func StripInherited(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if isInherited(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// StripAll removes every GIT_* variable and SSH_ASKPASS from env.
// Use this in test fixtures that build throwaway repos and want
// full isolation from the host environment. Unlike [StripInherited],
// it also removes transport/diagnostic variables (GIT_SSL_*,
// GIT_TRACE*, GIT_DEFAULT_HASH, etc.) that could change the shape
// of a freshly initialized repo.
func StripAll(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if strings.HasPrefix(key, "GIT_") || key == "SSH_ASKPASS" {
			continue
		}
		out = append(out, e)
	}
	return out
}

func isInherited(e string) bool {
	key, _, _ := strings.Cut(e, "=")
	switch key {
	case "GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_INDEX_FILE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_NAMESPACE",
		"GIT_PREFIX",
		"GIT_ASKPASS",
		"GIT_SSH_COMMAND",
		"SSH_ASKPASS":
		return true
	}
	if strings.HasPrefix(key, "GIT_CONFIG") ||
		strings.HasPrefix(key, "GIT_AUTHOR_") ||
		strings.HasPrefix(key, "GIT_COMMITTER_") {
		return true
	}
	return false
}
