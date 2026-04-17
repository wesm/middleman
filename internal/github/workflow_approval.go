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
// to those that target the given PR. An empty run.PullRequests array is
// treated as "association unavailable" rather than "not this PR": GitHub
// returns an empty array for fork-triggered runs, which is precisely when
// workflow approval is required. When the array is populated, the PR number
// must match so we don't mis-attribute runs across two PRs that happen to
// share a head SHA.
func FilterWorkflowRunsAwaitingApproval(
	runs []*gh.WorkflowRun,
	number int,
	headSHA string,
) []*gh.WorkflowRun {
	var filtered []*gh.WorkflowRun
	for _, run := range runs {
		if run.GetHeadSHA() != headSHA {
			continue
		}
		if !workflowRunMayTargetPR(run, number) {
			continue
		}
		filtered = append(filtered, run)
	}
	return filtered
}

func workflowRunMayTargetPR(run *gh.WorkflowRun, number int) bool {
	if len(run.PullRequests) == 0 {
		return true
	}
	for _, pr := range run.PullRequests {
		if pr.GetNumber() == number {
			return true
		}
	}
	return false
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
