package virtualization

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainvirtualization "github.com/kubecrux/kubecrux/internal/domain/virtualization"
	infravirtualization "github.com/kubecrux/kubecrux/internal/infrastructure/virtualization"
	"github.com/kubecrux/kubecrux/internal/platform/runtimeobs"
)

func (s *Service) runWorker(ctx context.Context) {
	defer close(s.workerDone)
	ticker := time.NewTicker(s.workerInterval)
	defer ticker.Stop()
	for {
		s.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) runOnce(ctx context.Context) {
	s.sweepTimedOutTasks(ctx)
	s.updateWorkerQueueDepth(ctx)
	for {
		if ctx.Err() != nil {
			return
		}
		task, err := s.repo.ClaimTask(ctx, s.workerID, time.Now().UTC())
		if err != nil {
			return
		}
		s.executeTask(ctx, task)
	}
}

func (s *Service) executeTask(ctx context.Context, task domainvirtualization.Task) {
	if taskTerminal(task.Status) || task.Status != TaskStatusRunning {
		return
	}
	startedAt := time.Now()
	s.recordWorkerStart(task)
	_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: "task started"})
	outcome := runtimeobs.OutcomeSucceeded
	var taskErr error
	defer func() {
		s.recordWorkerFinish(task, startedAt, outcome, taskErr)
	}()
	switch task.TaskKind {
	case TaskKindAssetSync:
		outcome, taskErr = s.executeAssetSync(ctx, task)
	case TaskKindVMCreate:
		outcome, taskErr = s.executeVMCreate(ctx, task)
	case TaskKindVMAction:
		outcome, taskErr = s.executeVMAction(ctx, task)
	default:
		taskErr = fmt.Errorf("unsupported virtualization task kind %q", task.TaskKind)
		outcome = runtimeobs.OutcomeFailed
		s.failTask(ctx, task, taskErr)
	}
}

func (s *Service) executeAssetSync(ctx context.Context, task domainvirtualization.Task) (string, error) {
	connection, err := s.repo.GetConnection(ctx, task.ConnectionID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	syncStartedAt := time.Now().UTC()
	_, _ = s.repo.UpdateConnectionHealth(ctx, connection.ID, map[string]any{
		"status":    "syncing",
		"message":   "asset sync running",
		"taskId":    task.ID,
		"updatedAt": syncStartedAt.Format(time.RFC3339),
	}, nil)
	adapter, adapterConnection, err := s.adapterForConnection(connection)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	result, err := adapter.SyncAssets(ctx, adapterConnection)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	if s.taskCanceled(ctx, task.ID) {
		return runtimeobs.OutcomeCanceled, nil
	}
	vmCount := 0
	imageCount := 0
	flavorCount := 0
	for _, asset := range result.Assets {
		if s.taskCanceled(ctx, task.ID) {
			return runtimeobs.OutcomeCanceled, nil
		}
		_ = s.repo.HeartbeatTask(ctx, task.ID, s.workerID, time.Now().UTC())
		switch strings.ToLower(asset.Type) {
		case "virtualmachine", "virtualmachineinstance", "qemu", "vm":
			if _, err := s.repo.UpsertVM(ctx, vmFromAsset(connection, asset)); err != nil {
				_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: err.Error()})
				continue
			}
			vmCount++
		case "datasource", "storage_content", "image":
			if _, err := s.repo.UpsertImage(ctx, imageFromAsset(connection, asset)); err != nil {
				_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: err.Error()})
				continue
			}
			imageCount++
		case "flavor":
			if _, err := s.repo.UpsertFlavor(ctx, flavorFromAsset(connection, asset)); err != nil {
				_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: err.Error()})
				continue
			}
			flavorCount++
		}
	}
	status := firstNonEmpty(result.Health.Status, "healthy")
	s.markConnectionAssetsStale(ctx, connection, syncStartedAt)
	finishedAt := time.Now().UTC()
	_, _ = s.repo.UpdateConnectionHealth(ctx, connection.ID, map[string]any{
		"status":      status,
		"message":     result.Health.Message,
		"taskId":      task.ID,
		"assetCount":  len(result.Assets),
		"vmCount":     vmCount,
		"imageCount":  imageCount,
		"flavorCount": flavorCount,
		"updatedAt":   finishedAt.Format(time.RFC3339),
	}, &finishedAt)
	task.Result = map[string]any{
		"health":      status,
		"message":     result.Health.Message,
		"assetCount":  len(result.Assets),
		"vmCount":     vmCount,
		"imageCount":  imageCount,
		"flavorCount": flavorCount,
	}
	s.completeTask(ctx, task)
	_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: fmt.Sprintf("asset sync completed: %d assets", len(result.Assets))})
	s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker.asset_sync", connection.ID, connection.Name, TaskStatusSucceeded, "virtualization asset sync completed", map[string]any{"taskId": task.ID, "assetCount": len(result.Assets)})
	return runtimeobs.OutcomeSucceeded, nil
}

func (s *Service) executeVMCreate(ctx context.Context, task domainvirtualization.Task) (string, error) {
	if s.taskCanceled(ctx, task.ID) {
		return runtimeobs.OutcomeCanceled, nil
	}
	connection, err := s.repo.GetConnection(ctx, task.ConnectionID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	adapter, adapterConnection, err := s.adapterForConnection(connection)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	input := infravirtualization.CreateVMInput{
		Name:             payloadString(task.Payload, "name"),
		Namespace:        payloadString(task.Payload, "namespace"),
		Node:             payloadString(task.Payload, "node"),
		CPU:              payloadInt(task.Payload, "cpu"),
		Memory:           memoryString(payloadInt(task.Payload, "memoryMiB")),
		BootImage:        firstNonEmpty(payloadString(task.Payload, "imageId"), payloadString(task.Payload, "bootImageId")),
		DiskSize:         diskString(payloadInt(task.Payload, "diskGiB")),
		Network:          payloadString(task.Payload, "network"),
		CloudInit:        payloadString(task.Payload, "cloudInit"),
		StartAfterCreate: boolValue(task.Payload, "startAfterCreate"),
		TemplateID:       payloadString(task.Payload, "templateId"),
	}
	vm, err := adapter.CreateVM(ctx, adapterConnection, input)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	if s.taskCanceled(ctx, task.ID) {
		return runtimeobs.OutcomeCanceled, nil
	}
	stored, err := s.repo.UpsertVM(ctx, domainvirtualization.VM{
		Provider:     connection.Provider,
		ConnectionID: connection.ID,
		ExternalID:   firstNonEmpty(vm.ID, vm.Name),
		Name:         vm.Name,
		Namespace:    vm.Namespace,
		Status:       firstNonEmpty(vm.Status, "created"),
		PowerState:   powerStateFromStatus(vm.Status),
		NodeName:     vm.Node,
		ImageID:      input.BootImage,
		FlavorID:     payloadString(task.Payload, "flavorId"),
		IPAddresses:  []string{},
		Labels:       metadataMap(vm.Metadata),
		Config: map[string]any{
			"cpu":       payloadInt(task.Payload, "cpu"),
			"memoryMiB": payloadInt(task.Payload, "memoryMiB"),
			"diskGiB":   payloadInt(task.Payload, "diskGiB"),
			"network":   payloadString(task.Payload, "network"),
		},
		Raw: map[string]any{
			"providerVmId":   vm.ID,
			"providerParams": task.Payload["providerParams"],
			"providerExtra":  task.Payload["providerExtra"],
		},
	})
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	task.VMID = stored.ID
	task.Result = map[string]any{"vmId": stored.ID, "name": stored.Name, "status": stored.Status}
	s.completeTask(ctx, task)
	_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: "virtual machine created"})
	s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker.vm_create", stored.ID, stored.Name, TaskStatusSucceeded, "virtual machine creation completed", map[string]any{"taskId": task.ID})
	return runtimeobs.OutcomeSucceeded, nil
}

func (s *Service) executeVMAction(ctx context.Context, task domainvirtualization.Task) (string, error) {
	if s.taskCanceled(ctx, task.ID) {
		return runtimeobs.OutcomeCanceled, nil
	}
	vm, err := s.repo.GetVM(ctx, task.VMID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	connection, err := s.repo.GetConnection(ctx, vm.ConnectionID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	adapter, adapterConnection, err := s.adapterForConnection(connection)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	action, err := normalizeAction(payloadString(task.Payload, "action"))
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	result, err := adapter.PowerAction(ctx, adapterConnection, infravirtualization.VM{
		ID:        firstNonEmpty(vm.ExternalID, vm.ID),
		Name:      vm.Name,
		Namespace: vm.Namespace,
		Node:      vm.NodeName,
		Status:    vm.Status,
	}, action)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	if s.taskCanceled(ctx, task.ID) {
		return runtimeobs.OutcomeCanceled, nil
	}
	vm.PowerState = powerStateAfterAction(action, vm.PowerState)
	if action == infravirtualization.PowerActionDelete {
		vm.Status = "deleted"
	} else if vm.Status == "" {
		vm.Status = "active"
	}
	stored, err := s.repo.UpsertVM(ctx, vm)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	task.Result = map[string]any{
		"accepted": result.Accepted,
		"action":   string(result.Action),
		"message":  result.Message,
		"vmId":     stored.ID,
	}
	s.completeTask(ctx, task)
	_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: "virtual machine action completed"})
	s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker.vm_action", stored.ID, stored.Name, TaskStatusSucceeded, "virtual machine action completed", map[string]any{"taskId": task.ID, "action": string(action)})
	return runtimeobs.OutcomeSucceeded, nil
}

func (s *Service) completeTask(ctx context.Context, task domainvirtualization.Task) {
	current, err := s.repo.GetTask(ctx, task.ID)
	if err == nil && current.Status == TaskStatusCanceled {
		return
	}
	now := time.Now().UTC()
	task.Status = TaskStatusSucceeded
	task.FinishedAt = &now
	_, _ = s.repo.UpdateTask(ctx, task)
}

func (s *Service) updateWorkerQueueDepth(ctx context.Context) {
	if s.metrics == nil {
		return
	}
	total, err := s.repo.CountTasks(ctx, domainvirtualization.TaskFilter{Status: TaskStatusQueued})
	if err != nil {
		return
	}
	s.metrics.SetQueueDepth(runtimeobs.ComponentVirtualizationWorker, total)
}

func (s *Service) recordWorkerStart(task domainvirtualization.Task) {
	if s.metrics != nil {
		s.metrics.RecordStart(runtimeobs.ComponentVirtualizationWorker, task.ID, 0, 1)
	}
}

func (s *Service) recordWorkerFinish(task domainvirtualization.Task, startedAt time.Time, outcome string, err error) {
	if s.metrics == nil {
		return
	}
	queueDepth := 0
	if total, countErr := s.repo.CountTasks(context.Background(), domainvirtualization.TaskFilter{Status: TaskStatusQueued}); countErr == nil {
		queueDepth = total
	}
	s.metrics.RecordFinish(runtimeobs.ComponentVirtualizationWorker, task.ID, time.Since(startedAt), queueDepth, 1, outcome, err)
}

func (s *Service) failTask(ctx context.Context, task domainvirtualization.Task, err error) {
	current, getErr := s.repo.GetTask(ctx, task.ID)
	if getErr == nil && current.Status == TaskStatusCanceled {
		return
	}
	message := "task failed"
	if err != nil {
		message = err.Error()
	}
	now := time.Now().UTC()
	task.Status = TaskStatusFailed
	task.FinishedAt = &now
	task.Result = map[string]any{"error": message}
	_, _ = s.repo.UpdateTask(ctx, task)
	_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: message})
	result := TaskStatusFailed
	if isUnsupported(err) {
		result = "unsupported"
	}
	s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker."+task.TaskKind, task.ConnectionID, task.TaskKind, result, "virtualization worker task failed", map[string]any{"taskId": task.ID, "error": message})
}

func (s *Service) sweepTimedOutTasks(ctx context.Context) {
	tasks, err := s.repo.ListTimedOutTasks(ctx, time.Now().UTC(), 50)
	if err != nil {
		return
	}
	for _, task := range tasks {
		now := time.Now().UTC()
		task.Status = TaskStatusTimeout
		task.FinishedAt = &now
		task.Result = map[string]any{
			"error":          fmt.Sprintf("virtualization task timed out after %d seconds", effectiveTimeoutSeconds(task)),
			"timeoutSeconds": effectiveTimeoutSeconds(task),
			"timedOutAt":     now.Format(time.RFC3339),
		}
		_, _ = s.repo.UpdateTask(ctx, task)
		_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "warn", Message: fmt.Sprintf("task timed out after %d seconds", effectiveTimeoutSeconds(task))})
		s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker.timeout", task.ID, task.TaskKind, TaskStatusTimeout, "virtualization worker task timed out", map[string]any{"taskId": task.ID})
	}
}

func (s *Service) taskCanceled(ctx context.Context, taskID string) bool {
	task, err := s.repo.GetTask(ctx, taskID)
	if err != nil {
		return false
	}
	return task.Status == TaskStatusCanceled
}

func (s *Service) markConnectionAssetsStale(ctx context.Context, connection domainvirtualization.Connection, seenBefore time.Time) {
	_ = s.repo.MarkVMsStale(ctx, connection.Provider, connection.ID, seenBefore)
	_ = s.repo.MarkImagesStale(ctx, connection.Provider, connection.ID, seenBefore)
	_ = s.repo.MarkFlavorsStale(ctx, connection.Provider, connection.ID, seenBefore)
}

func effectiveTimeoutSeconds(task domainvirtualization.Task) int {
	if task.TimeoutSeconds > 0 {
		return task.TimeoutSeconds
	}
	return defaultTaskTimeoutSeconds
}

func (s *Service) enqueueStartupSync(ctx context.Context, principal domainidentity.Principal, metadata map[string]any) ([]domainvirtualization.Task, error) {
	enabled := true
	connections, err := s.repo.ListConnections(ctx, domainvirtualization.ConnectionFilter{Enabled: &enabled, Limit: 1000})
	if err != nil {
		return nil, err
	}
	tasks := make([]domainvirtualization.Task, 0, len(connections))
	for _, connection := range connections {
		task, err := s.enqueueSyncTask(ctx, principal, connection, metadata)
		if err != nil {
			_ = s.repo.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: err.Error()})
			continue
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *Service) enqueueSyncTask(ctx context.Context, principal domainidentity.Principal, connection domainvirtualization.Connection, metadata map[string]any) (domainvirtualization.Task, error) {
	payload := cloneMap(metadata)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["connectionId"] = connection.ID
	return s.repo.CreateTask(ctx, domainvirtualization.Task{
		Provider:       connection.Provider,
		ConnectionID:   connection.ID,
		TaskKind:       TaskKindAssetSync,
		Status:         TaskStatusQueued,
		RequestedBy:    principal.UserID,
		MaxRetries:     defaultTaskMaxRetries,
		TimeoutSeconds: defaultTaskTimeoutSeconds,
		Payload:        payload,
	})
}

func (s *Service) adapterForConnection(connection domainvirtualization.Connection) (Adapter, infravirtualization.Connection, error) {
	adapter, err := s.adapterFor(connection.Provider)
	if err != nil {
		return nil, infravirtualization.Connection{}, err
	}
	adapterConnection, err := s.adapterConnection(connection)
	if err != nil {
		return nil, infravirtualization.Connection{}, err
	}
	return adapter, adapterConnection, nil
}

func vmFromAsset(connection domainvirtualization.Connection, asset infravirtualization.Asset) domainvirtualization.VM {
	externalID := firstNonEmpty(asset.Metadata["uid"], asset.Metadata["vmid"], asset.Name)
	return domainvirtualization.VM{
		Provider:     connection.Provider,
		ConnectionID: connection.ID,
		ExternalID:   externalID,
		Name:         asset.Name,
		Namespace:    asset.Namespace,
		Status:       firstNonEmpty(asset.Status, "active"),
		PowerState:   powerStateFromStatus(asset.Status),
		NodeName:     asset.Node,
		Labels:       metadataMap(asset.Metadata),
		Raw:          map[string]any{"assetType": asset.Type},
	}
}

func imageFromAsset(connection domainvirtualization.Connection, asset infravirtualization.Asset) domainvirtualization.Image {
	return domainvirtualization.Image{
		Provider:     connection.Provider,
		ConnectionID: connection.ID,
		ExternalID:   firstNonEmpty(asset.Metadata["uid"], asset.Metadata["volid"], asset.Name),
		Name:         asset.Name,
		Status:       firstNonEmpty(asset.Status, "active"),
		Config:       metadataMap(asset.Metadata),
		Raw:          map[string]any{"assetType": asset.Type, "namespace": asset.Namespace, "node": asset.Node},
	}
}

func flavorFromAsset(connection domainvirtualization.Connection, asset infravirtualization.Asset) domainvirtualization.Flavor {
	return domainvirtualization.Flavor{
		Provider:     connection.Provider,
		ConnectionID: connection.ID,
		ExternalID:   firstNonEmpty(asset.Metadata["uid"], asset.Name),
		Name:         asset.Name,
		Status:       firstNonEmpty(asset.Status, "active"),
		Config:       metadataMap(asset.Metadata),
		Raw:          map[string]any{"assetType": asset.Type},
	}
}

func memoryString(memoryMiB int) string {
	if memoryMiB <= 0 {
		return ""
	}
	return fmt.Sprintf("%dMi", memoryMiB)
}

func diskString(diskGiB int) string {
	if diskGiB <= 0 {
		return ""
	}
	return fmt.Sprintf("%dGi", diskGiB)
}

func boolValue(payload map[string]any, key string) bool {
	value, ok := payload[key].(bool)
	return ok && value
}

func metadataMap(values map[string]string) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func powerStateFromStatus(status string) string {
	switch strings.ToLower(status) {
	case "running", "active", "started":
		return "running"
	case "stopped", "halted", "shutdown":
		return "stopped"
	default:
		return ""
	}
}

func powerStateAfterAction(action infravirtualization.PowerAction, fallback string) string {
	switch action {
	case infravirtualization.PowerActionStart, infravirtualization.PowerActionRestart:
		return "running"
	case infravirtualization.PowerActionStop:
		return "stopped"
	default:
		return fallback
	}
}
