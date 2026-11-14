package proxypublication

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

// NodeMaterial compares all typed security and connection fields, never aliases.
func NodeMaterial(ir IR) map[string]string {
	out := map[string]string{}
	for _, node := range ir.Nodes {
		id := node.NodeID
		node.Label = ""
		node.NodeID = ""
		node.Server = strings.ToLower(node.Server)
		if node.TLS != nil {
			tls := *node.TLS
			tls.ServerName = strings.ToLower(tls.ServerName)
			node.TLS = &tls
		}
		raw, _ := json.Marshal(node) // #nosec G117 -- Secrets participate in material equality; serialized bytes are hashed immediately and never returned.
		sum := sha256.Sum256(raw)
		out[id] = hex.EncodeToString(sum[:])
	}
	return out
}
func InvalidatedNodes(old, next map[string]string) []string {
	out := []string{}
	for id, before := range old {
		if after, ok := next[id]; !ok || after != before {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}
func IntersectNodes(ceiling, allowed []string) []string {
	out := []string{}
	for _, id := range ceiling {
		if slices.Contains(allowed, id) {
			out = append(out, id)
		}
	}
	return out
}
func NarrowBundle(record BundleRecord, allowed []string) (BundleRecord, error) {
	if len(record.SourceNodeIDs) == 0 {
		return record, Invalid("invalid_source", "sourceRevision")
	}
	if record.Public.DeliveryMode == "gateway" {
		if len(allowed) != len(record.SourceNodeIDs) {
			return record, Invalid("invalid_target", "scope.nodeIds")
		}
		return record, nil
	}
	record.Scope.NodeIDs = IntersectNodes(record.Scope.NodeIDs, allowed)
	if len(record.Scope.NodeIDs) == 0 {
		return record, Invalid("invalid_target", "scope.nodeIds")
	}
	record.Nodes = IntersectNodes(record.Nodes, record.Scope.NodeIDs)
	return record, nil
}

func CredentialIdentity(c Credential) string {
	raw, _ := json.Marshal(struct {
		EndpointCredential, Runtime, Profile, Process, Bundle string
		Revision, Epoch                                       int64
	}{c.EndpointCredentialID, c.RuntimeID, c.ProfileID, c.ProcessEpoch, c.BundleID, c.Revision, c.Epoch})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
