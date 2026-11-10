package logs

import (
	"context"
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/telemetry"
)

type Scope = telemetry.LogScope
type SearchQuery = telemetry.LogSearchQuery
type SearchResult = telemetry.LogSearchResult
type HistogramQuery = telemetry.LogHistogramQuery
type ContextWindowQuery = telemetry.LogContextWindowQuery
type CorrelationQuery = telemetry.LogCorrelationQuery
type Record = telemetry.LogRecord
type Signature = telemetry.LogSignature
type CorrelationResult = telemetry.LogCorrelationResult

type Driver interface {
	BackendType() string
	ValidateConfig(config map[string]any) error
	Search(ctx context.Context, sourceID string, config map[string]any, query SearchQuery) (SearchResult, error)
	Correlate(ctx context.Context, sourceID string, config map[string]any, query CorrelationQuery) (CorrelationResult, error)
}

type Registry struct {
	drivers map[string]Driver
}

func NewRegistry() *Registry {
	driver := newLokiDriver()
	return &Registry{drivers: map[string]Driver{driver.BackendType(): driver}}
}

func (r *Registry) Get(backendType string) (Driver, bool) {
	driver, ok := r.drivers[strings.ToLower(strings.TrimSpace(backendType))]
	return driver, ok
}

func (r *Registry) Validate(backendType string, config map[string]any) error {
	driver, ok := r.Get(backendType)
	if !ok {
		return fmt.Errorf("unsupported log backend %s", backendType)
	}
	return driver.ValidateConfig(config)
}

func (r *Registry) Correlate(ctx context.Context, backendType, sourceID string, config map[string]any, query CorrelationQuery) (CorrelationResult, error) {
	driver, ok := r.Get(backendType)
	if !ok {
		return CorrelationResult{}, fmt.Errorf("unsupported log backend %s", backendType)
	}
	return driver.Correlate(ctx, sourceID, config, query)
}

func (r *Registry) Search(ctx context.Context, backendType, sourceID string, config map[string]any, query SearchQuery) (SearchResult, error) {
	driver, ok := r.Get(backendType)
	if !ok {
		return SearchResult{}, fmt.Errorf("unsupported log backend %s", backendType)
	}
	return driver.Search(ctx, sourceID, config, query)
}

var defaultRegistry = NewRegistry()

func DefaultRegistry() *Registry {
	return defaultRegistry
}
