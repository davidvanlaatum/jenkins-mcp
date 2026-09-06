package jenkins

import (
	"container/list"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	apperrors "github.com/david/jenkins-mcp/internal/errors"
)

// Snapshots are immutable and shared across the process, like the former signing
// key. Bound both the number of entries and serialized payload bytes. Keeping
// bytes rather than pointers prevents callers from mutating cached snapshots.
const watchStateIdleTTL = 30 * time.Minute

var watchStates = newWatchCache(1024, 32*1024*1024)

type watchCache struct {
	mu                          sync.Mutex
	timer                       *time.Timer
	lru                         *list.List
	byID                        map[string]*list.Element
	byContent                   map[[32]byte]*list.Element
	maxEntries, maxBytes, bytes int
}

type watchCacheEntry struct {
	id, kind  string
	payload   []byte
	digest    [32]byte
	expiresAt time.Time
}

func newWatchCache(maxEntries, maxBytes int) *watchCache {
	return &watchCache{lru: list.New(), byID: make(map[string]*list.Element), byContent: make(map[[32]byte]*list.Element), maxEntries: maxEntries, maxBytes: maxBytes}
}

func (c *watchCache) put(kind string, state any) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", apperrors.Wrap(apperrors.CodeJenkins, "failed to encode watch state", err.Error())
	}
	if len(payload) > maxWatchStateUncompressedBytes || len(payload) > c.maxBytes {
		return "", apperrors.Wrap(apperrors.CodeJenkins, "watch state too large", map[string]any{"maxBytes": min(maxWatchStateUncompressedBytes, c.maxBytes)})
	}
	digest := sha256.Sum256(append([]byte(kind+"\x00"), payload...))
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.expireLocked(now)
	if element := c.byContent[digest]; element != nil {
		c.touchLocked(element, now)
		return element.Value.(watchCacheEntry).id, nil
	}
	var id string
	for {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", apperrors.Wrap(apperrors.CodeUnavailable, "failed to create watch state ID", err.Error())
		}
		id = base64.RawURLEncoding.EncodeToString(random[:])
		if c.byID[id] == nil {
			break
		}
	}
	for c.lru.Len() >= c.maxEntries || c.bytes+len(payload) > c.maxBytes {
		element := c.lru.Back()
		c.removeLocked(element)
	}
	entry := watchCacheEntry{id: id, kind: kind, payload: payload, digest: digest, expiresAt: now.Add(watchStateIdleTTL)}
	element := c.lru.PushFront(entry)
	c.byID[id], c.byContent[digest] = element, element
	c.bytes += len(payload)
	c.scheduleExpiryLocked()
	return id, nil
}

func (c *watchCache) get(id, kind string, state any) error {
	c.mu.Lock()
	now := time.Now()
	c.expireLocked(now)
	element := c.byID[id]
	if element == nil || element.Value.(watchCacheEntry).kind != kind {
		c.mu.Unlock()
		return apperrors.Wrap(apperrors.CodeInvalidRequest, "watch state expired or invalid; you need to re-bootstrap without lastState", map[string]any{"rebootstrap": true})
	}
	c.touchLocked(element, now)
	payload := element.Value.(watchCacheEntry).payload
	c.mu.Unlock()
	if err := json.Unmarshal(payload, state); err != nil {
		return apperrors.Wrap(apperrors.CodeJenkins, "failed to read watch state", err.Error())
	}
	return nil
}

// All helpers below require mu. LRU order also orders idle deadlines, so expiry
// only needs to inspect the oldest entries.
func (c *watchCache) removeLocked(element *list.Element) {
	entry := element.Value.(watchCacheEntry)
	delete(c.byID, entry.id)
	delete(c.byContent, entry.digest)
	c.bytes -= len(entry.payload)
	c.lru.Remove(element)
}

func (c *watchCache) touchLocked(element *list.Element, now time.Time) {
	entry := element.Value.(watchCacheEntry)
	entry.expiresAt = now.Add(watchStateIdleTTL)
	element.Value = entry
	c.lru.MoveToFront(element)
}

func (c *watchCache) expireLocked(now time.Time) {
	for element := c.lru.Back(); element != nil; element = c.lru.Back() {
		if element.Value.(watchCacheEntry).expiresAt.After(now) {
			return
		}
		c.removeLocked(element)
	}
}

// One timer per nonempty cache removes expired entries without needing another
// request. Touches may leave an early timer in place; it rechecks deadlines and
// schedules the next expiry. No timer is retained after the cache empties.
func (c *watchCache) scheduleExpiryLocked() {
	if c.timer != nil || c.lru.Len() == 0 {
		return
	}
	next := c.lru.Back().Value.(watchCacheEntry).expiresAt
	c.timer = time.AfterFunc(time.Until(next), func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.timer = nil
		c.expireLocked(time.Now())
		c.scheduleExpiryLocked()
	})
}
