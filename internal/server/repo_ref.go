package server

import (
	"strings"

	"github.com/wesm/middleman/internal/db"
)

type repoRefResponse struct {
	Provider     string `json:"provider"`
	PlatformHost string `json:"platform_host"`
	RepoPath     string `json:"repo_path"`
	Owner        string `json:"owner"`
	Name         string `json:"name"`
}

func repoRefFromRepo(repo db.Repo) repoRefResponse {
	provider := strings.TrimSpace(repo.Platform)
	if provider == "" {
		provider = "github"
	}
	repoPath := strings.TrimSpace(repo.RepoPath)
	if repoPath == "" {
		repoPath = repo.Owner + "/" + repo.Name
	}
	return repoRefResponse{
		Provider:     provider,
		PlatformHost: repo.PlatformHost,
		RepoPath:     repoPath,
		Owner:        repo.Owner,
		Name:         repo.Name,
	}
}

func repoRefFromParts(provider, host, owner, name string) repoRefResponse {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "github"
	}
	return repoRefResponse{
		Provider:     provider,
		PlatformHost: host,
		RepoPath:     owner + "/" + name,
		Owner:        owner,
		Name:         name,
	}
}
