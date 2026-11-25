package appgateway

import (
	"sync"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const maxDecisionCacheEntries = 10_000

type decisionCacheKey struct {
	Host      string
	SessionID string
	UserID    string
}

type decisionCacheEntry struct {
	decision  sohaapi.HostedAppGatewayAccessDecision
	expiresAt time.Time
}

type decisionCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
	entries    map[decisionCacheKey]decisionCacheEntry
}

func newDecisionCache(ttl time.Duration, now func() time.Time) *decisionCache {
	if now == nil {
		now = time.Now
	}
	return &decisionCache{
		ttl:        ttl,
		maxEntries: maxDecisionCacheEntries,
		now:        now,
		entries:    make(map[decisionCacheKey]decisionCacheEntry),
	}
}

func (c *decisionCache) get(key decisionCacheKey) (sohaapi.HostedAppGatewayAccessDecision, bool) {
	if c == nil || c.ttl <= 0 || key.Host == "" || key.SessionID == "" || key.UserID == "" {
		return sohaapi.HostedAppGatewayAccessDecision{}, false
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return sohaapi.HostedAppGatewayAccessDecision{}, false
	}
	if !now.Before(entry.expiresAt) {
		delete(c.entries, key)
		return sohaapi.HostedAppGatewayAccessDecision{}, false
	}
	return cloneDecision(entry.decision), true
}

func (c *decisionCache) put(key decisionCacheKey, decision sohaapi.HostedAppGatewayAccessDecision, sessionExpiresAt time.Time) {
	if c == nil || c.ttl <= 0 || key.Host == "" || key.SessionID == "" || key.UserID == "" {
		return
	}
	if decision.Decision != sohaapi.HostedAppAccessDecisionKindAllow {
		return
	}
	now := c.now()
	expiresAt := now.Add(c.ttl)
	if !sessionExpiresAt.IsZero() && sessionExpiresAt.Before(expiresAt) {
		expiresAt = sessionExpiresAt
	}
	if !now.Before(expiresAt) {
		return
	}
	cloned := cloneDecision(decision)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		c.evictLocked(now)
	}
	c.entries[key] = decisionCacheEntry{
		decision:  cloned,
		expiresAt: expiresAt,
	}
}

func (c *decisionCache) invalidateSession(host, sessionID string) {
	if c == nil || sessionID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if k.SessionID == sessionID && (host == "" || k.Host == host) {
			delete(c.entries, k)
		}
	}
}

func (c *decisionCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
}

func (c *decisionCache) evictLocked(now time.Time) {
	var earliestKey decisionCacheKey
	var hasEarliest bool
	var earliestTime time.Time
	for k, v := range c.entries {
		if !now.Before(v.expiresAt) {
			delete(c.entries, k)
			continue
		}
		if !hasEarliest || v.expiresAt.Before(earliestTime) {
			earliestKey = k
			earliestTime = v.expiresAt
			hasEarliest = true
		}
	}
	if len(c.entries) >= c.maxEntries && hasEarliest {
		delete(c.entries, earliestKey)
	}
}

func cloneDecision(in sohaapi.HostedAppGatewayAccessDecision) sohaapi.HostedAppGatewayAccessDecision {
	out := in
	if len(in.Files) > 0 {
		out.Files = make([]sohaapi.HostedAppManifestFile, len(in.Files))
		copy(out.Files, in.Files)
	}
	if in.User != nil {
		userCopy := *in.User
		if in.User.Teams != nil {
			userCopy.Teams = make([]string, len(in.User.Teams))
			copy(userCopy.Teams, in.User.Teams)
		}
		out.User = &userCopy
	}
	return out
}
