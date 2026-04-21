package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/wesm/middleman/internal/config"
	ghclient "github.com/wesm/middleman/internal/github"
)

type settingsResponse struct {
	Repos    []ghclient.ConfiguredRepoStatus `json:"repos"`
	Activity config.Activity                 `json:"activity"`
	Terminal config.Terminal                 `json:"terminal"`
}

type updateSettingsRequest struct {
	Activity *config.Activity `json:"activity,omitempty"`
	Terminal *config.Terminal `json:"terminal,omitempty"`
}

func (s *Server) configuredClients(
	repos []config.Repo,
) map[string]ghclient.Client {
	clients := make(map[string]ghclient.Client)
	for _, repo := range repos {
		host := repo.PlatformHostOrDefault()
		if _, ok := clients[host]; ok {
			continue
		}
		client, err := s.syncer.ClientForHost(host)
		if err != nil {
			continue
		}
		clients[host] = client
	}
	return clients
}

// buildLocalSettingsResponse builds the settings response from
// in-memory state (syncer tracked repos) without calling GitHub.
func (s *Server) buildLocalSettingsResponse() settingsResponse {
	s.cfgMu.Lock()
	repos := append([]config.Repo(nil), s.cfg.Repos...)
	activity := s.cfg.Activity
	terminal := s.cfg.Terminal
	s.cfgMu.Unlock()

	tracked := s.syncer.TrackedRepos()
	configured := make(
		[]ghclient.ConfiguredRepoStatus, len(repos),
	)
	for i, raw := range repos {
		configured[i] = ghclient.ConfiguredRepoStatus{
			Owner:            raw.Owner,
			Name:             raw.Name,
			IsGlob:           raw.HasNameGlob(),
			MatchedRepoCount: matchedRepoCount(raw, tracked),
		}
	}
	return settingsResponse{
		Repos:    configured,
		Activity: activity,
		Terminal: terminal,
	}
}

func matchedRepoCount(
	raw config.Repo, tracked []ghclient.RepoRef,
) int {
	host := raw.PlatformHostOrDefault()
	count := 0
	for _, repo := range tracked {
		if !samePlatformHost(repo.PlatformHost, host) ||
			!strings.EqualFold(repo.Owner, raw.Owner) {
			continue
		}
		if raw.HasNameGlob() {
			matched, _ := path.Match(
				strings.ToLower(raw.Name),
				strings.ToLower(repo.Name),
			)
			if matched {
				count++
			}
		} else if strings.EqualFold(repo.Name, raw.Name) {
			count++
		}
	}
	return count
}

// mergeTrackedRepos adds repos to the syncer's tracked set,
// deduplicating by host/owner/name.
func (s *Server) mergeTrackedRepos(add []ghclient.RepoRef) {
	current := s.syncer.TrackedRepos()
	seen := make(map[string]struct{}, len(current))
	for _, r := range current {
		key := strings.ToLower(r.PlatformHost) + "\x00" +
			strings.ToLower(r.Owner) + "\x00" +
			strings.ToLower(r.Name)
		seen[key] = struct{}{}
	}
	for _, r := range add {
		key := strings.ToLower(r.PlatformHost) + "\x00" +
			strings.ToLower(r.Owner) + "\x00" +
			strings.ToLower(r.Name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		current = append(current, r)
	}
	s.syncer.SetRepos(current)
}

// replaceGlobRepos removes repos that only match the refreshed
// glob entry, preserves repos still matched by other config
// entries, then adds the newly resolved matches.
func (s *Server) replaceGlobRepos(
	raw config.Repo,
	expanded []ghclient.RepoRef,
	configured []config.Repo,
) {
	current := s.syncer.TrackedRepos()
	kept := make([]ghclient.RepoRef, 0, len(current))
	seen := make(map[string]struct{}, len(current)+len(expanded))
	for _, repo := range current {
		if repoMatchesConfig(repo, raw) &&
			!repoMatchesOtherConfig(repo, raw, configured) {
			continue
		}
		appendTrackedRepo(&kept, seen, repo)
	}
	for _, repo := range expanded {
		appendTrackedRepo(&kept, seen, repo)
	}
	s.syncer.SetRepos(kept)
}

// removeConfigRepos keeps only tracked repos that match at
// least one of the remaining config entries.
func (s *Server) removeConfigRepos(
	remaining []config.Repo,
) {
	current := s.syncer.TrackedRepos()
	kept := make([]ghclient.RepoRef, 0, len(current))
	for _, repo := range current {
		for _, raw := range remaining {
			if repoMatchesConfig(repo, raw) {
				kept = append(kept, repo)
				break
			}
		}
	}
	s.syncer.SetRepos(kept)
}

func repoMatchesOtherConfig(
	repo ghclient.RepoRef,
	target config.Repo,
	configured []config.Repo,
) bool {
	for _, raw := range configured {
		if sameConfiguredRepo(raw, target) {
			continue
		}
		if repoMatchesConfig(repo, raw) {
			return true
		}
	}
	return false
}

func sameConfiguredRepo(left, right config.Repo) bool {
	return samePlatformHost(
		left.PlatformHostOrDefault(),
		right.PlatformHostOrDefault(),
	) &&
		strings.EqualFold(left.Owner, right.Owner) &&
		strings.EqualFold(left.Name, right.Name)
}

func repoMatchesConfig(
	repo ghclient.RepoRef, raw config.Repo,
) bool {
	host := raw.PlatformHostOrDefault()
	if !samePlatformHost(repo.PlatformHost, host) ||
		!strings.EqualFold(repo.Owner, raw.Owner) {
		return false
	}
	if raw.HasNameGlob() {
		matched, _ := path.Match(
			strings.ToLower(raw.Name),
			strings.ToLower(repo.Name),
		)
		return matched
	}
	return strings.EqualFold(repo.Name, raw.Name)
}

func appendTrackedRepo(
	dst *[]ghclient.RepoRef,
	seen map[string]struct{},
	repo ghclient.RepoRef,
) {
	key := strings.ToLower(repo.PlatformHost) + "\x00" +
		strings.ToLower(repo.Owner) + "\x00" +
		strings.ToLower(repo.Name)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*dst = append(*dst, repo)
}

func samePlatformHost(left, right string) bool {
	if left == "" {
		left = "github.com"
	}
	if right == "" {
		right = "github.com"
	}
	return strings.EqualFold(left, right)
}

func classifyResolveError(err error) (int, string) {
	switch {
	case errors.Is(err, ghclient.ErrConfiguredRepoArchived):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusBadGateway, "GitHub API error: " + err.Error()
	}
}

func (s *Server) handleGetSettings(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfg == nil {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	writeJSON(w, http.StatusOK, s.buildLocalSettingsResponse())
}

func (s *Server) handleUpdateSettings(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfgPath == "" {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	var body updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	s.cfgMu.Lock()
	prevActivity := s.cfg.Activity
	prevTerminal := s.cfg.Terminal
	if body.Activity != nil {
		candidate := *body.Activity
		if candidate.ViewMode == "" {
			candidate.ViewMode = "threaded"
		}
		if candidate.TimeRange == "" {
			candidate.TimeRange = "7d"
		}
		s.cfg.Activity = candidate
	}
	if body.Terminal != nil {
		s.cfg.Terminal = *body.Terminal
	}
	if err := s.cfg.Validate(); err != nil {
		s.cfg.Activity = prevActivity
		s.cfg.Terminal = prevTerminal
		s.cfgMu.Unlock()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Activity = prevActivity
		s.cfg.Terminal = prevTerminal
		s.cfgMu.Unlock()
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}
	s.cfgMu.Unlock()

	writeJSON(w, http.StatusOK, s.buildLocalSettingsResponse())
}

func (s *Server) handleAddRepo(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfgPath == "" {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	var body struct {
		Owner string `json:"owner"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Owner == "" || body.Name == "" {
		writeError(w, http.StatusBadRequest,
			"owner and name are required")
		return
	}

	newRepo := config.Repo{Owner: body.Owner, Name: body.Name}

	// Pre-check (racy but gives a fast 400 before the GitHub call).
	s.cfgMu.Lock()
	for _, rp := range s.cfg.Repos {
		if sameConfiguredRepo(rp, newRepo) {
			s.cfgMu.Unlock()
			writeError(w, http.StatusBadRequest,
				body.Owner+"/"+body.Name+" is already configured")
			return
		}
	}
	allRepos := append(
		append([]config.Repo(nil), s.cfg.Repos...), newRepo,
	)
	s.cfgMu.Unlock()

	_, expanded, err := ghclient.ResolveConfiguredRepo(
		r.Context(), s.configuredClients(allRepos), newRepo,
	)
	if err != nil {
		status, msg := classifyResolveError(err)
		writeError(w, status, msg)
		return
	}

	// Re-acquire lock and apply the addition to current state
	// so concurrent activity/settings changes are not lost.
	s.cfgMu.Lock()
	for _, rp := range s.cfg.Repos {
		if sameConfiguredRepo(rp, newRepo) {
			s.cfgMu.Unlock()
			writeError(w, http.StatusBadRequest,
				body.Owner+"/"+body.Name+" is already configured")
			return
		}
	}
	s.cfg.Repos = append(s.cfg.Repos, newRepo)
	if err := s.cfg.Validate(); err != nil {
		s.cfg.Repos = s.cfg.Repos[:len(s.cfg.Repos)-1]
		s.cfgMu.Unlock()
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Repos = s.cfg.Repos[:len(s.cfg.Repos)-1]
		s.cfgMu.Unlock()
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}
	s.mergeTrackedRepos(expanded)
	s.cfgMu.Unlock()

	s.syncer.TriggerRun(context.WithoutCancel(r.Context()))
	writeJSON(w, http.StatusCreated, s.buildLocalSettingsResponse())
}

func (s *Server) handleRefreshRepo(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfgPath == "" {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	owner := r.PathValue("owner")
	name := r.PathValue("name")

	s.cfgMu.Lock()
	repos := append([]config.Repo(nil), s.cfg.Repos...)
	s.cfgMu.Unlock()

	var target *config.Repo
	for i := range repos {
		if sameConfiguredRepo(
			repos[i],
			config.Repo{Owner: owner, Name: name},
		) {
			target = &repos[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound,
			owner+"/"+name+" is not configured")
		return
	}
	if !target.HasNameGlob() {
		writeError(w, http.StatusBadRequest,
			"refresh is only supported for glob patterns")
		return
	}

	_, expanded, err := ghclient.ResolveConfiguredRepo(
		r.Context(), s.configuredClients(repos), *target,
	)
	if err != nil {
		status, msg := classifyResolveError(err)
		writeError(w, status, msg)
		return
	}

	// Re-acquire cfgMu and verify the target glob still exists
	// in the config before applying the resolved matches.
	// Without this, a concurrent DELETE on the same glob
	// could run between the unlock above and the helper below,
	// and the stale expansion would resurrect removed repos.
	s.cfgMu.Lock()
	stillExists := false
	currentRepos := append([]config.Repo(nil), s.cfg.Repos...)
	for _, rp := range currentRepos {
		if sameConfiguredRepo(
			rp,
			config.Repo{Owner: owner, Name: name},
		) {
			stillExists = true
			break
		}
	}
	if !stillExists {
		s.cfgMu.Unlock()
		writeError(w, http.StatusNotFound,
			owner+"/"+name+" is no longer configured")
		return
	}
	s.replaceGlobRepos(*target, expanded, currentRepos)
	s.cfgMu.Unlock()

	s.syncer.TriggerRun(context.WithoutCancel(r.Context()))
	writeJSON(w, http.StatusOK, s.buildLocalSettingsResponse())
}

func (s *Server) handleDeleteRepo(
	w http.ResponseWriter, r *http.Request,
) {
	if s.cfgPath == "" {
		writeError(w, http.StatusNotFound,
			"settings not available")
		return
	}

	owner := r.PathValue("owner")
	name := r.PathValue("name")

	s.cfgMu.Lock()
	idx := -1
	for i, rp := range s.cfg.Repos {
		if sameConfiguredRepo(
			rp,
			config.Repo{Owner: owner, Name: name},
		) {
			idx = i
			break
		}
	}
	if idx == -1 {
		s.cfgMu.Unlock()
		writeError(w, http.StatusNotFound,
			owner+"/"+name+" is not configured")
		return
	}

	prev := append([]config.Repo(nil), s.cfg.Repos...)
	s.cfg.Repos = append(
		s.cfg.Repos[:idx], s.cfg.Repos[idx+1:]...,
	)
	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Repos = prev
		s.cfgMu.Unlock()
		writeError(w, http.StatusInternalServerError,
			"save config: "+err.Error())
		return
	}
	s.removeConfigRepos(s.cfg.Repos)
	s.cfgMu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}
