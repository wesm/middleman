package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/wesm/middleman/internal/db"
)

const (
	capabilityCommentMutation  = "comment_mutation"
	capabilityStateMutation    = "state_mutation"
	capabilityMergeMutation    = "merge_mutation"
	capabilityReviewMutation   = "review_mutation"
	capabilityWorkflowApproval = "workflow_approval"
	capabilityReadyForReview   = "ready_for_review"
	capabilityIssueMutation    = "issue_mutation"
	capabilityReadLabels       = "read_labels"
	capabilityLabelMutation    = "label_mutation"
)

type unsupportedCapabilityDetail struct {
	code         string
	provider     string
	platformHost string
	capability   string
}

func (d unsupportedCapabilityDetail) Error() string {
	return d.code
}

func (d unsupportedCapabilityDetail) ErrorDetail() *huma.ErrorDetail {
	return &huma.ErrorDetail{
		Message:  d.code,
		Location: "provider.capabilities",
		Value: map[string]string{
			"code":          d.code,
			"provider":      d.provider,
			"platform_host": d.platformHost,
			"capability":    d.capability,
		},
	}
}

func unsupportedCapabilityProblem(
	repo db.Repo,
	capability string,
) huma.StatusError {
	return huma.NewError(
		http.StatusConflict,
		"Unsupported provider capability",
		unsupportedCapabilityDetail{
			code:         "unsupported_capability",
			provider:     string(repoProviderKind(repo)),
			platformHost: repoProviderHost(repo),
			capability:   capability,
		},
	)
}

func capabilityEnabled(
	caps providerCapabilitiesResponse,
	capability string,
) bool {
	switch capability {
	case capabilityCommentMutation:
		return caps.CommentMutation
	case capabilityStateMutation:
		return caps.StateMutation
	case capabilityMergeMutation:
		return caps.MergeMutation
	case capabilityReviewMutation:
		return caps.ReviewMutation
	case capabilityWorkflowApproval:
		return caps.WorkflowApproval
	case capabilityReadyForReview:
		return caps.ReadyForReview
	case capabilityIssueMutation:
		return caps.IssueMutation
	case capabilityReadLabels:
		return caps.ReadLabels
	case capabilityLabelMutation:
		return caps.LabelMutation
	default:
		return false
	}
}

func (s *Server) requireRepoRouteCapability(
	ctx context.Context,
	provider, platformHost, owner, name, capability string,
) (*db.Repo, error) {
	repo, err := s.lookupRepoByProviderRoute(
		ctx, provider, platformHost, owner, name,
	)
	if err != nil {
		return nil, providerRouteLookupError(err)
	}
	if !capabilityEnabled(s.capabilitiesForRepo(*repo), capability) {
		return nil, unsupportedCapabilityProblem(*repo, capability)
	}
	return repo, nil
}
