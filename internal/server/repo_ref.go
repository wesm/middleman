package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wesm/middleman/internal/db"
)

var errRepoPathRequired = errors.New("repo_path is required")

type repoRefInput struct {
	Provider     string `query:"provider"`
	PlatformHost string `query:"platform_host"`
	RepoPath     string `query:"repo_path"`
}

type repoRefResponse struct {
	Provider     string `json:"provider"`
	PlatformHost string `json:"platform_host"`
	RepoPath     string `json:"repo_path"`
	Owner        string `json:"owner"`
	Name         string `json:"name"`
}

func (s *Server) lookupRepoByRefInput(
	ctx context.Context,
	input repoRefInput,
) (*db.Repo, error) {
	provider := strings.TrimSpace(input.Provider)
	host := strings.TrimSpace(input.PlatformHost)
	repoPath := strings.Trim(input.RepoPath, "/ ")
	if provider == "" {
		provider = "github"
	}
	if host == "" {
		switch provider {
		case "gitlab":
			host = "gitlab.com"
		default:
			host = "github.com"
		}
	}
	if repoPath == "" {
		return nil, errRepoPathRequired
	}
	repo, err := s.db.GetRepoByIdentity(ctx, db.RepoIdentity{
		Platform:     provider,
		PlatformHost: host,
		RepoPath:     repoPath,
	})
	if err != nil {
		return nil, fmt.Errorf("lookup repo: %w", err)
	}
	if repo == nil {
		return nil, errRepoNotFound
	}
	return repo, nil
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
