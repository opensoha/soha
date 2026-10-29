package networkaccess_test

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

func (s *accessRepositoryScenario) checkEndpointCredentialRotation(t *testing.T) {
	t.Run("EndpointCredentialRotationUsesRuntimeGeneration", func(t *testing.T) {
		runtimeID := "rotated-endpoint-" + s.suffix
		for generation := 1; generation <= 2; generation++ {
			id := fmt.Sprintf("rotation-credential-%d-%s", generation, s.suffix)
			if err := s.store.Exec(s.ctx, `INSERT INTO network_runtime_credentials
                (id,enrollment_id,runtime_id,runtime_kind,device_id,subject_id,certificate_fingerprint,public_key_fingerprint,certificate_serial,generation,capabilities,status,not_before,expires_at,created_at)
                SELECT $1,enrollment_id,$2,'endpoint',$3,$4,$5,public_key_fingerprint,$6,$7,'["endpoint-presence-v1"]'::jsonb,'active',not_before,expires_at,created_at
				FROM network_runtime_credentials WHERE id=$8`, id, runtimeID, s.updatedDevice.ID, s.userID, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(id))), fmt.Sprintf("%x", generation+1), generation, "credential-"+s.suffix); err != nil {
				t.Fatal(err)
			}
			current, err := s.repository.EndpointCredentialCurrent(s.ctx, id, s.updatedDevice.ID, s.userID, s.now)
			if err != nil || !current {
				t.Fatalf("current runtime generation %d rejected: current=%v error=%v", generation, current, err)
			}
		}
		oldID := "rotation-credential-1-" + s.suffix
		newID := "rotation-credential-2-" + s.suffix
		if current, err := s.repository.EndpointCredentialCurrent(s.ctx, oldID, s.updatedDevice.ID, s.userID, s.now); err != nil || current {
			t.Fatalf("superseded certificate was current: %v %v", current, err)
		}
		for _, test := range []struct {
			name, device, subject string
			at                    time.Time
		}{
			{"wrong device", "other-device", s.userID, s.now},
			{"wrong subject", s.updatedDevice.ID, "other-subject", s.now},
			{"expired", s.updatedDevice.ID, s.userID, s.now.Add(2 * time.Hour)},
		} {
			t.Run(test.name, func(t *testing.T) {
				if current, err := s.repository.EndpointCredentialCurrent(s.ctx, newID, test.device, test.subject, test.at); err != nil || current {
					t.Fatalf("invalid credential was current: %v %v", current, err)
				}
			})
		}
		if err := s.store.Exec(s.ctx, `UPDATE network_runtime_credentials SET status='revoked', revoked_at=$1 WHERE id=$2`, s.now, newID); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{oldID, newID} {
			if current, err := s.repository.EndpointCredentialCurrent(s.ctx, id, s.updatedDevice.ID, s.userID, s.now); err != nil || current {
				t.Fatalf("revocation revived credential %s: %v %v", id, current, err)
			}
		}
	})
}
