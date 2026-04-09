package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// Compile-time assertion that liveClient satisfies Client.
var _ Client = (*liveClient)(nil)

func TestNewClientReturnsNonNil(t *testing.T) {
	c, err := NewClient("fake-token", "", nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientEnterprise(t *testing.T) {
	c, err := NewClient("test-token", "github.mycompany.com", nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientGitHubDotCom(t *testing.T) {
	c, err := NewClient("test-token", "github.com", nil)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewClientEmptyHost(t *testing.T) {
	c, err := NewClient("test-token", "", nil)
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
