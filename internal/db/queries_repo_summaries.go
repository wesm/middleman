package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	dbsqlc "github.com/wesm/middleman/internal/db/sqlc"
)

// ListRepoSummaries returns one summary per tracked repo. The summaries are
// assembled from cached database state only; no live GitHub calls are made.
func (d *DB) ListRepoSummaries(ctx context.Context) ([]RepoSummary, error) {
	repos, err := d.ListRepos(ctx)
	if err != nil {
		return nil, err
	}

	summaries := make([]RepoSummary, 0, len(repos))
	summaryByRepoID := make(map[int64]*RepoSummary, len(repos))
	for _, repo := range repos {
		summaries = append(summaries, RepoSummary{Repo: repo})
		summaryByRepoID[repo.ID] = &summaries[len(summaries)-1]
	}

	if err := d.loadRepoSummaryStats(ctx, summaryByRepoID); err != nil {
		return nil, err
	}
	if err := d.loadRepoSummaryOverviews(ctx, summaryByRepoID); err != nil {
		return nil, err
	}
	if err := d.loadRepoSummaryAuthors(ctx, summaryByRepoID); err != nil {
		return nil, err
	}
	if err := d.loadRepoSummaryIssues(ctx, summaryByRepoID); err != nil {
		return nil, err
	}

	return summaries, nil
}

func (d *DB) loadRepoSummaryStats(
	ctx context.Context,
	summaryByRepoID map[int64]*RepoSummary,
) error {
	rows, err := d.readQueries.ListRepoSummaryStats(ctx)
	if err != nil {
		return fmt.Errorf("list repo summary stats: %w", err)
	}

	for _, row := range rows {
		summary := summaryByRepoID[row.ID]
		if summary == nil {
			continue
		}
		summary.CachedPRCount = int(row.CachedPrCount)
		summary.OpenPRCount = int(row.OpenPrCount)
		summary.DraftPRCount = int(row.DraftPrCount)
		summary.CachedIssueCount = int(row.CachedIssueCount)
		summary.OpenIssueCount = int(row.OpenIssueCount)
		mostRecentActivity := stringFromSQLValue(row.MostRecentActivityAt)
		if mostRecentActivity != "" {
			t, err := parseDBTime(mostRecentActivity)
			if err != nil {
				return fmt.Errorf("parse repo summary activity %q: %w", mostRecentActivity, err)
			}
			summary.MostRecentActivityAt = &t
		}
	}

	return nil
}

type repoCommitTimelineJSON struct {
	SHA         string `json:"sha"`
	Message     string `json:"message"`
	CommittedAt string `json:"committed_at"`
}

type repoReleaseJSON struct {
	TagName         string `json:"tag_name"`
	Name            string `json:"name"`
	URL             string `json:"url"`
	TargetCommitish string `json:"target_commitish"`
	Prerelease      bool   `json:"prerelease"`
	PublishedAt     string `json:"published_at,omitempty"`
}

func (d *DB) UpsertRepoOverview(
	ctx context.Context,
	repoID int64,
	overview RepoOverview,
) error {
	timeline := make([]repoCommitTimelineJSON, 0, len(overview.CommitTimeline))
	for _, point := range overview.CommitTimeline {
		timeline = append(timeline, repoCommitTimelineJSON{
			SHA:         point.SHA,
			Message:     point.Message,
			CommittedAt: point.CommittedAt.UTC().Format(time.RFC3339),
		})
	}
	timelineJSON, err := json.Marshal(timeline)
	if err != nil {
		return fmt.Errorf("marshal repo overview timeline: %w", err)
	}
	releases := make([]repoReleaseJSON, 0, len(overview.Releases))
	for _, release := range overview.Releases {
		item := repoReleaseJSON{
			TagName:         release.TagName,
			Name:            release.Name,
			URL:             release.URL,
			TargetCommitish: release.TargetCommitish,
			Prerelease:      release.Prerelease,
		}
		if release.PublishedAt != nil {
			item.PublishedAt = release.PublishedAt.UTC().Format(time.RFC3339)
		}
		releases = append(releases, item)
	}
	releasesJSON, err := json.Marshal(releases)
	if err != nil {
		return fmt.Errorf("marshal repo overview releases: %w", err)
	}

	var (
		tagName         string
		releaseName     string
		releaseURL      string
		targetCommitish string
		prerelease      bool
		publishedAt     *time.Time
	)
	if overview.LatestRelease != nil {
		tagName = overview.LatestRelease.TagName
		releaseName = overview.LatestRelease.Name
		releaseURL = overview.LatestRelease.URL
		targetCommitish = overview.LatestRelease.TargetCommitish
		prerelease = overview.LatestRelease.Prerelease
		publishedAt = overview.LatestRelease.PublishedAt
	}

	commitsSince := sql.NullInt64{}
	if overview.CommitsSinceRelease != nil {
		commitsSince = sql.NullInt64{
			Int64: int64(*overview.CommitsSinceRelease),
			Valid: true,
		}
	}
	if err = d.writeQueries.UpsertRepoOverview(ctx, dbsqlc.UpsertRepoOverviewParams{
		RepoID:                   repoID,
		LatestReleaseTag:         tagName,
		LatestReleaseName:        releaseName,
		LatestReleaseUrl:         releaseURL,
		LatestReleaseTarget:      targetCommitish,
		LatestReleasePrerelease:  boolInt64(prerelease),
		LatestReleasePublishedAt: nullUTCTime(publishedAt),
		CommitsSinceRelease:      commitsSince,
		CommitTimelineJson:       string(timelineJSON),
		ReleasesJson:             string(releasesJSON),
		TimelineUpdatedAt:        nullUTCTime(overview.TimelineUpdatedAt),
		UpdatedAt:                time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("upsert repo overview: %w", err)
	}
	return nil
}

func (d *DB) loadRepoSummaryOverviews(
	ctx context.Context,
	summaryByRepoID map[int64]*RepoSummary,
) error {
	rows, err := d.readQueries.ListRepoSummaryOverviews(ctx)
	if err != nil {
		return fmt.Errorf("list repo summary overviews: %w", err)
	}

	for _, row := range rows {
		summary := summaryByRepoID[row.RepoID]
		if summary == nil {
			continue
		}

		if row.LatestReleaseTag != "" {
			release := &RepoRelease{
				TagName:         row.LatestReleaseTag,
				Name:            row.LatestReleaseName,
				URL:             row.LatestReleaseUrl,
				TargetCommitish: row.LatestReleaseTarget,
				Prerelease:      row.LatestReleasePrerelease != 0,
			}
			publishedAt := stringFromSQLValue(row.LatestReleasePublishedAt)
			if publishedAt != "" {
				t, err := parseDBTime(publishedAt)
				if err != nil {
					return fmt.Errorf("parse repo release published_at %q: %w", publishedAt, err)
				}
				release.PublishedAt = &t
			}
			summary.Overview.LatestRelease = release
		}
		if row.CommitsSinceRelease.Valid {
			count := int(row.CommitsSinceRelease.Int64)
			summary.Overview.CommitsSinceRelease = &count
		}
		releases, err := parseRepoReleasesJSON(row.ReleasesJson)
		if err != nil {
			return fmt.Errorf("parse repo releases json: %w", err)
		}
		summary.Overview.Releases = releases
		points, err := parseRepoTimelineJSON(row.CommitTimelineJson)
		if err != nil {
			return fmt.Errorf("parse repo timeline json: %w", err)
		}
		summary.Overview.CommitTimeline = points
		timelineUpdatedAt := stringFromSQLValue(row.TimelineUpdatedAt)
		if timelineUpdatedAt != "" {
			t, err := parseDBTime(timelineUpdatedAt)
			if err != nil {
				return fmt.Errorf("parse repo timeline updated_at %q: %w", timelineUpdatedAt, err)
			}
			summary.Overview.TimelineUpdatedAt = &t
		}
	}

	return nil
}

func parseRepoTimelineJSON(value string) ([]RepoCommitTimelinePoint, error) {
	if value == "" {
		return nil, nil
	}
	var raw []repoCommitTimelineJSON
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil, err
	}
	points := make([]RepoCommitTimelinePoint, 0, len(raw))
	for _, item := range raw {
		t, err := time.Parse(time.RFC3339, item.CommittedAt)
		if err != nil {
			return nil, fmt.Errorf("parse commit timeline date %q: %w", item.CommittedAt, err)
		}
		points = append(points, RepoCommitTimelinePoint{
			SHA:         item.SHA,
			Message:     item.Message,
			CommittedAt: t,
		})
	}
	return points, nil
}

func parseRepoReleasesJSON(value string) ([]RepoRelease, error) {
	if value == "" {
		return nil, nil
	}
	var raw []repoReleaseJSON
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return nil, err
	}
	releases := make([]RepoRelease, 0, len(raw))
	for _, item := range raw {
		release := RepoRelease{
			TagName:         item.TagName,
			Name:            item.Name,
			URL:             item.URL,
			TargetCommitish: item.TargetCommitish,
			Prerelease:      item.Prerelease,
		}
		if item.PublishedAt != "" {
			t, err := time.Parse(time.RFC3339, item.PublishedAt)
			if err != nil {
				return nil, fmt.Errorf("parse release date %q: %w", item.PublishedAt, err)
			}
			release.PublishedAt = &t
		}
		releases = append(releases, release)
	}
	return releases, nil
}

func (d *DB) loadRepoSummaryAuthors(
	ctx context.Context,
	summaryByRepoID map[int64]*RepoSummary,
) error {
	rows, err := d.readQueries.ListRepoSummaryAuthors(ctx)
	if err != nil {
		return fmt.Errorf("list repo summary authors: %w", err)
	}

	for _, row := range rows {
		summary := summaryByRepoID[row.RepoID]
		if summary == nil {
			continue
		}
		summary.ActiveAuthors = append(summary.ActiveAuthors, RepoActivityAuthor{
			Login:     row.Author,
			ItemCount: int(row.ItemCount),
		})
	}

	return nil
}

func (d *DB) loadRepoSummaryIssues(
	ctx context.Context,
	summaryByRepoID map[int64]*RepoSummary,
) error {
	rows, err := d.readQueries.ListRepoSummaryIssues(ctx)
	if err != nil {
		return fmt.Errorf("list repo summary issues: %w", err)
	}

	for _, row := range rows {
		t, err := parseDBTime(row.LastActivityAt)
		if err != nil {
			return fmt.Errorf("parse repo summary issue activity %q: %w", row.LastActivityAt, err)
		}
		issue := RepoIssueHeadline{
			Number:         int(row.Number),
			Title:          row.Title,
			Author:         row.Author,
			State:          row.State,
			URL:            row.Url,
			LastActivityAt: t,
		}

		summary := summaryByRepoID[row.RepoID]
		if summary == nil {
			continue
		}
		summary.RecentIssues = append(summary.RecentIssues, issue)
	}

	return nil
}
