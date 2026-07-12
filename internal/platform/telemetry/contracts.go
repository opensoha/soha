package telemetry

import "time"

type LogScope struct {
	ClusterID string
	Namespace string
	Service   string
	Workload  string
}

type LogSearchQuery struct {
	Scope    LogScope
	TimeFrom time.Time
	TimeTo   time.Time
	Query    string
	Limit    int
}

type LogHistogramQuery struct {
	Scope    LogScope
	TimeFrom time.Time
	TimeTo   time.Time
	GroupBy  string
}

type LogContextWindowQuery struct {
	Scope         LogScope
	Timestamp     time.Time
	BeforeSeconds int
	AfterSeconds  int
	Limit         int
}

type LogCorrelationQuery struct {
	Scope    LogScope
	AlertID  string
	Workload string
	TimeFrom time.Time
	TimeTo   time.Time
	Query    string
	Limit    int
}

type LogRecord struct {
	Timestamp  time.Time
	Severity   string
	Message    string
	Service    string
	Workload   string
	Namespace  string
	ClusterID  string
	Attributes map[string]any
}

type LogSignature struct {
	Signature string
	Count     int
	Sample    string
	Severity  string
}

type LogCorrelationResult struct {
	SourceID     string
	Summary      string
	Records      []LogRecord
	Signatures   []LogSignature
	Truncated    bool
	QueryCost    map[string]any
	ErrorKind    string
	SampleWindow map[string]any
}

type MetricScope struct {
	ClusterID string
	Namespace string
	Workload  string
	Service   string
}

type MetricRangeQuery struct {
	Scope     MetricScope
	MetricKey string
	TimeFrom  time.Time
	TimeTo    time.Time
	Step      time.Duration
}

type MetricPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

type MetricSeries struct {
	Key    string        `json:"key"`
	Label  string        `json:"label"`
	Unit   string        `json:"unit,omitempty"`
	Points []MetricPoint `json:"points"`
	Latest float64       `json:"latest"`
}

type MetricAnomalySummary struct {
	MetricKey    string           `json:"metricKey"`
	Scope        MetricScope      `json:"scope"`
	Series       []MetricSeries   `json:"series"`
	Signals      []map[string]any `json:"signals"`
	Summary      string           `json:"summary"`
	QueryCost    map[string]any   `json:"queryCost"`
	SampleWindow map[string]any   `json:"sampleWindow"`
}

type TraceScope struct {
	ClusterID string
	Namespace string
	Service   string
	Workload  string
}

type TraceQuery struct {
	Scope       TraceScope
	TimeFrom    time.Time
	TimeTo      time.Time
	MinDuration time.Duration
	Limit       int
}

type TraceSpan struct {
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

type TraceResult struct {
	SourceID     string           `json:"sourceId"`
	Summary      string           `json:"summary"`
	Spans        []TraceSpan      `json:"spans"`
	Hotspots     []map[string]any `json:"hotspots,omitempty"`
	QueryCost    map[string]any   `json:"queryCost,omitempty"`
	SampleWindow map[string]any   `json:"sampleWindow,omitempty"`
}
