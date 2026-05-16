package gitealike

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

const mergeableCaptureMaxBodyBytes = 1 << 20

type MergeableCache struct {
	mu        sync.Mutex
	byHTMLURL map[string]mergeableCacheEntry
}

func NewMergeableCache() *MergeableCache {
	return &MergeableCache{byHTMLURL: make(map[string]mergeableCacheEntry)}
}

func (c *MergeableCache) CapturePullRequestJSON(data []byte) {
	if c == nil || len(bytes.TrimSpace(data)) == 0 {
		return
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(data, &item); err != nil {
			return
		}
		items = []map[string]json.RawMessage{item}
	}
	for _, item := range items {
		c.capturePullRequest(item)
	}
}

func (c *MergeableCache) MergeableForHTMLURL(htmlURL string) (*bool, bool) {
	if c == nil || htmlURL == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byHTMLURL[htmlURL]
	if entry.mergeable == nil {
		return nil, ok
	}
	value := *entry.mergeable
	return &value, ok
}

func (c *MergeableCache) MergeableForPullRequest(htmlURL, headSHA, baseSHA string) (*bool, bool) {
	if c == nil || htmlURL == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.byHTMLURL[htmlURL]
	if !ok || !entry.matches(headSHA, baseSHA) {
		return nil, false
	}
	if entry.mergeable == nil {
		return nil, true
	}
	value := *entry.mergeable
	return &value, true
}

func (c *MergeableCache) capturePullRequest(item map[string]json.RawMessage) {
	var htmlURL string
	if raw := item["html_url"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &htmlURL)
	}
	if htmlURL == "" {
		return
	}

	var mergeable *bool
	if raw, ok := item["mergeable"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var value bool
		if err := json.Unmarshal(raw, &value); err == nil {
			mergeable = &value
		}
	}

	headSHA := pullRequestBranchSHA(item["head"])
	baseSHA := pullRequestBranchSHA(item["base"])
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byHTMLURL[htmlURL] = mergeableCacheEntry{
		mergeable: mergeable,
		headSHA:   headSHA,
		baseSHA:   baseSHA,
	}
}

func pullRequestBranchSHA(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var branch struct {
		SHA string `json:"sha"`
	}
	_ = json.Unmarshal(raw, &branch)
	return branch.SHA
}

type mergeableCacheEntry struct {
	mergeable *bool
	headSHA   string
	baseSHA   string
}

func (e mergeableCacheEntry) matches(headSHA, baseSHA string) bool {
	return e.headSHA != "" && headSHA != "" && e.headSHA == headSHA &&
		e.baseSHA != "" && baseSHA != "" && e.baseSHA == baseSHA
}

type MergeableCaptureTransport struct {
	Base  http.RoundTripper
	Cache *MergeableCache
}

func (t *MergeableCaptureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil || t.Cache == nil || !shouldCaptureMergeable(req, resp) {
		return resp, err
	}

	data, readErr := io.ReadAll(io.LimitReader(resp.Body, mergeableCaptureMaxBodyBytes+1))
	if readErr != nil {
		return resp, readErr
	}

	if len(data) > mergeableCaptureMaxBodyBytes {
		resp.Body = &readCloser{
			Reader: io.MultiReader(bytes.NewReader(data), resp.Body),
			close:  resp.Body.Close,
		}
		return resp, nil
	}

	closeErr := resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(data))
	if closeErr != nil {
		return resp, closeErr
	}
	t.Cache.CapturePullRequestJSON(data)
	return resp, nil
}

func shouldCaptureMergeable(req *http.Request, resp *http.Response) bool {
	if req == nil || req.URL == nil || !isMergeableCaptureMethod(req.Method) || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "application/json") {
		return false
	}
	return isPullRequestAPIPath(req.URL.Path)
}

func isMergeableCaptureMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPatch:
		return true
	default:
		return false
	}
}

func isPullRequestAPIPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] != "repos" {
			continue
		}
		pullsIndex := i + 3
		if parts[pullsIndex] != "pulls" {
			continue
		}
		return len(parts) == pullsIndex+1 || len(parts) == pullsIndex+2
	}
	return false
}

type readCloser struct {
	io.Reader
	close func() error
}

func (r *readCloser) Close() error {
	if r.close == nil {
		return nil
	}
	return r.close()
}
