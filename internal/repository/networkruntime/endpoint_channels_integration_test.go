package networkruntime_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	ec "github.com/opensoha/soha-contracts/endpointcontrol"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	channelrepo "github.com/opensoha/soha/internal/repository/endpointcontrol"
	runtimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
	"gorm.io/gorm"
)

func (s *runtimeRepositoryScenario) checkEndpointChannelLifecycle(t *testing.T) {
	t.Helper()
	rollback := errors.New("rollback isolated endpoint channels")
	err := s.store.DB().WithContext(s.ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC().Truncate(time.Microsecond)
		deviceID, runtimeID := "channel-device-"+s.suffix, "channel-runtime-"+s.suffix
		if err := tx.Exec(`INSERT INTO network_access_devices(id,owner_user_id,name,hostname,platform,status,posture_status,created_at,updated_at) VALUES(?,?::uuid,?,?,'windows','active','compliant',?,?)`, deviceID, s.subjectID, deviceID, runtimeID, now, now).Error; err != nil {
			return err
		}
		runtime := runtimerepo.New(tx)
		credential, err := s.enrollChannelRuntime(runtime, deviceID, runtimeID, "-1", now)
		if err != nil {
			return err
		}
		repo := channelrepo.New(tx)
		policy, err := s.publishInitialEndpointChannels(t, repo, deviceID, now)
		if err != nil {
			return err
		}
		if err := s.reportEndpointChannels(t, repo, credential, deviceID, runtimeID, "a", false); err != nil {
			return err
		}
		if err := s.assertEndpointChannelsFresh(t, repo, deviceID, true); err != nil {
			return err
		}
		assertEndpointChannelRemovalBlocked(t, tx, deviceID)
		policy, err = repo.Save(s.ctx, deviceID, s.subjectID, ec.SaveInput{ExpectedRevision: policy.Revision}, now)
		if err != nil {
			return err
		}
		policy, err = repo.Publish(s.ctx, deviceID, s.subjectID, policy.Revision, now)
		if err != nil {
			return err
		}
		if err := s.assertEndpointChannelsFresh(t, repo, deviceID, false); err != nil {
			return err
		}
		assertEndpointChannelRemovalBlocked(t, tx, deviceID)
		if err := s.reportEndpointChannels(t, repo, credential, deviceID, runtimeID, "b", true); err != nil {
			return err
		}
		if err := s.assertEndpointChannelsFresh(t, repo, deviceID, true); err != nil {
			return err
		}
		newCredential, err := s.enrollChannelRuntime(runtime, deviceID, runtimeID, "-2", now)
		if err != nil {
			return err
		}
		if _, err := repo.Desired(s.ctx, credential, strings.Repeat("c", 43)); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("superseded channel credential = %v", err)
		}
		if err := s.assertEndpointChannelsFresh(t, repo, deviceID, false); err != nil {
			return err
		}
		assertEndpointChannelRemovalBlocked(t, tx, deviceID)
		if err := s.reportEndpointChannels(t, repo, newCredential, deviceID, runtimeID, "d", true); err != nil {
			return err
		}
		if err := s.assertEndpointChannelsFresh(t, repo, deviceID, true); err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM network_access_devices WHERE id=?`, deviceID).Error; err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}

func (s *runtimeRepositoryScenario) enrollChannelRuntime(runtime *runtimerepo.Repository, deviceID, runtimeID, suffix string, now time.Time) (domain.Credential, error) {
	challenge := enrollment("channel-"+s.suffix+suffix, runtimeID, deviceID, s.subjectID, now)
	if err := runtime.CreateEnrollment(s.ctx, challenge); err != nil {
		return domain.Credential{}, err
	}
	input := consumption(challenge, "channel-cert-"+s.suffix+suffix, now)
	input.Capabilities = append(input.Capabilities, ec.Capability)
	credential, _, err := runtime.ConsumeEnrollment(s.ctx, input, s.snapshot, now.Add(5*time.Minute))
	return credential, err
}

func (s *runtimeRepositoryScenario) reportEndpointChannels(t *testing.T, repo *channelrepo.Repository, c domain.Credential, deviceID, runtimeID, nonce string, off bool) error {
	t.Helper()
	desired, err := repo.Desired(s.ctx, c, strings.Repeat(nonce, 43))
	if err != nil {
		return err
	}
	if !ec.ValidDesired(desired, time.Now().UTC()) {
		t.Fatalf("invalid persisted channel desired: %#v", desired)
	}
	channels := []ec.Channel{{Name: "usb", Scope: "current_usb_volumes"}, {Name: "print", Scope: "local_new_print_jobs"}, {Name: "clipboard", Scope: "enrolled_console_win32"}}
	for i := range channels {
		channels[i].State, channels[i].ReasonCode, channels[i].Targets, channels[i].Controlled = "controlled", "policy_applied", 1, 1
		if off {
			channels[i].State, channels[i].ReasonCode, channels[i].Controlled = "off", "restored", 0
		}
	}
	receipt := ec.Receipt{SchemaVersion: ec.Version, DeviceID: deviceID, RuntimeID: runtimeID, CredentialID: c.ID, PolicyVersion: desired.PolicyVersion, Challenge: desired.Challenge, ObservedAt: time.Now().UTC(), Channels: channels}
	if !ec.ValidReceipt(receipt, time.Now().UTC()) {
		t.Fatal("invalid channel receipt fixture")
	}
	if err := repo.Report(s.ctx, c, receipt); err != nil {
		return err
	}
	if err := repo.Report(s.ctx, c, receipt); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("channel receipt replay = %v", err)
	}
	return nil
}

func (s *runtimeRepositoryScenario) assertEndpointChannelsFresh(t *testing.T, repo *channelrepo.Repository, deviceID string, want bool) error {
	t.Helper()
	view, err := repo.View(s.ctx, deviceID)
	if err == nil && (view.Fresh != want || view.RuntimeCount != 1 || view.PendingRuntimeCount != map[bool]int{true: 0, false: 1}[want]) {
		t.Fatalf("channel receipt freshness = %#v, want %v", view, want)
	}
	return err
}

func assertEndpointChannelRemovalBlocked(t *testing.T, tx *gorm.DB, deviceID string) {
	t.Helper()
	err := tx.Transaction(func(attempt *gorm.DB) error {
		return attempt.Exec(`DELETE FROM network_access_devices WHERE id=?`, deviceID).Error
	})
	var postgres *pgconn.PgError
	if !errors.As(err, &postgres) || postgres.Code != "23503" || postgres.ConstraintName != "endpoint_channel_release_required" {
		t.Fatalf("device removal did not enforce native restoration: %v", err)
	}
}

func (s *runtimeRepositoryScenario) publishInitialEndpointChannels(t *testing.T, repo *channelrepo.Repository, deviceID string, now time.Time) (ec.Policy, error) {
	t.Helper()
	save := ec.SaveInput{Draft: ec.Controls{USB: true, Print: true, Clipboard: true}}
	policy, err := repo.Save(s.ctx, deviceID, s.subjectID, save, now)
	if err != nil {
		return ec.Policy{}, err
	}
	if _, err := repo.Save(s.ctx, deviceID, s.subjectID, save, now); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale channel save = %v", err)
	}
	policy, err = repo.Publish(s.ctx, deviceID, s.subjectID, policy.Revision, now)
	if err != nil {
		return ec.Policy{}, err
	}
	if policy.Published == nil || !policy.Published.USB || policy.PublishedVersion != 2 {
		t.Fatalf("published channel policy = %#v", policy)
	}
	return policy, nil
}
