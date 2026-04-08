package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gh "github.com/google/go-github/v84/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestETagTransport_StoresETagOn200(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"))
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"abc123"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req, _ := http.NewRequest("GET", "https://api.github.com/repos/owner/name/pulls", nil)
	resp, err := et.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// Second request should include If-None-Match
	req2, _ := http.NewRequest("GET", "https://api.github.com/repos/owner/name/pulls", nil)
	et.base = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, `"abc123"`, r.Header.Get("If-None-Match"))
		rec := httptest.NewRecorder()
		rec.WriteHeader(304)
		return rec.Result(), nil
	})
	resp2, err := et.RoundTrip(req2)
	require.NoError(t, err)
	assert.Equal(t, 304, resp2.StatusCode)
}

// TestETagTransport_304RefreshesCachedAt verifies that each 304
// response refreshes the cached entry's cachedAt timestamp. Each
// 304 is the server confirming the etag is still valid, so the
// TTL clock should reset — otherwise a stable repo burns one
// unconditional fetch per TTL window for nothing.
func TestETagTransport_304RefreshesCachedAt(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(304)
		return rec.Result(), nil
	})}

	// Seed the cache with an artificially old cachedAt — old enough
	// that a non-refreshing implementation would still treat it as
	// "fresh" for this single round trip, but young enough to be
	// distinguishable from a freshly-set time.Now().
	oldCachedAt := time.Now().Add(-10 * time.Minute)
	et.cache.Store(url, etagEntry{etag: `"e1"`, cachedAt: oldCachedAt})

	req, _ := http.NewRequest("GET", url, nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)

	val, ok := et.cache.Load(url)
	require.True(t, ok)
	got := val.(etagEntry)
	assert.Equal(t, `"e1"`, got.etag, "etag must be preserved across 304")
	assert.WithinDuration(t, time.Now(), got.cachedAt, time.Second,
		"cachedAt must be refreshed to ~now after 304")
}

func TestETagTransport_DifferentURLsIndependent(t *testing.T) {
	callCount := 0
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		callCount++
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"etag`+r.URL.Path+`"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req1, _ := http.NewRequest("GET", "https://api.github.com/repos/a/b/pulls", nil)
	req2, _ := http.NewRequest("GET", "https://api.github.com/repos/c/d/issues", nil)
	_, _ = et.RoundTrip(req1)
	_, _ = et.RoundTrip(req2)

	val1, _ := et.cache.Load("https://api.github.com/repos/a/b/pulls")
	val2, _ := et.cache.Load("https://api.github.com/repos/c/d/issues")
	assert.NotEqual(t, val1.(etagEntry).etag, val2.(etagEntry).etag)
}

func TestETagTransport_PageGt1BypassesETag(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"), "page>1 should not send If-None-Match")
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"should-not-cache"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/n/pulls?page=2", nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)

	_, ok := et.cache.Load(req.URL.String())
	assert.False(t, ok, "page>1 response should not be cached")
}

func TestETagTransport_EmptyETagEvictsCachedEntry(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"first"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	// First request caches an ETag.
	req, _ := http.NewRequest("GET", url, nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)
	_, ok := et.cache.Load(url)
	require.True(t, ok, "first response should cache")

	// Second request returns 200 with NO ETag header. Must evict so
	// the next request does not send a stale If-None-Match.
	et.base = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(200)
		return rec.Result(), nil
	})
	req2, _ := http.NewRequest("GET", url, nil)
	_, err = et.RoundTrip(req2)
	require.NoError(t, err)

	_, ok = et.cache.Load(url)
	assert.False(t, ok, "200 without ETag must evict cached entry")
}

func TestETagTransport_MultiPageEvictsCachedETag(t *testing.T) {
	// First request: single page, cache ETag
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"single"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	url := "https://api.github.com/repos/o/n/pulls"
	req, _ := http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req)
	_, ok := et.cache.Load(url)
	assert.True(t, ok, "single-page ETag should be cached")

	// Second request: multi-page (Link: next), should evict
	et.base = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"multi"`)
		rec.Header().Set("Link", `<https://api.github.com/repos/o/n/pulls?page=2>; rel="next"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})

	req2, _ := http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req2)
	_, ok = et.cache.Load(url)
	assert.False(t, ok, "multi-page response should evict cached ETag")
}

func TestETagTransport_NonGETBypassesCache(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"))
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"should-not-cache"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	for _, method := range []string{"POST", "PATCH", "DELETE"} {
		req, _ := http.NewRequest(method, "https://api.github.com/repos/o/n/pulls", nil)
		_, err := et.RoundTrip(req)
		require.NoError(t, err)
	}

	// Nothing should be cached
	count := 0
	et.cache.Range(func(_, _ any) bool { count++; return true })
	assert.Equal(t, 0, count)
}

func TestETagTransport_NonAllowlistedPathBypassesCache(t *testing.T) {
	// Pre-populate cache with the URL to prove gate blocks it
	url := "https://api.github.com/repos/o/n/commits/abc123/status"
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"))
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"nope"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}
	et.cache.Store(url, etagEntry{etag: `"stale"`, cachedAt: time.Now()})

	req, _ := http.NewRequest("GET", url, nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)
}

func TestETagTransport_AllowlistedPathPositiveControl(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"allowed"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/n/pulls", nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)

	_, ok := et.cache.Load("https://api.github.com/repos/o/n/pulls")
	assert.True(t, ok, "allowlisted path should be cached")
}

func TestETagTransport_SingleMultiSingleTransition(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	phase := 0
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		switch phase {
		case 0: // single-page
			rec.Header().Set("ETag", `"v1"`)
			rec.WriteHeader(200)
		case 1: // multi-page
			rec.Header().Set("ETag", `"v2"`)
			rec.Header().Set("Link", `<https://api.github.com/repos/o/n/pulls?page=2>; rel="next"`)
			rec.WriteHeader(200)
		case 2: // back to single-page
			rec.Header().Set("ETag", `"v3"`)
			rec.WriteHeader(200)
		}
		return rec.Result(), nil
	})}

	// Phase 0: single-page, should cache
	phase = 0
	req, _ := http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req)
	_, ok := et.cache.Load(url)
	assert.True(t, ok, "phase 0: single-page should cache")

	// Phase 1: multi-page, should evict
	phase = 1
	req, _ = http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req)
	_, ok = et.cache.Load(url)
	assert.False(t, ok, "phase 1: multi-page should evict")

	// Phase 2: back to single-page, should re-cache
	phase = 2
	req, _ = http.NewRequest("GET", url, nil)
	_, _ = et.RoundTrip(req)
	val, ok := et.cache.Load(url)
	assert.True(t, ok, "phase 2: single-page again should cache")
	assert.Equal(t, `"v3"`, val.(etagEntry).etag)
}

func TestETagTransport_TTLDrivenMultiPageDetection(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	requestCount := 0
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		rec := httptest.NewRecorder()
		if r.Header.Get("If-None-Match") != "" {
			// 304 — do NOT refresh cachedAt
			rec.WriteHeader(304)
			return rec.Result(), nil
		}
		// Unconditional fetch after TTL — now multi-page
		rec.Header().Set("ETag", `"multi"`)
		rec.Header().Set("Link", `<https://api.github.com/repos/o/n/pulls?page=2>; rel="next"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	// Initial cache with artificially old cachedAt (just under TTL)
	et.cache.Store(url, etagEntry{
		etag:     `"old"`,
		cachedAt: time.Now().Add(-etagTTL + time.Minute),
	})

	// Request with valid cache — sends If-None-Match, gets 304
	req, _ := http.NewRequest("GET", url, nil)
	resp, _ := et.RoundTrip(req)
	assert.Equal(t, 304, resp.StatusCode)

	// Now expire the cache
	et.cache.Store(url, etagEntry{
		etag:     `"old"`,
		cachedAt: time.Now().Add(-etagTTL - time.Minute),
	})

	// Request with expired cache — no If-None-Match, gets 200 multi-page
	req, _ = http.NewRequest("GET", url, nil)
	resp, _ = et.RoundTrip(req)
	assert.Equal(t, 200, resp.StatusCode)

	// Cache should be evicted (multi-page detected)
	_, ok := et.cache.Load(url)
	assert.False(t, ok, "multi-page detection after TTL should evict")
}

func TestETagTransport_ExpiredEntryTreatedAsUncached(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"), "expired entry should not send If-None-Match")
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"fresh"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	// Store expired entry
	et.cache.Store(url, etagEntry{etag: `"old"`, cachedAt: time.Now().Add(-etagTTL - time.Minute)})

	req, _ := http.NewRequest("GET", url, nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)

	val, _ := et.cache.Load(url)
	assert.Equal(t, `"fresh"`, val.(etagEntry).etag)
}

func TestETagTransport_DetailEndpointBypassesCache(t *testing.T) {
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		assert.Empty(t, r.Header.Get("If-None-Match"), "detail endpoints must bypass cache")
		rec := httptest.NewRecorder()
		rec.Header().Set("ETag", `"should-not-cache"`)
		rec.WriteHeader(200)
		return rec.Result(), nil
	})}

	for _, url := range []string{
		"https://api.github.com/repos/o/n/pulls/123",
		"https://api.github.com/repos/o/n/pulls/123/files",
		"https://api.github.com/repos/o/n/issues/456",
		"https://api.github.com/repos/o/n/issues/456/comments",
	} {
		req, _ := http.NewRequest("GET", url, nil)
		_, err := et.RoundTrip(req)
		require.NoError(t, err)
		_, ok := et.cache.Load(url)
		assert.False(t, ok, "%s should not be cached", url)
	}
}

// TestETagTransport_304ExtendsCacheLifetime verifies that a steady
// stream of 304s keeps the cached entry alive indefinitely. Without
// the cachedAt refresh on 304, the TTL fires from the timestamp of
// the original 200, forcing an unconditional refetch every TTL
// window even when nothing about the resource has changed.
func TestETagTransport_304ExtendsCacheLifetime(t *testing.T) {
	url := "https://api.github.com/repos/o/n/pulls"
	requestCount := 0
	unconditionalCount := 0
	et := &etagTransport{base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requestCount++
		rec := httptest.NewRecorder()
		if r.Header.Get("If-None-Match") == "" {
			unconditionalCount++
			rec.Header().Set("ETag", `"e1"`)
			rec.WriteHeader(200)
			return rec.Result(), nil
		}
		rec.WriteHeader(304)
		return rec.Result(), nil
	})}

	// First request: unconditional, caches the etag.
	req, _ := http.NewRequest("GET", url, nil)
	_, err := et.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, 1, unconditionalCount)

	// Simulate a long stable run. Between each round trip, age the
	// cache entry by just under a TTL — if cachedAt were never
	// refreshed, the second of these would expire it. The refresh
	// keeps it alive across an unbounded number of cycles.
	for i := range 5 {
		val, ok := et.cache.Load(url)
		require.True(t, ok, "entry must persist across 304 #%d", i)
		entry := val.(etagEntry)
		entry.cachedAt = time.Now().Add(-etagTTL + time.Second)
		et.cache.Store(url, entry)

		req, _ := http.NewRequest("GET", url, nil)
		resp, err := et.RoundTrip(req)
		require.NoError(t, err)
		require.Equal(t, 304, resp.StatusCode)
	}

	// Only the very first request should have been unconditional.
	// Every subsequent request must have sent If-None-Match and got 304.
	assert.Equal(t, 1, unconditionalCount,
		"304 must extend the cache so no unconditional refetch occurs on a stable repo")
	assert.Equal(t, 6, requestCount, "all 6 round trips should have hit the base transport")
}

func TestIsNotModified(t *testing.T) {
	resp304 := &http.Response{StatusCode: 304}
	err304 := &gh.ErrorResponse{Response: resp304}
	assert.True(t, IsNotModified(err304))

	resp403 := &http.Response{StatusCode: 403}
	err403 := &gh.ErrorResponse{Response: resp403}
	assert.False(t, IsNotModified(err403))

	assert.False(t, IsNotModified(errors.New("random error")))

	errNilResp := &gh.ErrorResponse{Response: nil}
	assert.False(t, IsNotModified(errNilResp), "nil Response should not panic")
}
