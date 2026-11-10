package contentdlp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	d "github.com/opensoha/soha/internal/domain/contentdlp"
)

func DataLevelRank(level string) int {
	switch level {
	case "public":
		return 0
	case "internal":
		return 1
	case "confidential":
		return 2
	case "restricted":
		return 3
	}
	return -1
}
func HigherDataLevel(a, b string) string {
	if DataLevelRank(b) > DataLevelRank(a) {
		return b
	}
	return a
}
func minimumLevel(c d.Content) string {
	if c.MinimumDataLevel == "" {
		return "public"
	}
	return c.MinimumDataLevel
}
func builtinLevel(id string) string {
	if id == "credential_assignment" {
		return "confidential"
	}
	return "restricted"
}

// Version identifies detection inputs, not file bytes or the enforcement mode.
func RuleSetVersion(c d.Content) string {
	raw, _ := json.Marshal([]any{detectorVersion(c), minimumLevel(c), c.Detectors, c.Rules})
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
