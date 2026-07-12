package virtualization

import (
	"context"
	"fmt"
	"reflect"
	"time"

	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ConnectionReader interface {
	GetConnection(context.Context, string) (domainvirtualization.Connection, error)
	ListConnections(context.Context, domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error)
	CountConnections(context.Context, domainvirtualization.ConnectionFilter) (int, error)
}

type ConnectionWriter interface {
	CreateConnection(context.Context, domainvirtualization.ConnectionInput) (domainvirtualization.Connection, error)
	UpdateConnection(context.Context, string, domainvirtualization.ConnectionInput) (domainvirtualization.Connection, error)
	DeleteConnection(context.Context, string) error
	UpdateConnectionHealth(context.Context, string, map[string]any, *time.Time) (domainvirtualization.Connection, error)
}

type DockerLinkRepository interface {
	CountDockerHostsByConnection(context.Context, string) (int, error)
	MarkDockerHostsUnavailableByConnection(context.Context, string) error
	MarkDockerHostsUnavailableByVM(context.Context, string) error
}

type VMRepository interface {
	UpsertVM(context.Context, domainvirtualization.VM) (domainvirtualization.VM, error)
	GetVM(context.Context, string) (domainvirtualization.VM, error)
	ListVMs(context.Context, domainvirtualization.VMFilter) ([]domainvirtualization.VM, error)
	CountVMs(context.Context, domainvirtualization.VMFilter) (int, error)
	MarkVMsStale(context.Context, string, string, time.Time) error
}

type ImageRepository interface {
	UpsertImage(context.Context, domainvirtualization.Image) (domainvirtualization.Image, error)
	GetImage(context.Context, string) (domainvirtualization.Image, error)
	ListImages(context.Context, domainvirtualization.ImageFilter) ([]domainvirtualization.Image, error)
	CountImages(context.Context, domainvirtualization.ImageFilter) (int, error)
	MarkImagesStale(context.Context, string, string, time.Time) error
}

type FlavorRepository interface {
	UpsertFlavor(context.Context, domainvirtualization.Flavor) (domainvirtualization.Flavor, error)
	GetFlavor(context.Context, string) (domainvirtualization.Flavor, error)
	ListFlavors(context.Context, domainvirtualization.FlavorFilter) ([]domainvirtualization.Flavor, error)
	CountFlavors(context.Context, domainvirtualization.FlavorFilter) (int, error)
	MarkFlavorsStale(context.Context, string, string, time.Time) error
}

type TaskRepository interface {
	CreateTask(context.Context, domainvirtualization.Task) (domainvirtualization.Task, error)
	UpdateTask(context.Context, domainvirtualization.Task) (domainvirtualization.Task, error)
	GetTask(context.Context, string) (domainvirtualization.Task, error)
	ListTasks(context.Context, domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error)
	CountTasks(context.Context, domainvirtualization.TaskFilter) (int, error)
}

type TaskQueueRepository interface {
	ClaimTask(context.Context, string, time.Time) (domainvirtualization.Task, error)
	HeartbeatTask(context.Context, string, string, time.Time) error
	ListTimedOutTasks(context.Context, time.Time, int) ([]domainvirtualization.Task, error)
}

type TaskLogRepository interface {
	CreateTaskLog(context.Context, domainvirtualization.TaskLog) error
	ListTaskLogs(context.Context, string, int) ([]domainvirtualization.TaskLog, error)
}

type Dependencies struct {
	Connections      ConnectionReader
	ConnectionWriter ConnectionWriter
	DockerLinks      DockerLinkRepository
	VMs              VMRepository
	Images           ImageRepository
	Flavors          FlavorRepository
	Tasks            TaskRepository
	TaskQueue        TaskQueueRepository
	TaskLogs         TaskLogRepository
}

func (d Dependencies) validate() error {
	required := []struct {
		name  string
		value any
	}{
		{"connections", d.Connections},
		{"connection writer", d.ConnectionWriter},
		{"docker links", d.DockerLinks},
		{"VMs", d.VMs},
		{"images", d.Images},
		{"flavors", d.Flavors},
		{"tasks", d.Tasks},
		{"task queue", d.TaskQueue},
		{"task logs", d.TaskLogs},
	}
	for _, dependency := range required {
		if nilDependency(dependency.value) {
			return fmt.Errorf("%w: virtualization service: %s dependency is required", apperrors.ErrInvalidArgument, dependency.name)
		}
	}
	return nil
}

func nilDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
