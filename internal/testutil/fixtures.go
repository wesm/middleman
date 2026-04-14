package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/db"
)

// SeedResult holds references to seeded data for use in E2E tests.
type SeedResult struct {
	// FixtureClient builds a FixtureClient populated with the seeded open items.
	FixtureClient func() *FixtureClient
}

// SeedFixtures populates d with a synthetic data set for E2E tests and returns
// a SeedResult containing a FixtureClient factory for the seeded open items.
func SeedFixtures(ctx context.Context, d *db.DB) (*SeedResult, error) {
	now := time.Now().UTC()

	// --- Repos ---
	widgetsID, err := d.UpsertRepo(ctx, "github.com", "acme", "widgets")
	if err != nil {
		return nil, fmt.Errorf("upsert acme/widgets: %w", err)
	}
	toolsID, err := d.UpsertRepo(ctx, "github.com", "acme", "tools")
	if err != nil {
		return nil, fmt.Errorf("upsert acme/tools: %w", err)
	}
	_, err = d.UpsertRepo(ctx, "github.com", "acme", "archived")
	if err != nil {
		return nil, fmt.Errorf("upsert acme/archived: %w", err)
	}

	// --- Pull Requests ---

	const widgetsPR1HeadSHA = "1111111111111111111111111111111111111111"

	ciChecksJSON, err := json.Marshal([]db.CICheck{
		{
			Name:       "build",
			Status:     "completed",
			Conclusion: "success",
			URL:        "https://github.com/acme/widgets/actions/runs/1/job/1",
			App:        "GitHub Actions",
		},
		{
			Name:       "test",
			Status:     "completed",
			Conclusion: "success",
			URL:        "https://github.com/acme/widgets/actions/runs/1/job/2",
			App:        "GitHub Actions",
		},
		{
			Name:       "lint",
			Status:     "completed",
			Conclusion: "success",
			URL:        "https://github.com/acme/widgets/actions/runs/1/job/3",
			App:        "GitHub Actions",
		},
		{
			Name:       "roborev",
			Status:     "in_progress",
			Conclusion: "",
			URL:        "",
			App:        "roborev",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal widgets#1 ci checks: %w", err)
	}

	// widgets#1: open, alice, has reviews+comments+4 commits
	w1Created := now.Add(-10 * 24 * time.Hour)
	w1ID, err := d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            widgetsID,
		PlatformID:        1001,
		Number:            1,
		URL:               "https://github.com/acme/widgets/pull/1",
		Title:             "Add widget caching layer",
		Author:            "alice",
		AuthorDisplayName: "Alice",
		State:             "open",
		HeadBranch:        "feature/caching",
		BaseBranch:        "main",
		Additions:         240,
		Deletions:         30,
		CommentCount:      3,
		CIStatus:          "success",
		CIChecksJSON:      string(ciChecksJSON),
		CreatedAt:         w1Created,
		UpdatedAt:         now.Add(-2 * time.Hour),
		LastActivityAt:    now.Add(-2 * time.Hour),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets#1: %w", err)
	}

	// widgets#2: open, bob, dirty merge state
	w2Created := now.Add(-8 * 24 * time.Hour)
	w2ID, err := d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            widgetsID,
		PlatformID:        1002,
		Number:            2,
		URL:               "https://github.com/acme/widgets/pull/2",
		Title:             "Fix race condition in event loop",
		Author:            "bob",
		AuthorDisplayName: "Bob",
		State:             "open",
		HeadBranch:        "fix/race-condition",
		BaseBranch:        "main",
		Additions:         55,
		Deletions:         12,
		CommentCount:      2,
		MergeableState:    "dirty",
		CreatedAt:         w2Created,
		UpdatedAt:         now.Add(-20 * time.Hour),
		LastActivityAt:    now.Add(-20 * time.Hour),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets#2: %w", err)
	}

	// widgets#3: merged 4d ago, carol
	w3Merged := now.Add(-4 * 24 * time.Hour)
	_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            widgetsID,
		PlatformID:        1003,
		Number:            3,
		URL:               "https://github.com/acme/widgets/pull/3",
		Title:             "Upgrade dependency versions",
		Author:            "carol",
		AuthorDisplayName: "Carol",
		State:             "merged",
		HeadBranch:        "chore/deps",
		BaseBranch:        "main",
		Additions:         80,
		Deletions:         80,
		CreatedAt:         now.Add(-10 * 24 * time.Hour),
		UpdatedAt:         w3Merged,
		LastActivityAt:    w3Merged,
		MergedAt:          &w3Merged,
		ClosedAt:          &w3Merged,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets#3: %w", err)
	}

	// widgets#4: merged 25d ago, alice
	w4Merged := now.Add(-25 * 24 * time.Hour)
	_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            widgetsID,
		PlatformID:        1004,
		Number:            4,
		URL:               "https://github.com/acme/widgets/pull/4",
		Title:             "Refactor storage backend",
		Author:            "alice",
		AuthorDisplayName: "Alice",
		State:             "merged",
		HeadBranch:        "refactor/storage",
		BaseBranch:        "main",
		Additions:         420,
		Deletions:         310,
		CreatedAt:         now.Add(-30 * 24 * time.Hour),
		UpdatedAt:         w4Merged,
		LastActivityAt:    w4Merged,
		MergedAt:          &w4Merged,
		ClosedAt:          &w4Merged,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets#4: %w", err)
	}

	// widgets#5: closed not merged, 5d ago, bob
	w5Closed := now.Add(-5 * 24 * time.Hour)
	_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            widgetsID,
		PlatformID:        1005,
		Number:            5,
		URL:               "https://github.com/acme/widgets/pull/5",
		Title:             "Experimental new API",
		Author:            "bob",
		AuthorDisplayName: "Bob",
		State:             "closed",
		HeadBranch:        "experiment/new-api",
		BaseBranch:        "main",
		Additions:         900,
		Deletions:         0,
		CreatedAt:         now.Add(-15 * 24 * time.Hour),
		UpdatedAt:         w5Closed,
		LastActivityAt:    w5Closed,
		ClosedAt:          &w5Closed,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets#5: %w", err)
	}

	// widgets#6: open draft, carol
	w6Created := now.Add(-3 * 24 * time.Hour)
	_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            widgetsID,
		PlatformID:        1006,
		Number:            6,
		URL:               "https://github.com/acme/widgets/pull/6",
		Title:             "WIP: new dashboard layout",
		Author:            "carol",
		AuthorDisplayName: "Carol",
		State:             "open",
		IsDraft:           true,
		HeadBranch:        "wip/dashboard",
		BaseBranch:        "main",
		Additions:         150,
		Deletions:         40,
		CreatedAt:         w6Created,
		UpdatedAt:         now.Add(-12 * time.Hour),
		LastActivityAt:    now.Add(-12 * time.Hour),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets#6: %w", err)
	}

	// widgets#7: open, dependabot[bot]
	w7Created := now.Add(-1 * 24 * time.Hour)
	_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            widgetsID,
		PlatformID:        1007,
		Number:            7,
		URL:               "https://github.com/acme/widgets/pull/7",
		Title:             "Bump lodash from 4.17.20 to 4.17.21",
		Author:            "dependabot[bot]",
		AuthorDisplayName: "dependabot[bot]",
		State:             "open",
		HeadBranch:        "dependabot/npm_and_yarn/lodash-4.17.21",
		BaseBranch:        "main",
		Additions:         1,
		Deletions:         1,
		CreatedAt:         w7Created,
		UpdatedAt:         now.Add(-6 * time.Hour),
		LastActivityAt:    now.Add(-6 * time.Hour),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets#7: %w", err)
	}

	// tools#1: open, dave
	t1Created := now.Add(-6 * 24 * time.Hour)
	t1ID, err := d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            toolsID,
		PlatformID:        2001,
		Number:            1,
		URL:               "https://github.com/acme/tools/pull/1",
		Title:             "Add CLI flag parser",
		Author:            "dave",
		AuthorDisplayName: "Dave",
		State:             "open",
		HeadBranch:        "feature/cli-flags",
		BaseBranch:        "main",
		Additions:         180,
		Deletions:         20,
		CreatedAt:         t1Created,
		UpdatedAt:         now.Add(-18 * time.Hour),
		LastActivityAt:    now.Add(-18 * time.Hour),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert tools#1: %w", err)
	}

	// tools#2: merged 60d ago, alice
	t2Merged := now.Add(-60 * 24 * time.Hour)
	_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
		RepoID:            toolsID,
		PlatformID:        2002,
		Number:            2,
		URL:               "https://github.com/acme/tools/pull/2",
		Title:             "Initial project setup",
		Author:            "alice",
		AuthorDisplayName: "Alice",
		State:             "merged",
		HeadBranch:        "init",
		BaseBranch:        "main",
		Additions:         500,
		Deletions:         0,
		CreatedAt:         now.Add(-62 * 24 * time.Hour),
		UpdatedAt:         t2Merged,
		LastActivityAt:    t2Merged,
		MergedAt:          &t2Merged,
		ClosedAt:          &t2Merged,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert tools#2: %w", err)
	}

	// tools#10/#11/#12: open stacked PR chain (auth refactor).
	// Forms: main <- feat/auth-base <- feat/auth-retry <- feat/auth-ui
	stackBase := now.Add(-4 * 24 * time.Hour)
	for i, pr := range []struct {
		num                int
		title, head, base  string
		ci, review, author string
	}{
		{10, "Auth: extract token refresh helper", "feat/auth-base", "main", "success", "APPROVED", "alice"},
		{11, "Auth: add retry with backoff", "feat/auth-retry", "feat/auth-base", "success", "", "alice"},
		{12, "Auth: error handling UI", "feat/auth-ui", "feat/auth-retry", "pending", "", "alice"},
	} {
		created := stackBase.Add(time.Duration(i) * time.Hour)
		_, err = d.UpsertMergeRequest(ctx, &db.MergeRequest{
			RepoID:            toolsID,
			PlatformID:        int64(2010 + i),
			Number:            pr.num,
			URL:               fmt.Sprintf("https://github.com/acme/tools/pull/%d", pr.num),
			Title:             pr.title,
			Author:            pr.author,
			AuthorDisplayName: "Alice",
			State:             "open",
			HeadBranch:        pr.head,
			BaseBranch:        pr.base,
			CIStatus:          pr.ci,
			ReviewDecision:    pr.review,
			Additions:         50 + i*10,
			Deletions:         5,
			CreatedAt:         created,
			UpdatedAt:         created,
			LastActivityAt:    created,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert tools#%d: %w", pr.num, err)
		}
	}

	// --- Issues ---

	// widgets#10: open, eve
	wi10Created := now.Add(-5 * 24 * time.Hour)
	wi10ID, err := d.UpsertIssue(ctx, &db.Issue{
		RepoID:         widgetsID,
		PlatformID:     3010,
		Number:         10,
		URL:            "https://github.com/acme/widgets/issues/10",
		Title:          "Widget rendering broken on Safari",
		Author:         "eve",
		State:          "open",
		CommentCount:   2,
		CreatedAt:      wi10Created,
		UpdatedAt:      now.Add(-4 * time.Hour),
		LastActivityAt: now.Add(-4 * time.Hour),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets issue#10: %w", err)
	}

	// widgets#11: open, alice, older (20d ago)
	wi11Created := now.Add(-20 * 24 * time.Hour)
	wi11ID, err := d.UpsertIssue(ctx, &db.Issue{
		RepoID:         widgetsID,
		PlatformID:     3011,
		Number:         11,
		URL:            "https://github.com/acme/widgets/issues/11",
		Title:          "Add dark mode support",
		Author:         "alice",
		State:          "open",
		CommentCount:   0,
		CreatedAt:      wi11Created,
		UpdatedAt:      wi11Created,
		LastActivityAt: wi11Created,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets issue#11: %w", err)
	}

	// widgets#12: closed 3d ago, bob
	wi12Closed := now.Add(-3 * 24 * time.Hour)
	wi12ID, err := d.UpsertIssue(ctx, &db.Issue{
		RepoID:         widgetsID,
		PlatformID:     3012,
		Number:         12,
		URL:            "https://github.com/acme/widgets/issues/12",
		Title:          "Crash on empty input",
		Author:         "bob",
		State:          "closed",
		CommentCount:   1,
		CreatedAt:      now.Add(-10 * 24 * time.Hour),
		UpdatedAt:      wi12Closed,
		LastActivityAt: wi12Closed,
		ClosedAt:       &wi12Closed,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets issue#12: %w", err)
	}

	// widgets#13: open, dependabot[bot]
	wi13Created := now.Add(-2 * 24 * time.Hour)
	wi13ID, err := d.UpsertIssue(ctx, &db.Issue{
		RepoID:         widgetsID,
		PlatformID:     3013,
		Number:         13,
		URL:            "https://github.com/acme/widgets/issues/13",
		Title:          "Security advisory: prototype pollution",
		Author:         "dependabot[bot]",
		State:          "open",
		CommentCount:   0,
		CreatedAt:      wi13Created,
		UpdatedAt:      wi13Created,
		LastActivityAt: wi13Created,
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets issue#13: %w", err)
	}

	// tools#5: open, dave
	ti5Created := now.Add(-7 * 24 * time.Hour)
	ti5ID, err := d.UpsertIssue(ctx, &db.Issue{
		RepoID:         toolsID,
		PlatformID:     4005,
		Number:         5,
		URL:            "https://github.com/acme/tools/issues/5",
		Title:          "Support config file loading",
		Author:         "dave",
		State:          "open",
		CommentCount:   1,
		CreatedAt:      ti5Created,
		UpdatedAt:      now.Add(-16 * time.Hour),
		LastActivityAt: now.Add(-16 * time.Hour),
	})
	if err != nil {
		return nil, fmt.Errorf("upsert tools issue#5: %w", err)
	}

	// --- PR Events ---

	// widgets PR#1: 2 comments (bob, carol), 1 review (bob APPROVED), 4 commits (alice)
	commitBase := now.Add(-9 * 24 * time.Hour)
	w1BobCommentUTC := time.Date(now.Year(), now.Month(), now.Day(), 1, 30, 0, 0, time.UTC).Add(-8 * 24 * time.Hour)
	w1BobComment, err := time.Parse(
		time.RFC3339,
		w1BobCommentUTC.Add(-4*time.Hour).Format("2006-01-02T15:04:05")+"-04:00",
	)
	if err != nil {
		return nil, fmt.Errorf("build widgets PR#1 non-UTC comment timestamp: %w", err)
	}
	err = d.UpsertMREvents(ctx, []db.MREvent{
		{
			MergeRequestID: w1ID,
			EventType:      "issue_comment",
			Author:         "bob",
			Body:           "Looks like a solid approach. Minor nit on naming.",
			CreatedAt:      w1BobComment,
			DedupeKey:      "w1-comment-bob-1",
		},
		{
			MergeRequestID: w1ID,
			EventType:      "issue_comment",
			Author:         "carol",
			Body:           "I agree, caching here will help a lot.",
			CreatedAt:      now.Add(-6 * 24 * time.Hour),
			DedupeKey:      "w1-comment-carol-1",
		},
		{
			MergeRequestID: w1ID,
			EventType:      "review",
			Author:         "bob",
			Summary:        "APPROVED",
			Body:           "LGTM after addressing the naming nit.",
			CreatedAt:      now.Add(-5 * 24 * time.Hour),
			DedupeKey:      "w1-review-bob-1",
		},
		{
			MergeRequestID: w1ID,
			EventType:      "commit",
			Author:         "alice",
			Summary:        "abc1111",
			Body:           "feat: add cache store",
			CreatedAt:      commitBase,
			DedupeKey:      "w1-commit-1",
		},
		{
			MergeRequestID: w1ID,
			EventType:      "commit",
			Author:         "alice",
			Summary:        "abc2222",
			Body:           "feat: wire cache into handler",
			CreatedAt:      commitBase.Add(2 * time.Hour),
			DedupeKey:      "w1-commit-2",
		},
		{
			MergeRequestID: w1ID,
			EventType:      "commit",
			Author:         "alice",
			Summary:        "abc3333",
			Body:           "test: add cache unit tests",
			CreatedAt:      commitBase.Add(4 * time.Hour),
			DedupeKey:      "w1-commit-3",
		},
		{
			MergeRequestID: w1ID,
			EventType:      "commit",
			Author:         "alice",
			Summary:        "abc4444",
			Body:           "fix: handle nil cache gracefully",
			CreatedAt:      commitBase.Add(6 * time.Hour),
			DedupeKey:      "w1-commit-4",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets PR#1 events: %w", err)
	}

	// widgets PR#2: 1 comment (alice), 1 review (alice CHANGES_REQUESTED)
	err = d.UpsertMREvents(ctx, []db.MREvent{
		{
			MergeRequestID: w2ID,
			EventType:      "issue_comment",
			Author:         "alice",
			Body:           "Have you considered using a mutex here instead?",
			CreatedAt:      now.Add(-6 * 24 * time.Hour),
			DedupeKey:      "w2-comment-alice-1",
		},
		{
			MergeRequestID: w2ID,
			EventType:      "review",
			Author:         "alice",
			Summary:        "CHANGES_REQUESTED",
			Body:           "Please add a test that reproduces the race condition.",
			CreatedAt:      now.Add(-5 * 24 * time.Hour),
			DedupeKey:      "w2-review-alice-1",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets PR#2 events: %w", err)
	}

	// tools PR#1: 1 comment (alice)
	err = d.UpsertMREvents(ctx, []db.MREvent{
		{
			MergeRequestID: t1ID,
			EventType:      "issue_comment",
			Author:         "alice",
			Body:           "Nice work! Should we support short flags too?",
			CreatedAt:      now.Add(-4 * 24 * time.Hour),
			DedupeKey:      "t1-comment-alice-1",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upsert tools PR#1 events: %w", err)
	}

	// --- Issue Events ---

	// widgets issue#10: 2 comments (alice, bob)
	err = d.UpsertIssueEvents(ctx, []db.IssueEvent{
		{
			IssueID:   wi10ID,
			EventType: "issue_comment",
			Author:    "alice",
			Body:      "Confirmed on Safari 17. Looks like a CSS isolation bug.",
			CreatedAt: now.Add(-3 * 24 * time.Hour),
			DedupeKey: "wi10-comment-alice-1",
		},
		{
			IssueID:   wi10ID,
			EventType: "issue_comment",
			Author:    "bob",
			Body:      "I can reproduce too. Will take a look.",
			CreatedAt: now.Add(-2 * 24 * time.Hour),
			DedupeKey: "wi10-comment-bob-1",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets issue#10 events: %w", err)
	}

	// widgets issue#12: 1 comment (carol)
	err = d.UpsertIssueEvents(ctx, []db.IssueEvent{
		{
			IssueID:   wi12ID,
			EventType: "issue_comment",
			Author:    "carol",
			Body:      "Fixed in PR#3.",
			CreatedAt: now.Add(-3 * 24 * time.Hour),
			DedupeKey: "wi12-comment-carol-1",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upsert widgets issue#12 events: %w", err)
	}

	// tools issue#5: 1 comment (dave)
	err = d.UpsertIssueEvents(ctx, []db.IssueEvent{
		{
			IssueID:   ti5ID,
			EventType: "issue_comment",
			Author:    "dave",
			Body:      "I'll start with TOML support first.",
			CreatedAt: now.Add(-5 * 24 * time.Hour),
			DedupeKey: "ti5-comment-dave-1",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("upsert tools issue#5 events: %w", err)
	}

	// --- Build FixtureClient open items ---

	openPRs := map[string][]*gh.PullRequest{
		"acme/widgets": {
			setPRHeadSHA(setPRStats(buildGHPR("acme", "widgets", 1001, 1, "Add widget caching layer", "alice", "open", false, "", w1Created, now.Add(-2*time.Hour)), 240, 30), widgetsPR1HeadSHA),
			setPRStats(buildGHPR("acme", "widgets", 1002, 2, "Fix race condition in event loop", "bob", "open", false, "dirty", w2Created, now.Add(-20*time.Hour)), 55, 12),
			setPRStats(buildGHPR("acme", "widgets", 1006, 6, "WIP: new dashboard layout", "carol", "open", true, "", w6Created, now.Add(-12*time.Hour)), 150, 40),
			setPRStats(buildGHPR("acme", "widgets", 1007, 7, "Bump lodash from 4.17.20 to 4.17.21", "dependabot[bot]", "open", false, "", w7Created, now.Add(-6*time.Hour)), 1, 1),
		},
		"acme/tools": {
			setPRStats(buildGHPR("acme", "tools", 2001, 1, "Add CLI flag parser", "dave", "open", false, "", t1Created, now.Add(-18*time.Hour)), 180, 20),
		},
	}

	allPRs := map[string][]*gh.PullRequest{
		"acme/widgets": {
			setPRHeadSHA(setPRStats(buildGHPR("acme", "widgets", 1001, 1, "Add widget caching layer", "alice", "open", false, "", w1Created, now.Add(-2*time.Hour)), 240, 30), widgetsPR1HeadSHA),
			setPRStats(buildGHPR("acme", "widgets", 1002, 2, "Fix race condition in event loop", "bob", "open", false, "dirty", w2Created, now.Add(-20*time.Hour)), 55, 12),
			setPRStats(buildGHPR("acme", "widgets", 1003, 3, "Upgrade dependency versions", "carol", "merged", false, "", now.Add(-10*24*time.Hour), w3Merged), 80, 80),
			setPRStats(buildGHPR("acme", "widgets", 1004, 4, "Refactor storage backend", "alice", "merged", false, "", now.Add(-30*24*time.Hour), w4Merged), 420, 310),
			setPRStats(buildGHPR("acme", "widgets", 1005, 5, "Experimental new API", "bob", "closed", false, "", now.Add(-15*24*time.Hour), w5Closed), 900, 0),
			setPRStats(buildGHPR("acme", "widgets", 1006, 6, "WIP: new dashboard layout", "carol", "open", true, "", w6Created, now.Add(-12*time.Hour)), 150, 40),
			setPRStats(buildGHPR("acme", "widgets", 1007, 7, "Bump lodash from 4.17.20 to 4.17.21", "dependabot[bot]", "open", false, "", w7Created, now.Add(-6*time.Hour)), 1, 1),
		},
		"acme/tools": {
			setPRStats(buildGHPR("acme", "tools", 2001, 1, "Add CLI flag parser", "dave", "open", false, "", t1Created, now.Add(-18*time.Hour)), 180, 20),
			setPRStats(buildGHPR("acme", "tools", 2002, 2, "Initial project setup", "alice", "merged", false, "", now.Add(-62*24*time.Hour), t2Merged), 500, 0),
		},
	}

	openIssues := map[string][]*gh.Issue{
		"acme/widgets": {
			buildGHIssue("acme", "widgets", 3010, 10, "Widget rendering broken on Safari", "eve", "open", wi10Created, now.Add(-4*time.Hour)),
			buildGHIssue("acme", "widgets", 3011, 11, "Add dark mode support", "alice", "open", wi11Created, wi11Created),
			buildGHIssue("acme", "widgets", 3013, 13, "Security advisory: prototype pollution", "dependabot[bot]", "open", wi13Created, wi13Created),
		},
		"acme/tools": {
			buildGHIssue("acme", "tools", 4005, 5, "Support config file loading", "dave", "open", ti5Created, now.Add(-16*time.Hour)),
		},
	}

	allIssues := map[string][]*gh.Issue{
		"acme/widgets": {
			buildGHIssue("acme", "widgets", 3010, 10, "Widget rendering broken on Safari", "eve", "open", wi10Created, now.Add(-4*time.Hour)),
			buildGHIssue("acme", "widgets", 3011, 11, "Add dark mode support", "alice", "open", wi11Created, wi11Created),
			buildGHIssue("acme", "widgets", 3012, 12, "Crash on empty input", "carol", "closed", now.Add(-7*24*time.Hour), wi12Closed),
			buildGHIssue("acme", "widgets", 3013, 13, "Security advisory: prototype pollution", "dependabot[bot]", "open", wi13Created, wi13Created),
		},
		"acme/tools": {
			buildGHIssue("acme", "tools", 4005, 5, "Support config file loading", "dave", "open", ti5Created, now.Add(-16*time.Hour)),
		},
	}

	// Suppress unused variable warnings for IDs only needed for event insertion.
	_ = wi11ID
	_ = wi13ID

	result := &SeedResult{
		FixtureClient: func() *FixtureClient {
			return &FixtureClient{
				OpenPRs:    openPRs,
				PRs:        allPRs,
				OpenIssues: openIssues,
				Issues:     allIssues,
				Comments:   make(map[string][]*gh.IssueComment),
				CombinedStatuses: map[string]*gh.CombinedStatus{
					refKey("acme", "widgets", widgetsPR1HeadSHA): {
						State: new("success"),
					},
				},
				CheckRuns: map[string][]*gh.CheckRun{
					refKey("acme", "widgets", widgetsPR1HeadSHA): {
						{
							Name:       new("build"),
							Status:     new("completed"),
							Conclusion: new("success"),
							HTMLURL:    new("https://github.com/acme/widgets/actions/runs/1/job/1"),
							App:        &gh.App{Name: new("GitHub Actions")},
						},
						{
							Name:       new("test"),
							Status:     new("completed"),
							Conclusion: new("success"),
							HTMLURL:    new("https://github.com/acme/widgets/actions/runs/1/job/2"),
							App:        &gh.App{Name: new("GitHub Actions")},
						},
						{
							Name:       new("lint"),
							Status:     new("completed"),
							Conclusion: new("success"),
							HTMLURL:    new("https://github.com/acme/widgets/actions/runs/1/job/3"),
							App:        &gh.App{Name: new("GitHub Actions")},
						},
						{
							Name:       new("roborev"),
							Status:     new("in_progress"),
							Conclusion: new(""),
							HTMLURL:    new(""),
							App:        &gh.App{Name: new("roborev")},
						},
					},
				},
				nextID: 10_000,
			}
		},
	}
	return result, nil
}

// buildGHPR creates a minimal *gh.PullRequest for the FixtureClient.
func buildGHPR(
	owner, repo string,
	id int64, number int, title, login, state string,
	draft bool, mergeableState string,
	createdAt, updatedAt time.Time,
) *gh.PullRequest {
	url := fmt.Sprintf(
		"https://github.com/%s/%s/pull/%d", owner, repo, number)
	pr := &gh.PullRequest{
		ID:        new(id),
		Number:    new(number),
		Title:     new(title),
		HTMLURL:   new(url),
		State:     new(state),
		Draft:     new(draft),
		User:      &gh.User{Login: new(login)},
		CreatedAt: &gh.Timestamp{Time: createdAt},
		UpdatedAt: &gh.Timestamp{Time: updatedAt},
		Head:      &gh.PullRequestBranch{Ref: new("feature")},
		Base:      &gh.PullRequestBranch{Ref: new("main")},
	}
	if mergeableState != "" {
		pr.MergeableState = new(mergeableState)
	}
	return pr
}

// setPRStats sets Additions and Deletions on a *gh.PullRequest so the
// fixture client returns non-zero diff stats for sync paths. Returns
// the same pointer for chain-friendly call sites.
func setPRStats(pr *gh.PullRequest, additions, deletions int) *gh.PullRequest {
	pr.Additions = &additions
	pr.Deletions = &deletions
	return pr
}

func setPRHeadSHA(pr *gh.PullRequest, sha string) *gh.PullRequest {
	if pr.Head == nil {
		pr.Head = &gh.PullRequestBranch{}
	}
	pr.Head.SHA = &sha
	return pr
}

// buildGHIssue creates a minimal *gh.Issue for the FixtureClient.
func buildGHIssue(
	owner, repo string,
	id int64, number int, title, login, state string,
	createdAt, updatedAt time.Time,
) *gh.Issue {
	url := fmt.Sprintf(
		"https://github.com/%s/%s/issues/%d", owner, repo, number)
	return &gh.Issue{
		ID:        new(id),
		Number:    new(number),
		Title:     new(title),
		HTMLURL:   new(url),
		State:     new(state),
		User:      &gh.User{Login: new(login)},
		CreatedAt: &gh.Timestamp{Time: createdAt},
		UpdatedAt: &gh.Timestamp{Time: updatedAt},
	}
}

// OpenFixtureTestDB opens a temporary SQLite database seeded with fixture data.
// It returns the DB and SeedResult. The DB is closed automatically via t.Cleanup.
func OpenFixtureTestDB(t *testing.T) (*db.DB, *SeedResult) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	result, err := SeedFixtures(context.Background(), d)
	require.NoError(t, err)
	return d, result
}
