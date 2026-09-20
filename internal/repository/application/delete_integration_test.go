package application

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	apierrors "github.com/opensoha/soha/internal/api/errors"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	clusterrepo "github.com/opensoha/soha/internal/repository/cluster"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestApplicationAndClusterDeletionWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_APPLICATION_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_APPLICATION_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	requireApplicationIntegrationNoError(t, err)
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable",
		MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	}, zap.NewNop())
	requireApplicationIntegrationNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	requireApplicationIntegrationNoError(t, store.MigrateFromFile(t.Context(), filepath.Join("..", "..", "..", "migrations", "postgres")))

	for _, archived := range []bool{false, true} {
		t.Run("application history archived="+strconv.FormatBool(archived), func(t *testing.T) {
			tx, id := deletionFixture(t, store.DB())
			addDeletionManifest(t, tx, id)
			if archived {
				requireApplicationIntegrationNoError(t, tx.Exec(`UPDATE manifest_packages SET archived_at=now() WHERE id=?`, id).Error)
			}
			requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO applications (id,name,app_key,app_group,language) VALUES (?, 'Other application', ?, 'test', 'go')`, id+"-other", id+"-other").Error)
			requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO manifest_packages (id,name,application_id) VALUES (?, 'Other manifest', ?)`, id+"-other", id+"-other").Error)
			requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO execution_tasks (id,application_id,task_kind,provider_kind,status) VALUES (?,?,'release','agent','completed')`, id, id).Error)
			requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO workflow_runs (id,application_id,workflow_name,status) VALUES (?,?,'Finished','completed')`, id, id).Error)
			requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO delivery_plans (id,application_id,application_environment_id,action) VALUES (?,?,?,'deploy')`, id, id, id).Error)
			requireApplicationIntegrationNoError(t, New(tx).Delete(t.Context(), " "+id+" "))
			for _, table := range []string{"applications", "application_environments", "manifest_packages", "manifest_revisions", "manifest_bindings", "manifest_deployments", "execution_tasks", "workflow_runs", "delivery_plans"} {
				assertDeletionRowCount(t, tx, table, id, 0)
			}
			assertDeletionRowCount(t, tx, "applications", id+"-other", 1)
			assertDeletionRowCount(t, tx, "manifest_packages", id+"-other", 1)
			assertDeletionRowCount(t, tx, "manifest_sources", "manifest-source-"+id, 0)
			assertDeletionRowCount(t, tx, "clusters", id, 1)
			requireApplicationIntegrationNoError(t, clusterrepo.New(tx).Delete(t.Context(), id))
			assertDeletionRowCount(t, tx, "clusters", id, 0)
			assertDeletionRowCount(t, tx, "cluster_credentials_meta", id, 0)
			if err := New(tx).Delete(t.Context(), id); !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("repeat delete = %v", err)
			}
		})
	}

	t.Run("completed shared batch is retained", func(t *testing.T) {
		tx, id := deletionFixture(t, store.DB())
		addDeletionManifest(t, tx, id)
		addDeletionBlocker(t, tx, id, "batch")
		requireApplicationIntegrationNoError(t, tx.Exec(`UPDATE workflow_runs SET status='completed' WHERE id=?`, id).Error)
		requireApplicationIntegrationNoError(t, New(tx).Delete(t.Context(), id))
		assertDeletionRowCount(t, tx, "applications", id, 0)
		assertDeletionRowCount(t, tx, "delivery_batches", id, 1)
		assertDeletionRowCount(t, tx, "workflow_runs", id, 1)
	})

	for _, state := range []string{"queued", "dispatching", "running", "canceling", "workflow", "batch", "external reference", "cross application binding"} {
		t.Run("application blocked by "+state, func(t *testing.T) {
			tx, id := deletionFixture(t, store.DB())
			addDeletionManifest(t, tx, id)
			addDeletionBlocker(t, tx, id, state)
			err := New(tx).Delete(t.Context(), id)
			if !errors.Is(err, apperrors.ErrConflict) || apierrors.StatusCode(err) != http.StatusConflict {
				t.Fatalf("delete error = %v", err)
			}
			for _, table := range []string{"applications", "application_environments", "manifest_packages", "manifest_revisions", "manifest_bindings", "manifest_deployments"} {
				assertDeletionRowCount(t, tx, table, id, 1)
			}
		})
	}

	for _, reference := range []string{"legacy environment", "release target", "manifest binding", "worker pool"} {
		t.Run("cluster blocked by "+reference, func(t *testing.T) {
			tx, id := deletionFixture(t, store.DB())
			switch reference {
			case "legacy environment":
				requireApplicationIntegrationNoError(t, tx.Exec(`UPDATE application_environments SET cluster_id=? WHERE id=?`, id, id).Error)
			case "release target":
				requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO release_targets (id,application_environment_id,cluster_id,namespace,workload_kind,workload_name,enabled) VALUES (?,?,?,'test','Deployment','test',false)`, id, id, id).Error)
			case "manifest binding":
				addDeletionManifest(t, tx, id)
			case "worker pool":
				requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO virtualization_connections (id,provider,name) VALUES (?,'kubevirt','Test')`, id).Error)
				requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO virtualization_worker_pools (id,connection_id,cluster_id,revision,spec,identity) VALUES (?,?,?,1,'{}','{}')`, id, id, id).Error)
			}
			err := clusterrepo.New(tx).Delete(t.Context(), id)
			if apierrors.StatusCode(err) != http.StatusConflict || apierrors.Code(err) != "cluster_in_use" || !strings.Contains(apierrors.Message(err, "zh-CN"), "使用") {
				t.Fatalf("delete error = %v", err)
			}
			assertDeletionRowCount(t, tx, "clusters", id, 1)
			assertDeletionRowCount(t, tx, "cluster_credentials_meta", id, 1)
		})
	}

	t.Run("cluster FK fallback rolls back credentials", func(t *testing.T) {
		tx, id := deletionFixture(t, store.DB())
		requireApplicationIntegrationNoError(t, tx.Exec(`CREATE TABLE deletion_cluster_reference (cluster_id text REFERENCES clusters(id))`).Error)
		requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO deletion_cluster_reference VALUES (?)`, id).Error)
		err := clusterrepo.New(tx).Delete(context.Background(), id)
		if apierrors.Code(err) != "cluster_in_use" || apierrors.StatusCode(err) != http.StatusConflict {
			t.Fatalf("delete error = %v", err)
		}
		assertDeletionRowCount(t, tx, "clusters", id, 1)
		assertDeletionRowCount(t, tx, "cluster_credentials_meta", id, 1)
	})
}

func deletionFixture(t *testing.T, db *gorm.DB) (*gorm.DB, string) {
	t.Helper()
	tx := db.WithContext(t.Context()).Begin()
	requireApplicationIntegrationNoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback().Error })
	id := uuid.NewString()
	requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO applications (id,name,app_key,app_group,language) VALUES (?,'Deletion regression',?,'test','go')`, id, id).Error)
	requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO application_environments (id,application_id,environment_id) VALUES (?,?,'test')`, id, id).Error)
	requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO clusters (id,name) VALUES (?,'Deletion regression')`, id).Error)
	requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO cluster_credentials_meta (id,cluster_id,credential_type,source_type) VALUES (?,?,'kubeconfig','inline')`, id, id).Error)
	return tx, id
}

func addDeletionManifest(t *testing.T, tx *gorm.DB, id string) {
	t.Helper()
	requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO manifest_packages (id,name,application_id,status,current_revision) VALUES (?,'Published manifest',?,'published',1)`, id, id).Error)
	assertDeletionRowCount(t, tx, "manifest_sources", "manifest-source-"+id, 1)
	requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO manifest_revisions (id,package_id,version,digest) VALUES (?,?,1,'test')`, id, id).Error)
	requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO manifest_bindings (id,package_id,application_environment_id,environment_key,cluster_id,namespace) VALUES (?,?,?,'test',?,'test')`, id, id, id, id).Error)
	requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO manifest_deployments (id,package_id,binding_id,desired_revision,desired_digest) VALUES (?,?,?,1,'test')`, id, id, id).Error)
}

func addDeletionBlocker(t *testing.T, tx *gorm.DB, id, state string) {
	t.Helper()
	switch state {
	case "workflow":
		requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO workflow_runs (id,application_id,workflow_name,status) VALUES (?,?,'Active','running')`, id, id).Error)
	case "batch":
		requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO workflow_runs (id,application_id,workflow_name,status,scope,delivery_batch_id) VALUES (?,'','Active','running','delivery_batch',?)`, id, id).Error)
		requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO delivery_batches (id,root_run_id,created_by,idempotency_key,request_digest,snapshot) VALUES (?,?,'test',?,'sha256:' || repeat('0',64), jsonb_build_object('targets',jsonb_build_array(jsonb_build_object('target',jsonb_build_object('applicationId',?::text)))))`, id, id, id, id).Error)
	case "external reference":
		requireApplicationIntegrationNoError(t, tx.Exec(`CREATE TABLE deletion_application_reference (application_id text REFERENCES applications(id))`).Error)
		requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO deletion_application_reference VALUES (?)`, id).Error)
	case "cross application binding":
		requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO applications (id,name,app_key,app_group,language) VALUES (?,'Other',?,'test','go')`, id+"-other", id+"-other").Error)
		requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO application_environments (id,application_id,environment_id) VALUES (?,?,'test')`, id+"-other", id+"-other").Error)
		requireApplicationIntegrationNoError(t, tx.Exec(`UPDATE manifest_bindings SET application_environment_id=? WHERE id=?`, id+"-other", id).Error)
	default:
		requireApplicationIntegrationNoError(t, tx.Exec(`INSERT INTO execution_tasks (id,application_id,task_kind,provider_kind,status) VALUES (?,?,'release','agent',?)`, id, id, state).Error)
	}
}

func assertDeletionRowCount(t *testing.T, tx *gorm.DB, table, id string, want int) {
	t.Helper()
	var count int
	requireApplicationIntegrationNoError(t, tx.Raw(`SELECT count(*) FROM `+table+` WHERE id=?`, id).Row().Scan(&count))
	if count != want {
		t.Fatalf("%s row count = %d, want %d", table, count, want)
	}
}
