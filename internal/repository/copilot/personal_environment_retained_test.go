package copilot

import "testing"

// Reuse Docker regressions without deleting the isolated database or user data.
func TestPersonalDockerBindingWithRetainedPostgres(t *testing.T) {
	repo, ctx := retainedPersonalGuestRepository(t)
	f := personalEnvironmentFixtureWithRepository(t, repo, repo.db, ctx)
	for _, test := range []struct {
		name string
		run  func(*testing.T, *personalEnvironmentFixture)
	}{
		{"owner_isolation", testPersonalEnvironmentOwnerIsolation},
		{"quota_preflight_before_data", testPersonalEnvironmentPreflightBeforeData},
		{"not_dispatched_restore", testPersonalEnvironmentNotDispatchedRestore},
		{"file_read_owner_fence", testPersonalEnvironmentFileReadOwnerFence},
		{"frozen_binding_sleep_wake", testPersonalEnvironmentFrozenBindingSleepWake},
		{"binding_identity_fence", testPersonalBindingIdentityFence},
		{"unsupported_backend", testPersonalBindingUnsupportedBackend},
		{"unknown_no_replay", testPersonalEnvironmentUnknownNoReplay},
		{"missing_data_not_recreated", testPersonalEnvironmentMissingDataNotRecreated},
		{"dispatch_cas", testPersonalEnvironmentDispatchCas},
		{"late_receipt_fence", testPersonalEnvironmentLateReceiptFence},
		{"quota_scope_ceiling", testPersonalEnvironmentQuotaScopeCeiling},
		{"canceled_receipt_persisted", testPersonalEnvironmentCanceledReceiptPersisted},
		{"current_permission_denied", testPersonalEnvironmentCurrentPermissionDenied},
		{"legacy_writer_compatibility", testPersonalBindingLegacyWriter},
	} {
		t.Run(test.name, func(t *testing.T) { f.t = t; test.run(t, f) })
	}
}
