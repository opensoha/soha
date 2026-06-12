package virtualization

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	infravirtualization "github.com/opensoha/soha/internal/infrastructure/virtualization"
)

func TestWorkerAssetSyncUpdatesTaskAndRecordsOperation(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{
		Provider: ProviderKubeVirt,
		Name:     "kv",
		Enabled:  true,
		Config:   map[string]any{},
	})
	task, err := repo.CreateTask(context.Background(), domainvirtualization.Task{
		Provider:     conn.Provider,
		ConnectionID: conn.ID,
		TaskKind:     TaskKindAssetSync,
		Status:       TaskStatusQueued,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	ops := &captureOperations{}
	service := newTestService(repo, ops, fakeAdapter{
		syncResult: infravirtualization.AssetSyncResult{
			Health: infravirtualization.AssetHealth{Status: "healthy"},
			Assets: []infravirtualization.Asset{
				{Type: "virtualmachine", Name: "vm-a", Namespace: "default", Status: "running", Metadata: map[string]string{"uid": "vm-a"}},
				{Type: "datasource", Name: "ubuntu", Status: "ready", Metadata: map[string]string{"uid": "img-a"}},
			},
		},
	})

	service.runOnce(context.Background())

	updated, err := repo.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if updated.Status != TaskStatusSucceeded {
		t.Fatalf("task status = %q, want %q", updated.Status, TaskStatusSucceeded)
	}
	if len(repo.vms) != 1 || len(repo.images) != 1 {
		t.Fatalf("synced assets vms=%d images=%d, want 1 and 1", len(repo.vms), len(repo.images))
	}
	if !ops.has("virtualization.worker.asset_sync") {
		t.Fatalf("operation log missing worker asset sync entry: %#v", ops.entries)
	}
}

func TestCreateConnectionEncryptsAndSanitizesCredential(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	item, err := service.CreateConnection(context.Background(), testPrincipal(), ConnectionInput{
		Provider: ProviderPVE,
		Name:     "pve",
		Endpoint: "https://pve.example",
		Credential: map[string]any{
			"tokenID":     "root@pam!api",
			"tokenSecret": "secret",
		},
	})
	if err != nil {
		t.Fatalf("CreateConnection() error = %v", err)
	}
	if !item.CredentialConfigured {
		t.Fatalf("CredentialConfigured = false, want true")
	}
	if len(item.EncryptedCredential) != 0 {
		t.Fatalf("response contains encrypted credential: %#v", item.EncryptedCredential)
	}
	stored, err := repo.GetConnection(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	if stored.EncryptedCredential["ciphertext"] == "" {
		t.Fatalf("stored credential missing ciphertext")
	}
}

func TestCreatePVECredentialRequiresEncryptionKey(t *testing.T) {
	repo := newMemoryRepo()
	service := New(repo, map[string]Adapter{ProviderPVE: fakeAdapter{}}, testPermissions(), nil, Options{})

	_, err := service.CreateConnection(context.Background(), testPrincipal(), ConnectionInput{
		Provider:   ProviderPVE,
		Name:       "pve",
		Credential: map[string]any{"tokenSecret": "secret"},
	})
	if err == nil {
		t.Fatalf("CreateConnection() error = nil, want invalid credential key error")
	}
	if len(repo.connections) != 0 {
		t.Fatalf("connection stored despite credential encryption failure")
	}
}

func TestSyncAllEnqueuesEnabledConnections(t *testing.T) {
	repo := newMemoryRepo()
	repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "enabled-a", Enabled: true})
	repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "disabled", Enabled: false})
	repo.addConnection(domainvirtualization.Connection{Provider: ProviderPVE, Name: "enabled-b", Enabled: true})
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	task, err := service.SyncAll(context.Background(), testPrincipal())
	if err != nil {
		t.Fatalf("SyncAll() error = %v", err)
	}
	if task.TaskKind != TaskKindAssetSync {
		t.Fatalf("first task kind = %q, want %q", task.TaskKind, TaskKindAssetSync)
	}
	if len(repo.tasks) != 2 {
		t.Fatalf("tasks = %d, want 2 enabled connection sync tasks", len(repo.tasks))
	}
}

func TestUpdateConnectionPreservesHealth(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{
		Provider:  ProviderKubeVirt,
		Name:      "kv",
		Enabled:   true,
		VerifyTLS: true,
		Health:    map[string]any{"status": "healthy", "message": "ok"},
		Config:    map[string]any{"region": "cn"},
	})
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	updated, err := service.UpdateConnection(context.Background(), testPrincipal(), conn.ID, ConnectionInput{
		Provider: ProviderKubeVirt,
		Name:     "kv-new",
	})
	if err != nil {
		t.Fatalf("UpdateConnection() error = %v", err)
	}
	if updated.Health["status"] != "healthy" {
		t.Fatalf("health status = %#v, want preserved healthy", updated.Health)
	}
}

func TestWorkerAssetSyncPersistsConnectionHealthAndMarksStale(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{
		Provider: ProviderKubeVirt,
		Name:     "kv",
		Enabled:  true,
		Config:   map[string]any{},
		Health:   map[string]any{"status": "unknown"},
	})
	oldTime := time.Now().Add(-time.Hour)
	repo.vms["stale-vm"] = domainvirtualization.VM{
		ID:           "stale-vm",
		Provider:     conn.Provider,
		ConnectionID: conn.ID,
		ExternalID:   "old",
		Name:         "old",
		Status:       "running",
		LastSeenAt:   &oldTime,
	}
	_, err := repo.CreateTask(context.Background(), domainvirtualization.Task{
		Provider:     conn.Provider,
		ConnectionID: conn.ID,
		TaskKind:     TaskKindAssetSync,
		Status:       TaskStatusQueued,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	service := newTestService(repo, &captureOperations{}, fakeAdapter{
		syncResult: infravirtualization.AssetSyncResult{
			Health: infravirtualization.AssetHealth{Status: "healthy", Message: "ok"},
			Assets: []infravirtualization.Asset{
				{Type: "virtualmachine", Name: "vm-a", Namespace: "default", Status: "running", Metadata: map[string]string{"uid": "vm-a"}},
			},
		},
	})

	service.runOnce(context.Background())

	updatedConn, err := repo.GetConnection(context.Background(), conn.ID)
	if err != nil {
		t.Fatalf("GetConnection() error = %v", err)
	}
	if updatedConn.Health["status"] != "healthy" || updatedConn.LastSyncedAt == nil {
		t.Fatalf("connection health/sync not updated: %#v synced=%v", updatedConn.Health, updatedConn.LastSyncedAt)
	}
	if repo.vms["stale-vm"].Status != "stale" {
		t.Fatalf("stale vm status = %q, want stale", repo.vms["stale-vm"].Status)
	}
}

func TestCancelAndRetryOperation(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})
	task, err := repo.CreateTask(context.Background(), domainvirtualization.Task{
		Provider:       ProviderKubeVirt,
		TaskKind:       TaskKindAssetSync,
		Status:         TaskStatusQueued,
		MaxRetries:     1,
		TimeoutSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	canceled, err := service.CancelOperation(context.Background(), testPrincipal(), task.ID)
	if err != nil {
		t.Fatalf("CancelOperation() error = %v", err)
	}
	if canceled.Status != TaskStatusCanceled {
		t.Fatalf("canceled status = %q", canceled.Status)
	}
	if canceled.OperationState == nil || !canceled.OperationState.Terminal || !canceled.OperationState.Retryable {
		t.Fatalf("canceled operation state = %#v", canceled.OperationState)
	}
	if repo.tasks[task.ID].OperationState != nil {
		t.Fatalf("stored task should not keep derived operation state: %#v", repo.tasks[task.ID].OperationState)
	}
	retried, err := service.RetryOperation(context.Background(), testPrincipal(), task.ID)
	if err != nil {
		t.Fatalf("RetryOperation() error = %v", err)
	}
	if retried.Status != TaskStatusQueued || retried.StartedAt != nil || retried.FinishedAt != nil {
		t.Fatalf("retry did not reset task: %#v", retried)
	}
	if retried.OperationState == nil || retried.OperationState.Phase != "pending" || !retried.OperationState.Cancelable {
		t.Fatalf("retried operation state = %#v", retried.OperationState)
	}
}

func TestListAndGetOperationsAttachOperationState(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})
	finishedAt := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	task, err := repo.CreateTask(context.Background(), domainvirtualization.Task{
		Provider:       ProviderPVE,
		ConnectionID:   "conn-1",
		TaskKind:       TaskKindVMAction,
		Status:         TaskStatusFailed,
		TimeoutSeconds: 300,
		Result:         map[string]any{"error": "pve action failed"},
		FinishedAt:     &finishedAt,
		CreatedAt:      finishedAt.Add(-time.Minute),
		UpdatedAt:      finishedAt,
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	items, err := service.ListOperations(context.Background(), testPrincipal(), domainvirtualization.TaskFilter{})
	if err != nil {
		t.Fatalf("ListOperations() error = %v", err)
	}
	if len(items) != 1 || items[0].OperationState == nil || items[0].OperationState.Phase != "failed" || !items[0].OperationState.Retryable {
		t.Fatalf("listed task missing operation state: %#v", items)
	}

	got, err := service.GetOperation(context.Background(), testPrincipal(), task.ID)
	if err != nil {
		t.Fatalf("GetOperation() error = %v", err)
	}
	if got.OperationState == nil || got.OperationState.FailureMessage != "pve action failed" {
		t.Fatalf("got task missing failure state: %#v", got.OperationState)
	}
	if repo.tasks[task.ID].OperationState != nil {
		t.Fatalf("stored task should not keep derived operation state: %#v", repo.tasks[task.ID].OperationState)
	}
}

func TestOverviewIncludesAttentionAndSummaries(t *testing.T) {
	repo := newMemoryRepo()
	unavailableConn := repo.addConnection(domainvirtualization.Connection{Provider: ProviderPVE, Name: "pve-a", Enabled: true, Health: map[string]any{"status": "unavailable"}})
	degradedConn := repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "kv-a", Enabled: true, CredentialConfigured: true, Health: map[string]any{"status": "degraded"}})
	repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "kv-b", Enabled: true, CredentialConfigured: false, Health: map[string]any{"status": "healthy"}})
	repo.vms["vm-pve"] = domainvirtualization.VM{ID: "vm-pve", Provider: ProviderPVE, ConnectionID: unavailableConn.ID, Name: "vm-pve", Status: "running"}
	repo.vms["vm-kv"] = domainvirtualization.VM{ID: "vm-kv", Provider: ProviderKubeVirt, ConnectionID: degradedConn.ID, Name: "vm-kv", Status: "stopped"}
	repo.tasks["sync-failed"] = domainvirtualization.Task{ID: "sync-failed", Provider: ProviderPVE, ConnectionID: unavailableConn.ID, TaskKind: TaskKindAssetSync, Status: TaskStatusFailed, Result: map[string]any{"message": "sync failed"}, CreatedAt: time.Now().UTC()}
	repo.tasks["vm-failed"] = domainvirtualization.Task{ID: "vm-failed", Provider: ProviderKubeVirt, ConnectionID: degradedConn.ID, TaskKind: TaskKindVMAction, Status: TaskStatusTimeout, Result: map[string]any{"error": "timeout"}, CreatedAt: time.Now().UTC().Add(-time.Minute)}
	repo.tasks["pending"] = domainvirtualization.Task{ID: "pending", Provider: ProviderKubeVirt, ConnectionID: degradedConn.ID, TaskKind: TaskKindVMCreate, Status: TaskStatusRunning, CreatedAt: time.Now().UTC().Add(-2 * time.Minute)}
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	overview, err := service.Overview(context.Background(), testPrincipal())
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.ConnectionSummary.Unavailable != 1 || overview.ConnectionSummary.Degraded != 1 || overview.ConnectionSummary.CredentialMissing != 2 || overview.ConnectionSummary.NeverSynced != 3 {
		t.Fatalf("connection summary = %#v", overview.ConnectionSummary)
	}
	if overview.TaskSummary.Running != 1 || overview.TaskSummary.Failed != 1 || overview.TaskSummary.Timeout != 1 {
		t.Fatalf("task summary = %#v", overview.TaskSummary)
	}
	if len(overview.Attention.RiskyConnections) == 0 || len(overview.Attention.FailedSyncTasks) != 1 || len(overview.Attention.FailedOperations) != 2 {
		t.Fatalf("attention = %#v", overview.Attention)
	}
	if len(overview.ProviderSummary) != 2 {
		t.Fatalf("provider summary = %#v", overview.ProviderSummary)
	}
	if overview.ProviderSummary[0].Provider != ProviderKubeVirt || overview.ProviderSummary[0].Connections != 2 || overview.ProviderSummary[0].VMs != 1 {
		t.Fatalf("kubevirt provider summary = %#v", overview.ProviderSummary[0])
	}
	if overview.ProviderSummary[1].Provider != ProviderPVE || overview.ProviderSummary[1].Unavailable != 1 || overview.ProviderSummary[1].RunningVMs != 1 {
		t.Fatalf("pve provider summary = %#v", overview.ProviderSummary[1])
	}
}

func TestCreateVMUsesFlavorAndImageSelection(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{
		Provider: ProviderKubeVirt,
		Name:     "kv",
		Enabled:  true,
		Config:   map[string]any{},
	})
	flavor := domainvirtualization.Flavor{
		ID:           "flavor-1",
		Provider:     ProviderKubeVirt,
		ConnectionID: "",
		ExternalID:   "standard-2c4g",
		Name:         "standard-2c4g",
		Status:       "active",
		CPUCores:     2,
		MemoryMB:     4096,
		DiskGB:       40,
	}
	repo.flavors[flavor.ID] = flavor
	image := domainvirtualization.Image{
		ID:           "image-1",
		Provider:     ProviderKubeVirt,
		ConnectionID: conn.ID,
		ExternalID:   "default/ubuntu",
		Name:         "ubuntu",
		Status:       "active",
	}
	repo.images[image.ID] = image
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	task, err := service.CreateVM(context.Background(), testPrincipal(), CreateVMInput{
		ConnectionID:     conn.ID,
		Name:             "vm-a",
		FlavorID:         flavor.ID,
		BootImageID:      image.ID,
		StartAfterCreate: true,
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if task.Payload["flavorId"] != flavor.ID || task.Payload["imageId"] != image.ID {
		t.Fatalf("task payload missing flavor/image: %#v", task.Payload)
	}
	if task.Payload["sourceId"] != image.ExternalID {
		t.Fatalf("task payload sourceId = %#v, want %q", task.Payload["sourceId"], image.ExternalID)
	}
	if task.Payload["cpu"] != flavor.CPUCores || task.Payload["memoryMiB"] != flavor.MemoryMB || task.Payload["diskGiB"] != flavor.DiskGB {
		t.Fatalf("task payload did not inherit flavor resources: %#v", task.Payload)
	}
}

func TestImageManagementAndVMDetail(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "kv", Enabled: true})
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	image, err := service.CreateImage(context.Background(), testPrincipal(), ImageInput{
		ConnectionID: conn.ID,
		Name:         "ubuntu",
		SourceKind:   "datasource",
		ExternalID:   "default/ubuntu",
		SizeGiB:      10,
		Config:       map[string]any{"sourceRef": "default/ubuntu"},
	})
	if err != nil {
		t.Fatalf("CreateImage() error = %v", err)
	}
	if image.SizeBytes != 10*1024*1024*1024 {
		t.Fatalf("SizeBytes = %d, want 10GiB", image.SizeBytes)
	}
	flavor := domainvirtualization.Flavor{ID: "flavor-1", Provider: ProviderKubeVirt, ExternalID: "flavor-1", Name: "small", CPUCores: 1, MemoryMB: 1024, DiskGB: 20}
	repo.flavors[flavor.ID] = flavor
	vm := domainvirtualization.VM{ID: "vm-1", Provider: ProviderKubeVirt, ConnectionID: conn.ID, ExternalID: "vm-1", Name: "vm-1", ImageID: image.ID, FlavorID: flavor.ID, Raw: map[string]any{"kind": "VirtualMachine"}}
	repo.vms[vm.ID] = vm
	_, _ = repo.CreateTask(context.Background(), domainvirtualization.Task{ID: "task-1", Provider: ProviderKubeVirt, ConnectionID: conn.ID, VMID: vm.ID, TaskKind: TaskKindVMAction, Status: TaskStatusSucceeded})
	_ = repo.CreateTaskLog(context.Background(), domainvirtualization.TaskLog{TaskID: "task-1", LogLevel: "info", Message: "started"})

	detail, err := service.GetVMDetail(context.Background(), testPrincipal(), vm.ID)
	if err != nil {
		t.Fatalf("GetVMDetail() error = %v", err)
	}
	if detail.Image == nil || detail.Image.ID != image.ID || detail.Flavor == nil || detail.Flavor.ID != flavor.ID {
		t.Fatalf("detail missing image/flavor: %#v", detail)
	}
	if len(detail.Operations) != 1 || len(detail.Logs) != 1 {
		t.Fatalf("detail operations/logs = %d/%d, want 1/1", len(detail.Operations), len(detail.Logs))
	}
}

func TestCreateImageAcceptsSourceRef(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "kv", Enabled: true})
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	image, err := service.CreateImage(context.Background(), testPrincipal(), ImageInput{
		ConnectionID: conn.ID,
		Name:         "cirros-containerdisk",
		SourceKind:   "containerdisk",
		SourceRef:    "quay.io/containerdisks/cirros:latest",
	})
	if err != nil {
		t.Fatalf("CreateImage() error = %v", err)
	}
	if got := image.Config["sourceRef"]; got != "quay.io/containerdisks/cirros:latest" {
		t.Fatalf("sourceRef = %#v, want container disk image reference", got)
	}
	if image.ExternalID != "quay.io/containerdisks/cirros:latest" {
		t.Fatalf("ExternalID = %q, want sourceRef", image.ExternalID)
	}
}

func TestWorkerVMCreateUsesImageSourceRefForProviderBootImage(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "kv", Enabled: true})
	flavor := domainvirtualization.Flavor{ID: "flavor-1", Provider: ProviderKubeVirt, Name: "tiny", CPUCores: 1, MemoryMB: 512, DiskGB: 1}
	repo.flavors[flavor.ID] = flavor
	image := domainvirtualization.Image{
		ID:           "image-1",
		Provider:     ProviderKubeVirt,
		ConnectionID: conn.ID,
		ExternalID:   "quay.io/containerdisks/cirros:latest",
		Name:         "cirros-containerdisk",
		Status:       "active",
		Config:       map[string]any{"sourceKind": "containerdisk", "sourceRef": "quay.io/containerdisks/cirros:latest"},
	}
	repo.images[image.ID] = image
	adapter := &recordingAdapter{}
	service := newTestService(repo, &captureOperations{}, adapter)

	_, err := service.CreateVM(context.Background(), testPrincipal(), CreateVMInput{
		ConnectionID: conn.ID,
		Name:         "vm-a",
		FlavorID:     flavor.ID,
		BootImageID:  image.ID,
		SourceMode:   "containerdisk",
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	service.runOnce(context.Background())

	if adapter.createInput.BootImage != image.ExternalID {
		t.Fatalf("BootImage = %q, want sourceRef %q", adapter.createInput.BootImage, image.ExternalID)
	}
	if adapter.createInput.SourceRef != image.ExternalID {
		t.Fatalf("SourceRef = %q, want sourceRef %q", adapter.createInput.SourceRef, image.ExternalID)
	}
	var stored domainvirtualization.VM
	for _, item := range repo.vms {
		stored = item
		break
	}
	if stored.ImageID != image.ID {
		t.Fatalf("stored ImageID = %q, want original image id %q", stored.ImageID, image.ID)
	}
}

func TestGetVMMetricsReturnsAdapterResult(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "kv", Enabled: true})
	vm := domainvirtualization.VM{ID: "vm-1", Provider: ProviderKubeVirt, ConnectionID: conn.ID, ExternalID: "vm-1", Name: "vm-1", Namespace: "default", Status: "running"}
	repo.vms[vm.ID] = vm

	expected := infravirtualization.VMMetricsResult{
		Series: []infravirtualization.MetricSeries{
			{Key: "cpu", Label: "CPU", Unit: "cores", Points: []infravirtualization.MetricPoint{{Timestamp: 1700000000, Value: 0.42}}},
		},
	}
	service := newTestService(repo, &captureOperations{}, fakeAdapter{metricsResult: expected})

	result, err := service.GetVMMetrics(context.Background(), testPrincipal(), vm.ID, 60, 60)
	if err != nil {
		t.Fatalf("GetVMMetrics() error = %v", err)
	}
	if len(result.Series) != 1 || result.Series[0].Key != "cpu" || len(result.Series[0].Points) != 1 {
		t.Fatalf("metrics series = %#v, want passthrough from adapter", result.Series)
	}
	if result.Series[0].Points[0].Value != 0.42 {
		t.Fatalf("metrics point value = %v, want 0.42", result.Series[0].Points[0].Value)
	}
}

func TestGetVMMetricsReturnsErrorWhenVMMissing(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	_, err := service.GetVMMetrics(context.Background(), testPrincipal(), "missing-vm", 60, 60)
	if err == nil {
		t.Fatalf("GetVMMetrics() expected error for missing VM")
	}
}

func TestGetConsoleURLReturnsAdapterResult(t *testing.T) {
	repo := newMemoryRepo()
	conn := repo.addConnection(domainvirtualization.Connection{Provider: ProviderKubeVirt, Name: "kv", Enabled: true})
	vm := domainvirtualization.VM{ID: "vm-1", Provider: ProviderKubeVirt, ConnectionID: conn.ID, ExternalID: "vm-1", Name: "vm-1", Namespace: "default", Status: "running"}
	repo.vms[vm.ID] = vm

	expected := infravirtualization.ConsoleURLResult{Type: "vnc", URL: "/api/v1/virtualization/vms/vm-1/console/vnc", Token: "secret"}
	service := newTestService(repo, &captureOperations{}, fakeAdapter{consoleResult: expected})

	result, err := service.GetConsoleURL(context.Background(), testPrincipal(), vm.ID)
	if err != nil {
		t.Fatalf("GetConsoleURL() error = %v", err)
	}
	if result.Type != expected.Type || result.URL != expected.URL || result.Token != expected.Token {
		t.Fatalf("console result = %#v, want %#v", result, expected)
	}
}

func TestGetConsoleURLReturnsErrorWhenVMMissing(t *testing.T) {
	repo := newMemoryRepo()
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})

	_, err := service.GetConsoleURL(context.Background(), testPrincipal(), "missing-vm")
	if err == nil {
		t.Fatalf("GetConsoleURL() expected error for missing VM")
	}
}

func newTestService(repo *memoryRepo, ops *captureOperations, adapter Adapter) *Service {
	return New(repo, map[string]Adapter{
		ProviderKubeVirt: adapter,
		ProviderPVE:      adapter,
	}, testPermissions(), ops, Options{CredentialEncryptionKey: "test-secret"})
}

func testPermissions() *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(testRoleReader{matrix: map[string][]string{
		"admin": {
			appaccess.PermVirtualizationOverviewView,
			appaccess.PermVirtualizationClustersView,
			appaccess.PermVirtualizationClustersManage,
			appaccess.PermVirtualizationVMsView,
			appaccess.PermVirtualizationVMsManage,
			appaccess.PermVirtualizationImagesView,
			appaccess.PermVirtualizationImagesManage,
			appaccess.PermVirtualizationFlavorsView,
			appaccess.PermVirtualizationFlavorsManage,
			appaccess.PermVirtualizationOperationsView,
			appaccess.PermVirtualizationOperationsManage,
			appaccess.PermVirtualizationSyncView,
			appaccess.PermVirtualizationSyncManage,
		},
	}})
}

func testPrincipal() domainidentity.Principal {
	return domainidentity.Principal{UserID: "admin", UserName: "Admin", Roles: []string{"admin"}}
}

type testRoleReader struct {
	matrix map[string][]string
}

func (r testRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.matrix, nil
}

type captureOperations struct {
	entries []domainoperation.Entry
}

func (c *captureOperations) Record(_ context.Context, entry domainoperation.Entry) error {
	c.entries = append(c.entries, entry)
	return nil
}

func (c *captureOperations) has(operationType string) bool {
	return slices.ContainsFunc(c.entries, func(entry domainoperation.Entry) bool {
		return entry.OperationType == operationType && entry.TargetScope["module"] == "virtualization"
	})
}

type fakeAdapter struct {
	syncResult    infravirtualization.AssetSyncResult
	metricsResult infravirtualization.VMMetricsResult
	consoleResult infravirtualization.ConsoleURLResult
}

func (a fakeAdapter) TestConnection(context.Context, infravirtualization.Connection) (infravirtualization.ConnectionTestResult, error) {
	return infravirtualization.ConnectionTestResult{Healthy: true, Status: "healthy"}, nil
}

func (a fakeAdapter) SyncAssets(context.Context, infravirtualization.Connection) (infravirtualization.AssetSyncResult, error) {
	if a.syncResult.Health.Status == "" {
		a.syncResult.Health.Status = "healthy"
	}
	return a.syncResult, nil
}

func (a fakeAdapter) CreateVM(_ context.Context, _ infravirtualization.Connection, input infravirtualization.CreateVMInput) (infravirtualization.VM, error) {
	return infravirtualization.VM{ID: "vm-1", Name: input.Name, Namespace: input.Namespace, Node: input.Node, Status: "running"}, nil
}

func (a fakeAdapter) PowerAction(_ context.Context, _ infravirtualization.Connection, vm infravirtualization.VM, action infravirtualization.PowerAction) (infravirtualization.PowerActionResult, error) {
	return infravirtualization.PowerActionResult{Accepted: true, Action: action}, nil
}

func (a fakeAdapter) GetVMMetrics(_ context.Context, _ infravirtualization.Connection, _ infravirtualization.VM, _, _ int) (infravirtualization.VMMetricsResult, error) {
	if a.metricsResult.Series != nil || a.metricsResult.Message != "" {
		return a.metricsResult, nil
	}
	return infravirtualization.VMMetricsResult{Series: []infravirtualization.MetricSeries{}}, nil
}

func (a fakeAdapter) GetConsoleURL(_ context.Context, _ infravirtualization.Connection, _ infravirtualization.VM) (infravirtualization.ConsoleURLResult, error) {
	if a.consoleResult.Type != "" || a.consoleResult.Message != "" {
		return a.consoleResult, nil
	}
	return infravirtualization.ConsoleURLResult{Type: "vnc", URL: "/console"}, nil
}

type recordingAdapter struct {
	fakeAdapter
	createInput infravirtualization.CreateVMInput
}

func (a *recordingAdapter) CreateVM(_ context.Context, _ infravirtualization.Connection, input infravirtualization.CreateVMInput) (infravirtualization.VM, error) {
	a.createInput = input
	return infravirtualization.VM{ID: "vm-1", Name: input.Name, Namespace: input.Namespace, Node: input.Node, Status: "running"}, nil
}

type memoryRepo struct {
	mu          sync.Mutex
	connections map[string]domainvirtualization.Connection
	vms         map[string]domainvirtualization.VM
	images      map[string]domainvirtualization.Image
	flavors     map[string]domainvirtualization.Flavor
	tasks       map[string]domainvirtualization.Task
	logs        map[string][]domainvirtualization.TaskLog
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		connections: map[string]domainvirtualization.Connection{},
		vms:         map[string]domainvirtualization.VM{},
		images:      map[string]domainvirtualization.Image{},
		flavors:     map[string]domainvirtualization.Flavor{},
		tasks:       map[string]domainvirtualization.Task{},
		logs:        map[string][]domainvirtualization.TaskLog{},
	}
}

func (r *memoryRepo) addConnection(item domainvirtualization.Connection) domainvirtualization.Connection {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.ID == "" {
		item.ID = newID()
	}
	r.connections[item.ID] = item
	return item
}

func (r *memoryRepo) CreateConnection(_ context.Context, input domainvirtualization.ConnectionInput) (domainvirtualization.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := domainvirtualization.Connection{
		ID:                   firstNonEmpty(input.ID, newID()),
		Provider:             input.Provider,
		Name:                 input.Name,
		Endpoint:             input.Endpoint,
		KubernetesClusterID:  input.KubernetesClusterID,
		DefaultNamespace:     input.DefaultNamespace,
		Enabled:              input.Enabled,
		VerifyTLS:            input.VerifyTLS,
		EncryptedCredential:  input.EncryptedCredential,
		CredentialConfigured: len(input.EncryptedCredential) > 0,
		Config:               input.Config,
		Health:               input.Health,
	}
	r.connections[item.ID] = item
	return item, nil
}

func (r *memoryRepo) UpdateConnection(_ context.Context, id string, input domainvirtualization.ConnectionInput) (domainvirtualization.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.connections[id]
	item.Provider = input.Provider
	item.Name = input.Name
	item.Endpoint = input.Endpoint
	item.KubernetesClusterID = input.KubernetesClusterID
	item.DefaultNamespace = input.DefaultNamespace
	item.Enabled = input.Enabled
	item.VerifyTLS = input.VerifyTLS
	item.EncryptedCredential = input.EncryptedCredential
	item.CredentialConfigured = len(input.EncryptedCredential) > 0
	item.Config = input.Config
	item.Health = input.Health
	r.connections[id] = item
	return item, nil
}

func (r *memoryRepo) DeleteConnection(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.connections, id)
	return nil
}

func (r *memoryRepo) GetConnection(_ context.Context, id string) (domainvirtualization.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.connections[id]
	if !ok {
		return domainvirtualization.Connection{}, errMemoryNotFound
	}
	return item, nil
}

func (r *memoryRepo) ListConnections(_ context.Context, filter domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []domainvirtualization.Connection{}
	for _, item := range r.connections {
		if filter.Provider != "" && item.Provider != filter.Provider {
			continue
		}
		if filter.Enabled != nil && item.Enabled != *filter.Enabled {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryRepo) CountConnections(ctx context.Context, filter domainvirtualization.ConnectionFilter) (int, error) {
	items, err := r.ListConnections(ctx, filter)
	return len(items), err
}

func (r *memoryRepo) UpsertVM(_ context.Context, item domainvirtualization.VM) (domainvirtualization.VM, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.ID == "" {
		item.ID = newID()
	}
	r.vms[item.ID] = item
	return item, nil
}

func (r *memoryRepo) GetVM(_ context.Context, id string) (domainvirtualization.VM, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.vms[id]
	if !ok {
		return domainvirtualization.VM{}, errMemoryNotFound
	}
	return item, nil
}

func (r *memoryRepo) ListVMs(context.Context, domainvirtualization.VMFilter) ([]domainvirtualization.VM, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []domainvirtualization.VM{}
	for _, item := range r.vms {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryRepo) CountVMs(ctx context.Context, filter domainvirtualization.VMFilter) (int, error) {
	items, err := r.ListVMs(ctx, filter)
	return len(items), err
}

func (r *memoryRepo) UpsertImage(_ context.Context, item domainvirtualization.Image) (domainvirtualization.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.ID == "" {
		item.ID = newID()
	}
	r.images[item.ID] = item
	return item, nil
}

func (r *memoryRepo) GetImage(_ context.Context, id string) (domainvirtualization.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.images[id]
	if !ok {
		return domainvirtualization.Image{}, errMemoryNotFound
	}
	return item, nil
}

func (r *memoryRepo) ListImages(context.Context, domainvirtualization.ImageFilter) ([]domainvirtualization.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []domainvirtualization.Image{}
	for _, item := range r.images {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryRepo) CountImages(ctx context.Context, filter domainvirtualization.ImageFilter) (int, error) {
	items, err := r.ListImages(ctx, filter)
	return len(items), err
}

func (r *memoryRepo) UpsertFlavor(_ context.Context, item domainvirtualization.Flavor) (domainvirtualization.Flavor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.ID == "" {
		item.ID = newID()
	}
	if item.ExternalID == "" {
		item.ExternalID = firstNonEmpty(item.ID, item.Name)
	}
	r.flavors[item.ID] = item
	return item, nil
}

func (r *memoryRepo) GetFlavor(_ context.Context, id string) (domainvirtualization.Flavor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.flavors[id]
	if !ok {
		return domainvirtualization.Flavor{}, errMemoryNotFound
	}
	return item, nil
}

func (r *memoryRepo) ListFlavors(_ context.Context, filter domainvirtualization.FlavorFilter) ([]domainvirtualization.Flavor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []domainvirtualization.Flavor{}
	for _, item := range r.flavors {
		if filter.ConnectionID != "" && item.ConnectionID != filter.ConnectionID {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryRepo) CountFlavors(ctx context.Context, filter domainvirtualization.FlavorFilter) (int, error) {
	items, err := r.ListFlavors(ctx, filter)
	return len(items), err
}

func (r *memoryRepo) CreateTask(_ context.Context, item domainvirtualization.Task) (domainvirtualization.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.ID == "" {
		item.ID = newID()
	}
	r.tasks[item.ID] = item
	return item, nil
}

func (r *memoryRepo) UpdateTask(_ context.Context, item domainvirtualization.Task) (domainvirtualization.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item.UpdatedAt = time.Now()
	r.tasks[item.ID] = item
	return item, nil
}

func (r *memoryRepo) ClaimTask(_ context.Context, workerID string, now time.Time) (domainvirtualization.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, item := range r.tasks {
		if item.Status != TaskStatusQueued {
			continue
		}
		item.Status = TaskStatusRunning
		item.ClaimedByWorkerID = workerID
		item.AttemptCount++
		if item.MaxRetries == 0 {
			item.MaxRetries = defaultTaskMaxRetries
		}
		if item.TimeoutSeconds == 0 {
			item.TimeoutSeconds = defaultTaskTimeoutSeconds
		}
		item.StartedAt = &now
		item.LastHeartbeatAt = &now
		r.tasks[id] = item
		return item, nil
	}
	return domainvirtualization.Task{}, errMemoryNotFound
}

func (r *memoryRepo) GetTask(_ context.Context, id string) (domainvirtualization.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.tasks[id]
	if !ok {
		return domainvirtualization.Task{}, errMemoryNotFound
	}
	return item, nil
}

func (r *memoryRepo) ListTasks(_ context.Context, filter domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []domainvirtualization.Task{}
	for _, item := range r.tasks {
		if filter.ConnectionID != "" && item.ConnectionID != filter.ConnectionID {
			continue
		}
		if filter.VMID != "" && item.VMID != filter.VMID {
			continue
		}
		if len(filter.Statuses) > 0 {
			if !slices.Contains(filter.Statuses, item.Status) {
				continue
			}
		} else if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if filter.Abnormal && item.Status != TaskStatusFailed && item.Status != TaskStatusTimeout {
			continue
		}
		if filter.Pending && item.Status != TaskStatusQueued && item.Status != TaskStatusRunning {
			continue
		}
		if filter.TaskKind != "" && item.TaskKind != filter.TaskKind {
			continue
		}
		items = append(items, item)
	}
	slices.SortFunc(items, func(left, right domainvirtualization.Task) int {
		if left.CreatedAt.After(right.CreatedAt) {
			return -1
		}
		if left.CreatedAt.Before(right.CreatedAt) {
			return 1
		}
		return strings.Compare(left.ID, right.ID)
	})
	return items, nil
}

func (r *memoryRepo) CountTasks(ctx context.Context, filter domainvirtualization.TaskFilter) (int, error) {
	items, err := r.ListTasks(ctx, filter)
	return len(items), err
}

func (r *memoryRepo) ListTimedOutTasks(_ context.Context, now time.Time, _ int) ([]domainvirtualization.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []domainvirtualization.Task{}
	for _, item := range r.tasks {
		if item.Status != TaskStatusRunning {
			continue
		}
		timeout := item.TimeoutSeconds
		if timeout <= 0 {
			timeout = defaultTaskTimeoutSeconds
		}
		reference := item.StartedAt
		if item.LastHeartbeatAt != nil {
			reference = item.LastHeartbeatAt
		}
		if reference != nil && now.After(reference.Add(time.Duration(timeout)*time.Second)) {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *memoryRepo) HeartbeatTask(_ context.Context, taskID string, workerID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.tasks[taskID]
	if !ok || item.ClaimedByWorkerID != workerID || item.Status != TaskStatusRunning {
		return errMemoryNotFound
	}
	item.LastHeartbeatAt = &now
	r.tasks[taskID] = item
	return nil
}

func (r *memoryRepo) UpdateConnectionHealth(_ context.Context, id string, health map[string]any, lastSyncedAt *time.Time) (domainvirtualization.Connection, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.connections[id]
	if !ok {
		return domainvirtualization.Connection{}, errMemoryNotFound
	}
	item.Health = health
	if lastSyncedAt != nil {
		item.LastSyncedAt = lastSyncedAt
	}
	r.connections[id] = item
	return item, nil
}

func (r *memoryRepo) MarkVMsStale(_ context.Context, provider, connectionID string, seenBefore time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, item := range r.vms {
		if item.Provider == provider && item.ConnectionID == connectionID && item.LastSeenAt != nil && item.LastSeenAt.Before(seenBefore) && item.Status != "deleted" {
			item.Status = "stale"
			r.vms[id] = item
		}
	}
	return nil
}

func (r *memoryRepo) MarkImagesStale(_ context.Context, provider, connectionID string, seenBefore time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, item := range r.images {
		if item.Provider == provider && item.ConnectionID == connectionID && item.LastSeenAt != nil && item.LastSeenAt.Before(seenBefore) && item.Status != "deleted" {
			item.Status = "stale"
			r.images[id] = item
		}
	}
	return nil
}

func (r *memoryRepo) MarkFlavorsStale(_ context.Context, provider, connectionID string, seenBefore time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, item := range r.flavors {
		if item.Provider == provider && item.ConnectionID == connectionID && item.LastSeenAt != nil && item.LastSeenAt.Before(seenBefore) && item.Status != "deleted" {
			item.Status = "stale"
			r.flavors[id] = item
		}
	}
	return nil
}

func (r *memoryRepo) CreateTaskLog(_ context.Context, item domainvirtualization.TaskLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs[item.TaskID] = append(r.logs[item.TaskID], item)
	return nil
}

func (r *memoryRepo) ListTaskLogs(_ context.Context, taskID string, _ int) ([]domainvirtualization.TaskLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domainvirtualization.TaskLog{}, r.logs[taskID]...), nil
}

var errMemoryNotFound = testingError("not found")

type testingError string

func (e testingError) Error() string { return string(e) }
