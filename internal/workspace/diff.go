package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	WorktreeDiffBaseHead        WorktreeDiffBase = "head"
	WorktreeDiffBasePushed      WorktreeDiffBase = "pushed"
	WorktreeDiffBaseMergeTarget WorktreeDiffBase = "merge-target"
)

const maxUntrackedTextFileBytes = 1 << 20

func WorktreeDiffFiles(
	ctx context.Context,
	dir string,
	base WorktreeDiffBase,
	hideWhitespace bool,
) ([]gitclone.DiffFile, bool, error) {
	baseRef, ok, err := worktreeDiffBaseRef(ctx, dir, base)
	if err != nil || !ok {
		return nil, ok, err
	}

	return worktreeDiffFilesFromRef(ctx, dir, baseRef, hideWhitespace)
}

func WorktreeDiffWhitespaceOnlyCount(
	ctx context.Context,
	dir string,
	base WorktreeDiffBase,
) (int, bool, error) {
	baseRef, ok, err := worktreeDiffBaseRef(ctx, dir, base)
	if err != nil || !ok {
		return 0, ok, err
	}

	count, err := worktreeWhitespaceOnlyCount(ctx, dir, baseRef, "")
	return count, true, err
}

func WorktreeDiffFilesAgainstMergeTarget(
	ctx context.Context,
	dir string,
	targetBranch string,
	hideWhitespace bool,
) ([]gitclone.DiffFile, bool, error) {
	baseRef, ok, err := worktreeMergeTargetBaseRef(ctx, dir, targetBranch)
	if err != nil || !ok {
		return nil, ok, err
	}

	return worktreeDiffFilesFromRef(ctx, dir, baseRef, hideWhitespace)
}

func WorktreeDiffWhitespaceOnlyCountAgainstMergeTarget(
	ctx context.Context,
	dir string,
	targetBranch string,
) (int, bool, error) {
	baseRef, ok, err := worktreeMergeTargetBaseRef(ctx, dir, targetBranch)
	if err != nil || !ok {
		return 0, ok, err
	}

	count, err := worktreeWhitespaceOnlyCount(ctx, dir, baseRef, "")
	return count, true, err
}

func worktreeDiffFilesFromRef(
	ctx context.Context,
	dir string,
	baseRef string,
	hideWhitespace bool,
) ([]gitclone.DiffFile, bool, error) {
	rawArgs := addWorktreeWhitespaceFlag([]string{
		"diff", "--raw", "-z", "-M", "-C", "--find-copies-harder",
		baseRef,
	}, hideWhitespace)
	rawOut, err := worktreeGitOutput(ctx, dir, rawArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("git diff --raw: %w", err)
	}
	files := gitclone.ParseRawZ(rawOut)
	if files == nil {
		files = []gitclone.DiffFile{}
	}

	numstatArgs := addWorktreeWhitespaceFlag([]string{
		"diff", "--numstat", "-z", "-M", "-C", "--find-copies-harder",
		baseRef,
	}, hideWhitespace)
	numstatOut, err := worktreeGitOutput(ctx, dir, numstatArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("git diff --numstat: %w", err)
	}
	counts := parseWorktreeNumstatZ(numstatOut)
	applyWorktreeNumstat(files, counts)
	if hideWhitespace {
		files = dropWhitespaceOnlyModifications(files, counts)
	}
	files = append(files, worktreeUntrackedFiles(ctx, dir, false, hideWhitespace)...)
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

	return worktreeDiffFromRef(ctx, dir, baseRef, hideWhitespace)
}

func WorktreeFileDiff(
	ctx context.Context,
	dir string,
	base WorktreeDiffBase,
	hideWhitespace bool,
	path string,
) (*gitclone.DiffResult, bool, error) {
	baseRef, ok, err := worktreeDiffBaseRef(ctx, dir, base)
	if err != nil || !ok {
		return nil, ok, err
	}

	return worktreeDiffFromRefPath(ctx, dir, baseRef, hideWhitespace, path)
}

func WorktreeDiffAgainstMergeTarget(
	ctx context.Context,
	dir string,
	targetBranch string,
	hideWhitespace bool,
) (*gitclone.DiffResult, bool, error) {
	baseRef, ok, err := worktreeMergeTargetBaseRef(ctx, dir, targetBranch)
	if err != nil || !ok {
		return nil, ok, err
	}

	return worktreeDiffFromRef(ctx, dir, baseRef, hideWhitespace)
}

func WorktreeFileDiffAgainstMergeTarget(
	ctx context.Context,
	dir string,
	targetBranch string,
	hideWhitespace bool,
	path string,
) (*gitclone.DiffResult, bool, error) {
	baseRef, ok, err := worktreeMergeTargetBaseRef(ctx, dir, targetBranch)
	if err != nil || !ok {
		return nil, ok, err
	}

	return worktreeDiffFromRefPath(ctx, dir, baseRef, hideWhitespace, path)
}

func worktreeDiffFromRef(
	ctx context.Context,
	dir string,
	baseRef string,
	hideWhitespace bool,
) (*gitclone.DiffResult, bool, error) {
	return worktreeDiffFromRefPath(ctx, dir, baseRef, hideWhitespace, "")
}

func worktreeDiffFromRefPath(
	ctx context.Context,
	dir string,
	baseRef string,
	hideWhitespace bool,
	path string,
) (*gitclone.DiffResult, bool, error) {
	path, err := cleanWorktreeDiffPath(path)
	if err != nil {
		return nil, false, err
	}

	wsCount, err := worktreeWhitespaceOnlyCount(ctx, dir, baseRef, path)
	if err != nil {
		return nil, false, fmt.Errorf("whitespace count: %w", err)
	}

	rawArgs := addWorktreeWhitespaceFlag([]string{
		"diff", "--raw", "-z", "-M", "-C", "--find-copies-harder",
		baseRef,
	}, hideWhitespace)
	rawArgs = appendWorktreePathspec(rawArgs, path)
	rawOut, err := worktreeGitOutput(ctx, dir, rawArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("git diff --raw: %w", err)
	}
	files := gitclone.ParseRawZ(rawOut)

	numstatArgs := addWorktreeWhitespaceFlag([]string{
		"diff", "--numstat", "-z", "-M", "-C", "--find-copies-harder",
		baseRef,
	}, hideWhitespace)
	numstatArgs = appendWorktreePathspec(numstatArgs, path)
	numstatOut, err := worktreeGitOutput(ctx, dir, numstatArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("git diff --numstat: %w", err)
	}

	patchArgs := addWorktreeWhitespaceFlag([]string{
		"diff", "-M", "-C", "--find-copies-harder", "-U3", baseRef,
	}, hideWhitespace)
	patchArgs = appendWorktreePathspec(patchArgs, path)
	patchOut, err := worktreeGitOutput(ctx, dir, patchArgs...)
	if err != nil {
		return nil, false, fmt.Errorf("git diff patch: %w", err)
	}
	files = gitclone.ParsePatch(patchOut, files)
	if files == nil {
		files = []gitclone.DiffFile{}
	}
	counts := parseWorktreeNumstatZ(numstatOut)
	applyWorktreeNumstat(files, counts)
	if hideWhitespace {
		files = dropWhitespaceOnlyModifications(files, counts)
	}

	if !hideWhitespace {
		wsFiles, err := worktreeWhitespaceOnlyFiles(ctx, dir, baseRef, path)
		if err == nil {
			for i := range files {
				files[i].IsWhitespaceOnly = wsFiles[files[i].Path]
			}
		}
	}
	if path == "" {
		files = append(files, worktreeUntrackedFiles(ctx, dir, true, hideWhitespace)...)
	} else if file, ok := worktreeUntrackedFile(
		ctx, dir, path, true, hideWhitespace,
	); ok {
		files = append(files, file)
	}

	return &gitclone.DiffResult{
		WhitespaceOnlyCount: wsCount,
		Files:               files,
	}, true, nil
}

func applyWorktreeNumstat(
	files []gitclone.DiffFile,
	counts map[string]worktreeNumstatCount,
) {
	for i := range files {
		if count, ok := counts[files[i].Path]; ok {
			files[i].Additions = count.additions
			files[i].Deletions = count.deletions
		}
		if files[i].Hunks == nil {
			files[i].Hunks = []gitclone.Hunk{}
		}
	}
}

// dropWhitespaceOnlyModifications removes "modified" entries that --raw lists
// but --numstat omits under -w. git's --raw output ignores -w (it compares
// blob SHAs), while --numstat honors it, so absence from the numstat map
// reliably indicates a whitespace-only modification. Renames, copies, adds,
// and deletes are preserved since their inclusion in --raw still represents
// a real history change even with 0/0 counts.
func dropWhitespaceOnlyModifications(
	files []gitclone.DiffFile,
	counts map[string]worktreeNumstatCount,
) []gitclone.DiffFile {
	out := files[:0]
	for i := range files {
		if files[i].Status == "modified" {
			if _, ok := counts[files[i].Path]; !ok {
				continue
			}
		}
		out = append(out, files[i])
	}
	return out
}

func addWorktreeWhitespaceFlag(
	args []string,
	hideWhitespace bool,
) []string {
	if !hideWhitespace {
		return args
	}
	withWhitespace := make([]string, 0, len(args)+1)
	withWhitespace = append(withWhitespace, args[:2]...)
	withWhitespace = append(withWhitespace, "-w")
	withWhitespace = append(withWhitespace, args[2:]...)
	return withWhitespace
}

func appendWorktreePathspec(args []string, path string) []string {
	if path == "" {
		return args
	}
	return append(args, "--", path)
}

func cleanWorktreeDiffPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if strings.Contains(path, "\x00") {
		return "", errors.New("diff path contains NUL byte")
	}
	path = filepath.ToSlash(path)
	if strings.HasPrefix(path, "/") {
		return "", errors.New("diff path must be relative")
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." ||
		clean == ".." ||
		strings.HasPrefix(clean, "../") {
		return "", errors.New("diff path must stay inside worktree")
	}
	return clean, nil
}

func worktreeUntrackedFiles(
	ctx context.Context,
	dir string,
	withHunks bool,
	hideWhitespace bool,
) []gitclone.DiffFile {
	out, err := worktreeGitOutput(
		ctx, dir, "ls-files", "--others", "--exclude-standard", "-z",
	)
	if err != nil {
		return nil
	}
	parts := bytes.Split(out, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		path := string(part)
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return worktreeUntrackedFilesFromPaths(
		dir, paths, withHunks, hideWhitespace,
	)
}

func worktreeUntrackedFile(
	ctx context.Context,
	dir string,
	path string,
	withHunks bool,
	hideWhitespace bool,
) (gitclone.DiffFile, bool) {
	out, err := worktreeGitOutput(
		ctx, dir, "ls-files", "--others", "--exclude-standard", "-z",
		"--", path,
	)
	if err != nil {
		return gitclone.DiffFile{}, false
	}
	for part := range bytes.SplitSeq(out, []byte{0}) {
		if string(part) != path {
			continue
		}
		files := worktreeUntrackedFilesFromPaths(
			dir, []string{path}, withHunks, hideWhitespace,
		)
		if len(files) == 0 {
			return gitclone.DiffFile{}, false
		}
		return files[0], true
	}
	return gitclone.DiffFile{}, false
}

func worktreeUntrackedFilesFromPaths(
	dir string,
	paths []string,
	withHunks bool,
	hideWhitespace bool,
) []gitclone.DiffFile {
	files := make([]gitclone.DiffFile, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		file := gitclone.DiffFile{
			Path:    filepath.ToSlash(path),
			OldPath: filepath.ToSlash(path),
			Status:  "added",
			Hunks:   []gitclone.Hunk{},
		}
		content, ok := readUntrackedFileContent(
			filepath.Join(dir, path),
		)
		if !ok {
			continue
		}
		if content == nil {
			file.IsBinary = true
			files = append(files, file)
			continue
		}
		if hideWhitespace && len(content) > 0 &&
			!bytes.Contains(content, []byte{0}) &&
			len(bytes.TrimSpace(content)) == 0 {
			continue
		}
		file.Additions = countAddedLines(content)
		if bytes.Contains(content, []byte{0}) {
			file.IsBinary = true
		} else if withHunks {
			file.Hunks = []gitclone.Hunk{
				untrackedFileHunk(content),
			}
		}
		files = append(files, file)
	}
	return files
}

func readUntrackedFileContent(path string) ([]byte, bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return nil, false
		}
		return []byte(target), true
	}
	if !info.Mode().IsRegular() {
		return nil, false
	}
	file, info, err := openRegularUntrackedFile(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	if info.Size() > maxUntrackedTextFileBytes {
		return nil, true
	}
	content, err := io.ReadAll(io.LimitReader(file, maxUntrackedTextFileBytes+1))
	if err != nil {
		return nil, false
	}
	if len(content) > maxUntrackedTextFileBytes {
		return nil, true
	}
	return content, true
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
	ctx context.Context, dir string, baseRef string, path string,
) (int, error) {
	files, err := worktreeWhitespaceOnlyFiles(ctx, dir, baseRef, path)
	if err != nil {
		return 0, err
	}
	return len(files), nil
}

func worktreeWhitespaceOnlyFiles(
	ctx context.Context, dir string, baseRef string, path string,
) (map[string]bool, error) {
	allArgs := appendWorktreePathspec([]string{
		"diff", "--raw", "-z", "--no-renames", baseRef,
	}, path)
	outAll, err := worktreeGitOutput(ctx, dir, allArgs...)
	if err != nil {
		return nil, err
	}
	noWhitespaceArgs := appendWorktreePathspec([]string{
		"diff", "--raw", "-z", "--no-renames", "-w", baseRef,
	}, path)
	outNoWhitespace, err := worktreeGitOutput(
		ctx, dir, noWhitespaceArgs...,
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
	case WorktreeDiffBasePushed:
		_, ok, err := WorktreeDivergence(ctx, dir)
		if err != nil || !ok {
			return "", ok, err
		}
		return "@{upstream}", true, nil
	default:
		return "", false, fmt.Errorf("unknown worktree diff base %q", base)
	}
}

func worktreeMergeTargetBaseRef(
	ctx context.Context,
	dir string,
	targetBranch string,
) (string, bool, error) {
	targetBranch = strings.TrimSpace(targetBranch)
	if targetBranch == "" {
		return "", false, nil
	}
	if _, err := worktreeGitOutput(
		ctx, dir, "check-ref-format", "--branch", targetBranch,
	); err != nil {
		return "", false, nil
	}

	targetRef := "refs/remotes/origin/" + targetBranch
	if _, err := worktreeGitOutput(
		ctx, dir, "rev-parse", "--verify", "--quiet",
		targetRef+"^{commit}",
	); err != nil {
		return "", false, nil
	}
	out, err := worktreeGitOutput(
		ctx, dir, "merge-base", targetRef, "HEAD",
	)
	if err != nil {
		return "", false, fmt.Errorf("git merge-base: %w", err)
	}
	baseRef := strings.TrimSpace(string(out))
	if baseRef == "" {
		return "", false, nil
	}
	return baseRef, true, nil
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
