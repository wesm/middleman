package github

import (
	"context"

	gh "github.com/google/go-github/v84/github"
)

// collectPages centralizes the standard go-github pagination loop for list
// endpoints that advance through a shared ListOptions page cursor.
func collectPages[T any](
	ctx context.Context,
	listPage func(*gh.ListOptions) ([]T, *gh.Response, error),
) ([]T, error) {
	var all []T
	opts := &gh.ListOptions{PerPage: 100}
	for {
		page, resp, err := listPage(opts)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if resp == nil || resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}
