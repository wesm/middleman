package gitclone

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrTooLarge is returned when a requested blob exceeds the caller's limit.
var ErrTooLarge = errors.New("git blob too large")

// DiffFiles returns file metadata (path, status, renames) without patch
// content. It combines git diff --raw and --numstat, which is much faster
// than a full patch diff for large PRs.
func (m *Manager) DiffFiles(
	ctx context.Context,
	host, owner, name, mergeBase, headSHA string,
) ([]DiffFile, error) {
	clonePath, err := m.ClonePath(host, owner, name)
	if err != nil {
		return nil, err
	}
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
		fields := strings.SplitN(record, "\t", 3)
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
	clonePath, err := m.ClonePath(host, owner, name)
	if err != nil {
		return nil, err
	}

	// Step 1: Compute whitespace-only file count.
	wsCount, err := m.computeWhitespaceOnlyCount(
		ctx, host, clonePath, mergeBase, headSHA)
	if err != nil {
		return nil, fmt.Errorf("whitespace count: %w", err)
	}

	// Step 2: Get file metadata from --raw -z (with rename/copy detection).
	rawArgs := diffRawArgs(mergeBase, headSHA, hideWhitespace)
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
	whitespaceOnlyFiles, err := m.whitespaceOnlyFiles(ctx, host, clonePath, mergeBase, headSHA)
	if err != nil {
		return 0, err
	}
	return len(whitespaceOnlyFiles), nil
}

func (m *Manager) getWhitespaceOnlyFiles(
	ctx context.Context, host, clonePath, mergeBase, headSHA string,
) map[string]bool {
	files, err := m.whitespaceOnlyFiles(ctx, host, clonePath, mergeBase, headSHA)
	if err != nil {
		return nil
	}
	return files
}

func (m *Manager) whitespaceOnlyFiles(
	ctx context.Context, host, clonePath, mergeBase, headSHA string,
) (map[string]bool, error) {
	out1, err := m.git(ctx, host, clonePath, diffRawNoRenameArgs(mergeBase, headSHA, false)...)
	if err != nil {
		return nil, err
	}
	out2, err := m.git(ctx, host, clonePath, diffRawNoRenameArgs(mergeBase, headSHA, true)...)
	if err != nil {
		return nil, err
	}

	allFiles := parseRawZPaths(out1)
	wFiles := parseRawZPaths(out2)

	result := make(map[string]bool)
	for f := range allFiles {
		if !wFiles[f] {
			result[f] = true
		}
	}
	return result, nil
}

func diffRawArgs(mergeBase, headSHA string, hideWhitespace bool) []string {
	args := []string{
		"diff", "--raw", "-z", "-M", "-C",
		"--find-copies-harder", mergeBase, headSHA,
	}
	if hideWhitespace {
		return append(args[:2], append([]string{"-w"}, args[2:]...)...)
	}
	return args
}

func diffRawNoRenameArgs(mergeBase, headSHA string, hideWhitespace bool) []string {
	args := []string{"diff", "--raw", "-z", "--no-renames", mergeBase, headSHA}
	if hideWhitespace {
		return append(args[:4], append([]string{"-w"}, args[4:]...)...)
	}
	return args
}

// FileContent returns one file's blob content at ref. maxBytes guards API
// callers from accidentally loading very large assets into memory.
func (m *Manager) FileContent(
	ctx context.Context,
	host, owner, name, ref, filePath string,
	maxBytes int64,
) (*FileContent, error) {
	clonePath, err := m.ClonePath(host, owner, name)
	if err != nil {
		return nil, err
	}
	object := ref + ":" + filePath
	sizeOut, err := m.git(ctx, host, clonePath, "cat-file", "-s", object)
	if err != nil {
		return nil, fmt.Errorf("git cat-file -s: %w", err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeOut)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse blob size: %w", err)
	}
	if maxBytes > 0 && size > maxBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, size)
	}
	data, err := m.git(ctx, host, clonePath, "cat-file", "blob", object)
	if err != nil {
		return nil, fmt.Errorf("git cat-file blob: %w", err)
	}
	return &FileContent{
		Path: filePath,
		Data: data,
		Size: size,
	}, nil
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
