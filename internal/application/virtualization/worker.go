package virtualization

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
	"k8s.io/apimachinery/pkg/api/resource"
)

func (s *Service) runWorker(ctx context.Context) {
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
		task, err := s.taskQueue.ClaimTask(ctx, s.workerID, time.Now().UTC())
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
	_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: "task started"})
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
	connection, err := s.connections.GetConnection(ctx, task.ConnectionID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	syncStartedAt := time.Now().UTC()
	_, _ = s.connectionWriter.UpdateConnectionHealth(ctx, connection.ID, map[string]any{
		"status":    "syncing",
		"message":   "asset sync running",
		"taskId":    task.ID,
		"updatedAt": syncStartedAt.Format(time.RFC3339),
	}, nil)
	adapter, adapterConnection, err := s.adapterForConnection(ctx, connection)
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
		_ = s.taskQueue.HeartbeatTask(ctx, task.ID, s.workerID, time.Now().UTC())
		switch strings.ToLower(asset.Type) {
		case "virtualmachine", "virtualmachineinstance", "qemu", "vm":
			if _, err := s.vms.UpsertVM(ctx, vmFromAsset(connection, asset)); err != nil {
				_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: err.Error()})
				continue
			}
			vmCount++
		case "datasource", "datavolume", "persistentvolumeclaim", "networkattachmentdefinition", "storage_content", "image", "template", "iso", "storage", "network", "lxc_template":
			if _, err := s.images.UpsertImage(ctx, imageFromAsset(connection, asset)); err != nil {
				_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: err.Error()})
				continue
			}
			imageCount++
		case "flavor":
			if _, err := s.flavors.UpsertFlavor(ctx, flavorFromAsset(connection, asset)); err != nil {
				_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: err.Error()})
				continue
			}
			flavorCount++
		}
	}
	status := firstNonEmpty(result.Health.Status, "healthy")
	s.markConnectionAssetsStale(ctx, connection, syncStartedAt)
	finishedAt := time.Now().UTC()
	_, _ = s.connectionWriter.UpdateConnectionHealth(ctx, connection.ID, map[string]any{
		"status":      status,
		"message":     result.Health.Message,
		"reason":      result.Health.Reason,
		"nextAction":  result.Health.NextAction,
		"httpStatus":  result.Health.HTTPStatus,
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
		"reason":      result.Health.Reason,
		"nextAction":  result.Health.NextAction,
		"httpStatus":  result.Health.HTTPStatus,
		"assetCount":  len(result.Assets),
		"vmCount":     vmCount,
		"imageCount":  imageCount,
		"flavorCount": flavorCount,
	}
	s.completeTask(ctx, task)
	_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: fmt.Sprintf("asset sync completed: %d assets", len(result.Assets))})
	s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker.asset_sync", connection.ID, connection.Name, TaskStatusSucceeded, "virtualization asset sync completed", map[string]any{"taskId": task.ID, "assetCount": len(result.Assets)})
	return runtimeobs.OutcomeSucceeded, nil
}

func (s *Service) executeVMCreate(ctx context.Context, task domainvirtualization.Task) (string, error) {
	if s.taskCanceled(ctx, task.ID) {
		return runtimeobs.OutcomeCanceled, nil
	}
	connection, err := s.connections.GetConnection(ctx, task.ConnectionID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	adapter, adapterConnection, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	input := adapterCreateVMInput(task.Payload)
	vm, err := adapter.CreateVM(ctx, adapterConnection, input)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	if s.taskCanceled(ctx, task.ID) {
		return runtimeobs.OutcomeCanceled, nil
	}
	vmRecord, ipAddresses, endpoint := createdVMRecord(connection, task.Payload, vm)
	stored, err := s.vms.UpsertVM(ctx, vmRecord)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	populateVMCreateTaskResult(&task, stored, vm, ipAddresses, endpoint)
	s.completeTask(ctx, task)
	_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: "virtual machine created"})
	s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker.vm_create", stored.ID, stored.Name, TaskStatusSucceeded, "virtual machine creation completed", map[string]any{"taskId": task.ID})
	return runtimeobs.OutcomeSucceeded, nil
}

func adapterCreateVMInput(payload map[string]any) domainvirtualization.AdapterCreateVMInput {
	sourceRef := firstNonEmpty(payloadString(payload, "sourceId"), payloadString(payload, "imageId"), payloadString(payload, "bootImageId"))
	input := domainvirtualization.AdapterCreateVMInput{
		Name: payloadString(payload, "name"), Architecture: payloadString(payload, "architecture"), Namespace: payloadString(payload, "namespace"), Node: payloadString(payload, "node"),
		CPU: payloadInt(payload, "cpu"), Memory: memoryString(payloadInt(payload, "memoryMiB")), BootImage: sourceRef, DiskSize: diskString(payloadInt(payload, "diskGiB")),
		Network: payloadString(payload, "network"), CloudInit: payloadString(payload, "cloudInit"), StartAfterCreate: boolValue(payload, "startAfterCreate"),
		TemplateID: payloadString(payload, "templateId"), SourceMode: payloadString(payload, "sourceMode"), SourceRef: sourceRef, ProviderParams: mapValue(payload, "providerParams"),
	}
	decodePayloadValue(payload["disks"], &input.Disks)
	decodePayloadValue(payload["networks"], &input.Networks)
	return input
}

func createdVMRecord(connection domainvirtualization.Connection, payload map[string]any, vm domainvirtualization.AdapterVM) (domainvirtualization.VM, []string, string) {
	ipAddresses := uniqueNonEmptyStrings(vm.IPAddresses)
	if ipAddress := firstNonEmpty(vm.Metadata["ipAddress"], vm.Metadata["ip"]); ipAddress != "" {
		ipAddresses = uniqueNonEmptyStrings(append([]string{ipAddress}, ipAddresses...))
	}
	endpoint := firstNonEmpty(vm.Endpoint, vm.Metadata["endpoint"], vm.Metadata["accessUrl"])
	config := map[string]any{"architecture": payloadString(payload, "architecture"), "cpu": payloadInt(payload, "cpu"), "memoryMiB": payloadInt(payload, "memoryMiB"), "diskGiB": payloadInt(payload, "diskGiB"), "network": payloadString(payload, "network"), "sourceMode": payloadString(payload, "sourceMode"), "sourceRef": payloadString(payload, "sourceId")}
	if len(ipAddresses) > 0 {
		config["ipAddress"], config["ipAddresses"] = ipAddresses[0], ipAddresses
	}
	if endpoint != "" {
		config["endpoint"] = endpoint
	}
	copyVMMetadata(config, vm.Metadata, []string{"dataVolumeName", "dataVolumePhase", "dataVolumeProgress", "dataVolumeFailureReason", "dataVolumeFailureMessage", "pvcName", "pvcPhase", "pvcStorageClass", "vmiStatus", "printableStatus"})
	raw := map[string]any{"providerVmId": vm.ID, "architecture": payloadString(payload, "architecture"), "providerParams": payload["providerParams"], "providerExtra": payload["providerExtra"], "sourceMode": payloadString(payload, "sourceMode"), "sourceRef": payloadString(payload, "sourceId")}
	copyVMMetadata(raw, vm.Metadata, []string{"pveCreateUpid", "pveConfigUpid", "pveStartUpid"})
	record := domainvirtualization.VM{Provider: connection.Provider, ConnectionID: connection.ID, ExternalID: firstNonEmpty(vm.ID, vm.Name), Name: vm.Name, Namespace: vm.Namespace, Status: firstNonEmpty(vm.Status, "created"), PowerState: powerStateFromStatus(vm.Status), NodeName: vm.Node, ImageID: firstNonEmpty(payloadString(payload, "imageId"), payloadString(payload, "bootImageId")), FlavorID: payloadString(payload, "flavorId"), IPAddresses: ipAddresses, Labels: metadataMap(vm.Metadata), Config: config, Raw: raw}
	return record, ipAddresses, endpoint
}

func populateVMCreateTaskResult(task *domainvirtualization.Task, stored domainvirtualization.VM, vm domainvirtualization.AdapterVM, ipAddresses []string, endpoint string) {
	task.VMID = stored.ID
	task.Result = map[string]any{"vmId": stored.ID, "name": stored.Name, "status": stored.Status, "architecture": payloadString(task.Payload, "architecture"), "providerVmId": vm.ID}
	if len(ipAddresses) > 0 {
		task.Result["ipAddress"], task.Result["ip"], task.Result["ipAddresses"] = ipAddresses[0], ipAddresses[0], ipAddresses
	}
	if endpoint != "" {
		task.Result["endpoint"], task.Result["accessUrl"] = endpoint, endpoint
	}
	copyVMMetadata(task.Result, vm.Metadata, []string{"pveCreateUpid", "pveConfigUpid", "pveStartUpid", "dataVolumeName", "dataVolumePhase", "dataVolumeProgress", "dataVolumeFailureReason", "dataVolumeFailureMessage", "pvcName", "pvcPhase", "vmiStatus", "printableStatus"})
}

func copyVMMetadata(target map[string]any, metadata map[string]string, keys []string) {
	for _, key := range keys {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			target[key] = value
		}
	}
}

func (s *Service) executeVMAction(ctx context.Context, task domainvirtualization.Task) (string, error) {
	if s.taskCanceled(ctx, task.ID) {
		return runtimeobs.OutcomeCanceled, nil
	}
	vm, err := s.vms.GetVM(ctx, task.VMID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	connection, err := s.connections.GetConnection(ctx, vm.ConnectionID)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	adapter, adapterConnection, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	rawAction := strings.ToLower(strings.TrimSpace(payloadString(task.Payload, "action")))
	if rawAction == "resize" {
		input := domainvirtualization.AdapterResizeVMInput{CPU: payloadInt(task.Payload, "cpu"), MemoryMiB: payloadInt(task.Payload, "memoryMiB"), DiskGiB: payloadInt(task.Payload, "diskGiB")}
		decodePayloadValue(task.Payload["disks"], &input.Disks)
		decodePayloadValue(task.Payload["networks"], &input.Networks)
		resizer, ok := adapter.(domainvirtualization.ResizeAdapter)
		if !ok {
			err := domainvirtualization.ErrUnsupported
			s.failTask(ctx, task, err)
			return runtimeobs.OutcomeFailed, err
		}
		result, err := resizer.ResizeVM(ctx, adapterConnection, domainvirtualization.AdapterVM{ID: firstNonEmpty(vm.ExternalID, vm.ID), Name: vm.Name, Namespace: vm.Namespace, Node: vm.NodeName, Status: vm.Status}, input)
		if err != nil {
			s.failTask(ctx, task, err)
			return runtimeobs.OutcomeFailed, err
		}
		if vm.Config == nil {
			vm.Config = map[string]any{}
		}
		if input.CPU > 0 {
			vm.Config["cpu"] = input.CPU
		}
		if input.MemoryMiB > 0 {
			vm.Config["memoryMiB"] = input.MemoryMiB
		}
		if input.DiskGiB > 0 {
			vm.Config["diskGiB"] = input.DiskGiB
		}
		stored, err := s.vms.UpsertVM(ctx, vm)
		if err != nil {
			s.failTask(ctx, task, err)
			return runtimeobs.OutcomeFailed, err
		}
		task.Result = map[string]any{"accepted": result.Accepted, "action": "resize", "message": result.Message, "vmId": stored.ID}
		s.completeTask(ctx, task)
		_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: "virtual machine resize completed"})
		return runtimeobs.OutcomeSucceeded, nil
	}
	action, err := normalizeAction(rawAction)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	result, err := adapter.PowerAction(ctx, adapterConnection, domainvirtualization.AdapterVM{
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
	if action == domainvirtualization.PowerActionDelete {
		vm.Status = "deleted"
	} else if vm.Status == "" {
		vm.Status = "active"
	}
	stored, err := s.vms.UpsertVM(ctx, vm)
	if err != nil {
		s.failTask(ctx, task, err)
		return runtimeobs.OutcomeFailed, err
	}
	if action == domainvirtualization.PowerActionDelete {
		if err := s.dockerLinks.MarkDockerHostsUnavailableByVM(ctx, stored.ID); err != nil {
			s.failTask(ctx, task, err)
			return runtimeobs.OutcomeFailed, err
		}
	}
	task.Result = map[string]any{
		"accepted": result.Accepted,
		"action":   string(result.Action),
		"message":  result.Message,
		"vmId":     stored.ID,
	}
	if result.UPID != "" {
		task.Result["pveUpid"] = result.UPID
	}
	s.completeTask(ctx, task)
	_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "info", Message: "virtual machine action completed"})
	s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker.vm_action", stored.ID, stored.Name, TaskStatusSucceeded, "virtual machine action completed", map[string]any{"taskId": task.ID, "action": string(action)})
	return runtimeobs.OutcomeSucceeded, nil
}

func decodePayloadValue(value any, target any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	_ = json.Unmarshal(encoded, target)
}

func (s *Service) completeTask(ctx context.Context, task domainvirtualization.Task) {
	current, err := s.tasks.GetTask(ctx, task.ID)
	if err == nil && current.Status == TaskStatusCanceled {
		return
	}
	now := time.Now().UTC()
	task.Status = TaskStatusSucceeded
	task.FinishedAt = &now
	_, _ = s.tasks.UpdateTask(ctx, task)
}

func (s *Service) updateWorkerQueueDepth(ctx context.Context) {
	if s.metrics == nil {
		return
	}
	total, err := s.tasks.CountTasks(ctx, domainvirtualization.TaskFilter{Status: TaskStatusQueued})
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
	if total, countErr := s.tasks.CountTasks(context.Background(), domainvirtualization.TaskFilter{Status: TaskStatusQueued}); countErr == nil {
		queueDepth = total
	}
	s.metrics.RecordFinish(runtimeobs.ComponentVirtualizationWorker, task.ID, time.Since(startedAt), queueDepth, 1, outcome, err)
}

func (s *Service) failTask(ctx context.Context, task domainvirtualization.Task, err error) {
	current, getErr := s.tasks.GetTask(ctx, task.ID)
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
	if details, ok := domainvirtualization.AdapterErrorDetails(err); ok {
		task.Result["failureReason"] = details.Reason
		task.Result["reason"] = details.Reason
		task.Result["errorCode"] = details.Reason
		task.Result["providerErrorClass"] = details.Reason
		if details.HTTPStatus > 0 {
			task.Result["lastHttpStatus"] = details.HTTPStatus
			task.Result["httpStatus"] = details.HTTPStatus
		}
		if details.NextAction != "" {
			task.Result["nextAction"] = details.NextAction
		}
	}
	_, _ = s.tasks.UpdateTask(ctx, task)
	_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: message})
	result := TaskStatusFailed
	if isUnsupported(err) {
		result = "unsupported"
	}
	s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker."+task.TaskKind, task.ConnectionID, task.TaskKind, result, "virtualization worker task failed", map[string]any{"taskId": task.ID, "error": message})
}

func (s *Service) sweepTimedOutTasks(ctx context.Context) {
	tasks, err := s.taskQueue.ListTimedOutTasks(ctx, time.Now().UTC(), 50)
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
		_, _ = s.tasks.UpdateTask(ctx, task)
		_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "warn", Message: fmt.Sprintf("task timed out after %d seconds", effectiveTimeoutSeconds(task))})
		s.recordOperation(ctx, s.workerPrincipal, "virtualization.worker.timeout", task.ID, task.TaskKind, TaskStatusTimeout, "virtualization worker task timed out", map[string]any{"taskId": task.ID})
	}
}

func (s *Service) taskCanceled(ctx context.Context, taskID string) bool {
	task, err := s.tasks.GetTask(ctx, taskID)
	if err != nil {
		return false
	}
	return task.Status == TaskStatusCanceled
}

func (s *Service) markConnectionAssetsStale(ctx context.Context, connection domainvirtualization.Connection, seenBefore time.Time) {
	_ = s.vms.MarkVMsStale(ctx, connection.Provider, connection.ID, seenBefore)
	_ = s.images.MarkImagesStale(ctx, connection.Provider, connection.ID, seenBefore)
	_ = s.flavors.MarkFlavorsStale(ctx, connection.Provider, connection.ID, seenBefore)
}

func effectiveTimeoutSeconds(task domainvirtualization.Task) int {
	if task.TimeoutSeconds > 0 {
		return task.TimeoutSeconds
	}
	return defaultTaskTimeoutSeconds
}

func (s *Service) enqueueStartupSync(ctx context.Context, principal domainidentity.Principal, metadata map[string]any) ([]domainvirtualization.Task, error) {
	enabled := true
	connections, err := s.connections.ListConnections(ctx, domainvirtualization.ConnectionFilter{Enabled: &enabled, Limit: 1000})
	if err != nil {
		return nil, err
	}
	tasks := make([]domainvirtualization.Task, 0, len(connections))
	for _, connection := range connections {
		task, err := s.enqueueSyncTask(ctx, principal, connection, metadata)
		if err != nil {
			_ = s.taskLogs.CreateTaskLog(ctx, domainvirtualization.TaskLog{TaskID: task.ID, LogLevel: "error", Message: err.Error()})
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
	return s.tasks.CreateTask(ctx, domainvirtualization.Task{
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

func (s *Service) adapterForConnection(ctx context.Context, connection domainvirtualization.Connection) (Adapter, domainvirtualization.AdapterConnection, error) {
	adapter, err := s.adapterFor(connection.Provider)
	if err != nil {
		return nil, domainvirtualization.AdapterConnection{}, err
	}
	adapterConnection, err := s.adapterConnection(ctx, connection)
	if err != nil {
		return nil, domainvirtualization.AdapterConnection{}, err
	}
	return adapter, adapterConnection, nil
}

func vmFromAsset(connection domainvirtualization.Connection, asset domainvirtualization.Asset) domainvirtualization.VM {
	externalID := firstNonEmpty(asset.Metadata["uid"], asset.Metadata["vmid"], asset.Name)
	config := metadataMap(asset.Metadata)
	config["source"] = "sync"
	config["orphanHint"] = "provider_discovered"
	if cpu := metadataInt(asset.Metadata, "cpu"); cpu > 0 {
		config["cpu"] = cpu
	}
	if memoryMiB := metadataMemoryMiB(asset.Metadata, "memory"); memoryMiB > 0 {
		config["memoryMiB"] = memoryMiB
	}
	ipAddresses := uniqueNonEmptyStrings(commaSeparatedValues(asset.Metadata["ipAddresses"]))
	if ipAddress := strings.TrimSpace(asset.Metadata["ipAddress"]); ipAddress != "" {
		ipAddresses = uniqueNonEmptyStrings(append([]string{ipAddress}, ipAddresses...))
	}
	return domainvirtualization.VM{
		Provider:     connection.Provider,
		ConnectionID: connection.ID,
		ExternalID:   externalID,
		Name:         asset.Name,
		Namespace:    asset.Namespace,
		Status:       firstNonEmpty(asset.Status, "active"),
		PowerState:   powerStateFromStatus(asset.Status),
		NodeName:     asset.Node,
		IPAddresses:  ipAddresses,
		Labels:       metadataMap(asset.Metadata),
		Config:       config,
		Raw:          map[string]any{"assetType": asset.Type},
	}
}

func imageFromAsset(connection domainvirtualization.Connection, asset domainvirtualization.Asset) domainvirtualization.Image {
	config := metadataMap(asset.Metadata)
	config["source"] = "sync"
	config["orphanHint"] = "provider_discovered"
	if asset.Node != "" {
		config["node"] = asset.Node
	}
	if asset.Namespace != "" {
		config["namespace"] = asset.Namespace
	}
	sourceKind := asset.Type
	switch asset.Type {
	case "iso":
		sourceKind = "iso"
	case "template":
		sourceKind = "template"
	case "storage_content":
		sourceKind = firstNonEmpty(asset.Metadata["contentType"], asset.Type)
	}
	config["sourceKind"] = sourceKind
	if sourceRef := firstNonEmpty(asset.Metadata["sourceRef"], asset.Metadata["volid"], asset.Metadata["vmid"], asset.Name); sourceRef != "" {
		config["sourceRef"] = sourceRef
	}
	return domainvirtualization.Image{
		Provider:     connection.Provider,
		ConnectionID: connection.ID,
		ExternalID:   firstNonEmpty(asset.Metadata["uid"], asset.Metadata["volid"], asset.Name),
		Name:         asset.Name,
		Status:       firstNonEmpty(asset.Status, "active"),
		Config:       config,
		Raw:          map[string]any{"assetType": asset.Type, "namespace": asset.Namespace, "node": asset.Node},
	}
}

func flavorFromAsset(connection domainvirtualization.Connection, asset domainvirtualization.Asset) domainvirtualization.Flavor {
	return domainvirtualization.Flavor{
		Provider:     connection.Provider,
		ConnectionID: connection.ID,
		ExternalID:   firstNonEmpty(asset.Metadata["uid"], asset.Name),
		Name:         asset.Name,
		Status:       firstNonEmpty(asset.Status, "active"),
		CPUCores:     metadataInt(asset.Metadata, "cpu"),
		MemoryMB:     metadataMemoryMiB(asset.Metadata, "memory"),
		Config:       metadataMap(asset.Metadata),
		Raw:          map[string]any{"assetType": asset.Type},
	}
}

func metadataInt(values map[string]string, key string) int {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

func metadataMemoryMiB(values map[string]string, key string) int {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return 0
	}
	quantity, err := resource.ParseQuantity(raw)
	if err != nil {
		return 0
	}
	return int(quantity.Value() / (1024 * 1024))
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

func mapValue(payload map[string]any, key string) map[string]any {
	value, ok := payload[key].(map[string]any)
	if !ok || value == nil {
		return map[string]any{}
	}
	return value
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

func uniqueNonEmptyStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func commaSeparatedValues(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
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

func powerStateAfterAction(action domainvirtualization.PowerAction, fallback string) string {
	switch action {
	case domainvirtualization.PowerActionStart, domainvirtualization.PowerActionRestart:
		return "running"
	case domainvirtualization.PowerActionStop:
		return "stopped"
	default:
		return fallback
	}
}
