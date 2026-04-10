package gitclone

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"
)

// emptyTreeSHA is git's well-known SHA for an empty tree object.
const emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// Commit holds metadata for a single commit in a PR's history.
type Commit struct {
	SHA        string
	AuthorName string
	AuthoredAt time.Time
	Message    string // first line only
}

// ListCommits returns commits between mergeBase and headSHA, newest first,
// following only the first-parent chain. If mergeBase is the empty tree
// sentinel (parentless root), all commits up to headSHA are returned.
func (m *Manager) ListCommits(
	ctx context.Context,
	host, owner, name, mergeBase, headSHA string,
) ([]Commit, error) {
	dir := m.ClonePath(host, owner, name)

	args := []string{"log", "--first-parent", "--format=%H%x00%an%x00%aI%x00%s"}
	if mergeBase == emptyTreeSHA {
		// Empty tree is not a commit — list all ancestors of head.
		args = append(args, headSHA)
	} else {
		args = append(args, mergeBase+".."+headSHA)
	}

	out, err := m.git(ctx, host, dir, args...)
	if err != nil {
		return nil, fmt.Errorf("list commits: %w", err)
	}

	var commits []Commit
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 4)
		if len(parts) != 4 {
			return nil, fmt.Errorf("unexpected git log line: %q", line)
		}
		t, err := time.Parse(time.RFC3339, parts[2])
		if err != nil {
			return nil, fmt.Errorf("parse commit date %q: %w", parts[2], err)
		}
		commits = append(commits, Commit{
			SHA:        parts[0],
			AuthorName: parts[1],
			AuthoredAt: t,
			Message:    parts[3],
		})
	}
	return commits, scanner.Err()
}

// ParentOf returns the first parent SHA of the given commit.
// For a parentless (root) commit it returns the empty tree sentinel.
// The caller must ensure sha exists in the clone; any failure here
// is a genuine server-side error, not a client-input issue.
func (m *Manager) ParentOf(
	ctx context.Context,
	host, owner, name, sha string,
) (string, error) {
	dir := m.ClonePath(host, owner, name)
	out, err := m.git(ctx, host, dir,
		"rev-list", "--parents", "-n", "1", sha,
	)
	if err != nil {
		return "", fmt.Errorf("parent of %s: %w", sha, err)
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return "", fmt.Errorf("parent of %s: empty rev-list output", sha)
	}
	if len(fields) == 1 {
		// Parentless commit — diff against the empty tree.
		return emptyTreeSHA, nil
	}
	return fields[1], nil
}
