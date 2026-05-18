package traces

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Scope struct {
	ClusterID string
	Namespace string
	Service   string
	Workload  string
}

type Query struct {
	Scope       Scope
	TimeFrom    time.Time
	TimeTo      time.Time
	MinDuration time.Duration
	Limit       int
}

type Span struct {
	TraceID      string         `json:"traceId"`
	SpanID       string         `json:"spanId"`
	ParentSpanID string         `json:"parentSpanId,omitempty"`
	Operation    string         `json:"operation"`
	Service      string         `json:"service"`
	DurationMS   float64        `json:"durationMs"`
	StartTime    time.Time      `json:"startTime"`
	Tags         map[string]any `json:"tags,omitempty"`
	Error        bool           `json:"error"`
}

type Result struct {
	SourceID        string         `json:"sourceId"`
	Summary         string         `json:"summary"`
	Spans           []Span         `json:"spans"`
	Hotspots        []map[string]any `json:"hotspots,omitempty"`
	QueryCost       map[string]any `json:"queryCost,omitempty"`
	SampleWindow    map[string]any `json:"sampleWindow,omitempty"`
}

type Driver interface {
	BackendType() string
	ValidateConfig(config map[string]any) error
	FindSlowSpans(ctx context.Context, sourceID string, config map[string]any, query Query) (Result, error)
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

var defaultRegistry = NewRegistry()

func DefaultRegistry() *Registry {
	return defaultRegistry
}
