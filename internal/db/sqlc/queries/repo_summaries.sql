-- name: ListRepoSummaryStats :many
WITH pr_stats AS (
    SELECT repo_id,
           COUNT(*) AS cached_pr_count,
           SUM(CASE WHEN state = 'open' THEN 1 ELSE 0 END) AS open_pr_count,
           SUM(CASE WHEN state = 'open' AND is_draft THEN 1 ELSE 0 END) AS draft_pr_count,
           MAX(last_activity_at) AS last_pr_activity_at
    FROM middleman_merge_requests
    GROUP BY repo_id
),
issue_stats AS (
    SELECT repo_id,
           COUNT(*) AS cached_issue_count,
           SUM(CASE WHEN state = 'open' THEN 1 ELSE 0 END) AS open_issue_count,
           MAX(last_activity_at) AS last_issue_activity_at
    FROM middleman_issues
    GROUP BY repo_id
)
SELECT r.id,
       COALESCE(pr.cached_pr_count, 0) AS cached_pr_count,
       COALESCE(pr.open_pr_count, 0) AS open_pr_count,
       COALESCE(pr.draft_pr_count, 0) AS draft_pr_count,
       COALESCE(i.cached_issue_count, 0) AS cached_issue_count,
       COALESCE(i.open_issue_count, 0) AS open_issue_count,
       COALESCE(CAST(
           CASE
               WHEN pr.last_pr_activity_at IS NULL THEN i.last_issue_activity_at
               WHEN i.last_issue_activity_at IS NULL THEN pr.last_pr_activity_at
               WHEN pr.last_pr_activity_at >= i.last_issue_activity_at THEN pr.last_pr_activity_at
               ELSE i.last_issue_activity_at
           END AS TEXT
       ), '') AS most_recent_activity_at
FROM middleman_repos r
LEFT JOIN pr_stats pr ON pr.repo_id = r.id
LEFT JOIN issue_stats i ON i.repo_id = r.id
ORDER BY r.owner, r.name;

-- name: UpsertRepoOverview :exec
INSERT INTO middleman_repo_overviews (
    repo_id, latest_release_tag, latest_release_name,
    latest_release_url, latest_release_target,
    latest_release_prerelease, latest_release_published_at,
    commits_since_release, commit_timeline_json,
    releases_json, timeline_updated_at, updated_at
)
VALUES (
    sqlc.arg(repo_id), sqlc.arg(latest_release_tag), sqlc.arg(latest_release_name),
    sqlc.arg(latest_release_url), sqlc.arg(latest_release_target),
    sqlc.arg(latest_release_prerelease), sqlc.narg(latest_release_published_at),
    sqlc.narg(commits_since_release), sqlc.arg(commit_timeline_json),
    sqlc.arg(releases_json), sqlc.narg(timeline_updated_at), sqlc.arg(updated_at)
)
ON CONFLICT(repo_id) DO UPDATE SET
    latest_release_tag = excluded.latest_release_tag,
    latest_release_name = excluded.latest_release_name,
    latest_release_url = excluded.latest_release_url,
    latest_release_target = excluded.latest_release_target,
    latest_release_prerelease = excluded.latest_release_prerelease,
    latest_release_published_at = excluded.latest_release_published_at,
    commits_since_release = CASE
        WHEN excluded.timeline_updated_at IS NOT NULL
        THEN excluded.commits_since_release
        WHEN middleman_repo_overviews.latest_release_tag IS excluded.latest_release_tag
        THEN COALESCE(
            excluded.commits_since_release,
            middleman_repo_overviews.commits_since_release
        )
        ELSE excluded.commits_since_release
    END,
    commit_timeline_json = CASE
        WHEN excluded.timeline_updated_at IS NULL
             AND middleman_repo_overviews.latest_release_tag IS excluded.latest_release_tag
        THEN middleman_repo_overviews.commit_timeline_json
        ELSE excluded.commit_timeline_json
    END,
    releases_json = excluded.releases_json,
    timeline_updated_at = CASE
        WHEN excluded.timeline_updated_at IS NOT NULL
        THEN excluded.timeline_updated_at
        WHEN middleman_repo_overviews.latest_release_tag IS excluded.latest_release_tag
        THEN middleman_repo_overviews.timeline_updated_at
        ELSE excluded.timeline_updated_at
    END,
    updated_at = excluded.updated_at;

-- name: ListRepoSummaryOverviews :many
SELECT repo_id,
       latest_release_tag,
       latest_release_name,
       latest_release_url,
       latest_release_target,
       latest_release_prerelease,
       COALESCE(CAST(latest_release_published_at AS TEXT), '') AS latest_release_published_at,
       commits_since_release,
       commit_timeline_json,
       releases_json,
       COALESCE(CAST(timeline_updated_at AS TEXT), '') AS timeline_updated_at
FROM middleman_repo_overviews;

-- name: ListRepoSummaryAuthors :many
WITH author_items AS (
    SELECT repo_id, author, last_activity_at
    FROM middleman_merge_requests
    WHERE author <> ''
    UNION ALL
    SELECT repo_id, author, last_activity_at
    FROM middleman_issues
    WHERE author <> ''
),
author_totals AS (
    SELECT repo_id,
           author,
           COUNT(*) AS item_count,
           MAX(last_activity_at) AS most_recent_activity_at
    FROM author_items
    GROUP BY repo_id, author
),
ranked AS (
    SELECT repo_id,
           author,
           item_count,
           ROW_NUMBER() OVER (
               PARTITION BY repo_id
               ORDER BY item_count DESC, most_recent_activity_at DESC, author ASC
           ) AS row_num
    FROM author_totals
)
SELECT repo_id, author, item_count
FROM ranked
WHERE row_num <= 3
ORDER BY repo_id, row_num;

-- name: ListRepoSummaryIssues :many
WITH ranked AS (
    SELECT repo_id,
           number,
           title,
           author,
           state,
           url,
           last_activity_at,
           ROW_NUMBER() OVER (
               PARTITION BY repo_id
               ORDER BY last_activity_at DESC, number DESC
           ) AS row_num
    FROM middleman_issues
    WHERE state = 'open'
)
SELECT repo_id, number, title, author, state, url,
       CAST(last_activity_at AS TEXT) AS last_activity_at
FROM ranked
WHERE row_num <= 3
ORDER BY repo_id, row_num;
