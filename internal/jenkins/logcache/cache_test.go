package logcache

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCacheCoalescesConcurrentFetches(t *testing.T) {
	r := require.New(t)
	cache, err := New(t.TempDir(), 10, 1024)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(cache.Close()) })

	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	fetch := func(context.Context) (Page, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return Page{Text: []byte("shared"), NextStart: 6, More: true, Truncated: true}, nil
	}

	const callers = 12
	results := make(chan Page, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			page, getErr := cache.Get(t.Context(), Key{Controller: "work", Job: "app", Build: 7, Limit: 1024}, alwaysFresh, fetch)
			results <- page
			errs <- getErr
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(results)
	close(errs)

	r.Equal(int32(1), calls.Load(), "fetch count")
	for getErr := range errs {
		r.NoError(getErr)
	}
	for page := range results {
		r.Equal("shared", string(page.Text))
	}
}

func TestCacheCallerCancellationDoesNotCancelSharedFetch(t *testing.T) {
	r := require.New(t)
	cache, err := New(t.TempDir(), 10, 1024)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(cache.Close()) })

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	fetch := func(ctx context.Context) (Page, error) {
		calls.Add(1)
		close(started)
		<-release
		return Page{Text: []byte("complete")}, ctx.Err()
	}

	ctx, cancel := context.WithCancel(t.Context())
	firstDone := make(chan error, 1)
	go func() {
		_, getErr := cache.Get(ctx, Key{Job: "app", Build: 8, Limit: 1024}, alwaysFresh, fetch)
		firstDone <- getErr
	}()
	<-started
	secondDone := make(chan error, 1)
	go func() {
		page, getErr := cache.Get(t.Context(), Key{Job: "app", Build: 8, Limit: 1024}, alwaysFresh, fetch)
		if getErr == nil && string(page.Text) != "complete" {
			getErr = errors.New("second caller received wrong page")
		}
		secondDone <- getErr
	}()
	cancel()
	r.ErrorIs(<-firstDone, context.Canceled)
	close(release)
	r.NoError(<-secondDone)
	r.Equal(int32(1), calls.Load(), "fetch count")
}

func TestCacheEvictsLeastRecentlyUsedPages(t *testing.T) {
	r := require.New(t)
	cache, err := New(t.TempDir(), 10, 6)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(cache.Close()) })

	var calls atomic.Int32
	fetch := func(text string) func(context.Context) (Page, error) {
		return func(context.Context) (Page, error) {
			calls.Add(1)
			return Page{Text: []byte(text)}, nil
		}
	}
	first := Key{Job: "app", Build: 1, Start: 0, Limit: 4}
	second := Key{Job: "app", Build: 1, Start: 4, Limit: 4}
	_, err = cache.Get(t.Context(), first, alwaysFresh, fetch("aaaa"))
	r.NoError(err)
	_, err = cache.Get(t.Context(), second, alwaysFresh, fetch("bbbb"))
	r.NoError(err)
	_, err = cache.Get(t.Context(), first, alwaysFresh, fetch("aaaa"))
	r.NoError(err)
	r.Equal(int32(3), calls.Load(), "evicted page should be fetched again")
}

func TestCacheRefreshesExpiredPage(t *testing.T) {
	r := require.New(t)
	cache, err := New(t.TempDir(), 10, 1024)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(cache.Close()) })

	now := time.Unix(100, 0)
	cache.now = func() time.Time { return now }
	var calls atomic.Int32
	fetch := func(context.Context) (Page, error) {
		call := calls.Add(1)
		return Page{Text: []byte{byte('0' + call)}}, nil
	}
	key := Key{Job: "app", Build: 2, Limit: 1024}
	freshForSecond := func(_ Page, age time.Duration) bool { return age <= time.Second }
	first, err := cache.Get(t.Context(), key, freshForSecond, fetch)
	r.NoError(err)
	now = now.Add(2 * time.Second)
	second, err := cache.Get(t.Context(), key, freshForSecond, fetch)
	r.NoError(err)
	r.Equal("1", string(first.Text))
	r.Equal("2", string(second.Text))
	r.Equal(int32(2), calls.Load(), "fetch count")
}

func TestCacheSeparatesLogResources(t *testing.T) {
	r := require.New(t)
	cache, err := New(t.TempDir(), 10, 1024)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(cache.Close()) })

	var calls atomic.Int32
	fetch := func(text string) func(context.Context) (Page, error) {
		return func(context.Context) (Page, error) {
			calls.Add(1)
			return Page{Text: []byte(text)}, nil
		}
	}
	base := Key{Controller: "work", Job: "app", Build: 7, Start: 0, Limit: 1024}
	buildKey := base
	buildKey.Resource = "build-console"
	nodeKey := base
	nodeKey.Resource = "pipeline-node:23"
	build, err := cache.Get(t.Context(), buildKey, alwaysFresh, fetch("build"))
	r.NoError(err)
	node, err := cache.Get(t.Context(), nodeKey, alwaysFresh, fetch("node"))
	r.NoError(err)
	r.Equal("build", string(build.Text))
	r.Equal("node", string(node.Text))
	r.Equal(int32(2), calls.Load(), "different log resources must not share cache entries")
}

func TestCacheUsesPrivateFilesAndRemovesDirectoryOnClose(t *testing.T) {
	r := require.New(t)
	cache, err := New(t.TempDir(), 10, 1024)
	r.NoError(err)
	dir := cache.Directory()
	info, err := os.Stat(dir)
	r.NoError(err)
	r.Equal(os.FileMode(0o700), info.Mode().Perm(), "directory permissions")

	_, err = cache.Get(t.Context(), Key{Job: "app", Build: 3, Limit: 1024}, alwaysFresh, func(context.Context) (Page, error) {
		return Page{Text: []byte("secret")}, nil
	})
	r.NoError(err)
	files, err := os.ReadDir(dir)
	r.NoError(err)
	r.Len(files, 1)
	fileInfo, err := files[0].Info()
	r.NoError(err)
	r.Equal(os.FileMode(0o600), fileInfo.Mode().Perm(), "file permissions")

	r.NoError(cache.Close())
	_, err = os.Stat(dir)
	r.ErrorIs(err, os.ErrNotExist)
	_, err = cache.Get(t.Context(), Key{}, alwaysFresh, func(context.Context) (Page, error) { return Page{}, nil })
	r.ErrorIs(err, ErrClosed)
}

func alwaysFresh(Page, time.Duration) bool { return true }
