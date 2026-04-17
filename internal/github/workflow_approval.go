package github

import gh "github.com/google/go-github/v84/github"

// WorkflowApprovalState describes whether workflow approval is needed for a PR.
type WorkflowApprovalState struct {
	Checked  bool
	Required bool
	Count    int
	RunIDs   []int64
}

// FilterWorkflowRunsAwaitingApproval narrows action-required workflow runs down
// to those at the given PR head SHA. The run.PullRequests association is not
// checked because GitHub returns an empty pull_requests array for fork-based
// PRs — which is precisely when workflow approval is required.
func FilterWorkflowRunsAwaitingApproval(
	runs []*gh.WorkflowRun,
	headSHA string,
) []*gh.WorkflowRun {
	var filtered []*gh.WorkflowRun
	for _, run := range runs {
		if run.GetHeadSHA() != headSHA {
			continue
		}
		filtered = append(filtered, run)
	}
	return filtered
}

// WorkflowApprovalStateFromRuns converts matched workflow runs into state.
func WorkflowApprovalStateFromRuns(runs []*gh.WorkflowRun) WorkflowApprovalState {
	state := WorkflowApprovalState{Checked: true}
	for _, run := range runs {
		state.RunIDs = append(state.RunIDs, run.GetID())
	}
	state.Count = len(state.RunIDs)
	state.Required = state.Count > 0
	return state
}
