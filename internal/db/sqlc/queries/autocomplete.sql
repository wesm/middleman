-- name: ListCommentAutocompleteUsers :many
SELECT login
FROM (
    SELECT mr.author AS login,
           mr.last_activity_at AS last_seen,
           CASE
               WHEN sqlc.arg(query) <> '' AND LOWER(mr.author) LIKE sqlc.arg(prefix_query) THEN 0
               ELSE 1
           END AS prefix_rank
    FROM middleman_merge_requests mr
    WHERE mr.repo_id = sqlc.arg(repo_id)
      AND mr.author <> ''
      AND (sqlc.arg(query) = '' OR LOWER(mr.author) LIKE sqlc.arg(contains_query))
    UNION ALL
    SELECT i.author AS login,
           i.last_activity_at AS last_seen,
           CASE
               WHEN sqlc.arg(query) <> '' AND LOWER(i.author) LIKE sqlc.arg(prefix_query) THEN 0
               ELSE 1
           END AS prefix_rank
    FROM middleman_issues i
    WHERE i.repo_id = sqlc.arg(repo_id)
      AND i.author <> ''
      AND (sqlc.arg(query) = '' OR LOWER(i.author) LIKE sqlc.arg(contains_query))
    UNION ALL
    SELECT e.author AS login,
           e.created_at AS last_seen,
           CASE
               WHEN sqlc.arg(query) <> '' AND LOWER(e.author) LIKE sqlc.arg(prefix_query) THEN 0
               ELSE 1
           END AS prefix_rank
    FROM middleman_mr_events e
    JOIN middleman_merge_requests mr ON mr.id = e.merge_request_id
    WHERE mr.repo_id = sqlc.arg(repo_id)
      AND e.author <> ''
      AND (sqlc.arg(query) = '' OR LOWER(e.author) LIKE sqlc.arg(contains_query))
    UNION ALL
    SELECT e.author AS login,
           e.created_at AS last_seen,
           CASE
               WHEN sqlc.arg(query) <> '' AND LOWER(e.author) LIKE sqlc.arg(prefix_query) THEN 0
               ELSE 1
           END AS prefix_rank
    FROM middleman_issue_events e
    JOIN middleman_issues i ON i.id = e.issue_id
    WHERE i.repo_id = sqlc.arg(repo_id)
      AND e.author <> ''
      AND (sqlc.arg(query) = '' OR LOWER(e.author) LIKE sqlc.arg(contains_query))
)
GROUP BY login
ORDER BY
    MIN(prefix_rank),
    MAX(last_seen) DESC,
    login ASC
LIMIT sqlc.arg(limit);

-- name: ListCommentAutocompleteReferences :many
SELECT kind, number, title, state
FROM (
    SELECT 'pull' AS kind,
           mr.number,
           mr.title,
           mr.state,
           mr.last_activity_at,
           CASE
               WHEN sqlc.arg(query) <> ''
                    AND CAST(mr.number AS TEXT) LIKE CAST(sqlc.arg(number_prefix) AS TEXT)
               THEN 0
               ELSE 1
           END AS number_rank,
           CASE
               WHEN sqlc.arg(query) <> ''
                    AND LOWER(mr.title) LIKE sqlc.arg(title_query)
               THEN 0
               ELSE 1
           END AS title_rank
    FROM middleman_merge_requests mr
    WHERE mr.repo_id = sqlc.arg(repo_id)
      AND (
          sqlc.arg(query) = ''
          OR CAST(mr.number AS TEXT) LIKE CAST(sqlc.arg(number_prefix) AS TEXT)
          OR LOWER(mr.title) LIKE sqlc.arg(title_query)
      )
    UNION ALL
    SELECT 'issue' AS kind,
           i.number,
           i.title,
           i.state,
           i.last_activity_at,
           CASE
               WHEN sqlc.arg(query) <> ''
                    AND CAST(i.number AS TEXT) LIKE CAST(sqlc.arg(number_prefix) AS TEXT)
               THEN 0
               ELSE 1
           END AS number_rank,
           CASE
               WHEN sqlc.arg(query) <> ''
                    AND LOWER(i.title) LIKE sqlc.arg(title_query)
               THEN 0
               ELSE 1
           END AS title_rank
    FROM middleman_issues i
    WHERE i.repo_id = sqlc.arg(repo_id)
      AND (
          sqlc.arg(query) = ''
          OR CAST(i.number AS TEXT) LIKE CAST(sqlc.arg(number_prefix) AS TEXT)
          OR LOWER(i.title) LIKE sqlc.arg(title_query)
      )
)
ORDER BY
    number_rank,
    title_rank,
    last_activity_at DESC,
    number DESC
LIMIT sqlc.arg(limit);
