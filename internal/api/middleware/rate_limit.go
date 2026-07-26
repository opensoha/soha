package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateLimitEntry struct {
	count       int
	windowStart time.Time
}

type BoundedRateLimiter struct {
	mu         sync.Mutex
	entries    map[string]rateLimitEntry
	maxEntries int
	now        func() time.Time
}

func NewBoundedRateLimiter(maxEntries int) *BoundedRateLimiter {
	if maxEntries <= 0 {
		maxEntries = 10_000
	}
	return &BoundedRateLimiter{entries: make(map[string]rateLimitEntry), maxEntries: maxEntries, now: time.Now}
}

func (l *BoundedRateLimiter) Middleware(class string, limit int, window time.Duration, identifier func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := ""
		if identifier != nil {
			id = strings.TrimSpace(identifier(c))
		}
		key := strings.TrimSpace(class) + "|" + c.ClientIP() + "|" + id
		allowed, retryAfter := l.allow(key, limit, window)
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate_limited",
				"message": "too many requests",
			})
			return
		}
		c.Next()
	}
}

func (l *BoundedRateLimiter) allow(key string, limit int, window time.Duration) (bool, int) {
	if limit <= 0 || window <= 0 {
		return true, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, exists := l.entries[key]
	if !exists || now.Sub(entry.windowStart) >= window {
		l.makeRoom(now, window)
		l.entries[key] = rateLimitEntry{count: 1, windowStart: now}
		return true, 0
	}
	if entry.count >= limit {
		remaining := entry.windowStart.Add(window).Sub(now)
		retryAfter := int((remaining + time.Second - 1) / time.Second)
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, retryAfter
	}
	entry.count++
	l.entries[key] = entry
	return true, 0
}

func (l *BoundedRateLimiter) makeRoom(now time.Time, window time.Duration) {
	for key, entry := range l.entries {
		if now.Sub(entry.windowStart) >= window {
			delete(l.entries, key)
		}
	}
	if len(l.entries) < l.maxEntries {
		return
	}
	// ponytail: O(n) eviction is bounded at 10k keys; use an ordered cache if churn becomes measurable.
	var oldestKey string
	var oldest time.Time
	for key, entry := range l.entries {
		if oldestKey == "" || entry.windowStart.Before(oldest) {
			oldestKey, oldest = key, entry.windowStart
		}
	}
	delete(l.entries, oldestKey)
}
