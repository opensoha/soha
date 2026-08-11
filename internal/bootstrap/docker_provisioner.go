package bootstrap

import (
	"context"
	"fmt"
	"strings"

	appdocker "github.com/opensoha/soha/internal/application/docker"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

type dockerHostProvisioner struct {
	virtualization interface {
		CreateVM(context.Context, domainidentity.Principal, appvirtualization.CreateVMInput) (domainvirtualization.Task, error)
		GetOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
		GetVM(context.Context, domainidentity.Principal, string) (domainvirtualization.VM, error)
		CancelOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
		RetryOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	}
}

func (p dockerHostProvisioner) ProvisionDockerHost(ctx context.Context, principal domainidentity.Principal, input appdocker.HostProvisionInput) (appdocker.HostProvisionTask, error) {
	task, err := p.virtualization.CreateVM(ctx, principal, appvirtualization.CreateVMInput{
		ConnectionID:      input.ConnectionID,
		Name:              input.Name,
		Architecture:      input.Architecture,
		CPU:               input.CPU,
		MemoryMiB:         input.MemoryMiB,
		DiskGiB:           input.DiskGiB,
		BootImageID:       input.BootImageID,
		ImageID:           input.ImageID,
		FlavorID:          input.FlavorID,
		Node:              stringFromMap(input.ProviderParams, "node"),
		Network:           input.Network,
		CloudInit:         input.CloudInit,
		SourceMode:        stringFromMap(input.ProviderParams, "sourceMode"),
		StartAfterCreate:  input.StartAfterCreate,
		TemplateID:        input.TemplateID,
		ProviderParams:    input.ProviderParams,
		ProviderExtraJSON: input.ProviderExtraJSON,
	})
	if err != nil {
		return appdocker.HostProvisionTask{}, err
	}
	return hostProvisionTaskFromVirtualizationTask(task, stringFromMap(task.Result, "name"), task.Result), nil
}

func (p dockerHostProvisioner) GetProvisionTask(ctx context.Context, taskID string) (appdocker.HostProvisionTask, error) {
	principal := dockerProvisionSystemPrincipal()
	task, err := p.virtualization.GetOperation(ctx, principal, taskID)
	if err != nil {
		return appdocker.HostProvisionTask{}, err
	}
	vmID := task.VMID
	vmName := stringFromMap(task.Result, "name")
	if vmID != "" && vmName == "" {
		if vm, vmErr := p.virtualization.GetVM(ctx, principal, vmID); vmErr == nil {
			vmName = vm.Name
		}
	}
	return hostProvisionTaskFromVirtualizationTask(task, vmName, task.Result), nil
}

func (p dockerHostProvisioner) CancelProvisionTask(ctx context.Context, principal domainidentity.Principal, taskID string) (appdocker.HostProvisionTask, error) {
	task, err := p.virtualization.CancelOperation(ctx, principal, taskID)
	if err != nil {
		return appdocker.HostProvisionTask{}, err
	}
	return hostProvisionTaskFromVirtualizationTask(task, stringFromMap(task.Result, "name"), task.Result), nil
}

func (p dockerHostProvisioner) RetryProvisionTask(ctx context.Context, principal domainidentity.Principal, taskID string) (appdocker.HostProvisionTask, error) {
	task, err := p.virtualization.RetryOperation(ctx, principal, taskID)
	if err != nil {
		return appdocker.HostProvisionTask{}, err
	}
	return hostProvisionTaskFromVirtualizationTask(task, stringFromMap(task.Result, "name"), task.Result), nil
}

func hostProvisionTaskFromVirtualizationTask(task domainvirtualization.Task, vmName string, result map[string]any) appdocker.HostProvisionTask {
	return appdocker.HostProvisionTask{
		ID:           task.ID,
		Provider:     task.Provider,
		ConnectionID: task.ConnectionID,
		Status:       task.Status,
		VMID:         task.VMID,
		VMName:       vmName,
		Result:       result,
	}
}

func dockerProvisionSystemPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "system",
		UserName: "System",
		Roles:    []string{"admin"},
	}
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
