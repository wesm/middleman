package github

import (
	"errors"
	"net/http"
	urlpkg "net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	gh "github.com/google/go-github/v84/github"
)

const etagTTL = 30 * time.Minute

// etagEligiblePath matches list endpoints that return a collection
// ETag. Supports both github.com (`/repos/{owner}/{name}/{pulls,issues}`)
// and GitHub Enterprise (`/api/v3/repos/...`), since GHE clients route
// through the same RoundTripper.
var etagEligiblePath = regexp.MustCompile(
	`^(?:/api/v3)?/repos/[^/]+/[^/]+/(pulls|issues)$`,
)

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

// invalidateRepo drops any cached ETag entries whose URL path targets
// the given repo's PR or issue list endpoints. Used by the sync engine
// to force an unconditional refetch after a partial failure so the
// next cycle re-applies per-item state that the previous cycle failed
// to persist. The cache is keyed by full URL (including query string),
// so this iterates the sync.Map and deletes by path match rather than
// computing the exact URL up front.
func (t *etagTransport) invalidateRepo(owner, name string) {
	prefixes := []string{
		"/repos/" + owner + "/" + name + "/pulls",
		"/repos/" + owner + "/" + name + "/issues",
		"/api/v3/repos/" + owner + "/" + name + "/pulls",
		"/api/v3/repos/" + owner + "/" + name + "/issues",
	}
	t.cache.Range(func(k, _ any) bool {
		urlStr, ok := k.(string)
		if !ok {
			return true
		}
		parsed, err := urlpkg.Parse(urlStr)
		if err != nil {
			return true
		}
		if slices.Contains(prefixes, parsed.Path) {
			t.cache.Delete(k)
		}
		return true
	})
}

// IsNotModified returns true if the error represents a 304 Not Modified
// response from the GitHub API.
func IsNotModified(err error) bool {
	var ghErr *gh.ErrorResponse
	return errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotModified
}
