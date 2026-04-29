package server

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/wesm/middleman/internal/config"
	ghclient "github.com/wesm/middleman/internal/github"
)

type repoPreviewInput struct {
	Body repoPreviewRequest
}

type repoPreviewRequest struct {
	Owner   string `json:"owner"`
	Pattern string `json:"pattern"`
}

type repoPreviewOutput struct {
	Body repoPreviewResponse
}

type repoPreviewResponse struct {
	Owner   string           `json:"owner"`
	Pattern string           `json:"pattern"`
	Repos   []repoPreviewRow `json:"repos"`
}

type repoPreviewRow struct {
	Owner             string  `json:"owner"`
	Name              string  `json:"name"`
	Description       *string `json:"description"`
	Private           bool    `json:"private"`
	PushedAt          *string `json:"pushed_at"`
	AlreadyConfigured bool    `json:"already_configured"`
}

type bulkAddReposInput struct {
	Body bulkAddReposRequest
}

type bulkAddReposRequest struct {
	Repos []bulkAddRepoRequest `json:"repos"`
}

type bulkAddReposOutput struct {
	Status int `status:"201"`
	Body   settingsResponse
}

type bulkAddRepoRequest struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type resolvedBulkRepo struct {
	Config config.Repo
	Ref    ghclient.RepoRef
}

func normalizeImportOwnerPattern(owner, pattern string) (string, string, error) {
	owner = strings.TrimSpace(owner)
	pattern = strings.TrimSpace(pattern)
	if owner == "" || pattern == "" {
		return "", "", fmt.Errorf("owner and pattern are required")
	}
	if strings.Contains(owner, "/") || strings.ContainsAny(owner, "*?[]") {
		return "", "", fmt.Errorf("glob syntax in owner is not supported")
	}
	if strings.Contains(pattern, "/") {
		return "", "", fmt.Errorf("pattern must not contain /")
	}
	if _, err := path.Match(strings.ToLower(pattern), ""); err != nil {
		return "", "", fmt.Errorf("invalid glob pattern: %w", err)
	}
	return owner, pattern, nil
}

func normalizeExactRepoInput(owner, name string) (config.Repo, error) {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if owner == "" || name == "" {
		return config.Repo{}, fmt.Errorf("owner and name are required")
	}
	if strings.Contains(owner, "/") || strings.Contains(name, "/") ||
		strings.ContainsAny(owner, "*?[]") || strings.ContainsAny(name, "*?[]") {
		return config.Repo{}, fmt.Errorf("bulk add only accepts exact owner/name repositories")
	}
	return config.Repo{Owner: owner, Name: name}, nil
}

func exactConfiguredRepoSet(repos []config.Repo) map[string]struct{} {
	set := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		if repo.HasNameGlob() {
			continue
		}
		owner := strings.ToLower(strings.TrimSpace(repo.Owner))
		name := strings.ToLower(strings.TrimSpace(repo.Name))
		if owner == "" || name == "" {
			continue
		}
		set[owner+"/"+name] = struct{}{}
	}
	return set
}

func buildRepoPreviewRows(
	ctx context.Context,
	client ghclient.Client,
	exactConfigured map[string]struct{},
	owner, pattern string,
) ([]repoPreviewRow, error) {
	repos, err := client.ListRepositoriesByOwner(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf(
			"list repositories for preview %s/%s: %w", owner, pattern, err,
		)
	}

	rows := make([]repoPreviewRow, 0, len(repos))
	for _, repo := range repos {
		if repo.GetArchived() {
			continue
		}
		name := repo.GetName()
		matched, err := path.Match(strings.ToLower(pattern), strings.ToLower(name))
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}
		if !matched {
			continue
		}
		canonicalOwner := repo.GetOwner().GetLogin()
		if canonicalOwner == "" {
			canonicalOwner = owner
		}
		canonicalOwner = strings.ToLower(canonicalOwner)
		canonicalName := strings.ToLower(name)
		var pushedAt *string
		if repo.PushedAt != nil {
			formatted := repo.PushedAt.Time.UTC().Format(time.RFC3339)
			pushedAt = &formatted
		}
		_, already := exactConfigured[canonicalOwner+"/"+canonicalName]
		rows = append(rows, repoPreviewRow{
			Owner:             canonicalOwner,
			Name:              canonicalName,
			Description:       repo.Description,
			Private:           repo.GetPrivate(),
			PushedAt:          pushedAt,
			AlreadyConfigured: already,
		})
	}
	return rows, nil
}

func (s *Server) previewRepos(
	ctx context.Context,
	input *repoPreviewInput,
) (*repoPreviewOutput, error) {
	if s.cfgPath == "" {
		return nil, huma.Error404NotFound("settings not available")
	}

	owner, pattern, err := normalizeImportOwnerPattern(
		input.Body.Owner, input.Body.Pattern,
	)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	client, err := s.syncer.ClientForHost("github.com")
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub API error: " + err.Error())
	}

	s.cfgMu.Lock()
	repos := append([]config.Repo(nil), s.cfg.Repos...)
	s.cfgMu.Unlock()

	rows, err := buildRepoPreviewRows(
		ctx, client, exactConfiguredRepoSet(repos), owner, pattern,
	)
	if err != nil {
		return nil, huma.Error502BadGateway("GitHub API error: " + err.Error())
	}
	return &repoPreviewOutput{
		Body: repoPreviewResponse{
			Owner:   owner,
			Pattern: pattern,
			Repos:   rows,
		},
	}, nil
}

func validateBulkExactRepos(
	ctx context.Context,
	clients map[string]ghclient.Client,
	candidates []config.Repo,
) ([]resolvedBulkRepo, error) {
	seenInput := make(map[string]struct{}, len(candidates))
	seenResolved := make(map[string]struct{}, len(candidates))
	resolved := make([]resolvedBulkRepo, 0, len(candidates))
	for _, candidate := range candidates {
		key := strings.ToLower(candidate.Owner) + "/" + strings.ToLower(candidate.Name)
		if _, ok := seenInput[key]; ok {
			continue
		}
		seenInput[key] = struct{}{}

		_, refs, err := ghclient.ResolveConfiguredRepo(ctx, clients, candidate)
		if err != nil {
			return nil, err
		}
		if len(refs) != 1 {
			return nil, fmt.Errorf(
				"resolve exact repo %s/%s returned %d matches",
				candidate.Owner, candidate.Name, len(refs),
			)
		}
		ref := refs[0]
		resolvedKey := strings.ToLower(ref.Owner) + "/" + strings.ToLower(ref.Name)
		if _, ok := seenResolved[resolvedKey]; ok {
			continue
		}
		seenResolved[resolvedKey] = struct{}{}
		resolved = append(resolved, resolvedBulkRepo{
			Config: config.Repo{Owner: ref.Owner, Name: ref.Name},
			Ref:    ref,
		})
	}
	return resolved, nil
}

func (s *Server) applyBulkExactRepos(
	resolved []resolvedBulkRepo,
) (settingsResponse, int, error) {
	s.cfgMu.Lock()
	existing := exactConfiguredRepoSet(s.cfg.Repos)
	addConfigs := make([]config.Repo, 0, len(resolved))
	addRefs := make([]ghclient.RepoRef, 0, len(resolved))
	for _, repo := range resolved {
		key := strings.ToLower(repo.Config.Owner) + "/" + strings.ToLower(repo.Config.Name)
		if _, ok := existing[key]; ok {
			continue
		}
		existing[key] = struct{}{}
		addConfigs = append(addConfigs, repo.Config)
		addRefs = append(addRefs, repo.Ref)
	}
	if len(addConfigs) == 0 {
		s.cfgMu.Unlock()
		return settingsResponse{}, http.StatusBadRequest,
			fmt.Errorf("all selected repositories are already configured")
	}

	prev := append([]config.Repo(nil), s.cfg.Repos...)
	s.cfg.Repos = append(s.cfg.Repos, addConfigs...)
	if err := s.cfg.Validate(); err != nil {
		s.cfg.Repos = prev
		s.cfgMu.Unlock()
		return settingsResponse{}, http.StatusBadRequest, err
	}
	if err := s.cfg.Save(s.cfgPath); err != nil {
		s.cfg.Repos = prev
		s.cfgMu.Unlock()
		return settingsResponse{}, http.StatusInternalServerError,
			fmt.Errorf("save config: %w", err)
	}
	s.mergeTrackedRepos(addRefs)
	s.cfgMu.Unlock()

	return s.buildLocalSettingsResponse(), http.StatusCreated, nil
}

func (s *Server) bulkAddRepos(
	ctx context.Context,
	input *bulkAddReposInput,
) (*bulkAddReposOutput, error) {
	if s.cfgPath == "" {
		return nil, huma.Error404NotFound("settings not available")
	}

	if len(input.Body.Repos) == 0 {
		return nil, huma.Error400BadRequest("repos are required")
	}

	candidates := make([]config.Repo, 0, len(input.Body.Repos))
	s.cfgMu.Lock()
	existing := exactConfiguredRepoSet(s.cfg.Repos)
	s.cfgMu.Unlock()
	for _, raw := range input.Body.Repos {
		repo, err := normalizeExactRepoInput(raw.Owner, raw.Name)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		key := strings.ToLower(repo.Owner) + "/" + strings.ToLower(repo.Name)
		if _, ok := existing[key]; ok {
			continue
		}
		candidates = append(candidates, repo)
	}
	if len(candidates) == 0 {
		return nil, huma.Error400BadRequest(
			"all selected repositories are already configured",
		)
	}

	clients := s.configuredClients(candidates)
	if _, ok := clients["github.com"]; !ok {
		client, err := s.syncer.ClientForHost("github.com")
		if err != nil {
			return nil, huma.Error502BadGateway("GitHub API error: " + err.Error())
		}
		clients["github.com"] = client
	}
	resolved, err := validateBulkExactRepos(ctx, clients, candidates)
	if err != nil {
		status, msg := classifyResolveError(err)
		return nil, huma.NewError(status, msg)
	}
	resp, status, err := s.applyBulkExactRepos(resolved)
	if err != nil {
		return nil, huma.NewError(status, err.Error())
	}

	s.syncer.TriggerRun(context.WithoutCancel(ctx))
	return &bulkAddReposOutput{Status: http.StatusCreated, Body: resp}, nil
}
