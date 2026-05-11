// Package projects holds project-registry helpers that sit between the
// generic db layer and the HTTP/Huma handlers. The package owns identity
// resolution from local repository paths and any other non-trivial logic
// that does not belong directly in the db package.
package projects

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitenv"
)

// ResolveIdentityFromPath runs `git remote get-url origin` in path and parses
// the result into an optional PlatformIdentity. Returns (nil, nil) when no
// origin remote is configured, when path is not a git repository, or when
// the URL does not match a recognized pattern. The contract is identity-
// only: validation that path exists and is a git repository is the caller's
// responsibility. Per the consolidation spec's Decision 7, unknown identity
// is allowed and does not block registration.
func ResolveIdentityFromPath(ctx context.Context, path string) (*db.PlatformIdentity, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = abs
	cmd.Env = gitenv.StripAll(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// `git remote get-url origin` exits non-zero when origin is
			// unset; treat as a clean no-identity result.
			return nil, nil
		}
		return nil, fmt.Errorf("git remote get-url origin: %w", err)
	}
	return ParseRemoteURL(string(out)), nil
}

// ParseRemoteURL recognizes the common remote URL shapes (HTTPS, SSH SCP-style,
// ssh://) and extracts a PlatformIdentity. Returns nil for anything it does
// not recognize, including local paths, file:// URLs, and remotes whose path
// does not contain exactly two segments (owner/name).
func ParseRemoteURL(remote string) *db.PlatformIdentity {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return nil
	}

	host, path := splitRemote(remote)
	if host == "" || path == "" {
		return nil
	}

	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		return nil
	}
	owner, name := parts[0], parts[1]
	if owner == "" || name == "" {
		return nil
	}

	return &db.PlatformIdentity{
		Host:  strings.ToLower(host),
		Owner: strings.ToLower(owner),
		Name:  strings.ToLower(name),
	}
}

// splitRemote separates a remote URL into (host, path) without hard-coding
// any specific platform. It accepts the three common shapes:
//
//	scheme://[user@]host[:port]/path
//	user@host:path     (SCP-style)
//	host:path          (rejected — no user@ disambiguator from local paths)
func splitRemote(remote string) (host, path string) {
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return "", ""
		}
		if u.Scheme == "file" {
			return "", ""
		}
		return u.Hostname(), strings.TrimPrefix(u.Path, "/")
	}

	atIdx := strings.Index(remote, "@")
	colonIdx := strings.Index(remote, ":")
	if atIdx > 0 && colonIdx > atIdx {
		return remote[atIdx+1 : colonIdx], remote[colonIdx+1:]
	}
	return "", ""
}
