package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/require"
	"github.com/wesm/middleman/internal/config"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
)

func TestTriggerSyncE2EBypassesCooldown(t *testing.T) {
	require := require.New(t)

	baseURL, client, database := startSyncCooldownE2EServer(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "acme"
name = "widget"
`, &mockGH{})

	status, body := postJSON(
		t, client, baseURL+"/api/v1/sync", nil,
	)
	require.Equal(http.StatusAccepted, status, body)

	first := waitForRepoSynced(t, database, "acme", "widget", nil)
	require.NotNil(first.LastSyncCompletedAt)

	time.Sleep(10 * time.Millisecond)

	status, body = postJSON(
		t, client, baseURL+"/api/v1/sync", nil,
	)
	require.Equal(http.StatusAccepted, status, body)

	second := waitForRepoSynced(
		t, database, "acme", "widget", first.LastSyncCompletedAt,
	)
	require.NotNil(second.LastSyncCompletedAt)
}

func TestAddRepoE2ETriggersImmediateSyncDuringCooldown(t *testing.T) {
	require := require.New(t)

	baseURL, client, database := startSyncCooldownE2EServer(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "acme"
name = "widget"
`, &mockGH{})

	status, body := postJSON(
		t, client, baseURL+"/api/v1/sync", nil,
	)
	require.Equal(http.StatusAccepted, status, body)
	waitForRepoSynced(t, database, "acme", "widget", nil)

	status, body = postJSON(t, client, baseURL+"/api/v1/repos", map[string]string{
		"owner": "other-org",
		"name":  "other-repo",
	})
	require.Equal(http.StatusCreated, status, body)

	added := waitForRepoSynced(
		t, database, "other-org", "other-repo", nil,
	)
	require.NotNil(added.LastSyncCompletedAt)
}

func TestRefreshRepoE2ETriggersImmediateSyncDuringCooldown(t *testing.T) {
	require := require.New(t)

	var includeRefreshRepo atomic.Bool
	mock := &mockGH{
		listReposByOwnerFn: func(
			_ context.Context, owner string,
		) ([]*gh.Repository, error) {
			repos := []*gh.Repository{{
				Name:     new("middleman"),
				Owner:    &gh.User{Login: new(owner)},
				Archived: new(false),
			}}
			if includeRefreshRepo.Load() {
				repos = append(repos, &gh.Repository{
					Name:     new("review-bot"),
					Owner:    &gh.User{Login: new(owner)},
					Archived: new(false),
				})
			}
			return repos, nil
		},
	}

	baseURL, client, database := startSyncCooldownE2EServer(t, `
sync_interval = "5m"
github_token_env = "MIDDLEMAN_GITHUB_TOKEN"
host = "127.0.0.1"
port = 8091

[[repos]]
owner = "roborev-dev"
name = "*"
`, mock)

	status, body := postJSON(
		t, client, baseURL+"/api/v1/sync", nil,
	)
	require.Equal(http.StatusAccepted, status, body)
	waitForRepoSynced(t, database, "roborev-dev", "middleman", nil)

	includeRefreshRepo.Store(true)

	status, body = postJSON(
		t, client,
		baseURL+"/api/v1/repo/gh/roborev-dev/%2A/refresh",
		nil,
	)
	require.Equal(http.StatusOK, status, body)

	refreshed := waitForRepoSynced(
		t, database, "roborev-dev", "review-bot", nil,
	)
	require.NotNil(refreshed.LastSyncCompletedAt)
}

func startSyncCooldownE2EServer(
	t *testing.T,
	cfgContent string,
	mock *mockGH,
) (string, *http.Client, *db.DB) {
	t.Helper()
	require := require.New(t)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { database.Close() })

	cfgPath := filepath.Join(dir, "config.toml")
	require.NoError(os.WriteFile(cfgPath, []byte(cfgContent), 0o644))

	cfg, err := config.Load(cfgPath)
	require.NoError(err)

	clients := map[string]ghclient.Client{"github.com": mock}
	resolved := ghclient.ResolveConfiguredRepos(
		t.Context(), clients, cfg.Repos,
	)
	trackers := map[string]*ghclient.RateTracker{
		"github.com": ghclient.NewRateTracker(
			database, "github.com", "rest",
		),
	}
	syncer := ghclient.NewSyncer(
		clients, database, nil, resolved.Expanded,
		time.Minute, trackers, nil,
	)
	t.Cleanup(syncer.Stop)

	srv := NewWithConfig(
		database, syncer, nil, nil, cfg, cfgPath,
		ServerOptions{},
	)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(ln)
	}()

	baseURL := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}

	require.Eventually(func() bool {
		resp, err := client.Get(baseURL + "/api/v1/version")
		if err != nil {
			return false
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	t.Cleanup(func() {
		gracefulShutdown(t, srv)
		select {
		case err := <-serveErr:
			require.ErrorIs(err, http.ErrServerClosed)
		case <-time.After(5 * time.Second):
			require.FailNow("server did not stop")
		}
	})

	return baseURL, client, database
}

func postJSON(
	t *testing.T,
	client *http.Client,
	url string,
	body any,
) (int, string) {
	t.Helper()
	require := require.New(t)

	var payload io.Reader = http.NoBody
	if body != nil {
		buf, err := json.Marshal(body)
		require.NoError(err)
		payload = bytes.NewReader(buf)
	}

	req, err := http.NewRequest(http.MethodPost, url, payload)
	require.NoError(err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(err)
	return resp.StatusCode, string(respBody)
}

func waitForRepoSynced(
	t *testing.T,
	database *db.DB,
	owner, name string,
	after *time.Time,
) *db.Repo {
	t.Helper()
	require := require.New(t)

	var repo *db.Repo
	require.Eventually(func() bool {
		got, err := database.GetRepoByOwnerName(
			t.Context(), owner, name,
		)
		if err != nil || got == nil || got.LastSyncCompletedAt == nil {
			return false
		}
		if after != nil &&
			!got.LastSyncCompletedAt.After(*after) {
			return false
		}
		repo = got
		return true
	}, 2*time.Second, 10*time.Millisecond)

	return repo
}
