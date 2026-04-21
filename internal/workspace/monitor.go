package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/wesm/middleman/internal/db"
	"github.com/wesm/middleman/internal/gitenv"
	ghclient "github.com/wesm/middleman/internal/github"
)

type PRMonitor struct {
	db *db.DB
}

func NewPRMonitor(database *db.DB) *PRMonitor {
	return &PRMonitor{db: database}
}

func (m *PRMonitor) RunOnce(
	ctx context.Context,
) ([]string, error) {
	workspaces, err := m.db.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}

	var updates []string
	for i := range workspaces {
		ws := workspaces[i]
		if !workspacePRMonitorEligible(&ws) {
			continue
		}

		prNumber, ok, detectErr := m.detectAssociatedPR(ctx, &ws)
		if detectErr != nil {
			slog.Warn(
				"workspace PR monitor git inspection failed",
				"workspace_id", ws.ID,
				"path", ws.WorktreePath,
				"err", detectErr,
			)
			continue
		}
		if !ok {
			continue
		}

		changed, err := m.db.SetWorkspaceAssociatedPRNumberIfNull(
			ctx, ws.ID, prNumber,
		)
		if err != nil {
			slog.Warn(
				"workspace PR monitor persistence failed",
				"workspace_id", ws.ID,
				"pr_number", prNumber,
				"err", err,
			)
			continue
		}
		if changed {
			updates = append(updates, ws.ID)
		}
	}

	return updates, nil
}

func workspacePRMonitorEligible(ws *Workspace) bool {
	return ws != nil &&
		ws.ItemType == db.WorkspaceItemTypeIssue &&
		ws.AssociatedPRNumber == nil &&
		ws.Status == "ready" &&
		strings.TrimSpace(ws.WorktreePath) != ""
}

type upstreamState struct {
	branchName    string
	remoteName    string
	remoteURL     string
	hasTracking   bool
	allowFallback bool
}

func (m *PRMonitor) detectAssociatedPR(
	ctx context.Context,
	ws *Workspace,
) (int, bool, error) {
	currentBranch, err := gitBranchName(ctx, ws.WorktreePath)
	if err != nil {
		return 0, false, err
	}
	if currentBranch == "" {
		return 0, false, nil
	}
	if currentBranch == issueWorkspaceBranch(ws.ItemNumber) {
		return 0, false, nil
	}

	candidates, err := m.db.ListMergeRequests(ctx, db.ListMergeRequestsOpts{
		PlatformHost: ws.PlatformHost,
		RepoOwner:    ws.RepoOwner,
		RepoName:     ws.RepoName,
		State:        "open",
	})
	if err != nil {
		return 0, false, fmt.Errorf("list merge requests: %w", err)
	}

	upstream, err := gitUpstreamState(ctx, ws.WorktreePath, currentBranch)
	if err != nil {
		return 0, false, err
	}
	if upstream.hasTracking {
		if prNumber, ok := selectPRByUpstream(candidates, upstream); ok {
			return prNumber, true, nil
		}
		if !upstream.allowFallback {
			return 0, false, nil
		}
	}

	if prNumber, ok := selectPRByLocalBranch(candidates, currentBranch); ok {
		return prNumber, true, nil
	}
	return 0, false, nil
}

func selectPRByUpstream(
	candidates []db.MergeRequest,
	upstream upstreamState,
) (int, bool) {
	if upstream.branchName == "" {
		return 0, false
	}

	remoteRepo := normalizeCloneRepoIdentity(upstream.remoteURL)
	matches := make([]db.MergeRequest, 0, len(candidates))
	for i := range candidates {
		candidate := candidates[i]
		if candidate.HeadBranch != upstream.branchName {
			continue
		}
		candidateRepo := normalizeCloneRepoIdentity(candidate.HeadRepoCloneURL)
		if remoteRepo != "" && candidateRepo != "" && candidateRepo != remoteRepo {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) != 1 {
		return 0, false
	}
	return matches[0].Number, true
}

func selectPRByLocalBranch(
	candidates []db.MergeRequest,
	currentBranch string,
) (int, bool) {
	matches := make([]db.MergeRequest, 0, len(candidates))
	for i := range candidates {
		candidate := candidates[i]
		if candidate.HeadBranch == currentBranch {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return 0, false
	}
	return matches[0].Number, true
}

func gitBranchName(
	ctx context.Context,
	dir string,
) (string, error) {
	out, err := gitOutput(ctx, dir, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("git branch --show-current: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func gitUpstreamState(
	ctx context.Context,
	dir, branch string,
) (upstreamState, error) {
	state := upstreamState{}
	remoteName, remoteErr := gitConfigValue(
		ctx, dir, "branch."+branch+".remote",
	)
	mergeRef, mergeErr := gitConfigValue(
		ctx, dir, "branch."+branch+".merge",
	)
	if remoteErr != nil || mergeErr != nil {
		state.allowFallback = true
		return state, nil
	}

	state.hasTracking = true
	state.allowFallback = false
	state.remoteName = remoteName
	state.branchName = strings.TrimPrefix(mergeRef, "refs/heads/")
	remoteURL, err := gitRemoteURL(ctx, dir, remoteName)
	if err != nil {
		state.allowFallback = true
		return state, nil
	}
	state.remoteURL = remoteURL
	return state, nil
}

func gitConfigValue(
	ctx context.Context,
	dir, key string,
) (string, error) {
	out, err := gitOutput(ctx, dir, "config", "--get", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitRemoteURL(
	ctx context.Context,
	dir, remote string,
) (string, error) {
	out, err := gitOutput(ctx, dir, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitOutput(
	ctx context.Context,
	dir string,
	args ...string,
) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(
		gitenv.StripAll(os.Environ()),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func normalizeCloneRepoIdentity(cloneURL string) string {
	fullName := ghclient.ParseHeadRepoFullName(cloneURL)
	if fullName == "" {
		return ""
	}
	return strings.ToLower(fullName)
}
