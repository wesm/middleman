package github

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	gh "github.com/google/go-github/v84/github"
	"github.com/wesm/middleman/internal/db"
)

// sanitizeURL returns the URL if it uses a safe scheme, or empty string.
func sanitizeURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "https" || scheme == "http" {
		return raw
	}
	return ""
}

// NormalizePR converts a GitHub PullRequest to a db.PullRequest.
// If the PR is merged, State is set to "merged". LastActivityAt is
// initialized to UpdatedAt.
func NormalizePR(repoID int64, ghPR *gh.PullRequest) *db.PullRequest {
	pr := &db.PullRequest{
		RepoID:    repoID,
		GitHubID:  ghPR.GetID(),
		Number:    ghPR.GetNumber(),
		URL:       ghPR.GetHTMLURL(),
		Title:     ghPR.GetTitle(),
		Author:    loginOrEmpty(ghPR.GetUser()),
		State:     ghPR.GetState(),
		IsDraft:   ghPR.GetDraft(),
		Body:      ghPR.GetBody(),
		Additions: ghPR.GetAdditions(),
		Deletions: ghPR.GetDeletions(),
	}

	if ghPR.GetMerged() {
		pr.State = "merged"
	}

	if ghPR.CreatedAt != nil {
		pr.CreatedAt = ghPR.CreatedAt.Time
	}
	if ghPR.UpdatedAt != nil {
		pr.UpdatedAt = ghPR.UpdatedAt.Time
		pr.LastActivityAt = ghPR.UpdatedAt.Time
	}
	if ghPR.MergedAt != nil {
		t := ghPR.MergedAt.Time
		pr.MergedAt = &t
	}
	if ghPR.ClosedAt != nil {
		t := ghPR.ClosedAt.Time
		pr.ClosedAt = &t
	}
	if ghPR.GetHead() != nil {
		pr.HeadBranch = ghPR.GetHead().GetRef()
	}
	if ghPR.GetBase() != nil {
		pr.BaseBranch = ghPR.GetBase().GetRef()
	}

	return pr
}

// NormalizeCommentEvent converts a GitHub IssueComment to a db.PREvent.
func NormalizeCommentEvent(prID int64, c *gh.IssueComment) db.PREvent {
	event := normalizeIssueCommentBase(c)
	event.PRID = prID
	event.DedupeKey = fmt.Sprintf("comment-%d", c.GetID())
	return event
}

// NormalizeReviewEvent converts a GitHub PullRequestReview to a db.PREvent.
func NormalizeReviewEvent(prID int64, r *gh.PullRequestReview) db.PREvent {
	event := db.PREvent{
		PRID:      prID,
		EventType: "review",
		DedupeKey: fmt.Sprintf("review-%d", r.GetID()),
		Author:    loginOrEmpty(r.GetUser()),
		Body:      r.GetBody(),
		Summary:   r.GetState(),
	}
	ghID := r.GetID()
	event.GitHubID = &ghID
	if r.SubmittedAt != nil {
		event.CreatedAt = r.SubmittedAt.Time
	}
	return event
}

// NormalizeCommitEvent converts a GitHub RepositoryCommit to a db.PREvent.
// Author is taken from the GitHub user login if available, falling back to
// the git commit author name.
func NormalizeCommitEvent(prID int64, c *gh.RepositoryCommit) db.PREvent {
	sha := c.GetSHA()
	dedupeKey := sha
	if len(sha) > 12 {
		dedupeKey = sha[:12]
	}

	author := loginOrEmpty(c.GetAuthor())
	if author == "" && c.GetCommit() != nil && c.GetCommit().GetAuthor() != nil {
		author = c.GetCommit().GetAuthor().GetName()
	}

	event := db.PREvent{
		PRID:      prID,
		EventType: "commit",
		DedupeKey: fmt.Sprintf("commit-%s", dedupeKey),
		Author:    author,
		Summary:   sha,
	}
	if c.GetCommit() != nil {
		event.Body = c.GetCommit().GetMessage()
		if c.GetCommit().Author != nil && c.GetCommit().Author.Date != nil {
			event.CreatedAt = c.GetCommit().Author.Date.Time
		}
	}
	return event
}

// NormalizeCIStatus extracts the combined CI state string from a CombinedStatus.
func NormalizeCIStatus(cs *gh.CombinedStatus) string {
	return cs.GetState()
}

// DeriveReviewDecision computes the aggregate review decision from a list of
// reviews. It keeps the latest APPROVED or CHANGES_REQUESTED review per user.
// Returns "changes_requested" if any user has that state, "approved" if at
// least one approval exists, or "" if no actionable reviews are present.
func DeriveReviewDecision(reviews []*gh.PullRequestReview) string {
	// latest state per reviewer login
	latest := make(map[string]string)
	for _, r := range reviews {
		login := loginOrEmpty(r.GetUser())
		if login == "" {
			continue
		}
		state := r.GetState()
		if state == "APPROVED" || state == "CHANGES_REQUESTED" {
			latest[login] = state
		}
	}

	hasApproved := false
	for _, state := range latest {
		if state == "CHANGES_REQUESTED" {
			return "changes_requested"
		}
		if state == "APPROVED" {
			hasApproved = true
		}
	}
	if hasApproved {
		return "approved"
	}
	return ""
}

// NormalizeCheckRuns converts GitHub check runs to a JSON string of CICheck objects.
func NormalizeCheckRuns(runs []*gh.CheckRun) string {
	if len(runs) == 0 {
		return ""
	}
	checks := make([]db.CICheck, 0, len(runs))
	for _, r := range runs {
		checks = append(checks, db.CICheck{
			Name:       r.GetName(),
			Status:     r.GetStatus(),
			Conclusion: r.GetConclusion(),
			URL:        r.GetHTMLURL(),
			App:        appName(r),
		})
	}
	b, err := json.Marshal(checks)
	if err != nil {
		return ""
	}
	return string(b)
}

// NormalizeCIChecks merges check runs and commit statuses into a single
// JSON string of CICheck objects. Commit statuses (used by GitHub Apps
// like roborev) use the older status API and need to be mapped into the
// same shape as check runs.
func NormalizeCIChecks(
	runs []*gh.CheckRun,
	combined *gh.CombinedStatus,
) string {
	var checks []db.CICheck
	for _, r := range runs {
		checks = append(checks, db.CICheck{
			Name:       r.GetName(),
			Status:     r.GetStatus(),
			Conclusion: r.GetConclusion(),
			URL:        r.GetHTMLURL(),
			App:        appName(r),
		})
	}
	if combined != nil {
		for _, s := range combined.Statuses {
			// Map commit status state to check run status/conclusion.
			status := "completed"
			conclusion := s.GetState()
			if conclusion == "pending" {
				status = "in_progress"
				conclusion = ""
			}
			checks = append(checks, db.CICheck{
				Name:       s.GetContext(),
				Status:     status,
				Conclusion: conclusion,
				URL:        sanitizeURL(s.GetTargetURL()),
				App:        s.GetContext(),
			})
		}
	}
	if len(checks) == 0 {
		return ""
	}
	b, err := json.Marshal(checks)
	if err != nil {
		return ""
	}
	return string(b)
}

func appName(r *gh.CheckRun) string {
	if r.GetApp() != nil {
		return r.GetApp().GetName()
	}
	return ""
}

// --- Issues ---

// Label represents a GitHub issue label for JSON serialization.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// NormalizeIssue converts a GitHub Issue to a db.Issue.
func NormalizeIssue(repoID int64, ghIssue *gh.Issue) *db.Issue {
	issue := &db.Issue{
		RepoID:       repoID,
		GitHubID:     ghIssue.GetID(),
		Number:       ghIssue.GetNumber(),
		URL:          ghIssue.GetHTMLURL(),
		Title:        ghIssue.GetTitle(),
		Author:       loginOrEmpty(ghIssue.GetUser()),
		State:        ghIssue.GetState(),
		Body:         ghIssue.GetBody(),
		CommentCount: ghIssue.GetComments(),
	}
	if ghIssue.CreatedAt != nil {
		issue.CreatedAt = ghIssue.CreatedAt.Time
	}
	if ghIssue.UpdatedAt != nil {
		issue.UpdatedAt = ghIssue.UpdatedAt.Time
		issue.LastActivityAt = ghIssue.UpdatedAt.Time
	}
	if ghIssue.ClosedAt != nil {
		t := ghIssue.ClosedAt.Time
		issue.ClosedAt = &t
	}
	issue.LabelsJSON = normalizeLabels(ghIssue.Labels)
	return issue
}

func normalizeLabels(labels []*gh.Label) string {
	if len(labels) == 0 {
		return ""
	}
	out := make([]Label, 0, len(labels))
	for _, l := range labels {
		out = append(out, Label{
			Name:  l.GetName(),
			Color: l.GetColor(),
		})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

// NormalizeIssueCommentEvent converts a GitHub IssueComment to a db.IssueEvent.
func NormalizeIssueCommentEvent(issueID int64, c *gh.IssueComment) db.IssueEvent {
	event := normalizeIssueCommentBase(c)
	return db.IssueEvent{
		IssueID:   issueID,
		GitHubID:  event.GitHubID,
		EventType: event.EventType,
		Author:    event.Author,
		Summary:   event.Summary,
		Body:      event.Body,
		CreatedAt: event.CreatedAt,
		DedupeKey: fmt.Sprintf("issue-comment-%d", c.GetID()),
	}
}

func normalizeIssueCommentBase(c *gh.IssueComment) db.PREvent {
	event := db.PREvent{
		EventType: "issue_comment",
		Author:    loginOrEmpty(c.GetUser()),
		Body:      c.GetBody(),
	}
	ghID := c.GetID()
	event.GitHubID = &ghID
	if c.CreatedAt != nil {
		event.CreatedAt = c.CreatedAt.Time
	}
	return event
}

// loginOrEmpty returns the GitHub login for a user, or "" if user is nil.
func loginOrEmpty(u *gh.User) string {
	if u == nil {
		return ""
	}
	return u.GetLogin()
}
