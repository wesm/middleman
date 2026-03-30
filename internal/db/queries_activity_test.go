package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func insertTestIssue(
	t *testing.T, d *DB,
	repoID int64, number int, title string, activity time.Time,
) int64 {
	t.Helper()
	issue := &Issue{
		RepoID:         repoID,
		GitHubID:       repoID*10000 + int64(number),
		Number:         number,
		URL:            "https://github.com/example/repo/issues/" + title,
		Title:          title,
		Author:         "author",
		State:          "open",
		CreatedAt:      activity,
		UpdatedAt:      activity,
		LastActivityAt: activity,
	}
	id, err := d.UpsertIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("UpsertIssue %d: %v", number, err)
	}
	return id
}

func TestListActivity(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	base := baseTime()

	repoA := insertTestRepo(t, d, "alice", "alpha")
	repoB := insertTestRepo(t, d, "bob", "beta")

	prID1 := insertTestPR(t, d, repoA, 1, "Fix bug", base)
	prID2 := insertTestPR(
		t, d, repoB, 2, "Add feature", base.Add(1*time.Minute))
	issueID1 := insertTestIssue(
		t, d, repoA, 10, "Crash on startup", base.Add(2*time.Minute))

	err := d.UpsertPREvents(ctx, []PREvent{
		{PRID: prID1, EventType: "issue_comment", Author: "carol",
			Body:      "Looks good to me",
			CreatedAt: base.Add(3 * time.Minute),
			DedupeKey: "comment-1"},
		{PRID: prID2, EventType: "review", Author: "dave",
			Summary:   "APPROVED",
			CreatedAt: base.Add(4 * time.Minute),
			DedupeKey: "review-1"},
		{PRID: prID1, EventType: "commit", Author: "alice",
			Summary: "abc123", Body: "fix: handle nil",
			CreatedAt: base.Add(5 * time.Minute),
			DedupeKey: "commit-abc123"},
		{PRID: prID1, EventType: "review_comment", Author: "eve",
			Body:      "nit: rename var",
			CreatedAt: base.Add(6 * time.Minute),
			DedupeKey: "review_comment-1"},
	})
	if err != nil {
		t.Fatalf("UpsertPREvents: %v", err)
	}

	err = d.UpsertIssueEvents(ctx, []IssueEvent{
		{IssueID: issueID1, EventType: "issue_comment", Author: "frank",
			Body:      "Can reproduce on macOS",
			CreatedAt: base.Add(7 * time.Minute),
			DedupeKey: "icomment-1"},
	})
	if err != nil {
		t.Fatalf("UpsertIssueEvents: %v", err)
	}

	t.Run("unfiltered returns all types in desc order", func(t *testing.T) {
		items, err := d.ListActivity(
			ctx, ListActivityOpts{Limit: 50})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		// Expected order (newest first):
		// 1. issue comment (base+7m) - review_comment excluded
		// 2. commit (base+5m)
		// 3. review (base+4m)
		// 4. PR comment (base+3m)
		// 5. new issue (base+2m)
		// 6. new PR bob/beta#2 (base+1m)
		// 7. new PR alice/alpha#1 (base)
		if len(items) != 7 {
			t.Fatalf("expected 7 items, got %d", len(items))
		}
		if items[0].ActivityType != "comment" ||
			items[0].ItemType != "issue" {
			t.Errorf("items[0]: got type=%s item=%s, want comment/issue",
				items[0].ActivityType, items[0].ItemType)
		}
		if items[1].ActivityType != "commit" {
			t.Errorf("items[1]: got type=%s, want commit",
				items[1].ActivityType)
		}
		if items[2].ActivityType != "review" {
			t.Errorf("items[2]: got type=%s, want review",
				items[2].ActivityType)
		}
		if items[3].ActivityType != "comment" ||
			items[3].ItemType != "pr" {
			t.Errorf("items[3]: got type=%s item=%s, want comment/pr",
				items[3].ActivityType, items[3].ItemType)
		}
		if items[4].ActivityType != "new_issue" {
			t.Errorf("items[4]: got type=%s, want new_issue",
				items[4].ActivityType)
		}
		if items[5].ActivityType != "new_pr" ||
			items[5].RepoOwner != "bob" {
			t.Errorf("items[5]: got type=%s owner=%s, want new_pr/bob",
				items[5].ActivityType, items[5].RepoOwner)
		}
		if items[6].ActivityType != "new_pr" ||
			items[6].RepoOwner != "alice" {
			t.Errorf("items[6]: got type=%s owner=%s, want new_pr/alice",
				items[6].ActivityType, items[6].RepoOwner)
		}
	})

	t.Run("repo filter", func(t *testing.T) {
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Repo: "alice/alpha", Limit: 50,
		})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		for _, it := range items {
			if it.RepoOwner != "alice" || it.RepoName != "alpha" {
				t.Errorf("expected alice/alpha, got %s/%s",
					it.RepoOwner, it.RepoName)
			}
		}
	})

	t.Run("type filter", func(t *testing.T) {
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Types: []string{"new_pr", "new_issue"},
			Limit: 50,
		})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items (2 PRs + 1 issue), got %d",
				len(items))
		}
		for _, it := range items {
			if it.ActivityType != "new_pr" &&
				it.ActivityType != "new_issue" {
				t.Errorf("unexpected type: %s", it.ActivityType)
			}
		}
	})

	t.Run("search filter", func(t *testing.T) {
		items, err := d.ListActivity(ctx, ListActivityOpts{
			Search: "bug", Limit: 50,
		})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		if len(items) == 0 {
			t.Fatal("expected at least one result for 'bug' search")
		}
		for _, it := range items {
			if it.ItemTitle != "Fix bug" {
				t.Errorf("unexpected item: %s", it.ItemTitle)
			}
		}
	})

	t.Run("limit and before cursor", func(t *testing.T) {
		page1, err := d.ListActivity(
			ctx, ListActivityOpts{Limit: 3})
		if err != nil {
			t.Fatalf("ListActivity page1: %v", err)
		}
		if len(page1) != 3 {
			t.Fatalf("expected 3, got %d", len(page1))
		}

		last := page1[2]
		page2, err := d.ListActivity(ctx, ListActivityOpts{
			Limit:          3,
			BeforeTime:     &last.CreatedAt,
			BeforeSource:   last.Source,
			BeforeSourceID: last.SourceID,
		})
		if err != nil {
			t.Fatalf("ListActivity page2: %v", err)
		}
		if len(page2) != 3 {
			t.Fatalf("expected 3, got %d", len(page2))
		}

		seen := make(map[string]bool)
		for _, it := range page1 {
			key := fmt.Sprintf("%s:%d", it.Source, it.SourceID)
			seen[key] = true
		}
		for _, it := range page2 {
			key := fmt.Sprintf("%s:%d", it.Source, it.SourceID)
			if seen[key] {
				t.Errorf("duplicate across pages: %s", key)
			}
		}
	})

	t.Run("after cursor for polling", func(t *testing.T) {
		all, err := d.ListActivity(
			ctx, ListActivityOpts{Limit: 50})
		if err != nil {
			t.Fatalf("ListActivity: %v", err)
		}
		newest := all[0]

		err = d.UpsertPREvents(ctx, []PREvent{
			{PRID: prID1, EventType: "issue_comment", Author: "grace",
				Body:      "New comment",
				CreatedAt: base.Add(10 * time.Minute),
				DedupeKey: "comment-new"},
		})
		if err != nil {
			t.Fatalf("UpsertPREvents: %v", err)
		}

		newItems, err := d.ListActivity(ctx, ListActivityOpts{
			Limit:         50,
			AfterTime:     &newest.CreatedAt,
			AfterSource:   newest.Source,
			AfterSourceID: newest.SourceID,
		})
		if err != nil {
			t.Fatalf("ListActivity after: %v", err)
		}
		if len(newItems) != 1 {
			t.Fatalf("expected 1 new item, got %d", len(newItems))
		}
		if newItems[0].Author != "grace" {
			t.Errorf("expected author grace, got %s",
				newItems[0].Author)
		}
	})

	_ = prID2
}
