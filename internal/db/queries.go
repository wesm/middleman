package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	dbsqlc "github.com/wesm/middleman/internal/db/sqlc"
)

func canonicalRepoIdentifier(host, owner, name string) (string, string, string) {
	if host == "" {
		host = "github.com"
	}
	return strings.ToLower(host), strings.ToLower(owner), strings.ToLower(name)
}

func lookupLabelIDByNameTx(ctx context.Context, q *dbsqlc.Queries, repoID int64, name string) (int64, bool, error) {
	id, err := q.LookupLabelIDByName(ctx, dbsqlc.LookupLabelIDByNameParams{
		RepoID: repoID,
		Name:   name,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func labelPlatformIDTx(ctx context.Context, q *dbsqlc.Queries, labelID int64) (sql.NullInt64, error) {
	platformID, err := q.GetLabelPlatformID(ctx, labelID)
	if err != nil {
		return sql.NullInt64{}, err
	}
	return platformID, nil
}

func mergeLabelRowAssociationsTx(ctx context.Context, q *dbsqlc.Queries, fromLabelID, toLabelID int64) error {
	if err := q.MoveIssueLabelAssociations(ctx, dbsqlc.MoveIssueLabelAssociationsParams{
		ToLabelID:   toLabelID,
		FromLabelID: fromLabelID,
	}); err != nil {
		return fmt.Errorf("move issue label associations: %w", err)
	}
	if err := q.DeleteIssueLabelAssociationsByLabel(ctx, fromLabelID); err != nil {
		return fmt.Errorf("delete old issue label associations: %w", err)
	}
	if err := q.MoveMergeRequestLabelAssociations(ctx, dbsqlc.MoveMergeRequestLabelAssociationsParams{
		ToLabelID:   toLabelID,
		FromLabelID: fromLabelID,
	}); err != nil {
		return fmt.Errorf("move merge request label associations: %w", err)
	}
	if err := q.DeleteMergeRequestLabelAssociationsByLabel(ctx, fromLabelID); err != nil {
		return fmt.Errorf("delete old merge request label associations: %w", err)
	}
	if err := q.DeleteLabelByID(ctx, fromLabelID); err != nil {
		return fmt.Errorf("delete old label row: %w", err)
	}
	return nil
}

func lookupLabelIDByPlatformIDTx(ctx context.Context, q *dbsqlc.Queries, repoID, platformID int64) (int64, bool, error) {
	if platformID == 0 {
		return 0, false, nil
	}
	id, err := q.LookupLabelIDByPlatformID(ctx, dbsqlc.LookupLabelIDByPlatformIDParams{
		RepoID:     repoID,
		PlatformID: sql.NullInt64{Int64: platformID, Valid: true},
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func labelIDForUpsertTx(ctx context.Context, q *dbsqlc.Queries, repoID int64, label Label) (int64, bool, error) {
	platformID, foundByPlatform, err := lookupLabelIDByPlatformIDTx(ctx, q, repoID, label.PlatformID)
	if err != nil {
		return 0, false, fmt.Errorf("lookup label %s by platform id: %w", label.Name, err)
	}
	nameID, foundByName, err := lookupLabelIDByNameTx(ctx, q, repoID, label.Name)
	if err != nil {
		return 0, false, fmt.Errorf("lookup label %s by name: %w", label.Name, err)
	}
	if foundByPlatform && foundByName && platformID != nameID {
		namePlatformID, err := labelPlatformIDTx(ctx, q, nameID)
		if err != nil {
			return 0, false, fmt.Errorf("lookup label %s platform id: %w", label.Name, err)
		}
		if !namePlatformID.Valid {
			if err := mergeLabelRowAssociationsTx(ctx, q, nameID, platformID); err != nil {
				return 0, false, fmt.Errorf("merge stale label %s into platform row: %w", label.Name, err)
			}
			return platformID, true, nil
		}
		return 0, false, fmt.Errorf("label %s in repo %d matches different rows by name and platform id", label.Name, repoID)
	}
	if foundByPlatform {
		return platformID, true, nil
	}
	if foundByName {
		return nameID, true, nil
	}
	return 0, false, nil
}

func repoIDForIssueTx(ctx context.Context, q *dbsqlc.Queries, issueID int64) (int64, error) {
	repoID, err := q.GetIssueRepoID(ctx, issueID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("issue %d not found", issueID)
	}
	if err != nil {
		return 0, fmt.Errorf("lookup issue repo: %w", err)
	}
	return repoID, nil
}

func repoIDForMergeRequestTx(ctx context.Context, q *dbsqlc.Queries, mrID int64) (int64, error) {
	repoID, err := q.GetMergeRequestRepoID(ctx, mrID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("merge request %d not found", mrID)
	}
	if err != nil {
		return 0, fmt.Errorf("lookup merge request repo: %w", err)
	}
	return repoID, nil
}

func upsertLabelsTx(ctx context.Context, q *dbsqlc.Queries, repoID int64, labels []Label) (map[string]int64, error) {
	ids := make(map[string]int64, len(labels))
	for _, label := range labels {
		label.UpdatedAt = canonicalUTCTime(label.UpdatedAt)
		id, found, err := labelIDForUpsertTx(ctx, q, repoID, label)
		if err != nil {
			return nil, err
		}
		if !found {
			id, err = q.InsertLabel(ctx, dbsqlc.InsertLabelParams{
				RepoID:      repoID,
				PlatformID:  label.PlatformID,
				Name:        label.Name,
				Description: label.Description,
				Color:       label.Color,
				IsDefault:   boolInt64(label.IsDefault),
				UpdatedAt:   label.UpdatedAt,
			})
			if err != nil {
				return nil, fmt.Errorf("insert label %s: %w", label.Name, err)
			}
		} else {
			err = q.UpdateLabel(ctx, dbsqlc.UpdateLabelParams{
				PlatformID:  label.PlatformID,
				Name:        label.Name,
				Description: label.Description,
				Color:       label.Color,
				IsDefault:   boolInt64(label.IsDefault),
				UpdatedAt:   label.UpdatedAt,
				ID:          id,
			})
			if err != nil {
				return nil, fmt.Errorf("update label %s: %w", label.Name, err)
			}
		}
		ids[label.Name] = id
	}
	return ids, nil
}

func replaceIssueLabelsTx(ctx context.Context, q *dbsqlc.Queries, repoID, issueID int64, labels []Label) error {
	actualRepoID, err := repoIDForIssueTx(ctx, q, issueID)
	if err != nil {
		return err
	}
	if actualRepoID != repoID {
		return fmt.Errorf("issue %d belongs to repo %d, not repo %d", issueID, actualRepoID, repoID)
	}
	if err := q.DeleteIssueLabelsByIssueID(ctx, issueID); err != nil {
		return fmt.Errorf("delete issue labels: %w", err)
	}
	if len(labels) == 0 {
		return nil
	}
	ids, err := upsertLabelsTx(ctx, q, actualRepoID, labels)
	if err != nil {
		return err
	}
	for _, label := range labels {
		if err := q.InsertIssueLabel(ctx, dbsqlc.InsertIssueLabelParams{
			IssueID: issueID,
			LabelID: ids[label.Name],
		}); err != nil {
			return fmt.Errorf("insert issue label %s: %w", label.Name, err)
		}
	}
	return nil
}

func replaceMergeRequestLabelsTx(ctx context.Context, q *dbsqlc.Queries, repoID, mrID int64, labels []Label) error {
	actualRepoID, err := repoIDForMergeRequestTx(ctx, q, mrID)
	if err != nil {
		return err
	}
	if actualRepoID != repoID {
		return fmt.Errorf("merge request %d belongs to repo %d, not repo %d", mrID, actualRepoID, repoID)
	}
	if err := q.DeleteMergeRequestLabelsByMRID(ctx, mrID); err != nil {
		return fmt.Errorf("delete merge request labels: %w", err)
	}
	if len(labels) == 0 {
		return nil
	}
	ids, err := upsertLabelsTx(ctx, q, actualRepoID, labels)
	if err != nil {
		return err
	}
	for _, label := range labels {
		if err := q.InsertMergeRequestLabel(ctx, dbsqlc.InsertMergeRequestLabelParams{
			MergeRequestID: mrID,
			LabelID:        ids[label.Name],
		}); err != nil {
			return fmt.Errorf("insert merge request label %s: %w", label.Name, err)
		}
	}
	return nil
}

func (d *DB) UpsertLabels(ctx context.Context, repoID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		q := d.writeQueries.WithTx(tx)
		_, err := upsertLabelsTx(ctx, q, repoID, labels)
		return err
	})
}

func (d *DB) ReplaceIssueLabels(ctx context.Context, repoID, issueID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		return replaceIssueLabelsTx(ctx, d.writeQueries.WithTx(tx), repoID, issueID, labels)
	})
}

func (d *DB) ReplaceMergeRequestLabels(ctx context.Context, repoID, mrID int64, labels []Label) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		return replaceMergeRequestLabelsTx(ctx, d.writeQueries.WithTx(tx), repoID, mrID, labels)
	})
}

func (d *DB) loadLabelsForMergeRequests(ctx context.Context, ids []int64) (map[int64][]Label, error) {
	if len(ids) == 0 {
		return map[int64][]Label{}, nil
	}
	rows, err := d.readQueries.ListLabelsForMergeRequestIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("query merge request labels: %w", err)
	}

	out := make(map[int64][]Label, len(ids))
	for _, row := range rows {
		out[row.MergeRequestID] = append(out[row.MergeRequestID], Label{
			ID:          row.ID,
			RepoID:      row.RepoID,
			PlatformID:  row.PlatformID,
			Name:        row.Name,
			Description: row.Description,
			Color:       row.Color,
			IsDefault:   repoBool(row.IsDefault),
			UpdatedAt:   row.UpdatedAt.UTC(),
		})
	}
	return out, nil
}

func (d *DB) loadLabelsForIssues(ctx context.Context, ids []int64) (map[int64][]Label, error) {
	if len(ids) == 0 {
		return map[int64][]Label{}, nil
	}
	rows, err := d.readQueries.ListLabelsForIssueIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("query issue labels: %w", err)
	}

	out := make(map[int64][]Label, len(ids))
	for _, row := range rows {
		out[row.IssueID] = append(out[row.IssueID], Label{
			ID:          row.ID,
			RepoID:      row.RepoID,
			PlatformID:  row.PlatformID,
			Name:        row.Name,
			Description: row.Description,
			Color:       row.Color,
			IsDefault:   repoBool(row.IsDefault),
			UpdatedAt:   row.UpdatedAt.UTC(),
		})
	}
	return out, nil
}

func mergeRequestFromOwnerNameRow(row dbsqlc.GetMergeRequestByOwnerNameNumberRow) MergeRequest {
	return MergeRequest{
		ID:                row.ID,
		RepoID:            row.RepoID,
		PlatformID:        row.PlatformID,
		Number:            int(row.Number),
		URL:               row.Url,
		Title:             row.Title,
		Author:            row.Author,
		AuthorDisplayName: row.AuthorDisplayName,
		State:             row.State,
		IsDraft:           repoBool(row.IsDraft),
		Body:              row.Body,
		HeadBranch:        row.HeadBranch,
		BaseBranch:        row.BaseBranch,
		PlatformHeadSHA:   row.PlatformHeadSha,
		PlatformBaseSHA:   row.PlatformBaseSha,
		DiffHeadSHA:       row.DiffHeadSha,
		DiffBaseSHA:       row.DiffBaseSha,
		MergeBaseSHA:      row.MergeBaseSha,
		HeadRepoCloneURL:  row.HeadRepoCloneUrl,
		Additions:         int(row.Additions),
		Deletions:         int(row.Deletions),
		CommentCount:      int(row.CommentCount),
		ReviewDecision:    row.ReviewDecision,
		CIStatus:          row.CiStatus,
		CIChecksJSON:      row.CiChecksJson,
		CreatedAt:         row.CreatedAt.UTC(),
		UpdatedAt:         row.UpdatedAt.UTC(),
		LastActivityAt:    row.LastActivityAt.UTC(),
		MergedAt:          timeFromNull(row.MergedAt),
		ClosedAt:          timeFromNull(row.ClosedAt),
		MergeableState:    row.MergeableState,
		DetailFetchedAt:   timeFromNull(row.DetailFetchedAt),
		CIHadPending:      repoBool(row.CiHadPending),
		KanbanStatus:      row.KanbanStatus,
		Starred:           boolFromSQLValue(row.Starred),
	}
}

func mergeRequestFromRepoIDRow(row dbsqlc.GetMergeRequestByRepoIDAndNumberRow) MergeRequest {
	return MergeRequest{
		ID:                row.ID,
		RepoID:            row.RepoID,
		PlatformID:        row.PlatformID,
		Number:            int(row.Number),
		URL:               row.Url,
		Title:             row.Title,
		Author:            row.Author,
		AuthorDisplayName: row.AuthorDisplayName,
		State:             row.State,
		IsDraft:           repoBool(row.IsDraft),
		Body:              row.Body,
		HeadBranch:        row.HeadBranch,
		BaseBranch:        row.BaseBranch,
		PlatformHeadSHA:   row.PlatformHeadSha,
		PlatformBaseSHA:   row.PlatformBaseSha,
		DiffHeadSHA:       row.DiffHeadSha,
		DiffBaseSHA:       row.DiffBaseSha,
		MergeBaseSHA:      row.MergeBaseSha,
		HeadRepoCloneURL:  row.HeadRepoCloneUrl,
		Additions:         int(row.Additions),
		Deletions:         int(row.Deletions),
		CommentCount:      int(row.CommentCount),
		ReviewDecision:    row.ReviewDecision,
		CIStatus:          row.CiStatus,
		CIChecksJSON:      row.CiChecksJson,
		CreatedAt:         row.CreatedAt.UTC(),
		UpdatedAt:         row.UpdatedAt.UTC(),
		LastActivityAt:    row.LastActivityAt.UTC(),
		MergedAt:          timeFromNull(row.MergedAt),
		ClosedAt:          timeFromNull(row.ClosedAt),
		MergeableState:    row.MergeableState,
		DetailFetchedAt:   timeFromNull(row.DetailFetchedAt),
		CIHadPending:      repoBool(row.CiHadPending),
		KanbanStatus:      row.KanbanStatus,
		Starred:           boolFromSQLValue(row.Starred),
	}
}

func issueFromOwnerNameRow(row dbsqlc.GetIssueByOwnerNameNumberRow) Issue {
	return Issue{
		ID:              row.ID,
		RepoID:          row.RepoID,
		PlatformID:      row.PlatformID,
		Number:          int(row.Number),
		URL:             row.Url,
		Title:           row.Title,
		Author:          row.Author,
		State:           row.State,
		Body:            row.Body,
		CommentCount:    int(row.CommentCount),
		LabelsJSON:      row.LabelsJson,
		CreatedAt:       row.CreatedAt.UTC(),
		UpdatedAt:       row.UpdatedAt.UTC(),
		LastActivityAt:  row.LastActivityAt.UTC(),
		ClosedAt:        timeFromNull(row.ClosedAt),
		DetailFetchedAt: timeFromNull(row.DetailFetchedAt),
		Starred:         boolFromSQLValue(row.Starred),
	}
}

func issueFromRepoIDRow(row dbsqlc.GetIssueByRepoIDAndNumberRow) Issue {
	return Issue{
		ID:              row.ID,
		RepoID:          row.RepoID,
		PlatformID:      row.PlatformID,
		Number:          int(row.Number),
		URL:             row.Url,
		Title:           row.Title,
		Author:          row.Author,
		State:           row.State,
		Body:            row.Body,
		CommentCount:    int(row.CommentCount),
		LabelsJSON:      row.LabelsJson,
		CreatedAt:       row.CreatedAt.UTC(),
		UpdatedAt:       row.UpdatedAt.UTC(),
		LastActivityAt:  row.LastActivityAt.UTC(),
		ClosedAt:        timeFromNull(row.ClosedAt),
		DetailFetchedAt: timeFromNull(row.DetailFetchedAt),
		Starred:         boolFromSQLValue(row.Starred),
	}
}

// PurgeOtherHosts deletes all data for platform hosts other
// than keepHost. Deletes in FK-dependency order so it works
// on existing DBs where CASCADE may not be retrofitted.
func (d *DB) PurgeOtherHosts(ctx context.Context, keepHost string) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		q := d.writeQueries.WithTx(tx)
		if err := q.PurgeOtherHostStarredItems(ctx, keepHost); err != nil {
			return err
		}
		if err := q.PurgeOtherHostWorktreeLinks(ctx, keepHost); err != nil {
			return err
		}
		if err := q.PurgeOtherHostKanbanState(ctx, keepHost); err != nil {
			return err
		}
		if err := q.PurgeOtherHostMergeRequestEvents(ctx, keepHost); err != nil {
			return err
		}
		if err := q.PurgeOtherHostMergeRequests(ctx, keepHost); err != nil {
			return err
		}
		if err := q.PurgeOtherHostIssueEvents(ctx, keepHost); err != nil {
			return err
		}
		if err := q.PurgeOtherHostIssues(ctx, keepHost); err != nil {
			return err
		}
		if err := q.PurgeOtherHostRepos(ctx, keepHost); err != nil {
			return err
		}
		if err := q.PurgeOtherHostRateLimits(ctx, keepHost); err != nil {
			return err
		}
		return nil
	})
}

// --- Repos ---

// UpsertRepo inserts a repo if it does not exist, then returns its ID.
// host is the platform hostname (e.g. "github.com" or a GHE hostname).
func (d *DB) UpsertRepo(ctx context.Context, host, owner, name string) (int64, error) {
	host, owner, name = canonicalRepoIdentifier(host, owner, name)
	id, err := d.writeQueries.UpsertRepo(ctx, dbsqlc.UpsertRepoParams{
		PlatformHost: host,
		Owner:        owner,
		Name:         name,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert repo: %w", err)
	}
	return id, nil
}

func repoTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	utc := t.Time.UTC()
	return &utc
}

func repoString(s sql.NullString) string {
	if !s.Valid {
		return ""
	}
	return s.String
}

func repoBool(v int64) bool {
	return v != 0
}

func boolInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func nullUTCTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	utc := canonicalUTCTime(*t)
	return sql.NullTime{Time: utc, Valid: true}
}

func timeFromNull(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	utc := t.Time.UTC()
	return &utc
}

func nullInt64FromPtr(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func ptrFromNullInt64(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func boolFromSQLValue(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case int64:
		return value != 0
	case int:
		return value != 0
	case []byte:
		return string(value) != "" && string(value) != "0"
	case string:
		return value != "" && value != "0"
	default:
		return false
	}
}

func stringFromSQLValue(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}

func repoFromListRow(row dbsqlc.ListReposRow) Repo {
	r := Repo{
		ID:                       row.ID,
		Platform:                 row.Platform,
		PlatformHost:             row.PlatformHost,
		Owner:                    row.Owner,
		Name:                     row.Name,
		LastSyncStartedAt:        repoTime(row.LastSyncStartedAt),
		LastSyncCompletedAt:      repoTime(row.LastSyncCompletedAt),
		LastSyncError:            repoString(row.LastSyncError),
		AllowSquashMerge:         repoBool(row.AllowSquashMerge),
		AllowMergeCommit:         repoBool(row.AllowMergeCommit),
		AllowRebaseMerge:         repoBool(row.AllowRebaseMerge),
		BackfillPRPage:           int(row.BackfillPrPage),
		BackfillPRComplete:       repoBool(row.BackfillPrComplete),
		BackfillPRCompletedAt:    repoTime(row.BackfillPrCompletedAt),
		BackfillIssuePage:        int(row.BackfillIssuePage),
		BackfillIssueComplete:    repoBool(row.BackfillIssueComplete),
		BackfillIssueCompletedAt: repoTime(row.BackfillIssueCompletedAt),
		CreatedAt:                row.CreatedAt,
	}
	normalizeRepoTimestamps(&r)
	return r
}

func repoFromOwnerNameRow(row dbsqlc.GetRepoByOwnerNameRow) Repo {
	return repoFromListRow(dbsqlc.ListReposRow(row))
}

func repoFromIDRow(row dbsqlc.GetRepoByIDRow) Repo {
	return repoFromListRow(dbsqlc.ListReposRow(row))
}

func repoFromHostOwnerNameRow(row dbsqlc.GetRepoByHostOwnerNameRow) Repo {
	return repoFromListRow(dbsqlc.ListReposRow(row))
}

// ListRepos returns all repos ordered by owner, name.
func (d *DB) ListRepos(ctx context.Context) ([]Repo, error) {
	rows, err := d.readQueries.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}

	var repos []Repo
	for _, row := range rows {
		repos = append(repos, repoFromListRow(row))
	}
	return repos, nil
}

// UpdateRepoSyncStarted records the time a sync began.
func (d *DB) UpdateRepoSyncStarted(ctx context.Context, id int64, t time.Time) error {
	t = canonicalUTCTime(t)
	err := d.writeQueries.UpdateRepoSyncStarted(ctx, dbsqlc.UpdateRepoSyncStartedParams{
		LastSyncStartedAt: sql.NullTime{Time: t, Valid: !t.IsZero()},
		ID:                id,
	})
	if err != nil {
		return fmt.Errorf("update repo sync started: %w", err)
	}
	return nil
}

// UpdateRepoSyncCompleted records the time and optional error a sync finished.
func (d *DB) UpdateRepoSyncCompleted(ctx context.Context, id int64, t time.Time, syncErr string) error {
	t = canonicalUTCTime(t)
	err := d.writeQueries.UpdateRepoSyncCompleted(ctx, dbsqlc.UpdateRepoSyncCompletedParams{
		LastSyncCompletedAt: sql.NullTime{Time: t, Valid: !t.IsZero()},
		LastSyncError:       sql.NullString{String: syncErr, Valid: true},
		ID:                  id,
	})
	if err != nil {
		return fmt.Errorf("update repo sync completed: %w", err)
	}
	return nil
}

// GetRepoByOwnerName returns the repo for the given owner/name, or nil if not found.
// Config validation rejects duplicate owner/name across hosts, so this should
// always be unambiguous. The ORDER BY provides deterministic results as a
// safety net if stale data from a previous config exists in the database.
func (d *DB) GetRepoByOwnerName(ctx context.Context, owner, name string) (*Repo, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	row, err := d.readQueries.GetRepoByOwnerName(ctx, dbsqlc.GetRepoByOwnerNameParams{
		Owner: owner,
		Name:  name,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get repo by owner/name: %w", err)
	}
	r := repoFromOwnerNameRow(row)
	return &r, nil
}

// GetRepoByID returns the repo with the given ID, or nil if not found.
func (d *DB) GetRepoByID(ctx context.Context, id int64) (*Repo, error) {
	row, err := d.readQueries.GetRepoByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get repo by id: %w", err)
	}
	r := repoFromIDRow(row)
	return &r, nil
}

func normalizeRepoTimestamps(r *Repo) {
	if r == nil {
		return
	}
	r.CreatedAt = r.CreatedAt.UTC()
	if r.LastSyncStartedAt != nil {
		t := r.LastSyncStartedAt.UTC()
		r.LastSyncStartedAt = &t
	}
	if r.LastSyncCompletedAt != nil {
		t := r.LastSyncCompletedAt.UTC()
		r.LastSyncCompletedAt = &t
	}
	if r.BackfillPRCompletedAt != nil {
		t := r.BackfillPRCompletedAt.UTC()
		r.BackfillPRCompletedAt = &t
	}
	if r.BackfillIssueCompletedAt != nil {
		t := r.BackfillIssueCompletedAt.UTC()
		r.BackfillIssueCompletedAt = &t
	}
}

// UpdateRepoSettings updates the merge method settings for a repo.
func (d *DB) UpdateRepoSettings(
	ctx context.Context,
	id int64,
	allowSquash, allowMerge, allowRebase bool,
) error {
	err := d.writeQueries.UpdateRepoSettings(ctx, dbsqlc.UpdateRepoSettingsParams{
		AllowSquashMerge: boolInt64(allowSquash),
		AllowMergeCommit: boolInt64(allowMerge),
		AllowRebaseMerge: boolInt64(allowRebase),
		ID:               id,
	})
	return err
}

// --- Merge Requests ---

// UpsertMergeRequest inserts or updates a merge request, returning its internal
// ID. Before writing, all timestamp fields are normalized to UTC so the raw
// SQLite DATETIME text stays comparable in SQL.
// On conflict (repo_id, number), stale snapshots are ignored wholesale.
func (d *DB) UpsertMergeRequest(ctx context.Context, mr *MergeRequest) (int64, error) {
	canonicalizeMergeRequestTimestamps(mr)
	err := d.writeQueries.UpsertMergeRequest(ctx, dbsqlc.UpsertMergeRequestParams{
		RepoID:            mr.RepoID,
		PlatformID:        mr.PlatformID,
		Number:            int64(mr.Number),
		Url:               mr.URL,
		Title:             mr.Title,
		Author:            mr.Author,
		AuthorDisplayName: mr.AuthorDisplayName,
		State:             mr.State,
		IsDraft:           boolInt64(mr.IsDraft),
		Body:              mr.Body,
		HeadBranch:        mr.HeadBranch,
		BaseBranch:        mr.BaseBranch,
		PlatformHeadSha:   mr.PlatformHeadSHA,
		PlatformBaseSha:   mr.PlatformBaseSHA,
		HeadRepoCloneUrl:  mr.HeadRepoCloneURL,
		Additions:         int64(mr.Additions),
		Deletions:         int64(mr.Deletions),
		CommentCount:      int64(mr.CommentCount),
		ReviewDecision:    mr.ReviewDecision,
		CiStatus:          mr.CIStatus,
		CiChecksJson:      mr.CIChecksJSON,
		DetailFetchedAt:   nullUTCTime(mr.DetailFetchedAt),
		CiHadPending:      boolInt64(mr.CIHadPending),
		CreatedAt:         mr.CreatedAt,
		UpdatedAt:         mr.UpdatedAt,
		LastActivityAt:    mr.LastActivityAt,
		MergedAt:          nullUTCTime(mr.MergedAt),
		ClosedAt:          nullUTCTime(mr.ClosedAt),
		MergeableState:    mr.MergeableState,
	})
	if err != nil {
		return 0, fmt.Errorf("upsert merge request: %w", err)
	}
	id, err := d.readQueries.GetMergeRequestIDByRepoIDAndNumber(ctx, dbsqlc.GetMergeRequestIDByRepoIDAndNumberParams{
		RepoID: mr.RepoID,
		Number: int64(mr.Number),
	})
	if err != nil {
		return 0, fmt.Errorf("get mr id after upsert: %w", err)
	}
	return id, nil
}

// GetMergeRequest returns a merge request by repo owner/name and MR number, or nil if not found.
func (d *DB) GetMergeRequest(ctx context.Context, owner, name string, number int) (*MergeRequest, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	row, err := d.readQueries.GetMergeRequestByOwnerNameNumber(ctx, dbsqlc.GetMergeRequestByOwnerNameNumberParams{
		Owner:  owner,
		Name:   name,
		Number: int64(number),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get merge request: %w", err)
	}
	mr := mergeRequestFromOwnerNameRow(row)
	labelsByMR, err := d.loadLabelsForMergeRequests(ctx, []int64{mr.ID})
	if err != nil {
		return nil, fmt.Errorf("load merge request labels: %w", err)
	}
	mr.Labels = labelsByMR[mr.ID]
	return &mr, nil
}

// GetMergeRequestByRepoIDAndNumber returns a merge request by repo ID and number.
func (d *DB) GetMergeRequestByRepoIDAndNumber(ctx context.Context, repoID int64, number int) (*MergeRequest, error) {
	row, err := d.readQueries.GetMergeRequestByRepoIDAndNumber(ctx, dbsqlc.GetMergeRequestByRepoIDAndNumberParams{
		RepoID: repoID,
		Number: int64(number),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get merge request by repo id: %w", err)
	}
	mr := mergeRequestFromRepoIDRow(row)
	labelsByMR, err := d.loadLabelsForMergeRequests(ctx, []int64{mr.ID})
	if err != nil {
		return nil, fmt.Errorf("load merge request labels: %w", err)
	}
	mr.Labels = labelsByMR[mr.ID]
	return &mr, nil
}

// ListMergeRequests returns merge requests matching the given options.
// Results are ordered by last_activity_at DESC.
func (d *DB) ListMergeRequests(ctx context.Context, opts ListMergeRequestsOpts) ([]MergeRequest, error) {
	state := opts.State
	if state == "" {
		state = "open"
	}
	var conds []string
	var args []any

	switch state {
	case "all":
		// no state filter
	case "closed":
		conds = append(conds, "p.state IN ('closed', 'merged')")
	default:
		conds = append(conds, "p.state = ?")
		args = append(args, state)
	}

	if opts.RepoOwner != "" && opts.RepoName != "" {
		_, owner, name := canonicalRepoIdentifier(
			"", opts.RepoOwner, opts.RepoName,
		)
		if opts.PlatformHost != "" {
			host, _, _ := canonicalRepoIdentifier(opts.PlatformHost, "", "")
			conds = append(conds, "r.platform_host = ?")
			args = append(args, host)
		}
		conds = append(conds, "r.owner = ? AND r.name = ?")
		args = append(args, owner, name)
	}
	if opts.KanbanState != "" {
		conds = append(conds, "COALESCE(k.status, '') = ?")
		args = append(args, opts.KanbanState)
	}
	if opts.Starred {
		conds = append(conds, "s.number IS NOT NULL")
	}
	if opts.Search != "" {
		conds = append(conds, "(p.title LIKE ? OR p.author LIKE ?)")
		like := "%" + opts.Search + "%"
		args = append(args, like, like)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT p.id, p.repo_id, p.platform_id, p.number, p.url, p.title,
		       p.author, p.author_display_name, p.state, p.is_draft,
		       p.body, p.head_branch, p.base_branch,
		       p.platform_head_sha, p.platform_base_sha,
		       p.diff_head_sha, p.diff_base_sha, p.merge_base_sha,
		       p.head_repo_clone_url,
		       p.additions, p.deletions, p.comment_count, p.review_decision,
		       p.ci_status, p.ci_checks_json,
		       p.created_at, p.updated_at, p.last_activity_at,
		       p.merged_at, p.closed_at, p.mergeable_state,
		       p.detail_fetched_at, p.ci_had_pending,
		       COALESCE(k.status, '') AS kanban_status,
		       (s.number IS NOT NULL) AS starred
		FROM middleman_merge_requests p
		JOIN middleman_repos r ON r.id = p.repo_id
		LEFT JOIN middleman_kanban_state k ON k.merge_request_id = p.id
		LEFT JOIN middleman_starred_items s
		    ON s.item_type = 'pr' AND s.repo_id = p.repo_id AND s.number = p.number
		%s
		ORDER BY p.last_activity_at DESC`, where)

	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list merge requests: %w", err)
	}
	defer rows.Close()

	var mrs []MergeRequest
	var mrIDs []int64
	for rows.Next() {
		var mr MergeRequest
		if err := rows.Scan(
			&mr.ID, &mr.RepoID, &mr.PlatformID, &mr.Number, &mr.URL, &mr.Title,
			&mr.Author, &mr.AuthorDisplayName, &mr.State, &mr.IsDraft,
			&mr.Body, &mr.HeadBranch, &mr.BaseBranch,
			&mr.PlatformHeadSHA, &mr.PlatformBaseSHA,
			&mr.DiffHeadSHA, &mr.DiffBaseSHA, &mr.MergeBaseSHA,
			&mr.HeadRepoCloneURL,
			&mr.Additions, &mr.Deletions, &mr.CommentCount, &mr.ReviewDecision,
			&mr.CIStatus, &mr.CIChecksJSON,
			&mr.CreatedAt, &mr.UpdatedAt, &mr.LastActivityAt,
			&mr.MergedAt, &mr.ClosedAt, &mr.MergeableState,
			&mr.DetailFetchedAt, &mr.CIHadPending,
			&mr.KanbanStatus, &mr.Starred,
		); err != nil {
			return nil, fmt.Errorf("scan merge request: %w", err)
		}
		mrs = append(mrs, mr)
		mrIDs = append(mrIDs, mr.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	labelsByMR, err := d.loadLabelsForMergeRequests(ctx, mrIDs)
	if err != nil {
		return nil, fmt.Errorf("load merge request labels: %w", err)
	}
	for i := range mrs {
		mrs[i].Labels = labelsByMR[mrs[i].ID]
	}
	return mrs, nil
}

// --- Events ---

// UpsertMREvents bulk-inserts events after normalizing CreatedAt to UTC.
// When a duplicate dedupe key is seen again, the conflict path refreshes
// mutable fields so edited events and legacy local-offset timestamps are
// repaired during normal sync.
func (d *DB) UpsertMREvents(ctx context.Context, events []MREvent) error {
	if len(events) == 0 {
		return nil
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		q := d.writeQueries.WithTx(tx)
		for i := range events {
			e := &events[i]
			canonicalizeMREventTimestamps(e)
			if err := q.UpsertMREvent(ctx, dbsqlc.UpsertMREventParams{
				MergeRequestID: e.MergeRequestID,
				PlatformID:     nullInt64FromPtr(e.PlatformID),
				EventType:      e.EventType,
				Author:         e.Author,
				Summary:        e.Summary,
				Body:           e.Body,
				MetadataJson:   e.MetadataJSON,
				CreatedAt:      e.CreatedAt,
				DedupeKey:      e.DedupeKey,
			}); err != nil {
				return fmt.Errorf("insert mr event (dedupe_key=%s): %w", e.DedupeKey, err)
			}
		}
		return nil
	})
}

func (d *DB) MRCommentEventExists(
	ctx context.Context,
	mrID int64,
	platformID int64,
) (bool, error) {
	exists, err := d.readQueries.MRCommentEventExists(ctx, dbsqlc.MRCommentEventExistsParams{
		MergeRequestID: mrID,
		PlatformID:     sql.NullInt64{Int64: platformID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check mr comment event exists: %w", err)
	}
	return exists, nil
}

// DeleteMissingMRCommentEvents removes issue_comment rows for a PR whose
// dedupe keys are absent from the latest GitHub comment list.
func (d *DB) DeleteMissingMRCommentEvents(
	ctx context.Context,
	mrID int64,
	dedupeKeys []string,
) error {
	var err error
	if len(dedupeKeys) > 0 {
		err = d.writeQueries.DeleteMissingMRCommentEvents(ctx, dbsqlc.DeleteMissingMRCommentEventsParams{
			MergeRequestID: mrID,
			DedupeKeys:     dedupeKeys,
		})
	} else {
		err = d.writeQueries.DeleteAllMRCommentEvents(ctx, mrID)
	}
	if err != nil {
		return fmt.Errorf("delete missing mr comment events: %w", err)
	}
	return nil
}

// GetMRLatestNonCommentEventTime returns the most recent created_at across
// non-comment events (reviews, commits, force pushes) for a merge request.
// Returns zero time when no such events exist. The comment-only refresh
// paths use this to avoid regressing last_activity_at to a comment-derived
// value when reviews or commits with a newer timestamp are already stored.
func (d *DB) GetMRLatestNonCommentEventTime(ctx context.Context, mrID int64) (time.Time, error) {
	createdAtValue, err := d.readQueries.GetMRLatestNonCommentEventTime(ctx, mrID)
	if err != nil {
		return time.Time{}, fmt.Errorf("query latest non-comment mr event: %w", err)
	}
	createdAt := stringFromSQLValue(createdAtValue)
	if createdAt == "" {
		return time.Time{}, nil
	}
	t, err := parseDBTime(createdAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse latest non-comment mr event time %q: %w", createdAt, err)
	}
	return t, nil
}

// ListMREvents returns all events for a merge request ordered by created_at DESC.
func (d *DB) ListMREvents(ctx context.Context, mrID int64) ([]MREvent, error) {
	rows, err := d.readQueries.ListMREvents(ctx, mrID)
	if err != nil {
		return nil, fmt.Errorf("list mr events: %w", err)
	}

	var events []MREvent
	for _, row := range rows {
		t, err := parseDBTime(row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf(
				"parse mr event created_at %q: %w",
				row.CreatedAt, err)
		}
		events = append(events, MREvent{
			ID:             row.ID,
			MergeRequestID: row.MergeRequestID,
			PlatformID:     ptrFromNullInt64(row.PlatformID),
			EventType:      row.EventType,
			Author:         row.Author,
			Summary:        row.Summary,
			Body:           row.Body,
			MetadataJSON:   row.MetadataJson,
			CreatedAt:      t,
			DedupeKey:      row.DedupeKey,
		})
	}
	return events, nil
}

// --- Kanban ---

// EnsureKanbanState creates a kanban row with status "new" if one does not exist.
func (d *DB) EnsureKanbanState(ctx context.Context, mrID int64) error {
	if err := d.writeQueries.EnsureKanbanState(ctx, mrID); err != nil {
		return fmt.Errorf("ensure kanban state: %w", err)
	}
	return nil
}

// SetKanbanState sets the kanban status for a merge request (upsert).
func (d *DB) SetKanbanState(ctx context.Context, mrID int64, status string) error {
	if err := d.writeQueries.SetKanbanState(ctx, dbsqlc.SetKanbanStateParams{
		MergeRequestID: mrID,
		Status:         status,
	}); err != nil {
		return fmt.Errorf("set kanban state: %w", err)
	}
	return nil
}

// GetKanbanState returns the kanban state for a merge request, or nil if not found.
func (d *DB) GetKanbanState(ctx context.Context, mrID int64) (*KanbanState, error) {
	row, err := d.readQueries.GetKanbanState(ctx, mrID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get kanban state: %w", err)
	}
	return &KanbanState{
		MergeRequestID: row.MergeRequestID,
		Status:         row.Status,
		UpdatedAt:      row.UpdatedAt.UTC(),
	}, nil
}

// --- Helpers ---

// GetMRIDByRepoAndNumber returns the internal MR ID for a given repo+number.
func (d *DB) GetMRIDByRepoAndNumber(ctx context.Context, owner, name string, number int) (int64, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	id, err := d.readQueries.GetMRIDByOwnerNameNumber(ctx, dbsqlc.GetMRIDByOwnerNameNumberParams{
		Owner:  owner,
		Name:   name,
		Number: int64(number),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("MR %s/%s#%d not found", owner, name, number)
	}
	if err != nil {
		return 0, fmt.Errorf("get mr id by repo and number: %w", err)
	}
	return id, nil
}

// GetPreviouslyOpenMRNumbers returns MR numbers that are open in the DB but
// not in the stillOpen set — i.e. MRs that were closed/merged since the last sync.
func (d *DB) GetPreviouslyOpenMRNumbers(
	ctx context.Context,
	repoID int64,
	stillOpen map[int]bool,
) ([]int, error) {
	rows, err := d.readQueries.ListOpenMRNumbersByRepo(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("get previously open mrs: %w", err)
	}

	var closed []int
	for _, row := range rows {
		n := int(row)
		if !stillOpen[n] {
			closed = append(closed, n)
		}
	}
	return closed, nil
}

// MRDerivedFields holds computed fields that are refreshed after fetching timeline events.
type MRDerivedFields struct {
	ReviewDecision string
	CommentCount   int
	LastActivityAt time.Time
}

// IssueDerivedFields holds computed fields that are refreshed after fetching issue events.
type IssueDerivedFields struct {
	CommentCount   int
	LastActivityAt time.Time
}

// UpdateMRTitleBody updates only the title, body, updated_at, and
// last_activity_at fields. last_activity_at is set to
// MAX(existing, updatedAt) to preserve correct list ordering.
// Derived fields (CommentCount, CIStatus, etc.) are untouched.
func (d *DB) UpdateMRTitleBody(
	ctx context.Context,
	id int64,
	title, body string,
	updatedAt time.Time,
) error {
	updatedAt = canonicalUTCTime(updatedAt)
	if err := d.writeQueries.UpdateMRTitleBody(ctx, dbsqlc.UpdateMRTitleBodyParams{
		Title:          title,
		Body:           body,
		UpdatedAt:      updatedAt,
		LastActivityAt: updatedAt,
		ID:             id,
	}); err != nil {
		return fmt.Errorf("update mr title/body: %w", err)
	}
	return nil
}

// UpdateMRDerivedFields writes computed fields back to the merge_requests row.
func (d *DB) UpdateMRDerivedFields(
	ctx context.Context,
	repoID int64,
	number int,
	fields MRDerivedFields,
) error {
	fields.LastActivityAt = canonicalUTCTime(fields.LastActivityAt)
	if err := d.writeQueries.UpdateMRDerivedFields(ctx, dbsqlc.UpdateMRDerivedFieldsParams{
		ReviewDecision: fields.ReviewDecision,
		CommentCount:   int64(fields.CommentCount),
		LastActivityAt: fields.LastActivityAt,
		RepoID:         repoID,
		Number:         int64(number),
	}); err != nil {
		return fmt.Errorf("update mr derived fields: %w", err)
	}
	return nil
}

// UpdateIssueDerivedFields writes computed fields back to the issues row.
func (d *DB) UpdateIssueDerivedFields(
	ctx context.Context,
	repoID int64,
	number int,
	fields IssueDerivedFields,
) error {
	fields.LastActivityAt = canonicalUTCTime(fields.LastActivityAt)
	if err := d.writeQueries.UpdateIssueDerivedFields(ctx, dbsqlc.UpdateIssueDerivedFieldsParams{
		CommentCount:   int64(fields.CommentCount),
		LastActivityAt: fields.LastActivityAt,
		RepoID:         repoID,
		Number:         int64(number),
	}); err != nil {
		return fmt.Errorf("update issue derived fields: %w", err)
	}
	return nil
}

// UpdateMRCIStatus writes CI status and check runs JSON for a merge request.
func (d *DB) UpdateMRCIStatus(
	ctx context.Context,
	repoID int64,
	number int,
	ciStatus string,
	ciChecksJSON string,
) error {
	if err := d.writeQueries.UpdateMRCIStatus(ctx, dbsqlc.UpdateMRCIStatusParams{
		CiStatus:     ciStatus,
		CiChecksJson: ciChecksJSON,
		RepoID:       repoID,
		Number:       int64(number),
	}); err != nil {
		return fmt.Errorf("update mr ci status: %w", err)
	}
	return nil
}

// ClearMRCI resets ci_status, ci_checks_json, and ci_had_pending for a
// merge request. UpsertMergeRequest preserves ci_had_pending across
// upserts, so callers that observe a head SHA change need this to drop
// the stale pending flag along with the rest of the CI fields.
func (d *DB) ClearMRCI(
	ctx context.Context,
	repoID int64,
	number int,
) error {
	if err := d.writeQueries.ClearMRCI(ctx, dbsqlc.ClearMRCIParams{
		RepoID: repoID,
		Number: int64(number),
	}); err != nil {
		return fmt.Errorf("clear mr ci: %w", err)
	}
	return nil
}

// UpdateClosedMRState atomically updates the state, timestamps, and final
// platform head/base SHAs for a MR that has transitioned to closed or merged.
// updatedAt should be the MR's UpdatedAt timestamp from the platform.
func (d *DB) UpdateClosedMRState(
	ctx context.Context,
	repoID int64,
	number int,
	state string,
	updatedAt time.Time,
	mergedAt, closedAt *time.Time,
	platformHeadSHA, platformBaseSHA string,
) error {
	updatedAt = canonicalUTCTime(updatedAt)
	if err := d.writeQueries.UpdateClosedMRState(ctx, dbsqlc.UpdateClosedMRStateParams{
		State:           state,
		MergedAt:        nullUTCTime(mergedAt),
		ClosedAt:        nullUTCTime(closedAt),
		UpdatedAt:       updatedAt,
		LastActivityAt:  updatedAt,
		PlatformHeadSha: platformHeadSHA,
		PlatformBaseSha: platformBaseSHA,
		RepoID:          repoID,
		Number:          int64(number),
	}); err != nil {
		return fmt.Errorf("update closed MR state: %w", err)
	}
	return nil
}

// UpdateDiffSHAs stores the locally-verified diff SHAs for a merge request.
// Called after a successful bare clone fetch and merge-base computation.
func (d *DB) UpdateDiffSHAs(ctx context.Context, repoID int64, number int, diffHead, diffBase, mergeBase string) error {
	if err := d.writeQueries.UpdateDiffSHAs(ctx, dbsqlc.UpdateDiffSHAsParams{
		DiffHeadSha:  diffHead,
		DiffBaseSha:  diffBase,
		MergeBaseSha: mergeBase,
		RepoID:       repoID,
		Number:       int64(number),
	}); err != nil {
		return fmt.Errorf("update diff SHAs for MR %d: %w", number, err)
	}
	return nil
}

// UpdatePlatformSHAs stores the platform head/base SHAs for a merge
// request. Called after normalizing GitHub API data or in test setup.
func (d *DB) UpdatePlatformSHAs(
	ctx context.Context,
	repoID int64, number int,
	platformHead, platformBase string,
) error {
	if err := d.writeQueries.UpdatePlatformSHAs(ctx, dbsqlc.UpdatePlatformSHAsParams{
		PlatformHeadSha: platformHead,
		PlatformBaseSha: platformBase,
		RepoID:          repoID,
		Number:          int64(number),
	}); err != nil {
		return fmt.Errorf(
			"update platform SHAs for MR %d: %w", number, err)
	}
	return nil
}

// DiffSHAs holds the SHA columns needed by the diff endpoint.
type DiffSHAs struct {
	PlatformHeadSHA string
	PlatformBaseSHA string
	DiffHeadSHA     string
	DiffBaseSHA     string
	MergeBaseSHA    string
	State           string
}

// Stale reports whether the recorded diff SHAs have drifted from the
// platform SHAs. For merged PRs only head drift matters (the base
// never advances after merge). For open/closed PRs both sides can
// advance and invalidate the diff.
func (s *DiffSHAs) Stale() bool {
	if s.State == "merged" {
		return s.DiffHeadSHA != s.PlatformHeadSHA
	}
	return s.DiffHeadSHA != s.PlatformHeadSHA || s.DiffBaseSHA != s.PlatformBaseSHA
}

// GetDiffSHAs returns the diff-related SHAs for a merge request.
func (d *DB) GetDiffSHAs(ctx context.Context, owner, name string, number int) (*DiffSHAs, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	row, err := d.readQueries.GetDiffSHAs(ctx, dbsqlc.GetDiffSHAsParams{
		Owner:  owner,
		Name:   name,
		Number: int64(number),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get diff SHAs: %w", err)
	}
	s := DiffSHAs{
		PlatformHeadSHA: row.PlatformHeadSha,
		PlatformBaseSHA: row.PlatformBaseSha,
		DiffHeadSHA:     row.DiffHeadSha,
		DiffBaseSHA:     row.DiffBaseSha,
		MergeBaseSHA:    row.MergeBaseSha,
		State:           row.State,
	}
	return &s, nil
}

// UpdateMRState sets the final state and timestamps for a MR after it is closed or merged.
func (d *DB) UpdateMRState(
	ctx context.Context,
	repoID int64,
	number int,
	state string,
	mergedAt, closedAt *time.Time,
) error {
	now := time.Now().UTC()
	if err := d.writeQueries.UpdateMRState(ctx, dbsqlc.UpdateMRStateParams{
		State:          state,
		MergedAt:       nullUTCTime(mergedAt),
		ClosedAt:       nullUTCTime(closedAt),
		UpdatedAt:      now,
		LastActivityAt: now,
		RepoID:         repoID,
		Number:         int64(number),
	}); err != nil {
		return fmt.Errorf("update mr state: %w", err)
	}
	return nil
}

// --- Issues ---

// UpsertIssue inserts or updates an issue, returning its internal ID. Before
// writing, all timestamp fields are normalized to UTC so SQL ordering/filtering
// operates on a consistent storage representation.
// On conflict (repo_id, number), stale snapshots are ignored wholesale.
func (d *DB) UpsertIssue(ctx context.Context, issue *Issue) (int64, error) {
	canonicalizeIssueTimestamps(issue)
	if err := d.writeQueries.UpsertIssue(ctx, dbsqlc.UpsertIssueParams{
		RepoID:          issue.RepoID,
		PlatformID:      issue.PlatformID,
		Number:          int64(issue.Number),
		Url:             issue.URL,
		Title:           issue.Title,
		Author:          issue.Author,
		State:           issue.State,
		Body:            issue.Body,
		CommentCount:    int64(issue.CommentCount),
		LabelsJson:      issue.LabelsJSON,
		DetailFetchedAt: nullUTCTime(issue.DetailFetchedAt),
		CreatedAt:       issue.CreatedAt,
		UpdatedAt:       issue.UpdatedAt,
		LastActivityAt:  issue.LastActivityAt,
		ClosedAt:        nullUTCTime(issue.ClosedAt),
	}); err != nil {
		return 0, fmt.Errorf("upsert issue: %w", err)
	}
	id, err := d.readQueries.GetIssueIDByRepoIDAndNumber(ctx, dbsqlc.GetIssueIDByRepoIDAndNumberParams{
		RepoID: issue.RepoID,
		Number: int64(issue.Number),
	})
	if err != nil {
		return 0, fmt.Errorf("get issue id after upsert: %w", err)
	}
	return id, nil
}

// GetIssue returns an issue by repo owner/name and issue number, or nil if not found.
func (d *DB) GetIssue(
	ctx context.Context, owner, name string, number int,
) (*Issue, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	row, err := d.readQueries.GetIssueByOwnerNameNumber(ctx, dbsqlc.GetIssueByOwnerNameNumberParams{
		Owner:  owner,
		Name:   name,
		Number: int64(number),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	issue := issueFromOwnerNameRow(row)
	labelsByIssue, err := d.loadLabelsForIssues(ctx, []int64{issue.ID})
	if err != nil {
		return nil, fmt.Errorf("load issue labels: %w", err)
	}
	issue.Labels = labelsByIssue[issue.ID]
	return &issue, nil
}

// GetIssueByRepoIDAndNumber returns an issue by repo ID and number.
func (d *DB) GetIssueByRepoIDAndNumber(ctx context.Context, repoID int64, number int) (*Issue, error) {
	row, err := d.readQueries.GetIssueByRepoIDAndNumber(ctx, dbsqlc.GetIssueByRepoIDAndNumberParams{
		RepoID: repoID,
		Number: int64(number),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get issue by repo id: %w", err)
	}
	issue := issueFromRepoIDRow(row)
	labelsByIssue, err := d.loadLabelsForIssues(ctx, []int64{issue.ID})
	if err != nil {
		return nil, fmt.Errorf("load issue labels: %w", err)
	}
	issue.Labels = labelsByIssue[issue.ID]
	return &issue, nil
}

// ListIssues returns issues matching the given options.
func (d *DB) ListIssues(
	ctx context.Context, opts ListIssuesOpts,
) ([]Issue, error) {
	state := opts.State
	if state == "" {
		state = "open"
	}
	var conds []string
	var args []any

	switch state {
	case "all":
		// no state filter
	case "closed":
		conds = append(conds, "i.state = 'closed'")
	default:
		conds = append(conds, "i.state = ?")
		args = append(args, state)
	}

	if opts.RepoOwner != "" && opts.RepoName != "" {
		host, owner, name := canonicalRepoIdentifier(opts.PlatformHost, opts.RepoOwner, opts.RepoName)
		if host != "" {
			conds = append(conds, "r.platform_host = ?")
			args = append(args, host)
		}
		conds = append(conds, "r.owner = ? AND r.name = ?")
		args = append(args, owner, name)
	}
	if opts.Starred {
		conds = append(conds, "s.number IS NOT NULL")
	}
	if opts.Search != "" {
		conds = append(conds, "(i.title LIKE ? OR i.author LIKE ?)")
		like := "%" + opts.Search + "%"
		args = append(args, like, like)
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT i.id, i.repo_id, i.platform_id, i.number, i.url, i.title,
		       i.author, i.state, i.body, i.comment_count, i.labels_json,
		       i.detail_fetched_at,
		       i.created_at, i.updated_at, i.last_activity_at, i.closed_at,
		       (s.number IS NOT NULL) AS starred
		FROM middleman_issues i
		JOIN middleman_repos r ON r.id = i.repo_id
		LEFT JOIN middleman_starred_items s
		    ON s.item_type = 'issue' AND s.repo_id = i.repo_id AND s.number = i.number
		%s
		ORDER BY i.last_activity_at DESC`, where)

	rows, err := d.ro.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer rows.Close()

	var issues []Issue
	var issueIDs []int64
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(
			&issue.ID, &issue.RepoID, &issue.PlatformID, &issue.Number,
			&issue.URL, &issue.Title, &issue.Author, &issue.State,
			&issue.Body, &issue.CommentCount, &issue.LabelsJSON,
			&issue.DetailFetchedAt,
			&issue.CreatedAt, &issue.UpdatedAt, &issue.LastActivityAt,
			&issue.ClosedAt, &issue.Starred,
		); err != nil {
			return nil, fmt.Errorf("scan issue: %w", err)
		}
		issues = append(issues, issue)
		issueIDs = append(issueIDs, issue.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	labelsByIssue, err := d.loadLabelsForIssues(ctx, issueIDs)
	if err != nil {
		return nil, fmt.Errorf("load issue labels: %w", err)
	}
	for i := range issues {
		issues[i].Labels = labelsByIssue[issues[i].ID]
	}
	return issues, nil
}

// GetIssueIDByRepoAndNumber returns the internal issue ID for a given repo+number.
func (d *DB) GetIssueIDByRepoAndNumber(
	ctx context.Context, owner, name string, number int,
) (int64, error) {
	_, owner, name = canonicalRepoIdentifier("", owner, name)
	id, err := d.readQueries.GetIssueIDByOwnerNameNumber(ctx, dbsqlc.GetIssueIDByOwnerNameNumberParams{
		Owner:  owner,
		Name:   name,
		Number: int64(number),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("issue %s/%s#%d not found", owner, name, number)
	}
	if err != nil {
		return 0, fmt.Errorf("get issue id by repo and number: %w", err)
	}
	return id, nil
}

// ResolveItemNumber checks whether the given number in a repo is a MR
// or issue. Returns the item type ("pr" or "issue") and whether it was
// found. MRs take precedence if both somehow exist.
func (d *DB) ResolveItemNumber(
	ctx context.Context, repoID int64, number int,
) (itemType string, found bool, err error) {
	_, err = d.readQueries.GetMergeRequestIDByRepoIDAndNumber(ctx, dbsqlc.GetMergeRequestIDByRepoIDAndNumberParams{
		RepoID: repoID,
		Number: int64(number),
	})
	if err == nil {
		return "pr", true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("check merge_requests: %w", err)
	}

	_, err = d.readQueries.GetIssueIDByRepoIDAndNumber(ctx, dbsqlc.GetIssueIDByRepoIDAndNumberParams{
		RepoID: repoID,
		Number: int64(number),
	})
	if err == nil {
		return "issue", true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, fmt.Errorf("check issues: %w", err)
	}

	return "", false, nil
}

// UpdateIssueState sets the state and closed_at for an issue.
func (d *DB) UpdateIssueState(
	ctx context.Context,
	repoID int64,
	number int,
	state string,
	closedAt *time.Time,
) error {
	now := time.Now().UTC()
	if err := d.writeQueries.UpdateIssueState(ctx, dbsqlc.UpdateIssueStateParams{
		State:          state,
		ClosedAt:       nullUTCTime(closedAt),
		UpdatedAt:      now,
		LastActivityAt: now,
		RepoID:         repoID,
		Number:         int64(number),
	}); err != nil {
		return fmt.Errorf("update issue state: %w", err)
	}
	return nil
}

// GetPreviouslyOpenIssueNumbers returns issue numbers that are open in the DB
// but not in the stillOpen set.
func (d *DB) GetPreviouslyOpenIssueNumbers(
	ctx context.Context,
	repoID int64,
	stillOpen map[int]bool,
) ([]int, error) {
	rows, err := d.readQueries.ListOpenIssueNumbersByRepo(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("get previously open issues: %w", err)
	}

	var closed []int
	for _, row := range rows {
		n := int(row)
		if !stillOpen[n] {
			closed = append(closed, n)
		}
	}
	return closed, nil
}

// --- Detail Fetch Tracking ---

// UpdateMRDetailFetched marks a merge request as having had its
// detail fetched and records whether CI had pending checks.
func (d *DB) UpdateMRDetailFetched(
	ctx context.Context,
	platformHost, repoOwner, repoName string,
	number int, ciHadPending bool,
) error {
	platformHost, repoOwner, repoName = canonicalRepoIdentifier(
		platformHost, repoOwner, repoName,
	)
	if err := d.writeQueries.UpdateMRDetailFetched(ctx, dbsqlc.UpdateMRDetailFetchedParams{
		CiHadPending: boolInt64(ciHadPending),
		PlatformHost: platformHost,
		Owner:        repoOwner,
		Name:         repoName,
		Number:       int64(number),
	}); err != nil {
		return fmt.Errorf("update mr detail fetched: %w", err)
	}
	return nil
}

// UpdateIssueDetailFetched marks an issue as having had its
// detail fetched.
func (d *DB) UpdateIssueDetailFetched(
	ctx context.Context,
	platformHost, repoOwner, repoName string, number int,
) error {
	platformHost, repoOwner, repoName = canonicalRepoIdentifier(
		platformHost, repoOwner, repoName,
	)
	if err := d.writeQueries.UpdateIssueDetailFetched(ctx, dbsqlc.UpdateIssueDetailFetchedParams{
		PlatformHost: platformHost,
		Owner:        repoOwner,
		Name:         repoName,
		Number:       int64(number),
	}); err != nil {
		return fmt.Errorf("update issue detail fetched: %w", err)
	}
	return nil
}

// UpdateBackfillCursor updates the backfill pagination state for a repo.
func (d *DB) UpdateBackfillCursor(
	ctx context.Context, repoID int64,
	prPage int, prComplete bool, prCompletedAt *time.Time,
	issuePage int, issueComplete bool,
	issueCompletedAt *time.Time,
) error {
	repo := &Repo{
		BackfillPRCompletedAt:    prCompletedAt,
		BackfillIssueCompletedAt: issueCompletedAt,
	}
	canonicalizeRepoTimestamps(repo)
	err := d.writeQueries.UpdateBackfillCursor(ctx, dbsqlc.UpdateBackfillCursorParams{
		BackfillPrPage:           int64(prPage),
		BackfillPrComplete:       boolInt64(prComplete),
		BackfillPrCompletedAt:    nullUTCTime(repo.BackfillPRCompletedAt),
		BackfillIssuePage:        int64(issuePage),
		BackfillIssueComplete:    boolInt64(issueComplete),
		BackfillIssueCompletedAt: nullUTCTime(repo.BackfillIssueCompletedAt),
		ID:                       repoID,
	})
	if err != nil {
		return fmt.Errorf("update backfill cursor: %w", err)
	}
	return nil
}

// --- Issue Events ---

// UpsertIssueEvents bulk-inserts issue events after normalizing CreatedAt to
// UTC. Duplicate keys refresh mutable fields so edited events and older local
// timestamp encodings are repaired during normal sync.
func (d *DB) UpsertIssueEvents(ctx context.Context, events []IssueEvent) error {
	if len(events) == 0 {
		return nil
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		q := d.writeQueries.WithTx(tx)
		for i := range events {
			e := &events[i]
			canonicalizeIssueEventTimestamps(e)
			if err := q.UpsertIssueEvent(ctx, dbsqlc.UpsertIssueEventParams{
				IssueID:      e.IssueID,
				PlatformID:   nullInt64FromPtr(e.PlatformID),
				EventType:    e.EventType,
				Author:       e.Author,
				Summary:      e.Summary,
				Body:         e.Body,
				MetadataJson: e.MetadataJSON,
				CreatedAt:    e.CreatedAt,
				DedupeKey:    e.DedupeKey,
			}); err != nil {
				return fmt.Errorf("insert issue event (dedupe_key=%s): %w", e.DedupeKey, err)
			}
		}
		return nil
	})
}

func (d *DB) IssueCommentEventExists(
	ctx context.Context,
	issueID int64,
	platformID int64,
) (bool, error) {
	exists, err := d.readQueries.IssueCommentEventExists(ctx, dbsqlc.IssueCommentEventExistsParams{
		IssueID:    issueID,
		PlatformID: sql.NullInt64{Int64: platformID, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("check issue comment event exists: %w", err)
	}
	return exists, nil
}

// DeleteMissingIssueCommentEvents removes issue_comment rows for an issue whose
// dedupe keys are absent from the latest GitHub comment list.
func (d *DB) DeleteMissingIssueCommentEvents(
	ctx context.Context,
	issueID int64,
	dedupeKeys []string,
) error {
	var err error
	if len(dedupeKeys) > 0 {
		err = d.writeQueries.DeleteMissingIssueCommentEvents(ctx, dbsqlc.DeleteMissingIssueCommentEventsParams{
			IssueID:    issueID,
			DedupeKeys: dedupeKeys,
		})
	} else {
		err = d.writeQueries.DeleteAllIssueCommentEvents(ctx, issueID)
	}
	if err != nil {
		return fmt.Errorf("delete missing issue comment events: %w", err)
	}
	return nil
}

// ListIssueEvents returns all events for an issue ordered by created_at DESC.
func (d *DB) ListIssueEvents(ctx context.Context, issueID int64) ([]IssueEvent, error) {
	rows, err := d.readQueries.ListIssueEvents(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("list issue events: %w", err)
	}

	var events []IssueEvent
	for _, row := range rows {
		t, err := parseDBTime(row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf(
				"parse issue event created_at %q: %w",
				row.CreatedAt, err)
		}
		events = append(events, IssueEvent{
			ID:           row.ID,
			IssueID:      row.IssueID,
			PlatformID:   ptrFromNullInt64(row.PlatformID),
			EventType:    row.EventType,
			Author:       row.Author,
			Summary:      row.Summary,
			Body:         row.Body,
			MetadataJSON: row.MetadataJson,
			CreatedAt:    t,
			DedupeKey:    row.DedupeKey,
		})
	}
	return events, nil
}

// ListCommentAutocompleteUsers returns repo-scoped username suggestions for comment mentions.
func (d *DB) ListCommentAutocompleteUsers(
	ctx context.Context,
	platformHost, owner, name, query string,
	limit int,
) ([]string, error) {
	platformHost, owner, name = canonicalRepoIdentifier(platformHost, owner, name)
	if limit <= 0 {
		limit = 10
	}
	query = strings.TrimSpace(query)
	containsQuery := "%" + strings.ToLower(query) + "%"
	prefixQuery := strings.ToLower(query) + "%"

	rows, err := d.ro.QueryContext(ctx, `
		WITH repo AS (
			SELECT id
			FROM middleman_repos
			WHERE platform_host = ? AND owner = ? AND name = ?
		), candidates AS (
			SELECT mr.author AS login, mr.last_activity_at AS last_seen
			FROM middleman_merge_requests mr
			WHERE mr.repo_id = (SELECT id FROM repo)
			UNION ALL
			SELECT i.author AS login, i.last_activity_at AS last_seen
			FROM middleman_issues i
			WHERE i.repo_id = (SELECT id FROM repo)
			UNION ALL
			SELECT e.author AS login, e.created_at AS last_seen
			FROM middleman_mr_events e
			JOIN middleman_merge_requests mr ON mr.id = e.merge_request_id
			WHERE mr.repo_id = (SELECT id FROM repo)
			UNION ALL
			SELECT e.author AS login, e.created_at AS last_seen
			FROM middleman_issue_events e
			JOIN middleman_issues i ON i.id = e.issue_id
			WHERE i.repo_id = (SELECT id FROM repo)
		), ranked AS (
			SELECT login, MAX(last_seen) AS last_seen
			FROM candidates
			WHERE login <> ''
			  AND (? = '' OR LOWER(login) LIKE ?)
			GROUP BY login
		)
		SELECT login
		FROM ranked
		ORDER BY
			CASE WHEN ? <> '' AND LOWER(login) LIKE ? THEN 0 ELSE 1 END,
			last_seen DESC,
			login ASC
		LIMIT ?`,
		platformHost, owner, name,
		query, containsQuery,
		query, prefixQuery,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list comment autocomplete users: %w", err)
	}
	defer rows.Close()

	users := make([]string, 0, limit)
	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			return nil, fmt.Errorf("scan comment autocomplete user: %w", err)
		}
		users = append(users, login)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comment autocomplete users: %w", err)
	}
	return users, nil
}

// ListCommentAutocompleteReferences returns repo-scoped # suggestions for pulls and issues.
func (d *DB) ListCommentAutocompleteReferences(
	ctx context.Context,
	platformHost, owner, name, query string,
	limit int,
) ([]CommentAutocompleteReference, error) {
	platformHost, owner, name = canonicalRepoIdentifier(platformHost, owner, name)
	if limit <= 0 {
		limit = 10
	}
	query = strings.TrimSpace(query)
	titleQuery := "%" + strings.ToLower(query) + "%"
	numberPrefix := query + "%"

	rows, err := d.ro.QueryContext(ctx, `
		WITH repo AS (
			SELECT id
			FROM middleman_repos
			WHERE platform_host = ? AND owner = ? AND name = ?
		), candidates AS (
			SELECT 'pull' AS kind, mr.number, mr.title, mr.state, mr.last_activity_at
			FROM middleman_merge_requests mr
			WHERE mr.repo_id = (SELECT id FROM repo)
			UNION ALL
			SELECT 'issue' AS kind, i.number, i.title, i.state, i.last_activity_at
			FROM middleman_issues i
			WHERE i.repo_id = (SELECT id FROM repo)
		)
		SELECT kind, number, title, state
		FROM candidates
		WHERE ? = ''
		   OR CAST(number AS TEXT) LIKE ?
		   OR LOWER(title) LIKE ?
		ORDER BY
			CASE WHEN ? <> '' AND CAST(number AS TEXT) LIKE ? THEN 0 ELSE 1 END,
			CASE WHEN ? <> '' AND LOWER(title) LIKE ? THEN 0 ELSE 1 END,
			last_activity_at DESC,
			number DESC
		LIMIT ?`,
		platformHost, owner, name,
		query, numberPrefix, titleQuery,
		query, numberPrefix,
		query, titleQuery,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list comment autocomplete references: %w", err)
	}
	defer rows.Close()

	references := make([]CommentAutocompleteReference, 0, limit)
	for rows.Next() {
		var ref CommentAutocompleteReference
		if err := rows.Scan(&ref.Kind, &ref.Number, &ref.Title, &ref.State); err != nil {
			return nil, fmt.Errorf("scan comment autocomplete reference: %w", err)
		}
		references = append(references, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comment autocomplete references: %w", err)
	}
	return references, nil
}

// --- Starring ---

// SetStarred stars an item (MR or issue).
func (d *DB) SetStarred(
	ctx context.Context, itemType string, repoID int64, number int,
) error {
	if err := d.writeQueries.SetStarred(ctx, dbsqlc.SetStarredParams{
		ItemType: itemType,
		RepoID:   repoID,
		Number:   int64(number),
	}); err != nil {
		return fmt.Errorf("set starred: %w", err)
	}
	return nil
}

// UnsetStarred removes a star from an item.
func (d *DB) UnsetStarred(
	ctx context.Context, itemType string, repoID int64, number int,
) error {
	if err := d.writeQueries.UnsetStarred(ctx, dbsqlc.UnsetStarredParams{
		ItemType: itemType,
		RepoID:   repoID,
		Number:   int64(number),
	}); err != nil {
		return fmt.Errorf("unset starred: %w", err)
	}
	return nil
}

// IsStarred checks whether an item is starred.
func (d *DB) IsStarred(
	ctx context.Context, itemType string, repoID int64, number int,
) (bool, error) {
	starred, err := d.readQueries.IsStarred(ctx, dbsqlc.IsStarredParams{
		ItemType: itemType,
		RepoID:   repoID,
		Number:   int64(number),
	})
	if err != nil {
		return false, fmt.Errorf("is starred: %w", err)
	}
	return starred, nil
}

// --- Rate Limits ---

// UpsertRateLimit inserts or updates a rate limit row by (platform_host, api_type).
func (d *DB) UpsertRateLimit(
	platformHost string,
	apiType string,
	requestsHour int,
	hourStart time.Time,
	rateRemaining int,
	rateLimit int,
	rateResetAt *time.Time,
) error {
	hourStart = canonicalUTCTime(hourStart)
	if err := d.writeQueries.UpsertRateLimit(context.Background(), dbsqlc.UpsertRateLimitParams{
		PlatformHost:  platformHost,
		ApiType:       apiType,
		RequestsHour:  int64(requestsHour),
		HourStart:     hourStart,
		RateRemaining: int64(rateRemaining),
		RateLimit:     int64(rateLimit),
		RateResetAt:   nullUTCTime(rateResetAt),
	}); err != nil {
		return fmt.Errorf("upsert rate limit: %w", err)
	}
	return nil
}

// GetRateLimit returns the rate limit row for a (platform_host, api_type) pair,
// or nil,nil if not found.
func (d *DB) GetRateLimit(
	platformHost string,
	apiType string,
) (*RateLimit, error) {
	row, err := d.readQueries.GetRateLimit(context.Background(), dbsqlc.GetRateLimitParams{
		PlatformHost: platformHost,
		ApiType:      apiType,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get rate limit: %w", err)
	}
	return &RateLimit{
		ID:            row.ID,
		PlatformHost:  row.PlatformHost,
		APIType:       row.ApiType,
		RequestsHour:  int(row.RequestsHour),
		HourStart:     row.HourStart.UTC(),
		RateRemaining: int(row.RateRemaining),
		RateLimit:     int(row.RateLimit),
		RateResetAt:   timeFromNull(row.RateResetAt),
		UpdatedAt:     row.UpdatedAt.UTC(),
	}, nil
}

// --- Worktree Links ---

// SetWorktreeLinks replaces all worktree links atomically.
// The existing rows are deleted and the provided links are
// inserted in a single transaction.
func (d *DB) SetWorktreeLinks(
	ctx context.Context, links []WorktreeLink,
) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		q := d.writeQueries.WithTx(tx)
		if err := q.DeleteAllWorktreeLinks(ctx); err != nil {
			return fmt.Errorf("delete worktree links: %w", err)
		}
		for i := range links {
			l := &links[i]
			if err := q.InsertWorktreeLink(ctx, dbsqlc.InsertWorktreeLinkParams{
				MergeRequestID: l.MergeRequestID,
				WorktreeKey:    l.WorktreeKey,
				WorktreePath: sql.NullString{
					String: l.WorktreePath,
					Valid:  true,
				},
				WorktreeBranch: sql.NullString{
					String: l.WorktreeBranch,
					Valid:  true,
				},
				LinkedAt: l.LinkedAt.UTC().Format(time.RFC3339),
			}); err != nil {
				return fmt.Errorf(
					"insert worktree link %s: %w",
					l.WorktreeKey, err,
				)
			}
		}
		return nil
	})
}

// GetWorktreeLinksForMR returns worktree links for a
// specific merge request.
func (d *DB) GetWorktreeLinksForMR(
	ctx context.Context, mergeRequestID int64,
) ([]WorktreeLink, error) {
	rows, err := d.readQueries.ListWorktreeLinksForMR(ctx, mergeRequestID)
	if err != nil {
		return nil, fmt.Errorf(
			"get worktree links for MR: %w", err,
		)
	}
	return worktreeLinksFromSQL(rows)
}

// GetWorktreeLinksForMRs returns worktree links for the
// given merge request IDs. IDs are batched to stay within
// SQLite's bind-parameter limit.
func (d *DB) GetWorktreeLinksForMRs(
	ctx context.Context, mrIDs []int64,
) ([]WorktreeLink, error) {
	if len(mrIDs) == 0 {
		return nil, nil
	}
	const batchSize = 500
	var all []WorktreeLink
	for start := 0; start < len(mrIDs); start += batchSize {
		end := min(start+batchSize, len(mrIDs))
		batch := mrIDs[start:end]
		rows, err := d.readQueries.ListWorktreeLinksForMRIDs(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf(
				"get worktree links for MRs: %w", err,
			)
		}
		links, err := worktreeLinksFromSQL(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, links...)
	}
	return all, nil
}

// GetAllWorktreeLinks returns all worktree links ordered
// by linked_at DESC.
func (d *DB) GetAllWorktreeLinks(
	ctx context.Context,
) ([]WorktreeLink, error) {
	rows, err := d.readQueries.ListAllWorktreeLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"get all worktree links: %w", err,
		)
	}
	return worktreeLinksFromSQL(rows)
}

// GetRepoByHostOwnerName returns the repo for the given
// host/owner/name triple, or nil if not found.
func (d *DB) GetRepoByHostOwnerName(
	ctx context.Context,
	host, owner, name string,
) (*Repo, error) {
	host, owner, name = canonicalRepoIdentifier(host, owner, name)
	row, err := d.readQueries.GetRepoByHostOwnerName(ctx, dbsqlc.GetRepoByHostOwnerNameParams{
		PlatformHost: host,
		Owner:        owner,
		Name:         name,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"get repo by host/owner/name: %w", err,
		)
	}
	r := repoFromHostOwnerNameRow(row)
	return &r, nil
}

// --- Workspaces ---

func nullStringFromPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func ptrFromNullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

func nullInt64FromIntPtr(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func intPtrFromNullInt64(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

func boolPtrFromNullInt64(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Int64 != 0
	return &b
}

func stringPtrFromSQLValue(v any) *string {
	switch value := v.(type) {
	case nil:
		return nil
	case string:
		return &value
	case []byte:
		s := string(value)
		return &s
	case sql.NullString:
		return ptrFromNullString(value)
	default:
		s := fmt.Sprint(value)
		return &s
	}
}

func workspaceFromSQLFields(
	id, platformHost, repoOwner, repoName string,
	itemType string,
	itemNumber int64,
	associatedPRNumber sql.NullInt64,
	gitHeadRef string,
	mrHeadRepo sql.NullString,
	workspaceBranch, worktreePath, tmuxSession, status string,
	errorMessage sql.NullString,
	createdAt time.Time,
) Workspace {
	return Workspace{
		ID:                 id,
		PlatformHost:       platformHost,
		RepoOwner:          repoOwner,
		RepoName:           repoName,
		ItemType:           itemType,
		ItemNumber:         int(itemNumber),
		AssociatedPRNumber: intPtrFromNullInt64(associatedPRNumber),
		GitHeadRef:         gitHeadRef,
		MRHeadRepo:         ptrFromNullString(mrHeadRepo),
		WorkspaceBranch:    workspaceBranch,
		WorktreePath:       worktreePath,
		TmuxSession:        tmuxSession,
		Status:             status,
		ErrorMessage:       ptrFromNullString(errorMessage),
		CreatedAt:          createdAt.UTC(),
	}
}

func workspaceFromGetRow(row dbsqlc.GetWorkspaceRow) Workspace {
	return workspaceFromSQLFields(
		row.ID, row.PlatformHost, row.RepoOwner, row.RepoName,
		row.ItemType, row.ItemNumber, row.AssociatedPrNumber,
		row.GitHeadRef, row.MrHeadRepo, row.WorkspaceBranch,
		row.WorktreePath, row.TmuxSession, row.Status,
		row.ErrorMessage, row.CreatedAt,
	)
}

func workspaceFromGetByItemRow(row dbsqlc.GetWorkspaceByItemRow) Workspace {
	return workspaceFromSQLFields(
		row.ID, row.PlatformHost, row.RepoOwner, row.RepoName,
		row.ItemType, row.ItemNumber, row.AssociatedPrNumber,
		row.GitHeadRef, row.MrHeadRepo, row.WorkspaceBranch,
		row.WorktreePath, row.TmuxSession, row.Status,
		row.ErrorMessage, row.CreatedAt,
	)
}

func workspaceFromListRow(row dbsqlc.ListWorkspacesRow) Workspace {
	return workspaceFromSQLFields(
		row.ID, row.PlatformHost, row.RepoOwner, row.RepoName,
		row.ItemType, row.ItemNumber, row.AssociatedPrNumber,
		row.GitHeadRef, row.MrHeadRepo, row.WorkspaceBranch,
		row.WorktreePath, row.TmuxSession, row.Status,
		row.ErrorMessage, row.CreatedAt,
	)
}

func workspaceInsertParams(ws *Workspace) dbsqlc.InsertWorkspaceParams {
	return dbsqlc.InsertWorkspaceParams{
		ID:                 ws.ID,
		PlatformHost:       ws.PlatformHost,
		RepoOwner:          ws.RepoOwner,
		RepoName:           ws.RepoName,
		ItemType:           ws.ItemType,
		ItemNumber:         int64(ws.ItemNumber),
		AssociatedPrNumber: nullInt64FromIntPtr(ws.AssociatedPRNumber),
		GitHeadRef:         ws.GitHeadRef,
		MrHeadRepo:         nullStringFromPtr(ws.MRHeadRepo),
		WorkspaceBranch:    ws.WorkspaceBranch,
		WorktreePath:       ws.WorktreePath,
		TmuxSession:        ws.TmuxSession,
		Status:             ws.Status,
		ErrorMessage:       nullStringFromPtr(ws.ErrorMessage),
	}
}

// InsertWorkspace inserts a new workspace row.
func (d *DB) InsertWorkspace(
	ctx context.Context, ws *Workspace,
) error {
	ws.PlatformHost, ws.RepoOwner, ws.RepoName = canonicalRepoIdentifier(
		ws.PlatformHost, ws.RepoOwner, ws.RepoName,
	)
	if err := d.writeQueries.InsertWorkspace(ctx, workspaceInsertParams(ws)); err != nil {
		return fmt.Errorf("insert workspace: %w", err)
	}
	return nil
}

// GetWorkspace returns a workspace by ID, or nil if not found.
func (d *DB) GetWorkspace(
	ctx context.Context, id string,
) (*Workspace, error) {
	row, err := d.readQueries.GetWorkspace(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	ws := workspaceFromGetRow(row)
	return &ws, nil
}

// GetWorkspaceByMR returns the workspace for a specific MR,
// or nil if not found.
func (d *DB) GetWorkspaceByMR(
	ctx context.Context,
	platformHost, owner, name string,
	mrNumber int,
) (*Workspace, error) {
	platformHost, owner, name = canonicalRepoIdentifier(platformHost, owner, name)
	row, err := d.readQueries.GetWorkspaceByItem(ctx, dbsqlc.GetWorkspaceByItemParams{
		PlatformHost: platformHost,
		RepoOwner:    owner,
		RepoName:     name,
		ItemType:     WorkspaceItemTypePullRequest,
		ItemNumber:   int64(mrNumber),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace by MR: %w", err)
	}
	ws := workspaceFromGetByItemRow(row)
	return &ws, nil
}

// GetWorkspaceByIssue returns the workspace for a specific issue,
// or nil if not found.
func (d *DB) GetWorkspaceByIssue(
	ctx context.Context,
	platformHost, owner, name string,
	issueNumber int,
) (*Workspace, error) {
	platformHost, owner, name = canonicalRepoIdentifier(platformHost, owner, name)
	row, err := d.readQueries.GetWorkspaceByItem(ctx, dbsqlc.GetWorkspaceByItemParams{
		PlatformHost: platformHost,
		RepoOwner:    owner,
		RepoName:     name,
		ItemType:     WorkspaceItemTypeIssue,
		ItemNumber:   int64(issueNumber),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace by issue: %w", err)
	}
	ws := workspaceFromGetByItemRow(row)
	return &ws, nil
}

// ListWorkspaces returns all workspaces ordered by
// created_at DESC.
func (d *DB) ListWorkspaces(
	ctx context.Context,
) ([]Workspace, error) {
	rows, err := d.readQueries.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	out := make([]Workspace, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceFromListRow(row))
	}
	return out, nil
}

// UpdateWorkspaceStatus sets the status and optional error
// message for a workspace.
func (d *DB) UpdateWorkspaceStatus(
	ctx context.Context,
	id, status string,
	errMsg *string,
) error {
	err := d.writeQueries.UpdateWorkspaceStatus(ctx, dbsqlc.UpdateWorkspaceStatusParams{
		Status:       status,
		ErrorMessage: nullStringFromPtr(errMsg),
		ID:           id,
	})
	if err != nil {
		return fmt.Errorf("update workspace status: %w", err)
	}
	return nil
}

// UpdateWorkspaceBranch stores the exact branch middleman created
// for a workspace. Empty means setup reused a pre-existing local
// branch and therefore does not own it.
func (d *DB) UpdateWorkspaceBranch(
	ctx context.Context, id, branch string,
) error {
	err := d.writeQueries.UpdateWorkspaceBranch(ctx, dbsqlc.UpdateWorkspaceBranchParams{
		WorkspaceBranch: branch,
		ID:              id,
	})
	if err != nil {
		return fmt.Errorf("update workspace branch: %w", err)
	}
	return nil
}

// StartWorkspaceRetry atomically transitions an errored workspace
// into setup state. It returns false when the workspace exists but
// was not in error status at the instant of the update.
func (d *DB) StartWorkspaceRetry(
	ctx context.Context, id string,
) (bool, error) {
	affected, err := d.writeQueries.StartWorkspaceRetry(ctx, id)
	if err != nil {
		return false, fmt.Errorf("start workspace retry: %w", err)
	}
	return affected == 1, nil
}

// SetWorkspaceAssociatedPRNumberIfNull stores a workspace's first detected
// associated PR without overwriting an existing association.
func (d *DB) SetWorkspaceAssociatedPRNumberIfNull(
	ctx context.Context, id string, prNumber int,
) (bool, error) {
	rows, err := d.writeQueries.SetWorkspaceAssociatedPRNumberIfNull(
		ctx,
		dbsqlc.SetWorkspaceAssociatedPRNumberIfNullParams{
			AssociatedPrNumber: sql.NullInt64{
				Int64: int64(prNumber),
				Valid: true,
			},
			ID: id,
		},
	)
	if err != nil {
		return false, fmt.Errorf(
			"set workspace associated PR number: %w", err,
		)
	}
	return rows > 0, nil
}

// InsertWorkspaceSetupEvent appends an audit event for workspace
// setup activity.
func (d *DB) InsertWorkspaceSetupEvent(
	ctx context.Context, event *WorkspaceSetupEvent,
) error {
	err := d.writeQueries.InsertWorkspaceSetupEvent(ctx, dbsqlc.InsertWorkspaceSetupEventParams{
		WorkspaceID: event.WorkspaceID,
		Stage:       event.Stage,
		Outcome:     event.Outcome,
		Message:     event.Message,
	})
	if err != nil {
		return fmt.Errorf(
			"insert workspace setup event: %w", err,
		)
	}
	return nil
}

// ListWorkspaceSetupEvents returns the audit trail for a single
// workspace setup, ordered by insertion.
func (d *DB) ListWorkspaceSetupEvents(
	ctx context.Context, workspaceID string,
) ([]WorkspaceSetupEvent, error) {
	rows, err := d.readQueries.ListWorkspaceSetupEvents(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf(
			"list workspace setup events: %w", err,
		)
	}
	out := make([]WorkspaceSetupEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, WorkspaceSetupEvent{
			ID:          row.ID,
			WorkspaceID: row.WorkspaceID,
			Stage:       row.Stage,
			Outcome:     row.Outcome,
			Message:     row.Message,
			CreatedAt:   row.CreatedAt.UTC(),
		})
	}
	return out, nil
}

func workspaceTmuxSessionFromSQL(
	row dbsqlc.MiddlemanWorkspaceTmuxSession,
) WorkspaceTmuxSession {
	return WorkspaceTmuxSession{
		WorkspaceID: row.WorkspaceID,
		SessionName: row.SessionName,
		TargetKey:   row.TargetKey,
		CreatedAt:   row.CreatedAt.UTC(),
	}
}

func workspaceTmuxSessionsFromSQL(
	rows []dbsqlc.MiddlemanWorkspaceTmuxSession,
) []WorkspaceTmuxSession {
	out := make([]WorkspaceTmuxSession, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceTmuxSessionFromSQL(row))
	}
	return out
}

func workspaceSummaryFromSQLFields(
	id, platformHost, repoOwner, repoName string,
	itemType string,
	itemNumber int64,
	associatedPRNumber sql.NullInt64,
	gitHeadRef string,
	mrHeadRepo sql.NullString,
	workspaceBranch, worktreePath, tmuxSession, status string,
	errorMessage sql.NullString,
	createdAt time.Time,
	mrTitle, mrState any,
	mrIsDraft sql.NullInt64,
	mrCIStatus, mrReviewDecision sql.NullString,
	mrAdditions, mrDeletions sql.NullInt64,
) WorkspaceSummary {
	return WorkspaceSummary{
		Workspace: workspaceFromSQLFields(
			id, platformHost, repoOwner, repoName,
			itemType, itemNumber, associatedPRNumber,
			gitHeadRef, mrHeadRepo, workspaceBranch,
			worktreePath, tmuxSession, status,
			errorMessage, createdAt,
		),
		MRTitle:          stringPtrFromSQLValue(mrTitle),
		MRState:          stringPtrFromSQLValue(mrState),
		MRIsDraft:        boolPtrFromNullInt64(mrIsDraft),
		MRCIStatus:       ptrFromNullString(mrCIStatus),
		MRReviewDecision: ptrFromNullString(mrReviewDecision),
		MRAdditions:      intPtrFromNullInt64(mrAdditions),
		MRDeletions:      intPtrFromNullInt64(mrDeletions),
	}
}

func workspaceSummaryFromListRow(
	row dbsqlc.ListWorkspaceSummariesRow,
) WorkspaceSummary {
	return workspaceSummaryFromSQLFields(
		row.ID, row.PlatformHost, row.RepoOwner, row.RepoName,
		row.ItemType, row.ItemNumber, row.AssociatedPrNumber,
		row.GitHeadRef, row.MrHeadRepo, row.WorkspaceBranch,
		row.WorktreePath, row.TmuxSession, row.Status,
		row.ErrorMessage, row.CreatedAt, row.MrTitle, row.MrState,
		row.MrIsDraft, row.MrCiStatus, row.MrReviewDecision,
		row.MrAdditions, row.MrDeletions,
	)
}

func workspaceSummaryFromGetRow(
	row dbsqlc.GetWorkspaceSummaryRow,
) WorkspaceSummary {
	return workspaceSummaryFromSQLFields(
		row.ID, row.PlatformHost, row.RepoOwner, row.RepoName,
		row.ItemType, row.ItemNumber, row.AssociatedPrNumber,
		row.GitHeadRef, row.MrHeadRepo, row.WorkspaceBranch,
		row.WorktreePath, row.TmuxSession, row.Status,
		row.ErrorMessage, row.CreatedAt, row.MrTitle, row.MrState,
		row.MrIsDraft, row.MrCiStatus, row.MrReviewDecision,
		row.MrAdditions, row.MrDeletions,
	)
}

func worktreeLinkFromSQL(row dbsqlc.MiddlemanMrWorktreeLink) (WorktreeLink, error) {
	linkedAt, err := time.Parse(time.RFC3339, row.LinkedAt)
	if err != nil {
		return WorktreeLink{}, fmt.Errorf(
			"parse linked_at %q: %w", row.LinkedAt, err,
		)
	}
	return WorktreeLink{
		ID:             row.ID,
		MergeRequestID: row.MergeRequestID,
		WorktreeKey:    row.WorktreeKey,
		WorktreePath:   row.WorktreePath.String,
		WorktreeBranch: row.WorktreeBranch.String,
		LinkedAt:       linkedAt,
	}, nil
}

func worktreeLinksFromSQL(
	rows []dbsqlc.MiddlemanMrWorktreeLink,
) ([]WorktreeLink, error) {
	out := make([]WorktreeLink, 0, len(rows))
	for _, row := range rows {
		link, err := worktreeLinkFromSQL(row)
		if err != nil {
			return nil, fmt.Errorf("scan worktree link: %w", err)
		}
		out = append(out, link)
	}
	return out, nil
}

// UpsertWorkspaceTmuxSession records a tmux session owned by a
// runtime launch inside a workspace. Re-launching the same target
// keeps the original row fresh without duplicating it.
func (d *DB) UpsertWorkspaceTmuxSession(
	ctx context.Context,
	session *WorkspaceTmuxSession,
) error {
	createdAt := canonicalUTCTime(session.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	err := d.writeQueries.UpsertWorkspaceTmuxSession(ctx, dbsqlc.UpsertWorkspaceTmuxSessionParams{
		WorkspaceID: session.WorkspaceID,
		SessionName: session.SessionName,
		TargetKey:   session.TargetKey,
		CreatedAt:   createdAt,
	})
	if err != nil {
		return fmt.Errorf("upsert workspace tmux session: %w", err)
	}
	return nil
}

// ListWorkspaceTmuxSessions returns stored runtime tmux sessions for
// a workspace ordered by target key and creation time.
func (d *DB) ListWorkspaceTmuxSessions(
	ctx context.Context,
	workspaceID string,
) ([]WorkspaceTmuxSession, error) {
	rows, err := d.readQueries.ListWorkspaceTmuxSessions(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace tmux sessions: %w", err)
	}
	return workspaceTmuxSessionsFromSQL(rows), nil
}

// ListAllWorkspaceTmuxSessions returns every stored runtime tmux
// session. It is used by startup cleanup to distinguish live owned
// sessions from stale managed sessions left behind by crashes.
func (d *DB) ListAllWorkspaceTmuxSessions(
	ctx context.Context,
) ([]WorkspaceTmuxSession, error) {
	rows, err := d.readQueries.ListAllWorkspaceTmuxSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all workspace tmux sessions: %w", err)
	}
	return workspaceTmuxSessionsFromSQL(rows), nil
}

// DeleteWorkspaceTmuxSession removes one stored runtime tmux session.
func (d *DB) DeleteWorkspaceTmuxSession(
	ctx context.Context,
	workspaceID string,
	sessionName string,
) error {
	err := d.writeQueries.DeleteWorkspaceTmuxSession(ctx, dbsqlc.DeleteWorkspaceTmuxSessionParams{
		WorkspaceID: workspaceID,
		SessionName: sessionName,
	})
	if err != nil {
		return fmt.Errorf("delete workspace tmux session: %w", err)
	}
	return nil
}

// DeleteWorkspaceTmuxSessionCreatedAt removes one stored runtime tmux session
// only if it still belongs to the same runtime session generation.
func (d *DB) DeleteWorkspaceTmuxSessionCreatedAt(
	ctx context.Context,
	workspaceID string,
	sessionName string,
	createdAt time.Time,
) (bool, error) {
	rows, err := d.writeQueries.DeleteWorkspaceTmuxSessionCreatedAt(
		ctx,
		dbsqlc.DeleteWorkspaceTmuxSessionCreatedAtParams{
			WorkspaceID: workspaceID,
			SessionName: sessionName,
			CreatedAt:   canonicalUTCTime(createdAt),
		},
	)
	if err != nil {
		return false, fmt.Errorf("delete workspace tmux session: %w", err)
	}
	return rows > 0, nil
}

// DeleteWorkspaceTmuxSessions removes every stored runtime tmux
// session for a workspace.
func (d *DB) DeleteWorkspaceTmuxSessions(
	ctx context.Context,
	workspaceID string,
) error {
	err := d.writeQueries.DeleteWorkspaceTmuxSessions(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("delete workspace tmux sessions: %w", err)
	}
	return nil
}

// DeleteWorkspace removes a workspace by ID.
func (d *DB) DeleteWorkspace(
	ctx context.Context, id string,
) error {
	if err := d.writeQueries.DeleteWorkspace(ctx, id); err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return nil
}

// ListWorkspaceSummaries returns all workspaces with joined MR
// metadata, ordered by created_at DESC.
func (d *DB) ListWorkspaceSummaries(
	ctx context.Context,
) ([]WorkspaceSummary, error) {
	rows, err := d.readQueries.ListWorkspaceSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"list workspace summaries: %w", err,
		)
	}
	out := make([]WorkspaceSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceSummaryFromListRow(row))
	}
	return out, nil
}

// GetWorkspaceSummary returns a single workspace with joined
// MR metadata, or nil if not found.
func (d *DB) GetWorkspaceSummary(
	ctx context.Context, id string,
) (*WorkspaceSummary, error) {
	row, err := d.readQueries.GetWorkspaceSummary(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"get workspace summary: %w", err,
		)
	}
	s := workspaceSummaryFromGetRow(row)
	return &s, nil
}
