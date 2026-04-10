package stacks

import (
	"context"
	"log/slog"

	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

// SyncCompletedHook returns a callback for Syncer.SetOnSyncCompleted
// that runs stack detection for each synced repo.
func SyncCompletedHook(ctx context.Context, database *db.DB, next func([]ghclient.RepoSyncResult)) func([]ghclient.RepoSyncResult) {
	return func(results []ghclient.RepoSyncResult) {
		defer func() {
			if next != nil {
				next(results)
			}
		}()
		for _, result := range results {
			if ctx.Err() != nil {
				return
			}
			if result.Error != "" {
				continue
			}
			repo, err := database.GetRepoByOwnerName(ctx, result.Owner, result.Name)
			if err != nil || repo == nil {
				continue
			}
			if err := RunDetection(ctx, database, repo.ID); err != nil {
				slog.Error("stack detection failed",
					"repo", result.Owner+"/"+result.Name, "err", err)
			}
		}
	}
}
