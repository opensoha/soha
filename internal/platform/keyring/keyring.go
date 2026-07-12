package keyring

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type Key struct {
	id        string
	secret    string
	createdAt time.Time
	expiresAt *time.Time
}

func NewKey(id, secret string, createdAt time.Time, expiresAt *time.Time) (Key, error) {
	id = strings.TrimSpace(id)
	secret = strings.TrimSpace(secret)
	if id == "" {
		return Key{}, errors.New("key id is required")
	}
	if secret == "" {
		return Key{}, errors.New("key secret is required")
	}
	if createdAt.IsZero() {
		return Key{}, errors.New("key created time is required")
	}
	var expiry *time.Time
	if expiresAt != nil {
		value := expiresAt.UTC()
		expiry = &value
	}
	return Key{id: id, secret: secret, createdAt: createdAt.UTC(), expiresAt: expiry}, nil
}

func (k Key) ID() string {
	return k.id
}

func (k Key) Secret() string {
	return k.secret
}

func (k Key) CreatedAt() time.Time {
	return k.createdAt
}

func (k Key) ExpiresAt() *time.Time {
	if k.expiresAt == nil {
		return nil
	}
	value := *k.expiresAt
	return &value
}

func (k Key) ValidAt(now time.Time) bool {
	return k.expiresAt == nil || now.Before(*k.expiresAt)
}

func (k Key) String() string {
	return fmt.Sprintf("key{id:%q,secret:[redacted]}", k.id)
}

func (k Key) GoString() string {
	return k.String()
}

type Ring struct {
	active   Key
	previous []Key
}

func New(active Key, previous []Key) (Ring, error) {
	if active.id == "" || active.secret == "" {
		return Ring{}, errors.New("active key is required")
	}
	seen := map[string]struct{}{active.id: {}}
	for _, item := range previous {
		if item.id == "" || item.secret == "" {
			return Ring{}, errors.New("previous key is invalid")
		}
		if _, exists := seen[item.id]; exists {
			return Ring{}, errors.New("key ids must be unique")
		}
		seen[item.id] = struct{}{}
	}
	return Ring{active: active, previous: slices.Clone(previous)}, nil
}

func (r Ring) Active() Key {
	return r.active
}

func (r Ring) Previous() []Key {
	return slices.Clone(r.previous)
}

func (r Ring) Find(id string, now time.Time) (Key, bool) {
	id = strings.TrimSpace(id)
	if id == r.active.id {
		return r.active, true
	}
	for _, item := range r.previous {
		if item.id == id && item.ValidAt(now) {
			return item, true
		}
	}
	return Key{}, false
}

func (r Ring) ValidKeys(now time.Time) []Key {
	items := make([]Key, 0, 1+len(r.previous))
	items = append(items, r.active)
	for _, item := range r.previous {
		if item.ValidAt(now) {
			items = append(items, item)
		}
	}
	return items
}

func (r Ring) Match(candidate string, now time.Time) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	matched := 0
	for _, item := range r.ValidKeys(now) {
		matched |= subtle.ConstantTimeCompare([]byte(candidate), []byte(item.secret))
	}
	return matched == 1
}

func (r Ring) String() string {
	return fmt.Sprintf("ring{active:%q,previous:%d,secrets:[redacted]}", r.active.id, len(r.previous))
}

func (r Ring) GoString() string {
	return r.String()
}
