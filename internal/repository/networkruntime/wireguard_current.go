package networkruntime

import (
	"bytes"
	"encoding/json"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"gorm.io/gorm"
)

// Policy versions do not fingerprint mutable gateway topology or resource
// targets. Recompile the data plane before reusing a cached VPN configuration.
func wireGuardConfigurationCurrent(tx *gorm.DB, runtimeID string, latest domain.Configuration, snapshot domain.PolicySnapshot, now time.Time) (bool, error) {
	current, err := desiredConfigurationForRuntime(tx, runtimeID, snapshot, now, latest.ValidUntil)
	if err != nil {
		return false, err
	}
	// Compare wire representations: optional empty slices become nil after
	// storage decoding and must not cause a replacement on every poll.
	previous, err := json.Marshal([]any{latest.Desired.WireGuard, latest.Desired.DNSRequests})
	if err != nil {
		return false, err
	}
	next, err := json.Marshal([]any{current.WireGuard, current.DNSRequests})
	return bytes.Equal(previous, next), err
}
