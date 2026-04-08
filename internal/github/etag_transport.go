package github

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v84/github"
)

const etagTTL = 30 * time.Minute

var etagEligiblePath = regexp.MustCompile(`^/repos/[^/]+/[^/]+/(pulls|issues)$`)

type etagEntry struct {
	etag     string
	cachedAt time.Time
}

type etagTransport struct {
	base  http.RoundTripper
	cache sync.Map // URL string -> etagEntry
}

func (t *etagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Gate: only GET requests to allowlisted endpoints
	if req.Method != http.MethodGet || !isETagEligible(req.URL.Path) {
		return t.base.RoundTrip(req)
	}

	// Skip later pages
	if page := req.URL.Query().Get("page"); page != "" && page != "1" {
		return t.base.RoundTrip(req)
	}

	url := req.URL.String()

	// Check cache
	if val, ok := t.cache.Load(url); ok {
		entry := val.(etagEntry)
		if time.Since(entry.cachedAt) < etagTTL {
			req = req.Clone(req.Context())
			req.Header.Set("If-None-Match", entry.etag)
		} else {
			t.cache.Delete(url)
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		etag := resp.Header.Get("ETag")
		if etag != "" && !hasLinkNext(resp) {
			t.cache.Store(url, etagEntry{etag: etag, cachedAt: time.Now()})
		} else {
			// No ETag, or response is paginated — drop any stale
			// validator so the next request fetches fresh data
			// instead of asserting an out-of-date If-None-Match.
			t.cache.Delete(url)
		}
	case http.StatusNotModified:
		// Each 304 is GitHub confirming the etag is still valid.
		// Refresh cachedAt so the TTL means "max time since the
		// last server confirmation," not "max time since first
		// cached." Without this, a stable repo burns one
		// unconditional fetch per TTL window for nothing.
		if val, ok := t.cache.Load(url); ok {
			entry := val.(etagEntry)
			entry.cachedAt = time.Now()
			t.cache.Store(url, entry)
		}
	}

	return resp, nil
}

func isETagEligible(path string) bool {
	return etagEligiblePath.MatchString(path)
}

func hasLinkNext(resp *http.Response) bool {
	for _, link := range resp.Header.Values("Link") {
		if strings.Contains(link, `rel="next"`) {
			return true
		}
	}
	return false
}

// IsNotModified returns true if the error represents a 304 Not Modified
// response from the GitHub API.
func IsNotModified(err error) bool {
	var ghErr *gh.ErrorResponse
	return errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotModified
}
