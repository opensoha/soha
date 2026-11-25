package systemintegration

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestConfigurationPersistenceUsesJSONObjectAndStableFields(t *testing.T) {
	raw, err := encodeConfiguration([]sohaapi.SystemIntegrationConfigurationField{{Key: "group_id", Value: "platform"}, {Key: "base_url", Value: "https://gitlab.example/api/v4"}})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]string
	if err := json.Unmarshal(raw, &object); err != nil || object["group_id"] != "platform" {
		t.Fatalf("configuration object = %#v, error = %v", object, err)
	}
	var fields []sohaapi.SystemIntegrationConfigurationField
	if err := decodeConfiguration([]byte(`{}`), &fields); err != nil || len(fields) != 0 {
		t.Fatalf("decode empty default = %#v, error = %v", fields, err)
	}
}

func TestSystemIntegrationMigrationUsesJSONObjectDefault(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/postgres/0027_system_integrations.sql")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "configuration jsonb NOT NULL DEFAULT '{}'::jsonb") {
		t.Fatalf("migration does not use object configuration default: %s", text)
	}
}

func TestRepositoryGetDecodesObjectConfigurationAndOnlyReturnsCredentialKeys(t *testing.T) {
	repo, mock := newSystemIntegrationRepository(t)
	now := time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	createRows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "category", "provider_type", "name", "description", "enabled", "configuration",
			"health_status", "last_checked_at", "last_error", "version", "created_by", "updated_by", "created_at", "updated_at", "credential_keys"}).
			AddRow("gitlab-1", "source_control", "gitlab", "GitLab", "", true, `{"base_url":"https://gitlab.example/api/v4"}`,
				"unknown", nil, "", int64(1), "admin", "admin", now, now, `["token"]`)
	}
	mock.ExpectQuery(`(?s)SELECT id, category, provider_type.*jsonb_agg\(c.credential_key\).*FROM system_integrations i WHERE id =`).
		WithArgs("gitlab-1").WillReturnRows(createRows())
	item, err := repo.Get(context.Background(), "gitlab-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if len(item.Configuration) != 1 || item.Configuration[0].Key != "base_url" || len(item.CredentialKeys) != 1 || item.CredentialKeys[0] != "token" {
		t.Fatalf("unexpected item: %#v", item)
	}
	mock.ExpectQuery(`(?s)SELECT id, category, provider_type.*FROM system_integrations i WHERE 1=1 AND category = .* AND provider_type =`).
		WithArgs("source_control", "gitlab").WillReturnRows(createRows())
	items, err := repo.List(context.Background(), domain.Filter{Category: "source_control", ProviderType: "gitlab"})
	if err != nil || len(items) != 1 || items[0].ID != item.ID || len(items[0].CredentialKeys) != 1 {
		t.Fatalf("List() = %#v, error = %v", items, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newSystemIntegrationRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return New(db), mock
}
