package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wesm/middleman/internal/db"
)

type repoNumberPathRef struct {
	owner  string
	name   string
	number int
}

type starredRequest struct {
	ItemType string `json:"item_type"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	Number   int    `json:"number"`
}

var errRepoNotFound = errors.New("repo not found")

// buildRepoLookup materializes a repo-id keyed map used to annotate list
// responses with owner/name information.
func buildRepoLookup(repos []db.Repo) map[int64]db.Repo {
	lookup := make(map[int64]db.Repo, len(repos))
	for _, repo := range repos {
		lookup[repo.ID] = repo
	}
	return lookup
}

// lookupRepoMap fetches repos and returns an ID-keyed map. When config is
// available, only configured repos are included so that removed repos are
// filtered out of list responses.
func (s *Server) lookupRepoMap(ctx context.Context) (map[int64]db.Repo, error) {
	repos, err := s.db.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	if s.cfg != nil {
		repos = s.filterConfiguredRepos(repos)
	}
	return buildRepoLookup(repos), nil
}

// filterConfiguredRepos returns only repos that are in the config.
func (s *Server) filterConfiguredRepos(repos []db.Repo) []db.Repo {
	s.cfgMu.Lock()
	configured := make(map[string]bool, len(s.cfg.Repos))
	for _, cr := range s.cfg.Repos {
		configured[cr.Owner+"/"+cr.Name] = true
	}
	s.cfgMu.Unlock()

	filtered := make([]db.Repo, 0, len(repos))
	for _, r := range repos {
		if configured[r.Owner+"/"+r.Name] {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// lookupRepoID resolves a repository from owner/name inputs and returns a
// stable not-found error for handlers that need repo identity only.
func (s *Server) lookupRepoID(ctx context.Context, owner, name string) (int64, error) {
	repo, err := s.db.GetRepoByOwnerName(ctx, owner, name)
	if err != nil {
		return 0, fmt.Errorf("get repo: %w", err)
	}
	if repo == nil {
		return 0, errRepoNotFound
	}
	return repo.ID, nil
}

// lookupMRID resolves the internal MR id from the common route tuple.
func (s *Server) lookupMRID(ctx context.Context, ref repoNumberPathRef) (int64, error) {
	return s.db.GetMRIDByRepoAndNumber(ctx, ref.owner, ref.name, ref.number)
}

// lookupIssueID resolves the internal issue id from the common route tuple.
func (s *Server) lookupIssueID(ctx context.Context, ref repoNumberPathRef) (int64, error) {
	return s.db.GetIssueIDByRepoAndNumber(ctx, ref.owner, ref.name, ref.number)
}

// parseRepoFilter splits the repo query parameter when it is in owner/name
// form and otherwise returns empty parts so callers can ignore invalid input.
func parseRepoFilter(repo string) (owner, name string) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func validateStarredRequest(body starredRequest) bool {
	return body.ItemType == "pr" || body.ItemType == "issue"
}
