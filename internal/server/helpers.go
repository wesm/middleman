package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
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

// parseRepoNumberPath extracts the common {owner}/{name}/{number} route tuple
// used by PR and issue handlers and writes a 400 when the number is invalid.
func parseRepoNumberPath(
	w http.ResponseWriter, r *http.Request, itemLabel string,
) (repoNumberPathRef, bool) {
	number, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid "+itemLabel+" number")
		return repoNumberPathRef{}, false
	}
	return repoNumberPathRef{
		owner:  r.PathValue("owner"),
		name:   r.PathValue("name"),
		number: number,
	}, true
}

// decodeJSONBody centralizes request body decoding while preserving each
// handler's existing 400 response semantics for malformed JSON.
func decodeJSONBody(
	w http.ResponseWriter, r *http.Request, dst any, invalidMsg string,
) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, invalidMsg)
		return false
	}
	return true
}

// buildRepoLookup materializes a repo-id keyed map used to annotate list
// responses with owner/name information.
func buildRepoLookup(repos []db.Repo) map[int64]db.Repo {
	lookup := make(map[int64]db.Repo, len(repos))
	for _, repo := range repos {
		lookup[repo.ID] = repo
	}
	return lookup
}

// lookupRepoMap fetches all repos once for handlers that need to decorate list
// responses with repository identity details.
func (s *Server) lookupRepoMap(ctx context.Context) (map[int64]db.Repo, error) {
	repos, err := s.db.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return buildRepoLookup(repos), nil
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

// lookupPRID resolves the internal PR id from the common route tuple.
func (s *Server) lookupPRID(ctx context.Context, ref repoNumberPathRef) (int64, error) {
	return s.db.GetPRIDByRepoAndNumber(ctx, ref.owner, ref.name, ref.number)
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

func parseOptionalInt(values queryValues, key string) int {
	v := values.Get(key)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0
	}
	return n
}

func parseBoolFlag(values queryValues, key string) bool {
	return values.Get(key) == "true"
}

func validateStarredRequest(body starredRequest) bool {
	return body.ItemType == "pr" || body.ItemType == "issue"
}

type queryValues interface {
	Get(string) string
}
