package gitclone

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// DiffFiles returns file metadata (path, status, renames) without patch
// content. It combines git diff --raw and --numstat, which is much faster
// than a full patch diff for large PRs.
func (m *Manager) DiffFiles(
	ctx context.Context,
	host, owner, name, mergeBase, headSHA string,
) ([]DiffFile, error) {
	clonePath := m.ClonePath(host, owner, name)
	rawOut, err := m.git(ctx, host, clonePath,
		"diff", "--raw", "-z", "-M", "-C", "--find-copies-harder", mergeBase, headSHA,
	)
	if err != nil {
		return nil, fmt.Errorf("git diff --raw: %w", err)
	}
	files := ParseRawZ(rawOut)
	if files == nil {
		files = []DiffFile{}
	}
	numstatOut, err := m.git(ctx, host, clonePath,
		"diff", "--numstat", "-z", "-M", "-C",
		"--find-copies-harder", mergeBase, headSHA,
	)
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat: %w", err)
	}
	counts := parseNumstatZ(numstatOut)
	// Ensure Hunks is never nil so JSON serializes as [] not null.
	for i := range files {
		if count, ok := counts[files[i].Path]; ok {
			files[i].Additions = count.additions
			files[i].Deletions = count.deletions
		}
		if files[i].Hunks == nil {
			files[i].Hunks = []Hunk{}
		}
	}
	return files, nil
}

type numstatCount struct {
	additions int
	deletions int
}

func parseNumstatZ(data []byte) map[string]numstatCount {
	records := bytes.Split(data, []byte{0})
	counts := make(map[string]numstatCount)
	for i := 0; i < len(records); {
		record := string(records[i])
		if record == "" {
			i++
			continue
		}
		fields := strings.Split(record, "\t")
		if len(fields) < 3 {
			i++
			continue
		}
		path := fields[2]
		if path == "" && i+2 < len(records) {
			path = string(records[i+2])
			i += 3
		} else {
			i++
		}
		if path == "" {
			continue
		}
		counts[path] = numstatCount{
			additions: parseNumstatInt(fields[0]),
			deletions: parseNumstatInt(fields[1]),
		}
	}
	return counts
}

func parseNumstatInt(value string) int {
	if value == "-" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

// Diff runs a two-dot git diff between mergeBase and headSHA and returns
// structured diff data. If hideWhitespace is true, passes -w to git diff.
func (m *Manager) Diff(
	ctx context.Context,
	host, owner, name, mergeBase, headSHA string,
	hideWhitespace bool,
) (*DiffResult, error) {
	clonePath := m.ClonePath(host, owner, name)

	// Step 1: Compute whitespace-only file count.
	wsCount, err := m.computeWhitespaceOnlyCount(
		ctx, host, clonePath, mergeBase, headSHA)
	if err != nil {
		return nil, fmt.Errorf("whitespace count: %w", err)
	}

	// Step 2: Get file metadata from --raw -z (with rename/copy detection).
	rawArgs := []string{
		"diff", "--raw", "-z", "-M", "-C",
		"--find-copies-harder", mergeBase, headSHA,
	}
	if hideWhitespace {
		rawArgs = append(rawArgs[:2],
			append([]string{"-w"}, rawArgs[2:]...)...)
	}
	rawOut, err := m.git(ctx, host, clonePath, rawArgs...)
	if err != nil {
		return nil, fmt.Errorf("git diff --raw: %w", err)
	}
	files := ParseRawZ(rawOut)

	// Step 3: Get patch content.
	patchArgs := []string{
		"diff", "-M", "-C", "--find-copies-harder",
		"-U3", mergeBase, headSHA,
	}
	if hideWhitespace {
		patchArgs = append(patchArgs[:2],
			append([]string{"-w"}, patchArgs[2:]...)...)
	}
	patchOut, err := m.git(ctx, host, clonePath, patchArgs...)
	if err != nil {
		return nil, fmt.Errorf("git diff patch: %w", err)
	}

	files = ParsePatch(patchOut, files)
	if files == nil {
		files = []DiffFile{}
	}

	// Step 4: Mark whitespace-only files (only in default mode).
	if !hideWhitespace {
		wsFiles := m.getWhitespaceOnlyFiles(
			ctx, host, clonePath, mergeBase, headSHA)
		for i := range files {
			if wsFiles[files[i].Path] {
				files[i].IsWhitespaceOnly = true
			}
		}
	}

	return &DiffResult{
		WhitespaceOnlyCount: wsCount,
		Files:               files,
	}, nil
}

func (m *Manager) computeWhitespaceOnlyCount(
	ctx context.Context, host, clonePath, mergeBase, headSHA string,
) (int, error) {
	// Non-whitespace-ignoring pass.
	out1, err := m.git(ctx, host, clonePath,
		"diff", "--raw", "-z", "--no-renames", mergeBase, headSHA)
	if err != nil {
		return 0, err
	}
	// Whitespace-ignoring pass.
	out2, err := m.git(ctx, host, clonePath,
		"diff", "--raw", "-z", "--no-renames", "-w", mergeBase, headSHA)
	if err != nil {
		return 0, err
	}

	allFiles := parseRawZPaths(out1)
	wFiles := parseRawZPaths(out2)

	count := 0
	for f := range allFiles {
		if !wFiles[f] {
			count++
		}
	}
	return count, nil
}

func (m *Manager) getWhitespaceOnlyFiles(
	ctx context.Context, host, clonePath, mergeBase, headSHA string,
) map[string]bool {
	out1, err := m.git(ctx, host, clonePath,
		"diff", "--raw", "-z", "--no-renames", mergeBase, headSHA)
	if err != nil {
		return nil
	}
	out2, err := m.git(ctx, host, clonePath,
		"diff", "--raw", "-z", "--no-renames", "-w", mergeBase, headSHA)
	if err != nil {
		return nil
	}

	allFiles := parseRawZPaths(out1)
	wFiles := parseRawZPaths(out2)

	result := make(map[string]bool)
	for f := range allFiles {
		if !wFiles[f] {
			result[f] = true
		}
	}
	return result
}

// parseRawZPaths extracts just the file paths from --raw -z output.
func parseRawZPaths(data []byte) map[string]bool {
	files := ParseRawZ(data)
	paths := make(map[string]bool, len(files))
	for _, f := range files {
		paths[f.Path] = true
	}
	return paths
}
