package logcache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const directoryPattern = "jenkins-mcp-log-cache-*"

var ErrClosed = errors.New("log cache is closed")

type Key struct {
	Controller string
	Job        string
	Build      int
	Resource   string
	Start      int64
	Limit      int64
}

type Page struct {
	Text      []byte
	NextStart int64
	TotalSize int64
	More      bool
	Truncated bool
	Complete  bool
}

type Freshness func(Page, time.Duration) bool

type Cache struct {
	mu         sync.Mutex
	dir        string
	maxEntries int
	maxBytes   int64
	bytes      int64
	closed     bool
	now        func() time.Time
	lru        *list.List
	entries    map[string]*list.Element
	inflight   map[string]*flight
}

type entry struct {
	digest    string
	path      string
	page      Page
	size      int64
	fetchedAt time.Time
}

type flight struct {
	done chan struct{}
	page Page
	err  error
}

func New(parentDir string, maxEntries int, maxBytes int64) (*Cache, error) {
	if maxEntries <= 0 {
		return nil, errors.New("log cache max entries must be positive")
	}
	if maxBytes <= 0 {
		return nil, errors.New("log cache max bytes must be positive")
	}
	dir, err := os.MkdirTemp(parentDir, directoryPattern)
	if err != nil {
		return nil, fmt.Errorf("create log cache directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("set log cache directory permissions: %w", err)
	}
	return &Cache{
		dir:        dir,
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		now:        time.Now,
		lru:        list.New(),
		entries:    make(map[string]*list.Element),
		inflight:   make(map[string]*flight),
	}, nil
}

// Get returns a cached page or coalesces callers behind one fetch. The caller
// decides whether a stored page is still fresh; concurrent cache misses are
// coalesced even when the freshness function always returns false.
func (c *Cache) Get(ctx context.Context, key Key, fresh Freshness, fetch func(context.Context) (Page, error)) (Page, error) {
	digest := keyDigest(key)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return Page{}, ErrClosed
	}
	if element := c.entries[digest]; element != nil {
		cached := element.Value.(entry)
		if fresh(clonePage(cached.page), c.now().Sub(cached.fetchedAt)) {
			page, err := c.readLocked(cached)
			if err == nil {
				c.mu.Unlock()
				return page, nil
			}
			c.removeLocked(element)
		} else {
			c.removeLocked(element)
		}
	}
	if active := c.inflight[digest]; active != nil {
		c.mu.Unlock()
		return wait(ctx, active)
	}
	active := &flight{done: make(chan struct{})}
	c.inflight[digest] = active
	c.mu.Unlock()

	go c.fetch(context.WithoutCancel(ctx), digest, active, fetch)
	return wait(ctx, active)
}

func (c *Cache) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.entries = make(map[string]*list.Element)
	c.lru.Init()
	c.bytes = 0
	dir := c.dir
	c.mu.Unlock()
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove log cache directory: %w", err)
	}
	return nil
}

func (c *Cache) Directory() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dir
}

func (c *Cache) fetch(ctx context.Context, digest string, active *flight, fetch func(context.Context) (Page, error)) {
	page, err := fetch(ctx)
	page = clonePage(page)

	c.mu.Lock()
	if err == nil && !c.closed {
		// A cache write failure must not turn a successful Jenkins request into a
		// failed tool call. The fetched page is still returned to every waiter.
		_ = c.storeLocked(digest, page)
	}
	active.page = page
	active.err = err
	delete(c.inflight, digest)
	close(active.done)
	c.mu.Unlock()
}

func (c *Cache) storeLocked(digest string, page Page) error {
	size := int64(len(page.Text))
	if size > c.maxBytes {
		return nil
	}
	for c.lru.Len() >= c.maxEntries || c.bytes+size > c.maxBytes {
		c.removeLocked(c.lru.Back())
	}
	path := filepath.Join(c.dir, digest+".log")
	if err := os.WriteFile(path, page.Text, 0o600); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write log cache page: %w", err)
	}
	page.Text = nil
	cached := entry{digest: digest, path: path, page: page, size: size, fetchedAt: c.now()}
	element := c.lru.PushFront(cached)
	c.entries[digest] = element
	c.bytes += size
	return nil
}

func (c *Cache) readLocked(cached entry) (Page, error) {
	text, err := os.ReadFile(cached.path)
	if err != nil {
		return Page{}, fmt.Errorf("read log cache page: %w", err)
	}
	element := c.entries[cached.digest]
	c.lru.MoveToFront(element)
	page := cached.page
	page.Text = text
	return page, nil
}

func (c *Cache) removeLocked(element *list.Element) {
	if element == nil {
		return
	}
	cached := element.Value.(entry)
	delete(c.entries, cached.digest)
	c.lru.Remove(element)
	c.bytes -= cached.size
	_ = os.Remove(cached.path)
}

func wait(ctx context.Context, active *flight) (Page, error) {
	select {
	case <-ctx.Done():
		return Page{}, ctx.Err()
	case <-active.done:
		return clonePage(active.page), active.err
	}
}

func keyDigest(key Key) string {
	raw := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%d\x00%d", key.Controller, key.Job, key.Build, key.Resource, key.Start, key.Limit)
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func clonePage(page Page) Page {
	page.Text = append([]byte(nil), page.Text...)
	return page
}
