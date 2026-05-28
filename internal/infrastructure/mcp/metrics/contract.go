package metrics

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type Scope struct {
	ClusterID string
	Namespace string
	Workload  string
	Service   string
}

type RangeQuery struct {
	Scope     Scope
	MetricKey string
	TimeFrom  time.Time
	TimeTo    time.Time
	Step      time.Duration
}

type Point struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type Series struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Unit   string  `json:"unit,omitempty"`
	Points []Point `json:"points"`
	Latest float64 `json:"latest"`
}

type AnomalySummary struct {
	MetricKey    string           `json:"metricKey"`
	Scope        Scope            `json:"scope"`
	Series       []Series         `json:"series"`
	Signals      []map[string]any `json:"signals"`
	Summary      string           `json:"summary"`
	QueryCost    map[string]any   `json:"queryCost"`
	SampleWindow map[string]any   `json:"sampleWindow"`
}

type Driver interface {
	BackendType() string
	ValidateConfig(config map[string]any) error
	RangeQuery(ctx context.Context, sourceID string, config map[string]any, query RangeQuery) ([]Series, map[string]any, error)
}

type Registry struct {
	drivers map[string]Driver
}

func NewRegistry() *Registry {
	registry := &Registry{drivers: map[string]Driver{}}
	driver := newPrometheusDriver()
	registry.drivers[driver.BackendType()] = driver
	return registry
}

func (r *Registry) Get(backendType string) (Driver, bool) {
	driver, ok := r.drivers[strings.TrimSpace(backendType)]
	return driver, ok
}

func (r *Registry) Validate(backendType string, config map[string]any) error {
	driver, ok := r.Get(backendType)
	if !ok {
		return fmt.Errorf("unsupported metrics backend %s", backendType)
	}
	return driver.ValidateConfig(config)
}

func (r *Registry) RangeQuery(ctx context.Context, backendType, sourceID string, config map[string]any, query RangeQuery) ([]Series, map[string]any, error) {
	driver, ok := r.Get(backendType)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported metrics backend %s", backendType)
	}
	return driver.RangeQuery(ctx, sourceID, config, query)
}

func (r *Registry) Analyze(ctx context.Context, backendType, sourceID string, config map[string]any, query RangeQuery) (AnomalySummary, error) {
	series, queryCost, err := r.RangeQuery(ctx, backendType, sourceID, config, query)
	if err != nil {
		return AnomalySummary{}, err
	}
	signals := make([]map[string]any, 0)
	for _, item := range series {
		if len(item.Points) < 2 {
			continue
		}
		maxValue := item.Points[0].Value
		minValue := item.Points[0].Value
		total := 0.0
		for _, point := range item.Points {
			maxValue = math.Max(maxValue, point.Value)
			minValue = math.Min(minValue, point.Value)
			total += point.Value
		}
		average := total / float64(len(item.Points))
		trend := "stable"
		if average > 0 && item.Latest >= average*1.5 {
			trend = "spike"
		} else if average > 0 && item.Latest <= average*0.5 {
			trend = "drop"
		}
		signals = append(signals, map[string]any{
			"metricKey": item.Key,
			"label":     item.Label,
			"latest":    item.Latest,
			"average":   average,
			"max":       maxValue,
			"min":       minValue,
			"trend":     trend,
		})
	}
	sort.Slice(signals, func(i, j int) bool {
		return fmt.Sprint(signals[i]["metricKey"]) < fmt.Sprint(signals[j]["metricKey"])
	})
	summary := "no metric anomalies detected"
	if len(signals) > 0 {
		parts := make([]string, 0, len(signals))
		for _, item := range signals {
			if fmt.Sprint(item["trend"]) == "stable" {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%s", item["metricKey"], item["trend"]))
		}
		if len(parts) > 0 {
			summary = strings.Join(parts, ", ")
		}
	}
	return AnomalySummary{
		MetricKey:    query.MetricKey,
		Scope:        query.Scope,
		Series:       series,
		Signals:      signals,
		Summary:      summary,
		QueryCost:    queryCost,
		SampleWindow: map[string]any{"timeFrom": query.TimeFrom.Format(time.RFC3339), "timeTo": query.TimeTo.Format(time.RFC3339)},
	}, nil
}

var defaultRegistry = NewRegistry()

func DefaultRegistry() *Registry {
	return defaultRegistry
}
