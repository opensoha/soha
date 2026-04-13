package logs

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Scope struct {
	ClusterID string
	Namespace string
	Service   string
	Workload  string
}

type SearchQuery struct {
	Scope    Scope
	TimeFrom time.Time
	TimeTo   time.Time
	Query    string
	Limit    int
}

type HistogramQuery struct {
	Scope    Scope
	TimeFrom time.Time
	TimeTo   time.Time
	GroupBy  string
}

type ContextWindowQuery struct {
	Scope         Scope
	Timestamp     time.Time
	BeforeSeconds int
	AfterSeconds  int
	Limit         int
}

type CorrelationQuery struct {
	Scope    Scope
	AlertID  string
	Workload string
	TimeFrom time.Time
	TimeTo   time.Time
	Query    string
	Limit    int
}

type Record struct {
	Timestamp  time.Time
	Severity   string
	Message    string
	Service    string
	Workload   string
	Namespace  string
	ClusterID  string
	Attributes map[string]any
}

type Signature struct {
	Signature string
	Count     int
	Sample    string
	Severity  string
}

type CorrelationResult struct {
	SourceID     string
	Summary      string
	Records      []Record
	Signatures   []Signature
	Truncated    bool
	QueryCost    map[string]any
	ErrorKind    string
	SampleWindow map[string]any
}

type Driver interface {
	BackendType() string
	ValidateConfig(config map[string]any) error
	Correlate(ctx context.Context, sourceID string, config map[string]any, query CorrelationQuery) (CorrelationResult, error)
}

type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

func NewRegistry() *Registry {
	registry := &Registry{drivers: map[string]Driver{}}
	for _, driver := range []Driver{newESDriver(), newLokiDriver(), newClickHouseDriver()} {
		registry.drivers[driver.BackendType()] = driver
	}
	return registry
}

func (r *Registry) Get(backendType string) (Driver, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.drivers[strings.TrimSpace(backendType)]
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

var defaultRegistry = NewRegistry()

func DefaultRegistry() *Registry {
	return defaultRegistry
}
