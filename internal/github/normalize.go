package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

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

var (
	ErrNilPullRequest = errors.New("nil pull request")
	ErrNilIssue       = errors.New("nil issue")
)

// NormalizePR converts a GitHub PullRequest to a db.MergeRequest.
// If the PR is merged, State is set to "merged". LastActivityAt is
// initialized to UpdatedAt.
func NormalizePR(repoID int64, ghPR *gh.PullRequest) (*db.MergeRequest, error) {
	if ghPR == nil {
		return nil, ErrNilPullRequest
	}
	mr := &db.MergeRequest{
		RepoID:            repoID,
		PlatformID:        ghPR.GetID(),
		Number:            ghPR.GetNumber(),
		URL:               ghPR.GetHTMLURL(),
		Title:             ghPR.GetTitle(),
		Author:            loginOrEmpty(ghPR.GetUser()),
		AuthorDisplayName: nameOrEmpty(ghPR.GetUser()),
		State:             ghPR.GetState(),
		IsDraft:           ghPR.GetDraft(),
		Body:              ghPR.GetBody(),
		Additions:         ghPR.GetAdditions(),
		Deletions:         ghPR.GetDeletions(),
	}

	if ghPR.GetMerged() {
		mr.State = "merged"
	}

	if ghPR.CreatedAt != nil {
		mr.CreatedAt = ghPR.CreatedAt.Time
	}
	if ghPR.UpdatedAt != nil {
		mr.UpdatedAt = ghPR.UpdatedAt.Time
		mr.LastActivityAt = ghPR.UpdatedAt.Time
	}
	if ghPR.MergedAt != nil {
		t := ghPR.MergedAt.Time
		mr.MergedAt = &t
	}
	if ghPR.ClosedAt != nil {
		t := ghPR.ClosedAt.Time
		mr.ClosedAt = &t
	}
	if ghPR.GetHead() != nil {
		mr.HeadBranch = ghPR.GetHead().GetRef()
		mr.PlatformHeadSHA = ghPR.GetHead().GetSHA()
		if ghPR.GetHead().GetRepo() != nil {
			mr.HeadRepoCloneURL = ghPR.GetHead().GetRepo().GetCloneURL()
		}
	}
	if ghPR.GetBase() != nil {
		mr.BaseBranch = ghPR.GetBase().GetRef()
		mr.PlatformBaseSHA = ghPR.GetBase().GetSHA()
	}
	mr.MergeableState = ghPR.GetMergeableState()
	mr.Labels = normalizeLabels(ghPR.Labels, itemLabelUpdatedAt(mr.UpdatedAt, mr.CreatedAt))

	return mr, nil
}

// NormalizeCommentEvent converts a GitHub IssueComment to a db.MREvent.
func NormalizeCommentEvent(mrID int64, c *gh.IssueComment) db.MREvent {
	event := normalizeIssueCommentBase(c)
	event.MergeRequestID = mrID
	event.DedupeKey = fmt.Sprintf("comment-%d", c.GetID())
	return event
}

// NormalizeReviewEvent converts a GitHub PullRequestReview to a db.MREvent.
func NormalizeReviewEvent(mrID int64, r *gh.PullRequestReview) db.MREvent {
	event := db.MREvent{
		MergeRequestID: mrID,
		EventType:      "review",
		DedupeKey:      fmt.Sprintf("review-%d", r.GetID()),
		Author:         loginOrEmpty(r.GetUser()),
		Body:           r.GetBody(),
		Summary:        r.GetState(),
	}
	ghID := r.GetID()
	event.PlatformID = &ghID
	if r.SubmittedAt != nil {
		event.CreatedAt = r.SubmittedAt.Time
	}
	return event
}

// NormalizeCommitEvent converts a GitHub RepositoryCommit to a db.MREvent.
// Author is taken from the GitHub user login if available, falling back to
// the git commit author name.
func NormalizeCommitEvent(mrID int64, c *gh.RepositoryCommit) db.MREvent {
	sha := c.GetSHA()
	dedupeKey := sha
	if len(sha) > 12 {
		dedupeKey = sha[:12]
	}

	author := loginOrEmpty(c.GetAuthor())
	if author == "" && c.GetCommit() != nil && c.GetCommit().GetAuthor() != nil {
		author = c.GetCommit().GetAuthor().GetName()
	}

	event := db.MREvent{
		MergeRequestID: mrID,
		EventType:      "commit",
		DedupeKey:      fmt.Sprintf("commit-%s", dedupeKey),
		Author:         author,
		Summary:        sha,
	}
	if c.GetCommit() != nil {
		event.Body = c.GetCommit().GetMessage()
		if c.GetCommit().Author != nil && c.GetCommit().Author.Date != nil {
			event.CreatedAt = c.GetCommit().Author.Date.UTC()
		}
	}
	return event
}

type forcePushMetadata struct {
	BeforeSHA string `json:"before_sha"`
	AfterSHA  string `json:"after_sha"`
	Ref       string `json:"ref"`
}

func NormalizeForcePushEvent(mrID int64, fp ForcePushEvent) db.MREvent {
	metadata, _ := json.Marshal(forcePushMetadata{
		BeforeSHA: fp.BeforeSHA,
		AfterSHA:  fp.AfterSHA,
		Ref:       fp.Ref,
	})

	return db.MREvent{
		MergeRequestID: mrID,
		EventType:      "force_push",
		Author:         fp.Actor,
		Summary:        shortSHA(fp.BeforeSHA) + " -> " + shortSHA(fp.AfterSHA),
		MetadataJSON:   string(metadata),
		CreatedAt:      fp.CreatedAt,
		DedupeKey:      fmt.Sprintf("force-push-%s-%s", fp.BeforeSHA, fp.AfterSHA),
	}
}

// DeriveOverallCIStatus computes an aggregate CI status from check runs
// and the legacy combined status API. The combined status API only reports
// on commit statuses (the older mechanism); repos using only GitHub Actions
// check runs will have an empty or "pending" combined state even when all
// checks pass. This function merges both sources to produce the correct
// overall status.
func DeriveOverallCIStatus(
	runs []*gh.CheckRun,
	combined *gh.CombinedStatus,
) string {
	hasAny := false
	hasPending := false
	hasFailed := false

	for _, r := range runs {
		hasAny = true
		if r.GetStatus() != "completed" {
			hasPending = true
			continue
		}
		switch r.GetConclusion() {
		case "success", "neutral", "skipped":
			// OK — not a failure.
		default:
			hasFailed = true
		}
	}

	// Use GitHub's pre-aggregated State rather than iterating
	// combined.Statuses, which may be truncated by pagination.
	if combined != nil && combined.GetTotalCount() > 0 {
		hasAny = true
		switch combined.GetState() {
		case "pending":
			hasPending = true
		case "failure", "error":
			hasFailed = true
		}
	}

	if !hasAny {
		return ""
	}
	if hasFailed {
		return "failure"
	}
	if hasPending {
		return "pending"
	}
	return "success"
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
	sortCIChecksByName(checks)
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
			if conclusion == "pending" || conclusion == "expected" {
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
	sortCIChecksByName(checks)
	b, err := json.Marshal(checks)
	if err != nil {
		return ""
	}
	return string(b)
}

func sortCIChecksByName(checks []db.CICheck) {
	sort.SliceStable(checks, func(i, j int) bool {
		leftFolded := strings.ToLower(checks[i].Name)
		rightFolded := strings.ToLower(checks[j].Name)
		if leftFolded != rightFolded {
			return leftFolded < rightFolded
		}
		return checks[i].Name < checks[j].Name
	})
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func appName(r *gh.CheckRun) string {
	if r.GetApp() != nil {
		return r.GetApp().GetName()
	}
	return ""
}

// --- Issues ---

// NormalizeIssue converts a GitHub Issue to a db.Issue.
func NormalizeIssue(repoID int64, ghIssue *gh.Issue) (*db.Issue, error) {
	if ghIssue == nil {
		return nil, ErrNilIssue
	}
	issue := &db.Issue{
		RepoID:       repoID,
		PlatformID:   ghIssue.GetID(),
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
	issue.Labels = normalizeLabels(ghIssue.Labels, itemLabelUpdatedAt(issue.UpdatedAt, issue.CreatedAt))
	return issue, nil
}

func itemLabelUpdatedAt(updatedAt, createdAt time.Time) time.Time {
	if !updatedAt.IsZero() {
		return updatedAt
	}
	return createdAt
}

func normalizeLabels(labels []*gh.Label, updatedAt time.Time) []db.Label {
	if len(labels) == 0 {
		return nil
	}
	out := make([]db.Label, 0, len(labels))
	for _, l := range labels {
		if l == nil {
			continue
		}
		name := strings.TrimSpace(l.GetName())
		if name == "" {
			continue
		}
		out = append(out, db.Label{
			PlatformID:  l.GetID(),
			Name:        name,
			Description: l.GetDescription(),
			Color:       l.GetColor(),
			IsDefault:   l.GetDefault(),
			UpdatedAt:   updatedAt,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeIssueCommentEvent converts a GitHub IssueComment to a db.IssueEvent.
func NormalizeIssueCommentEvent(issueID int64, c *gh.IssueComment) db.IssueEvent {
	event := normalizeIssueCommentBase(c)
	return db.IssueEvent{
		IssueID:    issueID,
		PlatformID: event.PlatformID,
		EventType:  event.EventType,
		Author:     event.Author,
		Summary:    event.Summary,
		Body:       event.Body,
		CreatedAt:  event.CreatedAt,
		DedupeKey:  fmt.Sprintf("issue-comment-%d", c.GetID()),
	}
}

func normalizeIssueCommentBase(c *gh.IssueComment) db.MREvent {
	event := db.MREvent{
		EventType: "issue_comment",
		Author:    loginOrEmpty(c.GetUser()),
		Body:      c.GetBody(),
	}
	ghID := c.GetID()
	event.PlatformID = &ghID
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

// nameOrEmpty returns the GitHub display name for a user, or "" if
// unavailable. Bot accounts (Type == "Bot") use their login as display name
// since they have no user-facing name on the GitHub API.
func nameOrEmpty(u *gh.User) string {
	if u == nil {
		return ""
	}
	if u.GetType() == "Bot" {
		return u.GetLogin()
	}
	return sanitizeDisplayName(u.GetName())
}

// sanitizeDisplayName strips characters that could inject trailers or
// corrupt git commit metadata when used in a Co-authored-by line.
func sanitizeDisplayName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '\n', '\r', '<', '>':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}
