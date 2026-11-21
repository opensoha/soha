package assistantmesh_test

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestAssistantMeshProfileDefaultsWithPostgres(t *testing.T) {
	store := receptionPostgres(t)
	ctx := t.Context()

	profile := func(t *testing.T, ownerID string) (open, exists bool) {
		t.Helper()
		err := store.DB().Raw(`SELECT open FROM assistant_mesh_profiles WHERE owner_id = ?`, ownerID).Row().Scan(&open)
		if errors.Is(err, sql.ErrNoRows) {
			return false, false
		}
		if err != nil {
			t.Fatal(err)
		}
		return open, true
	}
	setDefault := func(t *testing.T, raw string) {
		t.Helper()
		if err := store.Exec(ctx, `INSERT INTO app_settings (setting_key, category, value)
			VALUES ('assistantMesh.organization', 'ai', $1::json)
			ON CONFLICT (setting_key) DO UPDATE SET value = EXCLUDED.value`, raw); err != nil {
			t.Fatal(err)
		}
	}
	clearDefault := func(t *testing.T) {
		t.Helper()
		if err := store.Exec(ctx, `DELETE FROM app_settings WHERE setting_key = 'assistantMesh.organization'`); err != nil {
			t.Fatal(err)
		}
	}
	activeUser := func(t *testing.T) string {
		t.Helper()
		id := uuid.NewString()
		if err := store.Exec(ctx, `INSERT INTO users(id, username, email, status) VALUES($1, $2, $3, 'active')`,
			id, "profile-"+id, id+"@example.test"); err != nil {
			t.Fatal(err)
		}
		return id
	}

	approve := func(t *testing.T, ownerID string) {
		t.Helper()
		if err := store.Exec(ctx, `INSERT INTO ai_personal_agent_access (user_id, status, requested_at) VALUES ($1, 'approved', now())`, ownerID); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("missing_settings_and_non_boolean_default_are_closed", func(t *testing.T) {
		clearDefault(t)
		missing := activeUser(t)
		approve(t, missing)
		if open, exists := profile(t, missing); !exists || open {
			t.Fatalf("missing settings profile = open %v, exists %v", open, exists)
		}

		setDefault(t, `{"defaultProfileOpen":"true"}`)
		quoted := activeUser(t)
		approve(t, quoted)
		if open, exists := profile(t, quoted); !exists || open {
			t.Fatalf("non-boolean default profile = open %v, exists %v", open, exists)
		}
	})

	t.Run("new_approval_reads_current_default", func(t *testing.T) {
		setDefault(t, `{"defaultProfileOpen":false}`)
		closed := activeUser(t)
		approve(t, closed)

		setDefault(t, `{"defaultProfileOpen":true}`)
		opened := activeUser(t)
		approve(t, opened)

		if open, exists := profile(t, closed); !exists || open {
			t.Fatalf("existing closed profile changed = open %v, exists %v", open, exists)
		}
		if open, exists := profile(t, opened); !exists || !open {
			t.Fatalf("new approval profile = open %v, exists %v", open, exists)
		}
	})

	t.Run("upgrade_keeps_existing_approval_closed_on_reapproval", func(t *testing.T) {
		setDefault(t, `{"defaultProfileOpen":true}`)
		owner := activeUser(t)
		if err := store.Exec(ctx, `ALTER TABLE ai_personal_agent_access DISABLE TRIGGER assistant_mesh_profile_default_on_approval_insert`); err != nil {
			t.Fatal(err)
		}
		approve(t, owner)
		retry := activeUser(t)
		if err := store.Exec(ctx, `INSERT INTO ai_personal_agent_access (user_id, status, version, requested_at) VALUES ($1, 'pending', 3, now())`, retry); err != nil {
			t.Fatal(err)
		}
		sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "0163_assistant_mesh_profile_defaults.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"revoked", "pending", "approved"} {
			if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = $2 WHERE user_id = $1`, owner, status); err != nil {
				t.Fatal(err)
			}
		}
		if open, exists := profile(t, owner); !exists || open {
			t.Fatalf("pre-existing approval after reapproval = open %v, exists %v", open, exists)
		}
		if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = 'approved' WHERE user_id = $1`, retry); err != nil {
			t.Fatal(err)
		}
		if open, exists := profile(t, retry); !exists || !open {
			t.Fatalf("first approval after historic rejection = open %v, exists %v", open, exists)
		}
	})

	t.Run("manual_closure_survives_revocation_and_reapproval", func(t *testing.T) {
		setDefault(t, `{"defaultProfileOpen":true}`)
		owner := activeUser(t)
		approve(t, owner)
		if open, exists := profile(t, owner); !exists || !open {
			t.Fatalf("approved profile = open %v, exists %v", open, exists)
		}
		if err := store.Exec(ctx, `UPDATE assistant_mesh_profiles SET open = false WHERE owner_id = $1`, owner); err != nil {
			t.Fatal(err)
		}
		if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = 'revoked' WHERE user_id = $1`, owner); err != nil {
			t.Fatal(err)
		}
		if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = 'approved' WHERE user_id = $1`, owner); err != nil {
			t.Fatal(err)
		}
		if open, exists := profile(t, owner); !exists || open {
			t.Fatalf("manual closure after reapproval = open %v, exists %v", open, exists)
		}
	})

	t.Run("pending_only_provisions_on_active_approval", func(t *testing.T) {
		setDefault(t, `{"defaultProfileOpen":true}`)
		owner := activeUser(t)
		if err := store.Exec(ctx, `INSERT INTO ai_personal_agent_access (user_id, status, requested_at) VALUES ($1, 'pending', now())`, owner); err != nil {
			t.Fatal(err)
		}
		if open, exists := profile(t, owner); exists {
			t.Fatalf("pending approval gained a profile = open %v, exists %v", open, exists)
		}
		if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = 'approved' WHERE user_id = $1`, owner); err != nil {
			t.Fatal(err)
		}
		if open, exists := profile(t, owner); !exists || !open {
			t.Fatalf("first approval = open %v, exists %v", open, exists)
		}
		inactive := activeUser(t)
		if err := store.Exec(ctx, `UPDATE users SET status = 'disabled' WHERE id = $1`, inactive); err != nil {
			t.Fatal(err)
		}
		approve(t, inactive)
		if open, exists := profile(t, inactive); !exists || open {
			t.Fatalf("inactive approval must stay closed = open %v, exists %v", open, exists)
		}
		if err := store.Exec(ctx, `UPDATE users SET status = 'active' WHERE id = $1`, inactive); err != nil {
			t.Fatal(err)
		}
		for _, status := range []string{"revoked", "pending", "approved"} {
			if err := store.Exec(ctx, `UPDATE ai_personal_agent_access SET status = $2 WHERE user_id = $1`, inactive, status); err != nil {
				t.Fatal(err)
			}
		}
		if open, exists := profile(t, inactive); !exists || open {
			t.Fatalf("inactive approval should remain closed after reapproval = open %v, exists %v", open, exists)
		}

	})
}
