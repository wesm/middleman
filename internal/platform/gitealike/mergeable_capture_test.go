package gitealike

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	Assert "github.com/stretchr/testify/assert"
	Require "github.com/stretchr/testify/require"
)

func TestMergeableCaptureTransportOnlyCapturesPullRequestJSON(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	cache := NewMergeableCache()
	body := []byte(`[{"html_url":"https://gitea.test/owner/repo/pulls/1","mergeable":false}]`)
	transport := &MergeableCaptureTransport{
		Cache: cache,
		Base: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodGet, "https://gitea.test/api/v1/repos/owner/repo/pulls", nil)
	require.NoError(err)
	resp, err := transport.RoundTrip(req)
	require.NoError(err)
	copiedBody, err := io.ReadAll(resp.Body)
	require.NoError(err)

	assert.Equal(body, copiedBody)
	mergeable, ok := cache.MergeableForHTMLURL("https://gitea.test/owner/repo/pulls/1")
	require.True(ok)
	require.NotNil(mergeable)
	assert.False(*mergeable)
}

func TestMergeableCaptureTransportIgnoresNonPullRequestResponses(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	cache := NewMergeableCache()
	body := []byte(`{"html_url":"https://gitea.test/owner/repo/pulls/1","mergeable":false}`)
	transport := &MergeableCaptureTransport{
		Cache: cache,
		Base: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodGet, "https://gitea.test/api/v1/repos/owner/repo/issues", nil)
	require.NoError(err)
	resp, err := transport.RoundTrip(req)
	require.NoError(err)
	copiedBody, err := io.ReadAll(resp.Body)
	require.NoError(err)

	assert.Equal(body, copiedBody)
	_, ok := cache.MergeableForHTMLURL("https://gitea.test/owner/repo/pulls/1")
	assert.False(ok)
}

func TestMergeableCaptureTransportSkipsOversizedBodiesWithoutConsumingThem(t *testing.T) {
	assert := Assert.New(t)
	require := Require.New(t)
	cache := NewMergeableCache()
	body := []byte(`{"html_url":"https://gitea.test/owner/repo/pulls/1","mergeable":false,"padding":"` +
		strings.Repeat("x", mergeableCaptureMaxBodyBytes) + `"}`)
	transport := &MergeableCaptureTransport{
		Cache: cache,
		Base: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}),
	}

	req, err := http.NewRequest(http.MethodGet, "https://gitea.test/api/v1/repos/owner/repo/pulls/1", nil)
	require.NoError(err)
	resp, err := transport.RoundTrip(req)
	require.NoError(err)
	copiedBody, err := io.ReadAll(resp.Body)
	require.NoError(err)

	assert.Equal(body, copiedBody)
	_, ok := cache.MergeableForHTMLURL("https://gitea.test/owner/repo/pulls/1")
	assert.False(ok)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
