package assistantmesh_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
	mesh "github.com/opensoha/soha/internal/repository/assistantmesh"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func TestInheritanceTransferWithPostgres(t *testing.T) {
	store := receptionPostgres(t)
	repo := mesh.New(store.DB())
	ctx := t.Context()
	owner, heir, next := uuid.NewString(), uuid.NewString(), uuid.NewString()
	mustUser(t, ctx, store, owner, "disabled")
	mustUser(t, ctx, store, heir, "active")
	mustUser(t, ctx, store, next, "active")
	card, skill, private := uuid.NewString(), uuid.NewString(), uuid.NewString()
	snapshot := `{"version":2,"input":{"title":"Published incident guide","body":"Shared facts"},"digest":"published-digest","audience":{"visibility":"organization"},"expiresAt":"2027-01-01T00:00:00Z"}`
	published := json.RawMessage(`{"published":` + snapshot + `,"lastVersion":3,"draft":{"body":"Private draft"},"draftDigest":"other","review":{"status":"pending","candidate":{"body":"Private proposal"}},"trial":{"runId":"private-run"},"findings":["private-finding"]}`)
	for _, item := range []mesh.Record{
		{ID: card, OwnerID: owner, Kind: domain.KindExperienceCard, Payload: published},
		{ID: skill, OwnerID: owner, Kind: domain.KindSkill, Payload: published},
		{ID: private, OwnerID: owner, Kind: domain.KindExperienceCard, Payload: json.RawMessage(`{"draft":{"body":"Private only"}}`)},
	} {
		if err := repo.Create(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Exec(ctx, `INSERT INTO assistant_mesh_retired_versions(asset_id,version,snapshot) VALUES ($1,1,'{"version":1}')`, card); err != nil {
		t.Fatal(err)
	}
	list, err := repo.ListInheritableAssets(ctx, owner, "")
	if err != nil || len(list.Items) != 2 || list.Items[0].OriginalAuthorID != owner || list.Items[0].Title != "Published incident guide" {
		t.Fatalf("wrong published listing: %+v, %v", list, err)
	}
	refused := errors.New("audit write failure")
	input := domain.InheritanceInput{OwnerID: owner, HeirID: heir, AssetIDs: []string{card}}
	if _, err := repo.DisposeInheritedAssets(ctx, input, func(context.Context, []string) error { return refused }); !errors.Is(err, refused) {
		t.Fatalf("audit failure was lost: %v", err)
	}
	checkOwner := func(id, expected string) mesh.Record {
		t.Helper()
		got, err := repo.Get(ctx, id)
		if err != nil || got.OwnerID != expected {
			t.Fatalf("asset owner: %+v, %v", got, err)
		}
		return got
	}
	checkOwner(card, owner)
	input.AssetIDs = []string{card, private}
	accept := func(context.Context, []string) error { return nil }
	if _, err := repo.DisposeInheritedAssets(ctx, input, accept); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("non-published selection allowed partial transfer: %v", err)
	}
	checkOwner(card, owner)
	input.AssetIDs = []string{card}
	if count, err := repo.DisposeInheritedAssets(ctx, input, accept); err != nil || count != 1 {
		t.Fatalf("selected transfer: %d, %v", count, err)
	}
	checkOwner(skill, owner)
	checkOwner(private, owner)
	assertInheritedPublication(t, checkOwner(card, heir).Payload, owner, snapshot)
	input.AssetIDs = nil
	if count, err := repo.DisposeInheritedAssets(ctx, input, accept); err != nil || count != 1 {
		t.Fatalf("transfer remaining published assets: %d, %v", count, err)
	}
	checkOwner(skill, heir)
	if err := userrepo.New(store.DB()).DeleteUser(ctx, owner); err != nil {
		t.Fatalf("the original account is deletable after transfer: %v", err)
	}
	if err := store.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, heir); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.DisposeInheritedAssets(ctx, domain.InheritanceInput{OwnerID: heir, HeirID: next}, accept); err != nil || count != 2 {
		t.Fatalf("successive transfer: %d, %v", count, err)
	}
	assertInheritedPublication(t, checkOwner(card, next).Payload, owner, snapshot)
	var retired int
	if err := store.DB().Raw(`SELECT count(*) FROM assistant_mesh_retired_versions WHERE asset_id = ? AND version = 1`, card).Row().Scan(&retired); err != nil || retired != 1 {
		t.Fatalf("historic version count = %d, %v", retired, err)
	}
	if err := store.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, next); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.DisposeInheritedAssets(ctx, domain.InheritanceInput{OwnerID: next, AssetIDs: []string{card}}, accept); err != nil || count != 1 {
		t.Fatalf("direct retirement: %d, %v", count, err)
	}
	if err := store.DB().Raw(`SELECT count(*) FROM assistant_mesh_retired_versions WHERE asset_id = ? AND version IN (1, 2)`, card).Row().Scan(&retired); err != nil || retired != 2 {
		t.Fatalf("retiring failed to archive the original publication: %d, %v", retired, err)
	}
	if list, err := repo.ListInheritableAssets(ctx, next, ""); err != nil || len(list.Items) != 1 || list.Items[0].ID != skill {
		t.Fatalf("direct retirement did not remove the selected publication: %+v, %v", list, err)
	}
	if count, err := repo.DisposeInheritedAssets(ctx, domain.InheritanceInput{OwnerID: next}, accept); err != nil || count != 1 {
		t.Fatalf("retire remaining publications: %d, %v", count, err)
	}
	if err := userrepo.New(store.DB()).DeleteUser(ctx, next); err != nil {
		t.Fatalf("account is still blocked after retirement: %v", err)
	}
}

func assertInheritedPublication(t *testing.T, payload json.RawMessage, author, snapshot string) {
	t.Helper()
	var data struct {
		OriginalAuthorID string
		Published        map[string]any
		Draft            struct{ Body string }
		DraftDigest      string
		LastVersion      int
		Review           struct{ Status string }
		Trial            json.RawMessage
		Findings         json.RawMessage
	}
	var expected map[string]any
	if err := json.Unmarshal([]byte(snapshot), &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatal(err)
	}
	gotVersion, _ := json.Marshal(data.Published)
	wantVersion, _ := json.Marshal(expected)
	if data.OriginalAuthorID != author || string(gotVersion) != string(wantVersion) || data.Draft.Body != "Shared facts" ||
		data.DraftDigest != "published-digest" || data.LastVersion != 3 || data.Review.Status != "approved" || len(data.Trial)+len(data.Findings) != 0 {
		t.Fatalf("inheritance changed publication or retained private work: %s", payload)
	}
}

func TestUpgradeHeyaAssetDispositionPermissionWithPostgres(t *testing.T) {
	store := receptionPostgres(t)
	ctx := t.Context()
	if err := store.Exec(ctx, `INSERT INTO roles(id,name,permission_keys)
		VALUES ('admin','admin','["system.audit.view"]'), ('editor','editor','["access.users.manage"]')`); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile("../../../migrations/postgres/0161_assistant_mesh_disposition_permission.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.Exec(ctx, string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	for role, want := range map[string]string{
		"admin":  `["system.audit.view","access.users.inherit-heya-assets"]`,
		"editor": `["access.users.manage"]`,
	} {
		var got string
		if err := store.DB().Raw(`SELECT permission_keys::text FROM roles WHERE id=?`, role).Row().Scan(&got); err != nil ||
			stringWithoutJSONWhitespace(t, got) != want {
			t.Fatalf("role %s after upgrade: %q, %v", role, got, err)
		}
	}
}

func stringWithoutJSONWhitespace(t *testing.T, data string) string {
	t.Helper()
	var items []string
	if err := json.Unmarshal([]byte(data), &items); err != nil {
		t.Fatal(err)
	}
	result, _ := json.Marshal(items)
	return string(result)
}
