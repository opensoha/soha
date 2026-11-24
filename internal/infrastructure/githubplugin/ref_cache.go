package githubplugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	refCacheEntries = 128
	refCacheFlights = 8
	refCacheTTL     = time.Minute
	refErrorBackoff = 5 * time.Second
)

type refPageCache struct {
	mu      sync.Mutex
	entries map[string]*refCacheEntry
	active  int
}

type cachedRefPage struct {
	list domainplugin.PluginRepositoryRefList
	etag string
}

type refCacheEntry struct {
	value   cachedRefPage
	err     error
	expires time.Time
	flight  *refCacheFlight
}

type refCacheFlight struct {
	done  chan struct{}
	value cachedRefPage
	err   error
}

func (cache *refPageCache) load(
	ctx context.Context, key string, fetch func(context.Context, cachedRefPage) (cachedRefPage, error),
) (domainplugin.PluginRepositoryRefList, error) {
	if err := ctx.Err(); err != nil {
		return domainplugin.PluginRepositoryRefList{}, err
	}
	cache.mu.Lock()
	entry := cache.entries[key]
	if entry != nil && time.Now().Before(entry.expires) {
		value, err := entry.value, entry.err
		cache.mu.Unlock()
		return refPageResult(value, err)
	}
	flight, err := cache.flight(key, entry, fetch)
	cache.mu.Unlock()
	if err != nil {
		return domainplugin.PluginRepositoryRefList{}, err
	}
	select {
	case <-ctx.Done():
		return domainplugin.PluginRepositoryRefList{}, ctx.Err()
	case <-flight.done:
		return refPageResult(flight.value, flight.err)
	}
}

// flight runs with the cache locked. Each page has one shared refresh at a time.
func (cache *refPageCache) flight(
	key string, entry *refCacheEntry, fetch func(context.Context, cachedRefPage) (cachedRefPage, error),
) (*refCacheFlight, error) {
	if entry != nil && entry.flight != nil {
		return entry.flight, nil
	}
	if cache.active == refCacheFlights {
		return nil, fmt.Errorf("%w: github repository page requests are busy", apperrors.ErrServiceUnavailable)
	}
	if entry == nil {
		if cache.entries == nil {
			cache.entries = make(map[string]*refCacheEntry)
		}
		if len(cache.entries) == refCacheEntries {
			cache.evict()
		}
		entry = &refCacheEntry{}
		cache.entries[key] = entry
	}
	flight := &refCacheFlight{done: make(chan struct{})}
	entry.flight = flight
	cache.active++
	go cache.refresh(entry, flight, fetch)
	return flight, nil
}

func (cache *refPageCache) evict() {
	var oldestKey string
	var oldest *refCacheEntry
	for key, entry := range cache.entries {
		if entry.flight == nil && (oldest == nil || entry.expires.Before(oldest.expires)) {
			oldestKey, oldest = key, entry
		}
	}
	delete(cache.entries, oldestKey)
}

func (cache *refPageCache) refresh(
	entry *refCacheEntry, flight *refCacheFlight, fetch func(context.Context, cachedRefPage) (cachedRefPage, error),
) {
	// A caller can leave without aborting another caller's refresh.
	ctx, cancel := context.WithTimeout(context.Background(), 2*requestTimeout)
	defer cancel()
	flight.value, flight.err = fetch(ctx, entry.value)
	if ctx.Err() != nil {
		flight.err = fmt.Errorf("%w: %w", apperrors.ErrServiceUnavailable, ctx.Err())
	}
	now := time.Now()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry.err = flight.err
	entry.expires = now.Add(refCacheTTL)
	if flight.err == nil || errors.Is(flight.err, apperrors.ErrNotFound) {
		entry.value = flight.value
	}
	if flight.err != nil {
		entry.expires = now.Add(refErrorBackoff)
		var rateLimit *rateLimitError
		if errors.As(flight.err, &rateLimit) && rateLimit.until.After(entry.expires) {
			entry.expires = rateLimit.until
		}
	}
	entry.flight = nil
	cache.active--
	close(flight.done)
}

func refPageResult(value cachedRefPage, err error) (domainplugin.PluginRepositoryRefList, error) {
	if err != nil {
		return domainplugin.PluginRepositoryRefList{}, err
	}
	result := value.list
	result.Refs = slices.Clone(result.Refs)
	for index := range result.Refs {
		if publishedAt := result.Refs[index].PublishedAt; publishedAt != nil {
			copyTime := *publishedAt
			result.Refs[index].PublishedAt = &copyTime
		}
	}
	return result, nil
}

type rateLimitError struct{ until time.Time }

func (*rateLimitError) Error() string { return ErrRateLimited.Error() }
func (*rateLimitError) Unwrap() error { return ErrRateLimited }

func rateLimitUntil(now time.Time, header http.Header) time.Time {
	const defaultBackoff = time.Minute
	const maxBackoff = time.Hour
	if retry := header.Get("Retry-After"); retry != "" {
		if seconds, err := strconv.ParseInt(retry, 10, 64); err == nil && seconds > 0 {
			return now.Add(time.Duration(min(seconds, int64(maxBackoff/time.Second))) * time.Second)
		}
		if retryAt, err := http.ParseTime(retry); err == nil && retryAt.After(now) {
			return now.Add(min(retryAt.Sub(now), maxBackoff))
		}
	}
	if header.Get("X-RateLimit-Remaining") == "0" {
		if epoch, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			if delay := time.Unix(epoch, 0).Sub(now); delay > 0 {
				return now.Add(min(delay, maxBackoff))
			}
		}
	}
	return now.Add(defaultBackoff)
}
