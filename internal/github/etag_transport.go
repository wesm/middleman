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
	if req == nil || req.URL == nil {
		return nil, errors.New("nil request")
	}

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
		// Deliberately do NOT refresh cachedAt here. The TTL must
		// expire periodically so we issue an unconditional fetch
		// that can detect list growth beyond page 1 (a single-page
		// list that grows to two pages returns 304 for page 1 since
		// the first page content is unchanged). One unconditional
		// fetch per TTL window is a cheap safety net.
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

// invalidateRepo drops cached ETag entries for the given repo's list
// endpoints. The endpoints parameter controls which to invalidate:
// "pulls", "issues", or both. Used by the sync engine to force an
// unconditional refetch after a partial failure so the next cycle
// re-applies per-item state that the previous cycle failed to persist.
func (t *etagTransport) invalidateRepo(owner, name string, endpoints ...string) {
	base := "/repos/" + owner + "/" + name + "/"
	gheBase := "/api/v3/repos/" + owner + "/" + name + "/"
	var prefixes []string
	for _, ep := range endpoints {
		prefixes = append(prefixes, base+ep, gheBase+ep)
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
	if !errors.As(err, &ghErr) || ghErr == nil || ghErr.Response == nil {
		return false
	}
	return ghErr.Response.StatusCode == http.StatusNotModified
}
