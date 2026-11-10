package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// DeliveryContext is private operation admission data, never a runner payload.
type DeliveryContext struct {
	Version        int    `json:"version"`
	PlanID         string `json:"planId"`
	TargetID       string `json:"targetId"`
	TargetRevision string `json:"targetRevision"`
	ScopeRevision  string `json:"scopeRevision"`
}

func (c DeliveryContext) Validate() error {
	if c.Version != 1 || strings.TrimSpace(c.PlanID) == "" || strings.TrimSpace(c.TargetID) == "" {
		return fmt.Errorf("delivery context requires a version, plan and target")
	}
	for _, value := range []string{c.TargetRevision, c.ScopeRevision} {
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != sha256.Size || value != strings.ToLower(value) {
			return fmt.Errorf("delivery context requires immutable configuration digests")
		}
	}
	return nil
}

func DeliveryContextDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
