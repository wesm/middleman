package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	gh "github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion that liveClient satisfies Client.
var _ Client = (*liveClient)(nil)

func TestNewClientReturnsNonNil(t *testing.T) {
	c, err := NewClient("fake-token", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientEnterprise(t *testing.T) {
	c, err := NewClient("test-token", "github.mycompany.com", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientGitHubDotCom(t *testing.T) {
	c, err := NewClient("test-token", "github.com", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientEmptyHost(t *testing.T) {
	c, err := NewClient("test-token", "", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestGraphQLEndpointForHost(t *testing.T) {
	require.Equal(t, "https://api.github.com/graphql", graphQLEndpointForHost(""))
	require.Equal(t, "https://api.github.com/graphql", graphQLEndpointForHost("github.com"))
	require.Equal(t, "https://github.example.com/api/graphql", graphQLEndpointForHost("github.example.com"))
}

func TestClientInterfaceIncludesListForcePushEvents(t *testing.T) {
	_, ok := reflect.TypeFor[Client]().MethodByName("ListForcePushEvents")
	require.True(t, ok)
}

func TestListForcePushEvents(t *testing.T) {
	require := require.New(t)
	var calls int
	var methods []string
	var contentTypes []string
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		calls++
		methods = append(methods, r.Method)
		contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[{"actor":{"login":"alice"},"beforeCommit":{"oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"afterCommit":{"oid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"createdAt":"2024-06-01T12:00:00Z","ref":{"name":"feature"}}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[{"actor":{"login":"alice"},"beforeCommit":{"oid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},"afterCommit":{"oid":"cccccccccccccccccccccccccccccccccccccccc"},"createdAt":"2024-06-01T12:05:00Z","ref":{"name":"feature"}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &liveClient{
		httpClient:      srv.Client(),
		graphQLEndpoint: srv.URL + "/graphql",
	}

	events, err := c.ListForcePushEvents(context.Background(), "owner", "repo", 42)
	require.NoError(err)
	require.Len(events, 2)
	require.Equal("alice", events[0].Actor)
	require.Equal("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", events[0].BeforeSHA)
	require.Equal("cccccccccccccccccccccccccccccccccccccccc", events[1].AfterSHA)
	require.Equal("feature", events[0].Ref)
	require.Equal(2, calls)
	require.Equal([]string{http.MethodPost, http.MethodPost}, methods)
	require.Equal([]string{"application/json", "application/json"}, contentTypes)
}

func TestListForcePushEventsReturnsGraphQLErrors(t *testing.T) {
	require := require.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"permission denied"}],"data":{"repository":{"pullRequest":{"timelineItems":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`))
	}))
	defer srv.Close()

	c := &liveClient{
		httpClient:      srv.Client(),
		graphQLEndpoint: srv.URL,
	}

	events, err := c.ListForcePushEvents(context.Background(), "owner", "repo", 42)
	require.Nil(events)
	require.ErrorContains(err, "permission denied")
}

func TestListForcePushEventsRejectsNullGraphQLNodes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "null repository",
			body: `{"data":{"repository":null}}`,
			want: "missing repository",
		},
		{
			name: "null pull request",
			body: `{"data":{"repository":{"pullRequest":null}}}`,
			want: "missing pull request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := &liveClient{
				httpClient:      srv.Client(),
				graphQLEndpoint: srv.URL,
			}

			events, err := c.ListForcePushEvents(context.Background(), "owner", "repo", 42)
			require.Nil(events)
			require.ErrorContains(err, tt.want)
		})
	}
}

func TestMarkPullRequestReadyForReviewUsesGraphQLMutation(t *testing.T) {
	require := require.New(t)
	var calls int
	var methods []string
	var contentTypes []string
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		calls++
		methods = append(methods, r.Method)
		contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"id":"PR_kwDOAAABc84"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"markPullRequestReadyForReview":{"pullRequest":{"databaseId":1001,"number":141,"title":"Ready PR","state":"OPEN","isDraft":false,"body":"body","url":"https://github.com/wesm/middleman/pull/141","author":{"login":"wesm"},"createdAt":"2026-04-14T00:00:00Z","updatedAt":"2026-04-14T00:05:00Z","mergedAt":null,"closedAt":null,"additions":12,"deletions":3,"mergeable":"MERGEABLE","reviewDecision":"APPROVED","headRefName":"feature","baseRefName":"main","headRefOid":"abc123","baseRefOid":"def456","headRepository":{"url":"https://github.com/wesm/middleman"},"labels":{"nodes":[]}}}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ghClient, err := gh.NewClient(srv.Client()).WithEnterpriseURLs(srv.URL+"/api/v3/", srv.URL+"/api/uploads/")
	require.NoError(err)

	c := &liveClient{
		gh:              ghClient,
		httpClient:      srv.Client(),
		graphQLEndpoint: srv.URL + "/graphql",
	}

	pr, err := c.MarkPullRequestReadyForReview(context.Background(), "wesm", "middleman", 141)
	require.NoError(err)
	require.NotNil(pr)
	require.Equal(141, pr.GetNumber())
	require.Equal("Ready PR", pr.GetTitle())
	require.False(pr.GetDraft())
	require.Equal(2, calls)
	require.Equal([]string{http.MethodPost, http.MethodPost}, methods)
	require.Equal([]string{"application/json", "application/json"}, contentTypes)
}

func TestMarkPullRequestReadyForReviewReturnsTypedStaleStateError(t *testing.T) {
	require := require.New(t)
	call := 0
	var methods []string
	var contentTypes []string
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		call++
		methods = append(methods, r.Method)
		contentTypes = append(contentTypes, r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			_, _ = w.Write([]byte(`{"data":{"repository":{"pullRequest":{"id":"PR_kwDOAAABc84"}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"markPullRequestReadyForReview":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a PullRequest with the global id of 'PR_kwDOAAABc84'."}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ghClient, err := gh.NewClient(srv.Client()).WithEnterpriseURLs(srv.URL+"/api/v3/", srv.URL+"/api/uploads/")
	require.NoError(err)

	c := &liveClient{
		gh:              ghClient,
		httpClient:      srv.Client(),
		graphQLEndpoint: srv.URL + "/graphql",
	}

	pr, err := c.MarkPullRequestReadyForReview(context.Background(), "wesm", "middleman", 141)
	require.Nil(pr)
	require.Error(err)
	require.ErrorContains(err, "Could not resolve to a PullRequest")

	var statusErr interface{ StatusCode() int }
	require.ErrorAs(err, &statusErr, "expected status-bearing error, got %T", err)
	require.Equal(http.StatusNotFound, statusErr.StatusCode())

	var staleErr interface{ IsStaleState() bool }
	require.ErrorAs(err, &staleErr, "expected stale-state error, got %T", err)
	require.True(staleErr.IsStaleState())
	require.Equal(2, call)
	require.Equal([]string{http.MethodPost, http.MethodPost}, methods)
	require.Equal([]string{"application/json", "application/json"}, contentTypes)
}

// TestNewClientWiresETagTransport verifies that NewClient installs the
// etagTransport at the top of the underlying http.Client's transport
// chain. The transport's behavior is exercised exhaustively in
// etag_transport_test.go; this test guards against the constructor
// silently dropping or reordering the wrap so the wired-up chain
// stays in sync with the transport's contract.
func TestNewClientWiresETagTransport(t *testing.T) {
	c, err := NewClient("fake-token", "", nil, nil)
	require.NoError(t, err)
	lc, ok := c.(*liveClient)
	require.Truef(t, ok, "expected *liveClient, got %T", c)
	transport := lc.gh.Client().Transport
	_, ok = transport.(*etagTransport)
	require.Truef(t, ok, "expected *etagTransport at top of transport chain, got %T", transport)
}
