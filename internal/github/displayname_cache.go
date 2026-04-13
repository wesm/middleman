package github

import (
	"container/list"
	"sync"
	"time"
)

// displayNameCache is a bounded TTL + LRU cache for GitHub
// display-name lookups. Entries self-expire after a TTL that
// differs by outcome so a transient 404 does not suppress a
// re-lookup for hours. When full, least-recently-used entries
// are evicted on insert.
//
// The cache is safe for concurrent use. Singleflight dedup of
// upstream API calls is the caller's responsibility.
type displayNameCache struct {
	mu         sync.Mutex
	entries    map[string]*list.Element
	lru        *list.List
	maxSize    int
	successTTL time.Duration
	failureTTL time.Duration
	now        func() time.Time
}

// displayNameEntry is one cache entry. The exported-looking
// Name and Ok fields are read by callers that want to use a
// stale value as a fallback after a refetch failure.
type displayNameEntry struct {
	key       string
	name      string
	expiresAt time.Time
	ok        bool
}

// newDisplayNameCache creates an empty cache. maxSize must be
// positive.
func newDisplayNameCache(
	maxSize int, successTTL, failureTTL time.Duration,
) *displayNameCache {
	if maxSize <= 0 {
		maxSize = 1
	}
	return &displayNameCache{
		entries:    make(map[string]*list.Element, maxSize),
		lru:        list.New(),
		maxSize:    maxSize,
		successTTL: successTTL,
		failureTTL: failureTTL,
		now:        time.Now,
	}
}

// get returns the cached entry for key and whether it is fresh.
// fresh is true when the entry exists and has not expired. When
// the entry exists but is expired, fresh is false and the
// returned entry is the stale value — callers may fall back to
// it if a refetch fails. On miss, returns a zero entry and
// false.
func (c *displayNameCache) get(
	key string,
) (displayNameEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.entries[key]
	if !ok {
		return displayNameEntry{}, false
	}
	entry := elem.Value.(*displayNameEntry)
	if c.now().After(entry.expiresAt) {
		return *entry, false
	}
	c.lru.MoveToFront(elem)
	return *entry, true
}

// putSuccess records a successful lookup. Resets TTL.
func (c *displayNameCache) putSuccess(key, name string) {
	c.put(displayNameEntry{
		key:       key,
		name:      name,
		ok:        true,
		expiresAt: c.now().Add(c.successTTL),
	})
}

// putFailure records a failed lookup with the shorter
// failureTTL so the failure does not stick indefinitely.
func (c *displayNameCache) putFailure(key string) {
	c.put(displayNameEntry{
		key:       key,
		ok:        false,
		expiresAt: c.now().Add(c.failureTTL),
	})
}

// putStaleFallback records an entry that keeps a previously
// resolved name but uses the shorter failureTTL for its
// expiry. Used when a refresh call fails and the caller wants
// to keep serving the last known name without hammering the
// upstream API every sync.
func (c *displayNameCache) putStaleFallback(key, name string) {
	c.put(displayNameEntry{
		key:       key,
		name:      name,
		ok:        true,
		expiresAt: c.now().Add(c.failureTTL),
	})
}

func (c *displayNameCache) put(entry displayNameEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.entries[entry.key]; ok {
		*elem.Value.(*displayNameEntry) = entry
		c.lru.MoveToFront(elem)
		return
	}
	if c.lru.Len() >= c.maxSize {
		oldest := c.lru.Back()
		if oldest != nil {
			delete(c.entries, oldest.Value.(*displayNameEntry).key)
			c.lru.Remove(oldest)
		}
	}
	elem := c.lru.PushFront(&entry)
	c.entries[entry.key] = elem
}

// len returns the number of entries currently held. Test helper.
func (c *displayNameCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.Len()
}
