package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

// ConfigurationRevision excludes observation timestamps and canonicalizes the
// legacy service association before freezing publication control.
func (target ReleaseTarget) ConfigurationRevision() (string, error) {
	input, err := target.Input().WithCanonicalServiceID()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// ResolvedServiceID keeps older metadata-only targets readable.
func (target ReleaseTarget) ResolvedServiceID() string {
	return targetServiceID(target.ServiceID, target.Metadata)
}
func (target ReleaseTargetInput) ResolvedServiceID() string {
	return targetServiceID(target.ServiceID, target.Metadata)
}
func targetServiceID(id string, metadata map[string]any) string {
	if id = strings.TrimSpace(id); id != "" {
		return id
	}
	legacy, _ := metadata["serviceId"].(string)
	return strings.TrimSpace(legacy)
}

// WithCanonicalServiceID retains one stored identity for both old and new clients.
func (target ReleaseTargetInput) WithCanonicalServiceID() (ReleaseTargetInput, error) {
	target.ServiceID = strings.TrimSpace(target.ServiceID)
	legacy, ok := target.Metadata["serviceId"].(string)
	if raw, present := target.Metadata["serviceId"]; target.ServiceID != "" && present && (!ok || strings.TrimSpace(legacy) != "" && strings.TrimSpace(legacy) != target.ServiceID) {
		return target, fmt.Errorf("serviceId conflicts with the stored service association (%T)", raw)
	}
	target.ServiceID = target.ResolvedServiceID()
	target.Metadata = maps.Clone(target.Metadata)
	if target.ServiceID != "" {
		if target.Metadata == nil {
			target.Metadata = map[string]any{}
		}
		target.Metadata["serviceId"] = target.ServiceID
	}
	return target, nil
}

func (target ReleaseTarget) Input() ReleaseTargetInput {
	return ReleaseTargetInput{ServiceID: target.ResolvedServiceID(), ID: target.ID, ClusterID: target.ClusterID, Namespace: target.Namespace, TargetKind: target.TargetKind, ExecutorKind: target.ExecutorKind, GroupKey: target.GroupKey, WaveKey: target.WaveKey, RegionKey: target.RegionKey, ConfigRef: target.ConfigRef, WorkloadKind: target.WorkloadKind, WorkloadName: target.WorkloadName, ContainerName: target.ContainerName, Metadata: maps.Clone(target.Metadata), Enabled: target.Enabled, Docker: target.Docker, Helm: target.Helm}
}
