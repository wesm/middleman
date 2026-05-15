package gitealike

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

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
	if err != nil || resp == nil || resp.Body == nil || t.Cache == nil {
		return resp, err
	}

	data, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(data))
	if readErr != nil {
		return resp, readErr
	}
	if closeErr != nil {
		return resp, closeErr
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		t.Cache.CapturePullRequestJSON(data)
	}
	return resp, nil
}
