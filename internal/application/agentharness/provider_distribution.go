package agentharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type FleetTarget struct {
	Environments  []string          `json:"environments,omitempty"`
	Platforms     []string          `json:"platforms,omitempty"`
	Architectures []string          `json:"architectures,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type ProviderRegistrySnapshot struct {
	SchemaVersion string               `json:"schemaVersion"`
	Revision      uint64               `json:"revision"`
	Digest        string               `json:"digest,omitempty"`
	IssuedAt      time.Time            `json:"issuedAt"`
	Providers     []ProviderDefinition `json:"providers"`
	FleetTarget   FleetTarget          `json:"fleetTarget,omitempty"`
}

type RegistryAcknowledgement struct {
	RunnerID          string                      `json:"runnerId"`
	Revision          uint64                      `json:"revision"`
	DesiredRevision   uint64                      `json:"desiredRevision,omitempty"`
	ActiveRevision    uint64                      `json:"activeRevision"`
	LKGRevision       uint64                      `json:"lkgRevision,omitempty"`
	PreviousRevision  uint64                      `json:"previousRevision,omitempty"`
	Accepted          bool                        `json:"accepted"`
	Targeted          bool                        `json:"targeted"`
	RolloutState      string                      `json:"rolloutState,omitempty"`
	RolledBack        bool                        `json:"rolledBack,omitempty"`
	Reason            string                      `json:"reason,omitempty"`
	ObservedAt        time.Time                   `json:"observedAt"`
	ProviderStatuses  []RunnerProviderStatus      `json:"providerStatuses,omitempty"`
	ConformanceChecks []ProviderConformanceResult `json:"conformanceChecks,omitempty"`
}

type ProviderConformanceResult struct {
	ProviderID string `json:"providerId"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
}

type RunnerProviderStatus struct {
	ProviderID      string    `json:"providerId"`
	ProviderVersion string    `json:"providerVersion"`
	CatalogRevision uint64    `json:"catalogRevision"`
	Health          string    `json:"health"`
	Draining        bool      `json:"draining"`
	ActiveRuns      int       `json:"activeRuns"`
	Reason          string    `json:"reason,omitempty"`
	ObservedAt      time.Time `json:"observedAt"`
}

func ProjectRegistrySnapshot(catalog ProviderCatalog, target FleetTarget, now time.Time) (ProviderRegistrySnapshot, error) {
	snapshot := ProviderRegistrySnapshot{
		SchemaVersion: "opensoha.dev/agent-provider-registry/v1",
		Revision:      catalog.Revision,
		IssuedAt:      now.UTC(),
		Providers:     append([]ProviderDefinition(nil), catalog.Providers...),
		FleetTarget:   target,
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return ProviderRegistrySnapshot{}, err
	}
	sum := sha256.Sum256(data)
	snapshot.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return snapshot, nil
}
