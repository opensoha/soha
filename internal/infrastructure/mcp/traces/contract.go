package traces

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/opensoha/soha/internal/platform/telemetry"
)

type Scope = telemetry.TraceScope
type Query = telemetry.TraceQuery
type Span = telemetry.TraceSpan
type Result = telemetry.TraceResult

type Driver interface {
	BackendType() string
	ValidateConfig(config map[string]any) error
	FindSlowSpans(ctx context.Context, sourceID string, config map[string]any, query Query) (Result, error)
}

type ServiceDriver interface {
	ListServices(context.Context, string, map[string]any, telemetry.ServiceQuery) (telemetry.ServiceResult, error)
	GetService(context.Context, string, map[string]any, telemetry.ServiceQuery) (telemetry.Service, error)
	GetServiceTopology(context.Context, string, map[string]any, telemetry.ServiceQuery) (telemetry.ServiceTopology, error)
}

type Registry struct {
	drivers map[string]Driver
}

func NewRegistry() *Registry {
	registry := &Registry{drivers: map[string]Driver{}}
	for _, driver := range []Driver{newJaegerDriver(), newSkyWalkingDriver()} {
		registry.drivers[driver.BackendType()] = driver
	}
	return registry
}

func (r *Registry) Get(backendType string) (Driver, bool) {
	driver, ok := r.drivers[strings.TrimSpace(backendType)]
	return driver, ok
}

func (r *Registry) Validate(backendType string, config map[string]any) error {
	driver, ok := r.Get(backendType)
	if !ok {
		return fmt.Errorf("unsupported trace backend %s", backendType)
	}
	return driver.ValidateConfig(config)
}

func (r *Registry) FindSlowSpans(ctx context.Context, backendType, sourceID string, config map[string]any, query Query) (Result, error) {
	driver, ok := r.Get(backendType)
	if !ok {
		return Result{}, fmt.Errorf("unsupported trace backend %s", backendType)
	}
	result, err := driver.FindSlowSpans(ctx, sourceID, config, query)
	if err != nil {
		return Result{}, err
	}
	sort.Slice(result.Spans, func(i, j int) bool {
		return result.Spans[i].DurationMS > result.Spans[j].DurationMS
	})
	return result, nil
}

func (r *Registry) ListServices(ctx context.Context, backendType, sourceID string, config map[string]any, query telemetry.ServiceQuery) (telemetry.ServiceResult, error) {
	driver, ok := r.serviceDriver(backendType)
	if !ok {
		return telemetry.ServiceResult{}, fmt.Errorf("service catalog is unsupported by trace backend %s", backendType)
	}
	return driver.ListServices(ctx, sourceID, config, query)
}

func (r *Registry) GetService(ctx context.Context, backendType, sourceID string, config map[string]any, query telemetry.ServiceQuery) (telemetry.Service, error) {
	driver, ok := r.serviceDriver(backendType)
	if !ok {
		return telemetry.Service{}, fmt.Errorf("service catalog is unsupported by trace backend %s", backendType)
	}
	return driver.GetService(ctx, sourceID, config, query)
}

func (r *Registry) GetServiceTopology(ctx context.Context, backendType, sourceID string, config map[string]any, query telemetry.ServiceQuery) (telemetry.ServiceTopology, error) {
	driver, ok := r.serviceDriver(backendType)
	if !ok {
		return telemetry.ServiceTopology{}, fmt.Errorf("service topology is unsupported by trace backend %s", backendType)
	}
	return driver.GetServiceTopology(ctx, sourceID, config, query)
}

func (r *Registry) serviceDriver(backendType string) (ServiceDriver, bool) {
	driver, ok := r.Get(backendType)
	if !ok {
		return nil, false
	}
	serviceDriver, ok := driver.(ServiceDriver)
	return serviceDriver, ok
}

var defaultRegistry = NewRegistry()

func DefaultRegistry() *Registry {
	return defaultRegistry
}
