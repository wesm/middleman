package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Divergence reports how the worktree's HEAD has drifted from its
// upstream tracking branch.
type Divergence struct {
	Ahead  int // commits on HEAD not on upstream
	Behind int // commits on upstream not on HEAD
}

// WorktreeDivergence computes ahead/behind counts between the
// worktree at dir and its `@{upstream}` tracking branch.
//
// The second return value is false when the branch has no upstream
// configured — that is a normal state for fresh issue branches and
// not an error. The error return is reserved for unexpected git
// failures (missing worktree, command crashed, parse failure).
func WorktreeDivergence(
	ctx context.Context, dir string,
) (Divergence, bool, error) {
	if dir == "" {
		return Divergence{}, false, errors.New("empty worktree dir")
	}

	cmd := workspaceGitCommand(
		ctx, dir,
		"rev-list", "--left-right", "--count",
		"@{upstream}...HEAD",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// Git emits exit code 128 with a "no upstream configured"
		// (or "no such ref") message when the branch isn't tracking
		// anything yet. Treat that as a normal "no data" outcome
		// instead of a hard error.
		stderrText := stderr.String()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && isNoUpstreamMessage(stderrText) {
			return Divergence{}, false, nil
		}
		return Divergence{}, false, fmt.Errorf(
			"git rev-list: %w: %s", err, strings.TrimSpace(stderrText),
		)
	}

	fields := strings.Fields(stdout.String())
	if len(fields) != 2 {
		return Divergence{}, false, fmt.Errorf(
			"unexpected rev-list output: %q", stdout.String(),
		)
	}
	behind, err := strconv.Atoi(fields[0])
	if err != nil {
		return Divergence{}, false, fmt.Errorf(
			"parse behind count %q: %w", fields[0], err,
		)
	}
	ahead, err := strconv.Atoi(fields[1])
	if err != nil {
		return Divergence{}, false, fmt.Errorf(
			"parse ahead count %q: %w", fields[1], err,
		)
	}
	return Divergence{Ahead: ahead, Behind: behind}, true, nil
}

// isNoUpstreamMessage matches the stderr texts git uses when the
// current branch has no `@{upstream}` configured.
func isNoUpstreamMessage(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "no upstream configured") ||
		strings.Contains(s, "unknown revision") ||
		strings.Contains(s, "no such ref") ||
		strings.Contains(s, "ambiguous argument")
}
