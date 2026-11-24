package assistantmesh_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
)

func TestAssistantMeshProfileDefaultsWithPostgres(t *testing.T) {
	fixture := profileDefaultsFixture{store: receptionPostgres(t), ctx: t.Context()}
	t.Run("missing_settings_and_non_boolean_default_are_closed", fixture.checkInvalidProfileDefaults)
	t.Run("new_approval_reads_current_default", fixture.checkApprovalDefault)
	t.Run("upgrade_keeps_existing_approval_closed_on_reapproval", fixture.checkUpgradedProfileDefaults)
	t.Run("manual_closure_survives_revocation_and_reapproval", fixture.checkManualProfileClosure)
	t.Run("pending_only_provisions_on_active_approval", fixture.checkPendingProfileApproval)
}

type profileDefaultsFixture struct {
	store *dbstore.Store
	ctx   context.Context
}

func (fixture profileDefaultsFixture) profile(t *testing.T, ownerID string) (open, exists bool) {
	t.Helper()
	err := fixture.store.DB().Raw(`SELECT open FROM assistant_mesh_profiles WHERE owner_id = ?`, ownerID).Row().Scan(&open)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return open, true
}

func (fixture profileDefaultsFixture) setDefault(t *testing.T, raw string) {
	t.Helper()
	if err := fixture.store.Exec(fixture.ctx, `INSERT INTO app_settings (setting_key, category, value)
		VALUES ('assistantMesh.organization', 'ai', $1::json)
		ON CONFLICT (setting_key) DO UPDATE SET value = EXCLUDED.value`, raw); err != nil {
		t.Fatal(err)
	}
}

func (fixture profileDefaultsFixture) clearDefault(t *testing.T) {
	t.Helper()
	if err := fixture.store.Exec(fixture.ctx, `DELETE FROM app_settings WHERE setting_key = 'assistantMesh.organization'`); err != nil {
		t.Fatal(err)
	}
}

func (fixture profileDefaultsFixture) activeUser(t *testing.T) string {
	t.Helper()
	id := uuid.NewString()
	if err := fixture.store.Exec(fixture.ctx, `INSERT INTO users(id, username, email, status) VALUES($1, $2, $3, 'active')`,
		id, "profile-"+id, id+"@example.test"); err != nil {
		t.Fatal(err)
	}
	return id
}

func (fixture profileDefaultsFixture) approve(t *testing.T, ownerID string) {
	t.Helper()
	if err := fixture.store.Exec(fixture.ctx, `INSERT INTO ai_personal_agent_access (user_id, status, requested_at) VALUES ($1, 'approved', now())`, ownerID); err != nil {
		t.Fatal(err)
	}
}

func (fixture profileDefaultsFixture) checkInvalidProfileDefaults(t *testing.T) {
	fixture.clearDefault(t)
	missing := fixture.activeUser(t)
	fixture.approve(t, missing)
	if open, exists := fixture.profile(t, missing); !exists || open {
		t.Fatalf("missing settings profile = open %v, exists %v", open, exists)
	}

	fixture.setDefault(t, `{"defaultProfileOpen":"true"}`)
	quoted := fixture.activeUser(t)
	fixture.approve(t, quoted)
	if open, exists := fixture.profile(t, quoted); !exists || open {
		t.Fatalf("non-boolean default profile = open %v, exists %v", open, exists)
	}
}

func (fixture profileDefaultsFixture) checkApprovalDefault(t *testing.T) {
	fixture.setDefault(t, `{"defaultProfileOpen":false}`)
	closed := fixture.activeUser(t)
	fixture.approve(t, closed)

	fixture.setDefault(t, `{"defaultProfileOpen":true}`)
	opened := fixture.activeUser(t)
	fixture.approve(t, opened)

	if open, exists := fixture.profile(t, closed); !exists || open {
		t.Fatalf("existing closed profile changed = open %v, exists %v", open, exists)
	}
	if open, exists := fixture.profile(t, opened); !exists || !open {
		t.Fatalf("new approval profile = open %v, exists %v", open, exists)
	}
}

func (fixture profileDefaultsFixture) checkUpgradedProfileDefaults(t *testing.T) {
	fixture.setDefault(t, `{"defaultProfileOpen":true}`)
	owner := fixture.activeUser(t)
	if err := fixture.store.Exec(fixture.ctx, `ALTER TABLE ai_personal_agent_access DISABLE TRIGGER assistant_mesh_profile_default_on_approval_insert`); err != nil {
		t.Fatal(err)
	}
	fixture.approve(t, owner)
	retry := fixture.activeUser(t)
	if err := fixture.store.Exec(fixture.ctx, `INSERT INTO ai_personal_agent_access (user_id, status, version, requested_at) VALUES ($1, 'pending', 3, now())`, retry); err != nil {
		t.Fatal(err)
	}
	sql := readMigrationFixture(t, "0163_assistant_mesh_profile_defaults.sql")
	if err := fixture.store.Exec(fixture.ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"revoked", "pending", "approved"} {
		if err := fixture.store.Exec(fixture.ctx, `UPDATE ai_personal_agent_access SET status = $2 WHERE user_id = $1`, owner, status); err != nil {
			t.Fatal(err)
		}
	}
	if open, exists := fixture.profile(t, owner); !exists || open {
		t.Fatalf("pre-existing approval after reapproval = open %v, exists %v", open, exists)
	}
	if err := fixture.store.Exec(fixture.ctx, `UPDATE ai_personal_agent_access SET status = 'approved' WHERE user_id = $1`, retry); err != nil {
		t.Fatal(err)
	}
	if open, exists := fixture.profile(t, retry); !exists || !open {
		t.Fatalf("first approval after historic rejection = open %v, exists %v", open, exists)
	}
}

func (fixture profileDefaultsFixture) checkManualProfileClosure(t *testing.T) {
	fixture.setDefault(t, `{"defaultProfileOpen":true}`)
	owner := fixture.activeUser(t)
	fixture.approve(t, owner)
	if open, exists := fixture.profile(t, owner); !exists || !open {
		t.Fatalf("approved profile = open %v, exists %v", open, exists)
	}
	if err := fixture.store.Exec(fixture.ctx, `UPDATE assistant_mesh_profiles SET open = false WHERE owner_id = $1`, owner); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.Exec(fixture.ctx, `UPDATE ai_personal_agent_access SET status = 'revoked' WHERE user_id = $1`, owner); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.Exec(fixture.ctx, `UPDATE ai_personal_agent_access SET status = 'approved' WHERE user_id = $1`, owner); err != nil {
		t.Fatal(err)
	}
	if open, exists := fixture.profile(t, owner); !exists || open {
		t.Fatalf("manual closure after reapproval = open %v, exists %v", open, exists)
	}
}

func (fixture profileDefaultsFixture) checkPendingProfileApproval(t *testing.T) {
	fixture.setDefault(t, `{"defaultProfileOpen":true}`)
	owner := fixture.activeUser(t)
	if err := fixture.store.Exec(fixture.ctx, `INSERT INTO ai_personal_agent_access (user_id, status, requested_at) VALUES ($1, 'pending', now())`, owner); err != nil {
		t.Fatal(err)
	}
	if open, exists := fixture.profile(t, owner); exists {
		t.Fatalf("pending approval gained a profile = open %v, exists %v", open, exists)
	}
	if err := fixture.store.Exec(fixture.ctx, `UPDATE ai_personal_agent_access SET status = 'approved' WHERE user_id = $1`, owner); err != nil {
		t.Fatal(err)
	}
	if open, exists := fixture.profile(t, owner); !exists || !open {
		t.Fatalf("first approval = open %v, exists %v", open, exists)
	}
	inactive := fixture.activeUser(t)
	if err := fixture.store.Exec(fixture.ctx, `UPDATE users SET status = 'disabled' WHERE id = $1`, inactive); err != nil {
		t.Fatal(err)
	}
	fixture.approve(t, inactive)
	if open, exists := fixture.profile(t, inactive); !exists || open {
		t.Fatalf("inactive approval must stay closed = open %v, exists %v", open, exists)
	}
	if err := fixture.store.Exec(fixture.ctx, `UPDATE users SET status = 'active' WHERE id = $1`, inactive); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"revoked", "pending", "approved"} {
		if err := fixture.store.Exec(fixture.ctx, `UPDATE ai_personal_agent_access SET status = $2 WHERE user_id = $1`, inactive, status); err != nil {
			t.Fatal(err)
		}
	}
	if open, exists := fixture.profile(t, inactive); !exists || open {
		t.Fatalf("inactive approval should remain closed after reapproval = open %v, exists %v", open, exists)
	}
}
