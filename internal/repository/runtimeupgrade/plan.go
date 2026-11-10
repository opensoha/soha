// Package runtimeupgrade implements the explicit, offline Task123 cutover. It is
// never called by normal server startup. Reports contain identities and hashes,
// not messages, checkpoints, credentials, memory facts or provider commands.
package runtimeupgrade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	memory "github.com/opensoha/soha/internal/application/memory"
)

type Item struct {
	Entity string `json:"entity"`
	ID     string `json:"id"`
	Hash   string `json:"hash"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}
type Plan struct {
	Version int    `json:"version"`
	Digest  string `json:"digest"`
	Items   []Item `json:"items"`
}
type row struct {
	Entity, ID string
	Data       json.RawMessage
}

func hash(b []byte) string                  { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func object(b []byte) map[string]any        { var v map[string]any; _ = json.Unmarshal(b, &v); return v }
func str(v map[string]any, k string) string { s, _ := v[k].(string); return s }
func legacy(v map[string]any) bool {
	k := str(v, "provider_kind")
	return k == "native" || k == "codex"
}
func terminal(s string) bool {
	switch s {
	case "completed", "failed", "cancelled", "canceled", "succeeded", "success", "error", "callback_timeout", "timeout", "timed_out":
		return true
	}
	return false
}
func build(rows []row, now time.Time) Plan {
	p := Plan{Version: 1, Items: []Item{}}
	policies := map[string]memory.Policy{}
	legacyProviders := providerInventory(rows)
	for _, r := range rows {
		if r.Entity == "policy" {
			var policy memory.Policy
			b, _ := json.Marshal(object(r.Data)["payload"])
			_ = json.Unmarshal(b, &policy)
			policies[policy.ID+":"+policy.Version] = policy
		}
	}
	for _, r := range rows {
		i := classify(r, policies, legacyProviders, now)
		p.Items = append(p.Items, i)
	}
	for id := range legacyProviders {
		p.Items = append(p.Items, Item{Entity: "legacy_provider", ID: id, Hash: hash([]byte(id)), Action: "fence_provider"})
	}
	sort.Slice(p.Items, func(i, j int) bool { a, b := p.Items[i], p.Items[j]; return a.Entity+":"+a.ID < b.Entity+":"+b.ID })
	b, _ := json.Marshal(p)
	p.Digest = hash(b)
	return p
}
func providerInventory(rows []row) map[string]bool {
	legacyProviders := map[string]bool{}
	for _, r := range rows {
		if r.Entity == "provider_catalog" {
			catalog, _ := object(r.Data)["catalog"].(map[string]any)
			providers, _ := catalog["providers"].([]any)
			for _, entry := range providers {
				v, _ := entry.(map[string]any)
				runtime, _ := v["runtime"].(map[string]any)
				raw, _ := json.Marshal(runtime)
				if str(v, "kind") == "codex" || strings.Contains(string(raw), "codex-runner/adapter.py") {
					legacyProviders[str(v, "id")] = true
				}
			}
		}
	}
	return legacyProviders
}
func classify(r row, policies map[string]memory.Policy, legacyProviders map[string]bool, now time.Time) Item {
	v := object(r.Data)
	i := Item{Entity: r.Entity, ID: r.ID, Hash: hash(r.Data), Action: "preserve"}
	switch r.Entity {
	case "session":
		m, isObject := v["metadata"].(map[string]any)
		if !isObject && v["metadata"] != nil {
			i.Action = "manual_review"
			i.Reason = "invalid_session_metadata"
			return i
		}
		if x := str(m, "agentProviderId"); x == "" || x == "internal" || x == "native" || legacyProviders[x] {
			i.Action = "map_dsh"
		}
	case "run":
		if quarantined, _ := v["_quarantined"].(bool); quarantined {
			i.Action = "preserve_quarantine"
			i.Reason = "legacy_reconciliation_required"
			return i
		}
		old := legacy(v) || legacyProviders[str(v, "provider_id")]
		if old && !terminal(str(v, "status")) {
			i.Action = "drain"
			i.Reason = "legacy_nonterminal_requires_reconciliation"
		}
		if old {
			if t, e := time.Parse(time.RFC3339Nano, str(v, "native_lease_until")); e == nil && t.After(now) {
				i.Action = "live_lease"
				i.Reason = "live_lease"
			}
		}
	case "memory":
		i.Action, i.Reason = memoryAction(v, policies, now)
	case "outbox":
		if t, e := time.Parse(time.RFC3339Nano, str(v, "lease_until")); e == nil && t.After(now) {
			i.Action = "live_lease"
			i.Reason = "memory_sync_live_lease"
		}
	}
	return i
}
func memoryAction(v map[string]any, policies map[string]memory.Policy, now time.Time) (string, string) {
	b, _ := json.Marshal(v["payload"])
	var r memory.Record
	if json.Unmarshal(b, &r) != nil {
		return "quarantine", "invalid_payload"
	}
	if r.OwnerType != "user" {
		return "quarantine", "unsupported_owner_type"
	}
	if str(v, "status") != "active" || v["deleted_at"] != nil {
		return "disabled", "tombstone_or_disabled"
	}
	if r.DeletedAt != nil || r.Status != "active" {
		return "quarantine", "conflicting_tombstone"
	}
	expiry, err := time.Parse(time.RFC3339Nano, str(v, "expires_at"))
	if err != nil || r.ExpiresAt == nil {
		return "quarantine", "missing_or_invalid_ttl"
	}
	if !expiry.Equal(r.ExpiresAt.Truncate(time.Microsecond)) && !expiry.Equal(r.ExpiresAt.Round(time.Microsecond)) {
		return "quarantine", "conflicting_ttl"
	}
	if !expiry.After(now) {
		return "disabled", "expired"
	}
	if reason := memoryIdentity(v, r, now); reason != "" {
		return "quarantine", reason
	}
	if exists, _ := v["_ownerExists"].(bool); !exists {
		return "quarantine", "orphan_owner"
	}
	return memoryPolicy(r, policies, expiry)
}
func memoryIdentity(v map[string]any, r memory.Record, now time.Time) string {
	if r.ID != str(v, "id") || r.OwnerID == "" || r.OwnerID != str(v, "owner_id") || r.OwnerType != str(v, "owner_type") || r.ScopeHash == "" || r.ScopeHash != str(v, "scope_hash") {
		return "ambiguous_identity_scope"
	}
	if r.OwnerType != "user" && r.OwnerType != "session" && r.OwnerType != "agent" {
		return "invalid_owner_type"
	}
	return memoryProvenance(r, now)
}
func memoryProvenance(r memory.Record, now time.Time) string {
	if !strings.HasPrefix(r.ScopeHash, "sha256:") || len(r.ScopeHash) != 71 {
		return "invalid_scope_hash"
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(r.ScopeHash, "sha256:")); err != nil {
		return "invalid_scope_hash"
	}
	if r.ValidFrom.IsZero() || r.ValidFrom.After(now) || r.CreatedAt.IsZero() || r.CreatedAt.After(now) || r.Fact == "" || r.SourceType == "" || len(r.SourceRefs) == 0 {
		return "missing_provenance"
	}
	for _, ref := range r.SourceRefs {
		h := r.SourceHashes[ref]
		if ref == "" || len(h) != 64 {
			return "missing_source_hash"
		}
		if _, e := hex.DecodeString(h); e != nil {
			return "invalid_source_hash"
		}
	}
	return ""
}
func memoryPolicy(r memory.Record, policies map[string]memory.Policy, expiry time.Time) (string, string) {
	policy, ok := policies[r.PolicyID+":"+r.PolicyVer]
	created := r.CreatedAt.Truncate(time.Microsecond)
	maxExpiry := created.Add(policy.MaximumTTL).Add(time.Microsecond)
	if !ok || !policy.Enabled || policy.OwnerID != "" && policy.OwnerID != r.OwnerID || policy.MaximumTTL <= 0 || !expiry.After(created) || expiry.After(maxExpiry) {
		return "quarantine", "invalid_policy_or_ttl"
	}
	if !supportedScope(r, policy) {
		return "quarantine", "unsupported_scope"
	}
	allowed := false
	for _, o := range policy.OwnerTypes {
		allowed = allowed || o == r.OwnerType
	}
	if !allowed || r.Confidence < policy.MinimumConfidence || r.Confidence > 1 {
		return "quarantine", "policy_rejected"
	}
	if policy.ExplicitWriteOnly && r.SourceType != "explicit_user" {
		return "quarantine", "policy_requires_explicit_source"
	}
	if !memory.WriteAllowed(policy) {
		return "disabled", "policy_writes_disabled"
	}
	return "enqueue", "current_source_permission_revalidated_by_worker"
}
func supportedScope(r memory.Record, p memory.Policy) bool {
	mode := p.Mode
	if mode == "" {
		mode = "chat"
	}
	return (mode == "chat" || mode == "heya") && r.ScopeHash == memory.Scope(r.OwnerID, mode)
}
func stableJob(id string, revision int64) (string, string) {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("task123:%s:%d", id, revision))).String(), fmt.Sprintf("soha-%s-r%d", hash([]byte(id)), revision)
}
func boundedIDs(ids []string) (map[string]bool, error) {
	m := map[string]bool{}
	if len(ids) > 1000 {
		return nil, fmt.Errorf("quarantine exceeds 1000 run limit")
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || m[id] {
			return nil, fmt.Errorf("invalid quarantine identity")
		}
		m[id] = true
	}
	return m, nil
}
