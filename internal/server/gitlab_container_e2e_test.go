package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	Assert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/wesm/middleman/internal/db"
	ghclient "github.com/wesm/middleman/internal/github"
	"github.com/wesm/middleman/internal/platform"
	platformgitlab "github.com/wesm/middleman/internal/platform/gitlab"
)

type gitLabContainerManifest struct {
	BaseURL           string `json:"base_url"`
	APIURL            string `json:"api_url"`
	Host              string `json:"host"`
	Token             string `json:"token"`
	Owner             string `json:"owner"`
	Name              string `json:"name"`
	RepoPath          string `json:"repo_path"`
	WebURL            string `json:"web_url"`
	CloneURL          string `json:"clone_url"`
	DefaultBranch     string `json:"default_branch"`
	ProjectID         int64  `json:"project_id"`
	ProjectExternalID string `json:"project_external_id"`
	MergeRequestIID   int    `json:"merge_request_iid"`
	IssueIID          int    `json:"issue_iid"`
	Label             string `json:"label"`
	ReleaseTag        string `json:"release_tag"`
}

func TestGitLabContainerE2E(t *testing.T) {
	if os.Getenv("MIDDLEMAN_GITLAB_CONTAINER_E2E") != "1" {
		t.Skip("set MIDDLEMAN_GITLAB_CONTAINER_E2E=1 to run GitLab CE container e2e")
	}

	assert := Assert.New(t)
	require := require.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Minute)
	defer cancel()

	image := envOrDefault("MIDDLEMAN_GITLAB_IMAGE", "gitlab/gitlab-ce:18.9.5-ce.0")
	rootPassword := envOrDefault("GITLAB_ROOT_PASSWORD", "V9q!T3m#R7p-L2x@N6s")
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image,
			ExposedPorts: []string{"80/tcp"},
			Env: map[string]string{
				"GITLAB_ROOT_PASSWORD": rootPassword,
				"GITLAB_OMNIBUS_CONFIG": "" +
					"external_url 'http://localhost'\n" +
					"gitlab_rails['initial_root_password'] = '" + rootPassword + "'\n" +
					"letsencrypt['enable'] = false\n" +
					"prometheus_monitoring['enable'] = false\n" +
					"puma['worker_processes'] = 0\n" +
					"sidekiq['concurrency'] = 5\n",
			},
			WaitingFor: wait.ForHTTP("/users/sign_in").
				WithPort("80/tcp").
				WithStartupTimeout(20 * time.Minute).
				WithStatusCodeMatcher(func(status int) bool {
					return status == 200
				}),
		},
		Started: true,
	})
	if err != nil {
		if container != nil {
			require.NoError(err, containerLogs(ctx, container))
		}
		require.NoError(err)
	}
	if os.Getenv("MIDDLEMAN_KEEP_GITLAB_FIXTURE") == "1" {
		t.Logf("keeping GitLab container %s", container.GetContainerID())
	} else {
		t.Cleanup(func() {
			assert.NoError(container.Terminate(context.Background()))
		})
	}

	host, err := container.Host(ctx)
	require.NoError(err)
	port, err := container.MappedPort(ctx, "80/tcp")
	require.NoError(err)
	baseURL := fmt.Sprintf("http://%s:%s", host, port.Port())

	manifestPath := filepath.Join(t.TempDir(), "gitlab-manifest.json")
	cmd := exec.CommandContext(
		ctx,
		filepath.Join(repoRoot(t), "scripts/e2e/gitlab/bootstrap.sh"),
		manifestPath,
	)
	cmd.Env = append(os.Environ(),
		"GITLAB_URL="+baseURL,
		"GITLAB_ROOT_PASSWORD="+rootPassword,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		require.NoError(err, string(output)+"\n"+containerLogs(ctx, container))
	}

	manifestFile, err := os.Open(manifestPath)
	require.NoError(err)
	defer manifestFile.Close()
	var manifest gitLabContainerManifest
	require.NoError(json.NewDecoder(manifestFile).Decode(&manifest))

	client, err := platformgitlab.NewClient(
		manifest.Host,
		manifest.Token,
		platformgitlab.WithBaseURLForTesting(manifest.APIURL),
		platformgitlab.WithForegroundTimeoutForTesting(time.Minute),
	)
	require.NoError(err)
	registry, err := platform.NewRegistry(client)
	require.NoError(err)

	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(err)
	t.Cleanup(func() { require.NoError(database.Close()) })
	repo := ghclient.RepoRef{
		Platform:           platform.KindGitLab,
		PlatformHost:       manifest.Host,
		Owner:              manifest.Owner,
		Name:               manifest.Name,
		RepoPath:           manifest.RepoPath,
		PlatformRepoID:     manifest.ProjectID,
		PlatformExternalID: manifest.ProjectExternalID,
		WebURL:             manifest.WebURL,
		CloneURL:           manifest.CloneURL,
		DefaultBranch:      manifest.DefaultBranch,
	}
	syncer := ghclient.NewSyncerWithRegistry(
		registry, database, nil, []ghclient.RepoRef{repo}, time.Minute, nil, nil,
	)
	t.Cleanup(syncer.Stop)

	syncer.RunOnce(ctx)
	require.NoError(syncer.SyncMR(ctx, manifest.Owner, manifest.Name, manifest.MergeRequestIID))
	require.NoError(syncer.SyncIssue(ctx, manifest.Owner, manifest.Name, manifest.IssueIID))

	repoRow, err := database.GetRepoByIdentity(ctx, db.RepoIdentity{
		Platform:       "gitlab",
		PlatformHost:   manifest.Host,
		PlatformRepoID: manifest.ProjectExternalID,
		Owner:          manifest.Owner,
		Name:           manifest.Name,
		RepoPath:       manifest.RepoPath,
	})
	require.NoError(err)
	require.NotNil(repoRow)
	assert.Equal(manifest.RepoPath, repoRow.RepoPath)

	mr, err := database.GetMergeRequestByRepoIDAndNumber(ctx, repoRow.ID, manifest.MergeRequestIID)
	require.NoError(err)
	require.NotNil(mr)
	assert.Equal("GitLab container MR", mr.Title)
	require.NotEmpty(mr.Labels)
	assert.Equal(manifest.Label, mr.Labels[0].Name)
	mrEvents, err := database.ListMREvents(ctx, mr.ID)
	require.NoError(err)
	assert.NotEmpty(mrEvents)

	issue, err := database.GetIssueByRepoIDAndNumber(ctx, repoRow.ID, manifest.IssueIID)
	require.NoError(err)
	require.NotNil(issue)
	assert.Equal("GitLab container issue", issue.Title)
	issueEvents, err := database.ListIssueEvents(ctx, issue.ID)
	require.NoError(err)
	assert.NotEmpty(issueEvents)

	summaries, err := database.ListRepoSummaries(ctx)
	require.NoError(err)
	require.Len(summaries, 1)
	require.NotNil(summaries[0].Overview.LatestRelease)
	assert.Equal(manifest.ReleaseTag, summaries[0].Overview.LatestRelease.TagName)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "could not find repo root from %s", dir)
		dir = parent
	}
}

func containerLogs(ctx context.Context, container testcontainers.Container) string {
	logs, err := container.Logs(ctx)
	if err != nil {
		return fmt.Sprintf("failed to read GitLab container logs: %v", err)
	}
	defer logs.Close()
	body, err := io.ReadAll(io.LimitReader(logs, 128*1024))
	if err != nil {
		return fmt.Sprintf("failed to read GitLab container logs: %v", err)
	}
	return string(body)
}
