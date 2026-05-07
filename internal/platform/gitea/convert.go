package gitea

import (
	"fmt"
	"time"

	giteasdk "code.gitea.io/sdk/gitea"
	"github.com/wesm/middleman/internal/platform/gitealike"
)

func convertRepository(repo *giteasdk.Repository) (gitealike.RepositoryDTO, error) {
	if repo == nil {
		return gitealike.RepositoryDTO{}, fmt.Errorf("gitea repository is nil")
	}
	return gitealike.RepositoryDTO{
		ID:            repo.ID,
		Owner:         convertUser(repo.Owner),
		Name:          repo.Name,
		FullName:      repo.FullName,
		HTMLURL:       repo.HTMLURL,
		CloneURL:      repo.CloneURL,
		DefaultBranch: repo.DefaultBranch,
		Private:       repo.Private,
		Archived:      repo.Archived,
		Description:   repo.Description,
		Created:       repo.Created,
		Updated:       repo.Updated,
	}, nil
}

func convertPullRequest(pr *giteasdk.PullRequest) gitealike.PullRequestDTO {
	if pr == nil {
		return gitealike.PullRequestDTO{}
	}
	return gitealike.PullRequestDTO{
		ID:       pr.ID,
		Index:    int(pr.Index),
		HTMLURL:  pr.HTMLURL,
		Title:    pr.Title,
		User:     convertUser(pr.Poster),
		State:    string(pr.State),
		Draft:    pr.Draft,
		IsLocked: pr.IsLocked,
		Body:     pr.Body,
		Head:     convertBranch(pr.Head),
		Base:     convertBranch(pr.Base),
		Labels:   convertLabels(pr.Labels),
		Comments: pr.Comments,
		Created:  timeValue(pr.Created),
		Updated:  timeValue(pr.Updated),
		Merged:   pr.HasMerged,
		MergedAt: timePtrValue(pr.Merged),
		Closed:   timePtrValue(pr.Closed),
	}
}

func convertIssue(issue *giteasdk.Issue) gitealike.IssueDTO {
	if issue == nil {
		return gitealike.IssueDTO{}
	}
	return gitealike.IssueDTO{
		ID:            issue.ID,
		Index:         int(issue.Index),
		HTMLURL:       issue.HTMLURL,
		Title:         issue.Title,
		User:          convertUser(issue.Poster),
		State:         string(issue.State),
		Body:          issue.Body,
		Comments:      issue.Comments,
		Labels:        convertLabels(issue.Labels),
		Created:       issue.Created,
		Updated:       issue.Updated,
		Closed:        timePtrValue(issue.Closed),
		IsPullRequest: issue.PullRequest != nil,
	}
}

func convertComment(comment *giteasdk.Comment) gitealike.CommentDTO {
	if comment == nil {
		return gitealike.CommentDTO{}
	}
	return gitealike.CommentDTO{
		ID:      comment.ID,
		User:    convertUser(comment.Poster),
		Body:    comment.Body,
		Created: comment.Created,
		Updated: comment.Updated,
	}
}

func convertReview(review *giteasdk.PullReview) gitealike.ReviewDTO {
	if review == nil {
		return gitealike.ReviewDTO{}
	}
	return gitealike.ReviewDTO{
		ID:        review.ID,
		User:      convertUser(review.Reviewer),
		State:     string(review.State),
		Body:      review.Body,
		Submitted: review.Submitted,
	}
}

func convertRelease(release *giteasdk.Release) gitealike.ReleaseDTO {
	if release == nil {
		return gitealike.ReleaseDTO{}
	}
	return gitealike.ReleaseDTO{
		ID:          release.ID,
		TagName:     release.TagName,
		Title:       release.Title,
		HTMLURL:     release.HTMLURL,
		Target:      release.Target,
		Prerelease:  release.IsPrerelease,
		PublishedAt: nonZeroTimePtr(release.PublishedAt),
		CreatedAt:   release.CreatedAt,
	}
}

func convertTag(tag *giteasdk.Tag) gitealike.TagDTO {
	if tag == nil {
		return gitealike.TagDTO{}
	}
	return gitealike.TagDTO{
		Name:   tag.Name,
		Commit: convertCommitMeta(tag.Commit),
	}
}

func convertStatus(status *giteasdk.Status) gitealike.StatusDTO {
	if status == nil {
		return gitealike.StatusDTO{}
	}
	return gitealike.StatusDTO{
		ID:          status.ID,
		Context:     status.Context,
		State:       string(status.State),
		TargetURL:   status.TargetURL,
		Description: status.Description,
		Created:     status.Created,
		Updated:     status.Updated,
	}
}

func convertUser(user *giteasdk.User) gitealike.UserDTO {
	if user == nil {
		return gitealike.UserDTO{}
	}
	return gitealike.UserDTO{
		ID:       user.ID,
		UserName: user.UserName,
		FullName: user.FullName,
	}
}

func convertLabels(labels []*giteasdk.Label) []gitealike.LabelDTO {
	if len(labels) == 0 {
		return nil
	}
	out := make([]gitealike.LabelDTO, 0, len(labels))
	for _, label := range labels {
		if label == nil {
			continue
		}
		out = append(out, gitealike.LabelDTO{
			ID:          label.ID,
			Name:        label.Name,
			Description: label.Description,
			Color:       label.Color,
		})
	}
	return out
}

func convertBranch(branch *giteasdk.PRBranchInfo) gitealike.BranchDTO {
	if branch == nil {
		return gitealike.BranchDTO{}
	}
	out := gitealike.BranchDTO{
		Ref: branch.Ref,
		SHA: branch.Sha,
	}
	if branch.Repository != nil {
		out.RepoCloneURL = branch.Repository.CloneURL
	}
	return out
}

func convertCommitMeta(commit *giteasdk.CommitMeta) gitealike.CommitDTO {
	if commit == nil {
		return gitealike.CommitDTO{}
	}
	return gitealike.CommitDTO{
		SHA:     commit.SHA,
		URL:     commit.URL,
		Created: commit.Created,
	}
}

func timeValue(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func timePtrValue(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	out := *t
	return &out
}

func nonZeroTimePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
