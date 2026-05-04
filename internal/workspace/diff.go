package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wesm/middleman/internal/gitclone"
	"github.com/wesm/middleman/internal/gitenv"
	"github.com/wesm/middleman/internal/procutil"
)

type WorktreeDiffBase string

const (
	WorktreeDiffBaseHead     WorktreeDiffBase = "head"
	WorktreeDiffBaseUpstream WorktreeDiffBase = "origin"
)

func WorktreeDiffFiles(
	ctx context.Context,
	dir string,
	base WorktreeDiffBase,
) ([]gitclone.DiffFile, bool, error) {
	baseRef, ok, err := worktreeDiffBaseRef(ctx, dir, base)
	if err != nil || !ok {
		return nil, ok, err
	}

	rawOut, err := worktreeGitOutput(ctx, dir,
		"diff", "--raw", "-z", "-M", "-C", "--find-copies-harder",
		baseRef,
	)
	if err != nil {
		return nil, false, fmt.Errorf("git diff --raw: %w", err)
	}
	files := gitclone.ParseRawZ(rawOut)
	if files == nil {
		files = []gitclone.DiffFile{}
	}

	numstatOut, err := worktreeGitOutput(ctx, dir,
		"diff", "--numstat", "-z", "-M", "-C", "--find-copies-harder",
		baseRef,
	)
	if err != nil {
		return nil, false, fmt.Errorf("git diff --numstat: %w", err)
	}
	counts := parseWorktreeNumstatZ(numstatOut)
	for i := range files {
		if count, ok := counts[files[i].Path]; ok {
			files[i].Additions = count.additions
			files[i].Deletions = count.deletions
		}
		if files[i].Hunks == nil {
			files[i].Hunks = []gitclone.Hunk{}
		}
	}
	files = append(files, worktreeUntrackedFiles(ctx, dir, false)...)
	return files, true, nil
}

func WorktreeDiff(
	ctx context.Context,
	dir string,
	base WorktreeDiffBase,
	hideWhitespace bool,
) (*gitclone.DiffResult, bool, error) {
	baseRef, ok, err := worktreeDiffBaseRef(ctx, dir, base)
	if err != nil || !ok {
		return nil, ok, err
	}

	wsCount, err := worktreeWhitespaceOnlyCount(ctx, dir, baseRef)
	if err != nil {
		return nil, false, fmt.Errorf("whitespace count: %w", err)
	}

	rawArgs := []string{
		"diff", "--raw", "-z", "-M", "-C", "--find-copies-harder",
		baseRef,
	}
	if hideWhitespace {
		rawArgs = append(rawArgs[:2], append([]string{"-w"}, rawArgs[2:]...)...)
	}
	rawOut, err := worktreeGitOutput(ctx, dir, rawArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("git diff --raw: %w", err)
	}
	files := gitclone.ParseRawZ(rawOut)

	patchArgs := []string{
		"diff", "-M", "-C", "--find-copies-harder", "-U3", baseRef,
	}
	if hideWhitespace {
		patchArgs = append(patchArgs[:2], append([]string{"-w"}, patchArgs[2:]...)...)
	}
	patchOut, err := worktreeGitOutput(ctx, dir, patchArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("git diff patch: %w", err)
	}
	files = gitclone.ParsePatch(patchOut, files)
	if files == nil {
		files = []gitclone.DiffFile{}
	}

	if !hideWhitespace {
		wsFiles, err := worktreeWhitespaceOnlyFiles(ctx, dir, baseRef)
		if err == nil {
			for i := range files {
				files[i].IsWhitespaceOnly = wsFiles[files[i].Path]
			}
		}
	}
	files = append(files, worktreeUntrackedFiles(ctx, dir, true)...)

	return &gitclone.DiffResult{
		WhitespaceOnlyCount: wsCount,
		Files:               files,
	}, true, nil
}

func worktreeUntrackedFiles(
	ctx context.Context,
	dir string,
	withHunks bool,
) []gitclone.DiffFile {
	out, err := worktreeGitOutput(
		ctx, dir, "ls-files", "--others", "--exclude-standard", "-z",
	)
	if err != nil {
		return nil
	}
	parts := bytes.Split(out, []byte{0})
	files := make([]gitclone.DiffFile, 0, len(parts))
	for _, part := range parts {
		path := string(part)
		if path == "" {
			continue
		}
		file := gitclone.DiffFile{
			Path:    filepath.ToSlash(path),
			OldPath: filepath.ToSlash(path),
			Status:  "added",
			Hunks:   []gitclone.Hunk{},
		}
		content, readErr := os.ReadFile(filepath.Join(dir, path))
		if readErr == nil {
			file.Additions = countAddedLines(content)
			if bytes.Contains(content, []byte{0}) {
				file.IsBinary = true
			} else if withHunks {
				file.Hunks = []gitclone.Hunk{
					untrackedFileHunk(content),
				}
			}
		}
		files = append(files, file)
	}
	return files
}

func countAddedLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		count++
	}
	return count
}

func untrackedFileHunk(content []byte) gitclone.Hunk {
	text := string(content)
	rawLines := strings.Split(text, "\n")
	lines := make([]gitclone.Line, 0, len(rawLines))
	for i, line := range rawLines {
		if i == len(rawLines)-1 && line == "" {
			continue
		}
		lines = append(lines, gitclone.Line{
			Type:      "add",
			Content:   line,
			NewNum:    len(lines) + 1,
			NoNewline: i == len(rawLines)-1 && !strings.HasSuffix(text, "\n"),
		})
	}
	return gitclone.Hunk{
		OldStart: 0,
		OldCount: 0,
		NewStart: 1,
		NewCount: len(lines),
		Lines:    lines,
	}
}

type worktreeNumstatCount struct {
	additions int
	deletions int
}

func parseWorktreeNumstatZ(data []byte) map[string]worktreeNumstatCount {
	records := bytes.Split(data, []byte{0})
	counts := make(map[string]worktreeNumstatCount)
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
		counts[path] = worktreeNumstatCount{
			additions: parseWorktreeNumstatInt(fields[0]),
			deletions: parseWorktreeNumstatInt(fields[1]),
		}
	}
	return counts
}

func parseWorktreeNumstatInt(value string) int {
	if value == "-" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

func worktreeWhitespaceOnlyCount(
	ctx context.Context, dir string, baseRef string,
) (int, error) {
	files, err := worktreeWhitespaceOnlyFiles(ctx, dir, baseRef)
	if err != nil {
		return 0, err
	}
	return len(files), nil
}

func worktreeWhitespaceOnlyFiles(
	ctx context.Context, dir string, baseRef string,
) (map[string]bool, error) {
	outAll, err := worktreeGitOutput(ctx, dir,
		"diff", "--raw", "-z", "--no-renames", baseRef,
	)
	if err != nil {
		return nil, err
	}
	outNoWhitespace, err := worktreeGitOutput(ctx, dir,
		"diff", "--raw", "-z", "--no-renames", "-w", baseRef,
	)
	if err != nil {
		return nil, err
	}

	allFiles := worktreeRawPaths(outAll)
	noWhitespaceFiles := worktreeRawPaths(outNoWhitespace)
	result := make(map[string]bool)
	for file := range allFiles {
		if !noWhitespaceFiles[file] {
			result[file] = true
		}
	}
	return result, nil
}

func worktreeRawPaths(data []byte) map[string]bool {
	files := gitclone.ParseRawZ(data)
	paths := make(map[string]bool, len(files))
	for _, file := range files {
		paths[file.Path] = true
	}
	return paths
}

func worktreeDiffBaseRef(
	ctx context.Context,
	dir string,
	base WorktreeDiffBase,
) (string, bool, error) {
	switch base {
	case WorktreeDiffBaseHead:
		return "HEAD", true, nil
	case WorktreeDiffBaseUpstream:
		_, ok, err := WorktreeDivergence(ctx, dir)
		if err != nil || !ok {
			return "", ok, err
		}
		return "@{upstream}", true, nil
	default:
		return "", false, fmt.Errorf("unknown worktree diff base %q", base)
	}
}

func worktreeGitOutput(
	ctx context.Context,
	dir string,
	args ...string,
) ([]byte, error) {
	if dir == "" {
		return nil, errors.New("empty worktree dir")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(gitenv.StripAll(os.Environ()),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := procutil.Output(ctx, cmd, "git workspace diff subprocess capacity")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
