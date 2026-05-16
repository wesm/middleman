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
	byHTMLURL map[string]*bool
}

func NewMergeableCache() *MergeableCache {
	return &MergeableCache{byHTMLURL: make(map[string]*bool)}
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
	mergeable, ok := c.byHTMLURL[htmlURL]
	if mergeable == nil {
		return nil, ok
	}
	value := *mergeable
	return &value, ok
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

	c.mu.Lock()
	defer c.mu.Unlock()
	c.byHTMLURL[htmlURL] = mergeable
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
	if req == nil || req.URL == nil || req.Method != http.MethodGet || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(contentType, "application/json") {
		return false
	}
	return isPullRequestAPIPath(req.URL.Path)
}

func isPullRequestAPIPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] != "repos" {
			continue
		}
		pullsIndex := i + 3
		if parts[pullsIndex] != "pulls" {
			return false
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
