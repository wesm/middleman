-- name: SetStarred :exec
INSERT INTO middleman_starred_items (item_type, repo_id, number)
VALUES (sqlc.arg(item_type), sqlc.arg(repo_id), sqlc.arg(number))
ON CONFLICT(item_type, repo_id, number) DO NOTHING;

-- name: UnsetStarred :exec
DELETE FROM middleman_starred_items
WHERE item_type = sqlc.arg(item_type)
  AND repo_id = sqlc.arg(repo_id)
  AND number = sqlc.arg(number);

-- name: IsStarred :one
SELECT EXISTS (
    SELECT 1
    FROM middleman_starred_items
    WHERE item_type = sqlc.arg(item_type)
      AND repo_id = sqlc.arg(repo_id)
      AND number = sqlc.arg(number)
);
