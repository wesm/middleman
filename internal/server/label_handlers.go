package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/wesm/middleman/internal/db"
)

type listRepoLabelsOutput = bodyOutput[repoLabelsResponse]
type setLabelsOutput = bodyOutput[itemLabelsResponse]

type setPullLabelsInput struct {
	Provider     string `path:"provider"`
	PlatformHost string
	Owner        string `path:"owner"`
	Name         string `path:"name"`
	Number       int    `path:"number"`
	Body         setLabelsRequest
}

type setIssueLabelsInput struct {
	Provider     string `path:"provider"`
	PlatformHost string
	Owner        string `path:"owner"`
	Name         string `path:"name"`
	Number       int    `path:"number"`
	Body         setLabelsRequest
}

type setLabelsRequest struct {
	Labels []string `json:"labels"`
}

type repoLabelsResponse struct {
	Labels    []db.Label `json:"labels"`
	Stale     bool       `json:"stale"`
	Syncing   bool       `json:"syncing"`
	SyncedAt  string     `json:"synced_at,omitempty"`
	CheckedAt string     `json:"checked_at,omitempty"`
	SyncError string     `json:"sync_error"`
}

type itemLabelsResponse struct {
	Labels []db.Label `json:"labels"`
}

func (s *Server) listRepoLabels(
	ctx context.Context,
	input *getRepoInput,
) (*listRepoLabelsOutput, error) {
	repo, err := s.lookupRepoByProviderRoute(
		ctx, input.Provider, input.PlatformHost, input.Owner, input.Name,
	)
	if err != nil {
		return nil, providerRouteLookupError(err)
	}
	if !capabilityEnabled(s.capabilitiesForRepo(*repo), capabilityReadLabels) {
		return nil, unsupportedCapabilityProblem(*repo, capabilityReadLabels)
	}

	labels, freshness, err := s.db.ListRepoLabelCatalog(ctx, repo.ID)
	if err != nil {
		return nil, huma.Error500InternalServerError("list repo labels failed")
	}
	return &listRepoLabelsOutput{Body: repoLabelsResponse{
		Labels:    labels,
		Stale:     labelCatalogStale(freshness, time.Now().UTC()),
		Syncing:   false,
		SyncedAt:  optionalTimeString(freshness.SyncedAt),
		CheckedAt: optionalTimeString(freshness.CheckedAt),
		SyncError: freshness.SyncError,
	}}, nil
}

func (s *Server) setPullLabels(
	ctx context.Context,
	input *setPullLabelsInput,
) (*setLabelsOutput, error) {
	repo, labels, err := s.resolveRequestedLabels(
		ctx,
		input.Provider,
		input.PlatformHost,
		input.Owner,
		input.Name,
		input.Body.Labels,
	)
	if err != nil {
		return nil, err
	}

	mr, err := s.db.GetMergeRequestByRepoIDAndNumber(ctx, repo.ID, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get pull failed")
	}
	if mr == nil {
		return nil, huma.Error404NotFound("pull not found")
	}
	if err := s.db.ReplaceMergeRequestLabels(ctx, repo.ID, mr.ID, labels); err != nil {
		return nil, huma.Error500InternalServerError("save pull labels failed")
	}
	return &setLabelsOutput{Body: itemLabelsResponse{Labels: labels}}, nil
}

func (s *Server) setIssueLabels(
	ctx context.Context,
	input *setIssueLabelsInput,
) (*setLabelsOutput, error) {
	repo, labels, err := s.resolveRequestedLabels(
		ctx,
		input.Provider,
		input.PlatformHost,
		input.Owner,
		input.Name,
		input.Body.Labels,
	)
	if err != nil {
		return nil, err
	}

	issue, err := s.db.GetIssueByRepoIDAndNumber(ctx, repo.ID, input.Number)
	if err != nil {
		return nil, huma.Error500InternalServerError("get issue failed")
	}
	if issue == nil {
		return nil, huma.Error404NotFound("issue not found")
	}
	if err := s.db.ReplaceIssueLabels(ctx, repo.ID, issue.ID, labels); err != nil {
		return nil, huma.Error500InternalServerError("save issue labels failed")
	}
	return &setLabelsOutput{Body: itemLabelsResponse{Labels: labels}}, nil
}

func (s *Server) resolveRequestedLabels(
	ctx context.Context,
	provider string,
	platformHost string,
	owner string,
	name string,
	names []string,
) (*db.Repo, []db.Label, error) {
	repo, err := s.lookupRepoByProviderRoute(ctx, provider, platformHost, owner, name)
	if err != nil {
		return nil, nil, providerRouteLookupError(err)
	}
	caps := s.capabilitiesForRepo(*repo)
	if !capabilityEnabled(caps, capabilityReadLabels) {
		return nil, nil, unsupportedCapabilityProblem(*repo, capabilityReadLabels)
	}
	if !capabilityEnabled(caps, capabilityLabelMutation) {
		return nil, nil, unsupportedCapabilityProblem(*repo, capabilityLabelMutation)
	}

	catalog, _, err := s.db.ListRepoLabelCatalog(ctx, repo.ID)
	if err != nil {
		return nil, nil, huma.Error500InternalServerError("list repo labels failed")
	}
	catalogByName := make(map[string]db.Label, len(catalog))
	for _, label := range catalog {
		catalogByName[label.Name] = label
	}

	seen := make(map[string]struct{}, len(names))
	labels := make([]db.Label, 0, len(names))
	for _, raw := range names {
		labelName := strings.TrimSpace(raw)
		if labelName == "" {
			return nil, nil, huma.Error400BadRequest("label names must not be empty")
		}
		if _, ok := seen[labelName]; ok {
			return nil, nil, huma.Error400BadRequest(fmt.Sprintf("duplicate label %q", labelName))
		}
		label, ok := catalogByName[labelName]
		if !ok {
			return nil, nil, huma.Error400BadRequest(fmt.Sprintf("label %q is not in the repository label catalog", labelName))
		}
		seen[labelName] = struct{}{}
		labels = append(labels, label)
	}
	return repo, labels, nil
}

func labelCatalogStale(freshness db.LabelCatalogFreshness, now time.Time) bool {
	if freshness.CheckedAt == nil {
		return true
	}
	return freshness.CheckedAt.Before(now.Add(-10 * time.Minute))
}

func optionalTimeString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatUTCRFC3339(*t)
}
