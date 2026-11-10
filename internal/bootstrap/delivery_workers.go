package bootstrap

import (
	"context"
	"fmt"
	"reflect"
)

type deliveryBackgroundWorker interface {
	Start(context.Context)
}

type deliveryHelmWorkers interface {
	ValidateRuntimeDependencies() error
	StartHelmDelivery(context.Context)
}

type deliveryWorkflowWorkers interface {
	ValidateDeliveryRuntime() error
	Start(context.Context)
}

// This is the startup cohort for delivery. Construction and cross-owner
// bindings must complete before validating or starting any member.
type deliveryWorkers struct {
	execution deliveryBackgroundWorker
	manifests deliveryBackgroundWorker
	helms     deliveryHelmWorkers
	workflows deliveryWorkflowWorkers
	triggers  deliveryBackgroundWorker
	updates   deliveryBackgroundWorker
}

func (workers deliveryWorkers) start(ctx context.Context) error {
	for _, dependency := range []struct {
		name  string
		value any
	}{
		{"execution", workers.execution},
		{"manifests", workers.manifests}, {"Helm", workers.helms},
		{"workflows", workers.workflows}, {"triggers", workers.triggers}, {"template updates", workers.updates},
	} {
		if dependency.value == nil || reflect.ValueOf(dependency.value).Kind() == reflect.Pointer && reflect.ValueOf(dependency.value).IsNil() {
			return fmt.Errorf("delivery startup requires %s", dependency.name)
		}
	}
	if err := workers.helms.ValidateRuntimeDependencies(); err != nil {
		return err
	}
	if err := workers.workflows.ValidateDeliveryRuntime(); err != nil {
		return err
	}
	workers.execution.Start(ctx)
	workers.manifests.Start(ctx)
	workers.helms.StartHelmDelivery(ctx)
	workers.workflows.Start(ctx)
	workers.triggers.Start(ctx)
	workers.updates.Start(ctx)
	return nil
}
