package agentharness

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	appagentharness "github.com/opensoha/soha/internal/application/agentharness"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestLoadProviderStateRestoresCatalogAndBoundedAcknowledgements(t *testing.T) {
	repository, mock := newRepository(t)
	createdAt := time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)
	catalog := `{"schemaVersion":"opensoha.dev/agent-provider-catalog/v1","revision":7,"digest":"digest-7","createdAt":"2026-07-14T01:02:03Z","providers":[]}`
	ack := `{"runnerId":"runner-1","revision":7,"activeRevision":7,"accepted":true,"observedAt":"2026-07-14T01:03:03Z","providerStatuses":[]}`
	mock.ExpectQuery(`SELECT catalog FROM ai_agent_provider_catalog_state`).
		WithArgs(providerCatalogStateID).
		WillReturnRows(sqlmock.NewRows([]string{"catalog"}).AddRow([]byte(catalog)))
	mock.ExpectQuery(`SELECT acknowledgement FROM ai_agent_provider_registry_acks`).
		WithArgs(2).
		WillReturnRows(sqlmock.NewRows([]string{"acknowledgement"}).AddRow([]byte(ack)))

	state, err := repository.LoadProviderState(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if state.Catalog == nil || state.Catalog.Revision != 7 || !state.Catalog.CreatedAt.Equal(createdAt) {
		t.Fatalf("catalog = %#v", state.Catalog)
	}
	if len(state.Acknowledgements) != 1 || state.Acknowledgements[0].RunnerID != "runner-1" {
		t.Fatalf("acknowledgements = %#v", state.Acknowledgements)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveProviderCatalogUsesMonotonicUpsert(t *testing.T) {
	repository, mock := newRepository(t)
	catalog := appagentharness.ProviderCatalog{
		SchemaVersion: appagentharness.ProviderCatalogSchemaVersion,
		Revision:      8,
		Digest:        "digest-8",
		CreatedAt:     time.Now().UTC(),
		Providers:     []appagentharness.ProviderDefinition{},
	}
	mock.ExpectExec(`(?s)INSERT INTO ai_agent_provider_catalog_state.*EXCLUDED.revision > ai_agent_provider_catalog_state.revision`).
		WithArgs(providerCatalogStateID, uint64(8), "digest-8", sqlmock.AnyArg(), catalog.CreatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.SaveProviderCatalog(context.Background(), catalog); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRegistryAcknowledgementUpsertsAndPrunesInTransaction(t *testing.T) {
	repository, mock := newRepository(t)
	ack := appagentharness.RegistryAcknowledgement{
		RunnerID: "runner-1", Revision: 8, ActiveRevision: 8, Accepted: true,
		ObservedAt: time.Now().UTC(), ProviderStatuses: []appagentharness.RunnerProviderStatus{},
	}
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT INTO ai_agent_provider_registry_acks.*EXCLUDED.observed_at > ai_agent_provider_registry_acks.observed_at`).
		WithArgs(ack.RunnerID, ack.Revision, ack.ActiveRevision, ack.Accepted, ack.Reason, sqlmock.AnyArg(), ack.ObservedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)DELETE FROM ai_agent_provider_registry_acks.*OFFSET`).
		WithArgs(1000).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	if err := repository.SaveRegistryAcknowledgement(context.Background(), ack, 1000); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveRegistryAcknowledgementRejectsPersistedNewerState(t *testing.T) {
	repository, mock := newRepository(t)
	ack := appagentharness.RegistryAcknowledgement{
		RunnerID: "runner-1", Revision: 7, ActiveRevision: 7, Accepted: true,
		ObservedAt: time.Now().UTC(), ProviderStatuses: []appagentharness.RunnerProviderStatus{},
	}
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)INSERT INTO ai_agent_provider_registry_acks.*EXCLUDED.observed_at > ai_agent_provider_registry_acks.observed_at`).
		WithArgs(ack.RunnerID, ack.Revision, ack.ActiveRevision, ack.Accepted, ack.Reason, sqlmock.AnyArg(), ack.ObservedAt).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	if err := repository.SaveRegistryAcknowledgement(context.Background(), ack, 1000); err == nil {
		t.Fatal("expected persisted newer acknowledgement to be rejected")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
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
