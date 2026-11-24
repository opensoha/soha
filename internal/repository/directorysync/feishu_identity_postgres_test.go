package directorysync_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	directoryrepo "github.com/opensoha/soha/internal/repository/directorysync"
)

func TestFeishuDirectoryBindingWithPostgres(t *testing.T) {
	store := originalDirectoryStore(t)
	ctx := t.Context()
	repo := directoryrepo.New(store.DB(), keyring.Ring{})
	local := uuid.NewString()
	requireDirectoryConnection(t, store.Exec(ctx, `INSERT INTO users(id, username, email, status)
		VALUES($1, 'feishu-fixture', 'feishu-fixture@example.test', 'active')`, local))
	requireDirectoryConnection(t, store.Exec(ctx, `INSERT INTO directory_connections(id, name, provider_type, login_provider_id)
		VALUES('feishu-fixture', 'Feishu fixture', 'feishu', 'fixture-provider')`))
	requireDirectoryConnection(t, store.Exec(ctx, `INSERT INTO directory_people(id, connection_id, external_id, provider_subject, local_user_id, first_seen_at, last_seen_at)
		VALUES('fixture-identity', 'feishu-fixture', 'ou_fixture', 'ou_fixture', $1, now(), now())`, local))
	requireDirectoryConnection(t, store.Exec(ctx, `INSERT INTO user_identities(id, user_id, provider_type, provider_id, provider_user_id)
		VALUES('fixture-binding', $1, 'feishu', 'fixture-provider', 'ou_fixture')`, local))

	if got, err := repo.ResolveActiveFeishuOpenID(ctx, "feishu-fixture", "fixture-provider", "ou_fixture"); err != nil || got != local {
		t.Fatalf("valid binding = %q, %v", got, err)
	}
	if _, err := repo.ResolveActiveFeishuOpenID(ctx, "feishu-fixture", "other-provider", "ou_fixture"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross-app/provider lookup returned %v", err)
	}
	requireDirectoryConnection(t, store.Exec(ctx, `DELETE FROM user_identities WHERE id='fixture-binding'`))
	if _, err := repo.ResolveActiveFeishuOpenID(ctx, "feishu-fixture", "fixture-provider", "ou_fixture"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("stale projection remained usable after unlink: %v", err)
	}
}
