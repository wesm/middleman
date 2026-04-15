package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	defaultBaseRef      = "origin/main"
	defaultMigrationDir = "internal/db/migrations"
)

var gitEnv []string

func main() {
	os.Exit(run(os.Stderr))
}

func run(stderr io.Writer) int {
	baseRef := getenvDefault("MIDDLEMAN_MIGRATION_BASE_REF", defaultBaseRef)
	migrationDir := strings.TrimRight(getenvDefault("MIDDLEMAN_MIGRATION_DIR", defaultMigrationDir), "/")

	if _, err := git("rev-parse", "--git-dir"); err != nil {
		fmt.Fprintln(stderr, "migration history check must run inside a git worktree")
		return 1
	}

	if _, err := git("rev-parse", "--verify", "--quiet", baseRef+"^{commit}"); err != nil {
		fmt.Fprintf(stderr, "Cannot verify migration history because %s is unavailable.\n", baseRef)
		fmt.Fprintln(stderr, "Fetch the main branch or set MIDDLEMAN_MIGRATION_BASE_REF to the main-branch ref to compare against.")
		return 1
	}

	diff, err := git("diff", "--cached", "--name-status", "--", migrationDir)
	if err != nil {
		fmt.Fprintf(stderr, "failed to inspect staged migrations: %v\n", err)
		return 1
	}

	violations := changedMainBranchMigrations(baseRef, migrationDir, diff)
	if len(violations) == 0 {
		return 0
	}

	fmt.Fprintf(stderr, "Refusing to commit edits to migrations that already exist on %s.\n\n", baseRef)
	fmt.Fprintln(stderr, "Migrations are append-only once they land on main. Add a new numbered migration instead.")
	fmt.Fprintln(stderr, "\nBlocked files:")
	for _, path := range violations {
		fmt.Fprintf(stderr, "  %s\n", path)
	}
	return 1
}

func changedMainBranchMigrations(baseRef, migrationDir, diff string) []string {
	var violations []string
	for line := range strings.SplitSeq(diff, "\n") {
		if line == "" {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}

		for _, path := range changedPaths(fields) {
			if !strings.HasPrefix(path, migrationDir+"/") {
				continue
			}
			if _, err := git("cat-file", "-e", baseRef+":"+path); err == nil {
				violations = append(violations, path)
			}
		}
	}
	return violations
}

func changedPaths(fields []string) []string {
	status := fields[0]
	paths := fields[1:]
	if strings.HasPrefix(status, "R") {
		return paths
	}
	if len(paths) == 0 {
		return nil
	}
	if strings.HasPrefix(status, "C") && len(paths) > 1 {
		return paths[1:]
	}
	return paths[:1]
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if gitEnv != nil {
		cmd.Env = gitEnv
	}
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
