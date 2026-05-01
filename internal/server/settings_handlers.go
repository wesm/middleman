package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/wesm/middleman/internal/config"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/workspace/localruntime"
)

type settingsResponse struct {
	Repos    []ghclient.ConfiguredRepoStatus `json:"repos"`
	Activity config.Activity                 `json:"activity"`
	Terminal config.Terminal                 `json:"terminal"`
	Agents   []config.Agent                  `json:"agents"`
}

type updateSettingsRequest struct {
	Activity *config.Activity `json:"activity,omitempty"`
	Terminal *config.Terminal `json:"terminal,omitempty"`
	Agents   *[]config.Agent  `json:"agents,omitempty"`
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
	repos := slices.Clone(s.cfg.Repos)
	activity := s.cfg.Activity
	terminal := s.cfg.Terminal
	agents := cloneConfigAgents(s.cfg.Agents)
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
		Agents:   agents,
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

func (s *Server) persistResolvedRepos(
	ctx context.Context,
	repos []ghclient.RepoRef,
) error {
	for _, repo := range repos {
		if _, err := s.db.UpsertRepo(
			ctx, repo.PlatformHost, repo.Owner, repo.Name,
		); err != nil {
			return fmt.Errorf(
				"upsert resolved repo %s/%s: %w",
				repo.Owner, repo.Name, err,
			)
		}
	}
	return nil
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

func (s *Server) defaultPlatformHost() string {
	if s.cfg == nil {
		return "github.com"
	}
	s.cfgMu.Lock()
	host := s.cfg.DefaultPlatformHost
	s.cfgMu.Unlock()
	if strings.TrimSpace(host) == "" {
		return "github.com"
	}
	return strings.ToLower(strings.TrimSpace(host))
}

func classifyResolveError(err error) (int, string) {
	switch {
	case errors.Is(err, ghclient.ErrConfiguredRepoArchived):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusBadGateway, "GitHub API error: " + err.Error()
	}
}

func (s *Server) getSettings(
	_ context.Context, _ *struct{},
) (*getSettingsOutput, error) {
	if s.cfg == nil {
		return nil, huma.Error404NotFound("settings not available")
	}

	return &getSettingsOutput{Body: s.buildLocalSettingsResponse()}, nil
}

func (s *Server) updateSettings(
	_ context.Context, input *updateSettingsInput,
) (*settingsOutput, error) {
	if s.cfgPath == "" {
		return nil, huma.Error404NotFound("settings not available")
	}

	s.cfgMu.Lock()
	prevActivity := s.cfg.Activity
	prevTerminal := s.cfg.Terminal
	prevAgents := cloneConfigAgents(s.cfg.Agents)
	if input.Body.Activity != nil {
		candidate := *input.Body.Activity
		if candidate.ViewMode == "" {
			candidate.ViewMode = "threaded"
		}
		if candidate.TimeRange == "" {
			candidate.TimeRange = "7d"
		}
		s.cfg.Activity = candidate
	}
	if input.Body.Terminal != nil {
		s.cfg.Terminal = *input.Body.Terminal
	}
	if input.Body.Agents != nil {
		s.cfg.Agents = cloneConfigAgents(*input.Body.Agents)
	}
	if err := s.cfg.Validate(); err != nil {
		s.cfg.Activity = prevActivity
		s.cfg.Terminal = prevTerminal
		s.cfg.Agents = prevAgents
		s.cfgMu.Unlock()
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Activity = prevActivity
		s.cfg.Terminal = prevTerminal
		s.cfg.Agents = prevAgents
		s.cfgMu.Unlock()
		return nil, huma.Error500InternalServerError(
			"save config: " + err.Error())
	}
	s.refreshRuntimeTargetsLocked()
	s.cfgMu.Unlock()

	return &settingsOutput{Body: s.buildLocalSettingsResponse()}, nil
}

func cloneConfigAgents(agents []config.Agent) []config.Agent {
	if agents == nil {
		return []config.Agent{}
	}
	cloned := make([]config.Agent, len(agents))
	for i, agent := range agents {
		cloned[i] = agent
		cloned[i].Command = slices.Clone(agent.Command)
	}
	return cloned
}

func (s *Server) refreshRuntimeTargetsLocked() {
	if s.runtime == nil || s.cfg == nil {
		return
	}
	tmuxCmd := s.cfg.TmuxCommand()
	s.runtime.UpdateTargets(localruntime.ResolveLaunchTargets(
		s.cfg.Agents, tmuxCmd, nil,
	))
}

func (s *Server) addConfiguredRepo(
	ctx context.Context, input *addRepoInput,
) (*settingsOutput, error) {
	if s.cfgPath == "" {
		return nil, huma.Error404NotFound("settings not available")
	}
	if input.Body.Owner == "" || input.Body.Name == "" {
		return nil, huma.Error400BadRequest("owner and name are required")
	}

	newRepo := config.Repo{Owner: input.Body.Owner, Name: input.Body.Name}

	// Pre-check (racy but gives a fast 400 before the GitHub call).
	s.cfgMu.Lock()
	for _, rp := range s.cfg.Repos {
		if sameConfiguredRepo(rp, newRepo) {
			s.cfgMu.Unlock()
			return nil, huma.Error400BadRequest(
				input.Body.Owner + "/" + input.Body.Name +
					" is already configured")
		}
	}
	allRepos := append(slices.Clone(s.cfg.Repos), newRepo)
	s.cfgMu.Unlock()

	_, expanded, err := ghclient.ResolveConfiguredRepo(
		ctx, s.configuredClients(allRepos), newRepo,
	)
	if err != nil {
		status, msg := classifyResolveError(err)
		return nil, huma.NewError(status, msg)
	}

	// Re-acquire lock and apply the addition to current state
	// so concurrent activity/settings changes are not lost.
	s.cfgMu.Lock()
	for _, rp := range s.cfg.Repos {
		if sameConfiguredRepo(rp, newRepo) {
			s.cfgMu.Unlock()
			return nil, huma.Error400BadRequest(
				input.Body.Owner + "/" + input.Body.Name +
					" is already configured")
		}
	}
	s.cfg.Repos = append(s.cfg.Repos, newRepo)
	if err := s.cfg.Validate(); err != nil {
		s.cfg.Repos = s.cfg.Repos[:len(s.cfg.Repos)-1]
		s.cfgMu.Unlock()
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Repos = s.cfg.Repos[:len(s.cfg.Repos)-1]
		s.cfgMu.Unlock()
		return nil, huma.Error500InternalServerError(
			"save config: " + err.Error())
	}
	s.mergeTrackedRepos(expanded)
	s.cfgMu.Unlock()

	s.syncer.TriggerRun(context.WithoutCancel(ctx))
	return &settingsOutput{Body: s.buildLocalSettingsResponse()}, nil
}

func (s *Server) refreshConfiguredRepo(
	ctx context.Context, input *repoConfigInput,
) (*settingsOutput, error) {
	if s.cfgPath == "" {
		return nil, huma.Error404NotFound("settings not available")
	}

	owner := input.Owner
	name := input.Name

	s.cfgMu.Lock()
	repos := slices.Clone(s.cfg.Repos)
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
		return nil, huma.Error404NotFound(
			owner + "/" + name + " is not configured")
	}
	if !target.HasNameGlob() {
		return nil, huma.Error400BadRequest(
			"refresh is only supported for glob patterns")
	}

	_, expanded, err := ghclient.ResolveConfiguredRepo(
		ctx, s.configuredClients(repos), *target,
	)
	if err != nil {
		status, msg := classifyResolveError(err)
		return nil, huma.NewError(status, msg)
	}

	// Re-acquire cfgMu and verify the target glob still exists
	// in the config before applying the resolved matches.
	// Without this, a concurrent DELETE on the same glob
	// could run between the unlock above and the helper below,
	// and the stale expansion would resurrect removed repos.
	s.cfgMu.Lock()
	stillExists := false
	currentRepos := slices.Clone(s.cfg.Repos)
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
		return nil, huma.Error404NotFound(
			owner + "/" + name + " is no longer configured")
	}
	if err := s.persistResolvedRepos(ctx, expanded); err != nil {
		s.cfgMu.Unlock()
		return nil, huma.Error500InternalServerError(
			"persist resolved repos: " + err.Error())
	}
	s.replaceGlobRepos(*target, expanded, currentRepos)
	s.cfgMu.Unlock()

	s.syncer.TriggerRun(context.WithoutCancel(ctx))
	return &settingsOutput{Body: s.buildLocalSettingsResponse()}, nil
}

func (s *Server) deleteConfiguredRepo(
	_ context.Context, input *repoConfigInput,
) (*struct{}, error) {
	if s.cfgPath == "" {
		return nil, huma.Error404NotFound("settings not available")
	}

	owner := input.Owner
	name := input.Name

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
		return nil, huma.Error404NotFound(
			owner + "/" + name + " is not configured")
	}

	prev := slices.Clone(s.cfg.Repos)
	s.cfg.Repos = append(
		s.cfg.Repos[:idx], s.cfg.Repos[idx+1:]...,
	)
	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Repos = prev
		s.cfgMu.Unlock()
		return nil, huma.Error500InternalServerError(
			"save config: " + err.Error())
	}
	s.removeConfigRepos(s.cfg.Repos)
	s.cfgMu.Unlock()

	return nil, nil
}
