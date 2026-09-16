package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/dbtx"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
	vmrepo "github.com/opensoha/soha/internal/repository/virtualization"
	"go.uber.org/zap"
)

type hostCapacityAdapter struct {
	appvirtualization.Adapter
	source string
}

func (a hostCapacityAdapter) ObserveCapacity(context.Context, domain.AdapterConnection, domain.AdapterCreateVMInput) (domain.CapacitySnapshot, error) {
	return domain.CapacitySnapshot{SourceID: a.source, ObservedAt: time.Now(), Complete: true, Nodes: []domain.CapacityNode{{Name: "node-a", CPU: 8, MemoryMiB: 16000}}, Storage: []domain.CapacityStorage{{Key: "disk", Name: "disk", Nodes: []string{"node-a"}, AvailableGiB: 100}}}, nil
}

func TestDockerHostCapacityTransactionWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	dbConfig := config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4}
	admin, err := dbstore.New(dbConfig, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	dbName := "soha_host_capacity_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(context.Background(), "CREATE DATABASE "+dbName); err != nil {
		t.Fatal(err)
	}
	dbConfig.Name = dbName
	store, err := dbstore.New(dbConfig, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(); _ = admin.Exec(context.Background(), "DROP DATABASE "+dbName) })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	db := store.DB()
	vms := vmrepo.New(db)
	hosts := dockerrepo.New(db)
	connection, err := vms.CreateConnection(ctx, domain.ConnectionInput{Provider: "pve", Name: "capacity-test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	source := "host-capacity/" + uuid.NewString()
	t.Cleanup(func() {
		_ = db.Exec(`DELETE FROM virtualization_capacity_reservations WHERE source_id=?`, source).Error
		_ = db.Exec(`DELETE FROM virtualization_capacity_observations WHERE source_id=?`, source).Error
		_ = db.Exec(`DELETE FROM docker_operations WHERE host_id IN (SELECT id FROM docker_hosts WHERE virtualization_connection_id=?)`, connection.ID).Error
		_ = db.Exec(`DELETE FROM docker_hosts WHERE virtualization_connection_id=?`, connection.ID).Error
		_ = db.Exec(`DELETE FROM virtualization_tasks WHERE connection_id=?`, connection.ID).Error
		_ = db.Exec(`DELETE FROM virtualization_connections WHERE id=?`, connection.ID).Error
	})
	permissions := appaccess.NewPermissionResolver(connectorRuntimeRoleReader{matrix: map[string][]string{"test": {appaccess.ManagedActionPermission(appaccess.PermDockerHostsManage, "create"), appaccess.ManagedActionPermission(appaccess.PermDockerOperationsManage, "retry"), appaccess.ManagedActionPermission(appaccess.PermVirtualizationOperationsManage, "retry"), appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationVMsCreate, appaccess.PermDockerOperationsView, appaccess.PermDockerHostsView}, "admin": {appaccess.PermVirtualizationOperationsView, appaccess.PermVirtualizationVMsView}}})
	principal := domainidentity.Principal{UserID: "capacity-test", UserName: "capacity-test", Roles: []string{"test"}}
	virtualization := appvirtualization.MustNew(appvirtualization.Dependencies{Connections: vms, ConnectionWriter: vms, DockerLinks: vms, VMs: vms, Images: vms, Flavors: vms, Tasks: vms, TaskQueue: vms, TaskLogs: vms}, map[string]appvirtualization.Adapter{"pve": hostCapacityAdapter{source: source}}, permissions, nil, appvirtualization.Options{CredentialEncryptionKey: "isolated-test-key"})
	makeService := func(fail bool) *appdocker.Service {
		return appdocker.New(hosts, permissions, nil, appdocker.WithHostProvisioner(dockerHostProvisioner{virtualization: virtualization}), appdocker.WithAtomicHostCreation(func(ctx context.Context, key string, apply func(context.Context) error) error {
			return dbtx.Within(ctx, db, func(txCtx context.Context) error {
				if err := dbtx.DB(txCtx, db).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "docker-host-create/"+key).Error; err != nil {
					return err
				}
				if err := apply(txCtx); err != nil {
					return err
				}
				if fail {
					return errors.New("failure after all host, VM, reservation and operation writes")
				}
				return nil
			})
		}))
	}
	input := domaindocker.QuickCreateHostInput{Name: "stable-host", VirtualizationConnectionID: connection.ID, RequireCapacity: true, CPUCoreCount: 2, MemoryBytes: 4 << 30, DiskBytes: 40 << 30, CloudInit: "#cloud-config", IdempotencyKey: uuid.NewString()}
	if _, err := makeService(true).QuickCreateHost(ctx, principal, input); err == nil {
		t.Fatal("injected transaction failure was hidden")
	}
	verifyDockerCapacityRowCounts(t, ctx, store, connection.ID, source, 0)
	service := makeService(false)
	id := createDockerHostConcurrently(t, ctx, service, principal, input)
	verifyDockerCapacityRowCounts(t, ctx, store, connection.ID, source, 1)
	verifyDockerHostRetryTransaction(t, ctx, store, principal, id, service, makeService(true))
}

func createDockerHostConcurrently(t *testing.T, ctx context.Context, service *appdocker.Service, principal domainidentity.Principal, input domaindocker.QuickCreateHostInput) string {
	t.Helper()
	var wg sync.WaitGroup
	results := make(chan domaindocker.Operation, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := service.QuickCreateHost(ctx, principal, input)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for result := range results {
		if id == "" {
			id = result.ID
		}
		if result.ID != id || result.Payload["virtualizationTaskId"] == nil {
			t.Fatalf("lost shared receipt: %+v", result)
		}
	}
	return id
}

func verifyDockerHostRetryTransaction(t *testing.T, ctx context.Context, store *dbstore.Store, principal domainidentity.Principal, id string, service, failing *appdocker.Service) {
	t.Helper()
	db := store.DB()
	vms := vmrepo.New(db)
	hosts := dockerrepo.New(db)
	operation, err := hosts.GetOperation(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	vmTaskID, ok := operation.Payload["virtualizationTaskId"].(string)
	if !ok {
		t.Fatalf("virtualizationTaskId type = %T, want string", operation.Payload["virtualizationTaskId"])
	}
	if err := db.Exec(`UPDATE docker_operations SET status='failed' WHERE id=?`, id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE virtualization_tasks SET status='failed',result='{"providerEffect":"not_started"}'::jsonb WHERE id=?`, vmTaskID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := failing.RetryOperation(ctx, principal, id); err == nil || !strings.Contains(err.Error(), "failure after all") {
		t.Fatalf("retry did not reach injected rollback: %v", err)
	}
	vmTask, _ := vms.GetTask(ctx, vmTaskID)
	operation, _ = hosts.GetOperation(ctx, id)
	if vmTask.Status != "failed" || operation.Status != "failed" {
		t.Fatalf("partial parent/VM retry survived rollback: %s %s", operation.Status, vmTask.Status)
	}
	if _, err := service.RetryOperation(ctx, principal, id); err != nil {
		t.Fatal(err)
	}
	vmTask, _ = vms.GetTask(ctx, vmTaskID)
	operation, _ = hosts.GetOperation(ctx, id)
	if vmTask.Status != "queued" || operation.Status != "queued" {
		t.Fatalf("retry did not atomically queue original parent and VM: %s %s", operation.Status, vmTask.Status)
	}
}

func verifyDockerCapacityRowCounts(t *testing.T, ctx context.Context, store *dbstore.Store, connectionID, source string, expected int64) {
	t.Helper()
	for _, query := range []struct{ sql, id string }{
		{`SELECT count(*) FROM docker_hosts WHERE virtualization_connection_id=?`, connectionID},
		{`SELECT count(*) FROM virtualization_tasks WHERE connection_id=?`, connectionID},
		{`SELECT count(*) FROM virtualization_capacity_reservations WHERE source_id=?`, source},
	} {
		var count int64
		if err := store.DB().WithContext(ctx).Raw(query.sql, query.id).Scan(&count).Error; err != nil || count != expected {
			t.Fatalf("creation rows = %d, expected %d: %s: %v", count, expected, query.sql, err)
		}
	}
}
