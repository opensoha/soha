package environment

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrLeaseNotFound = errors.New("environment lease not found")
	ErrLeaseExpired  = errors.New("environment lease expired")
)

type Quota struct {
	DiskBytes     int64 `json:"diskBytes"`
	MemoryBytes   int64 `json:"memoryBytes"`
	CPUUnits      int64 `json:"cpuUnits"`
	MaxProcesses  int   `json:"maxProcesses"`
	NetworkEgress bool  `json:"networkEgress"`
}

type Lease struct {
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	OwnerID       string    `json:"ownerId"`
	ScopeHash     string    `json:"scopeHash"`
	Mode          string    `json:"mode"`
	WorkspacePath string    `json:"workspacePath"`
	Quota         Quota     `json:"quota"`
	IssuedAt      time.Time `json:"issuedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
	ReleasedAt    time.Time `json:"releasedAt,omitempty"`
}

type ResourceUsage struct {
	DiskBytes    int64 `json:"diskBytes"`
	MemoryBytes  int64 `json:"memoryBytes"`
	CPUTimeNanos int64 `json:"cpuTimeNanos"`
	Processes    int   `json:"processes"`
}

type Snapshot struct {
	SchemaVersion string        `json:"schemaVersion"`
	LeaseID       string        `json:"leaseId"`
	RunID         string        `json:"runId"`
	State         string        `json:"state"`
	ResourceUsage ResourceUsage `json:"resourceUsage"`
	ArtifactRefs  []string      `json:"artifactRefs,omitempty"`
	ObservedAt    time.Time     `json:"observedAt"`
}

type Manager struct {
	mu     sync.RWMutex
	leases map[string]Lease
}

func NewManager() *Manager {
	return &Manager{leases: map[string]Lease{}}
}

func (m *Manager) Acquire(lease Lease, now time.Time) (Lease, error) {
	if err := validateLease(lease, now); err != nil {
		return Lease{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.leases[lease.ID]; exists {
		return Lease{}, fmt.Errorf("environment lease %q already exists", lease.ID)
	}
	if lease.IssuedAt.IsZero() {
		lease.IssuedAt = now.UTC()
	}
	lease.ReleasedAt = time.Time{}
	m.leases[lease.ID] = lease
	return lease, nil
}

func (m *Manager) Release(id string, now time.Time) (Lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lease, ok := m.leases[id]
	if !ok {
		return Lease{}, ErrLeaseNotFound
	}
	if lease.ReleasedAt.IsZero() {
		lease.ReleasedAt = now.UTC()
		m.leases[id] = lease
	}
	return lease, nil
}

func (m *Manager) Get(id string) (Lease, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	lease, ok := m.leases[id]
	if !ok {
		return Lease{}, ErrLeaseNotFound
	}
	return lease, nil
}

func (m *Manager) remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.leases, id)
}

func (m *Manager) Snapshot(id string, usage ResourceUsage, artifactRefs []string, now time.Time) (Snapshot, error) {
	m.mu.RLock()
	lease, ok := m.leases[id]
	m.mu.RUnlock()
	if !ok {
		return Snapshot{}, ErrLeaseNotFound
	}
	state := "active"
	if !lease.ReleasedAt.IsZero() {
		state = "released"
	} else if !now.Before(lease.ExpiresAt) {
		state = "expired"
	}
	if usage.DiskBytes > lease.Quota.DiskBytes || usage.MemoryBytes > lease.Quota.MemoryBytes || usage.Processes > lease.Quota.MaxProcesses {
		state = "quota_exceeded"
	}
	return Snapshot{
		SchemaVersion: "opensoha.dev/environment-snapshot/v1",
		LeaseID:       lease.ID,
		RunID:         lease.RunID,
		State:         state,
		ResourceUsage: usage,
		ArtifactRefs:  compactArtifactRefs(artifactRefs),
		ObservedAt:    now.UTC(),
	}, nil
}

func (m *Manager) GCExpired(now time.Time) []Lease {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Lease, 0)
	for id, lease := range m.leases {
		if lease.ReleasedAt.IsZero() && now.Before(lease.ExpiresAt) {
			continue
		}
		out = append(out, lease)
		delete(m.leases, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func validateLease(lease Lease, now time.Time) error {
	if strings.TrimSpace(lease.ID) == "" || strings.TrimSpace(lease.RunID) == "" || strings.TrimSpace(lease.OwnerID) == "" {
		return fmt.Errorf("environment lease id, run id, and owner id are required")
	}
	if strings.TrimSpace(lease.ScopeHash) == "" {
		return fmt.Errorf("environment lease scope hash is required")
	}
	if lease.Mode != "observe-only" && lease.Mode != "restricted-write" {
		return fmt.Errorf("unsupported environment mode %q", lease.Mode)
	}
	if !lease.ExpiresAt.After(now) {
		return ErrLeaseExpired
	}
	if lease.Quota.DiskBytes <= 0 || lease.Quota.MemoryBytes <= 0 || lease.Quota.CPUUnits <= 0 || lease.Quota.MaxProcesses <= 0 {
		return fmt.Errorf("environment lease quota must be bounded")
	}
	return nil
}

func compactArtifactRefs(refs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if len(out) >= 128 {
			break
		}
		ref = strings.TrimSpace(ref)
		if ref == "" || len(ref) > 512 {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}
