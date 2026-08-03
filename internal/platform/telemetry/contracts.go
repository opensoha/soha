package telemetry

import "time"

type LogScope struct {
	ClusterID  string   `json:"clusterId,omitempty"`
	Namespace  string   `json:"namespace,omitempty"`
	Service    string   `json:"service,omitempty"`
	Workload   string   `json:"workload,omitempty"`
	Pod        string   `json:"pod,omitempty"`
	Pods       []string `json:"pods,omitempty"`
	Container  string   `json:"container,omitempty"`
	Containers []string `json:"containers,omitempty"`
}

type LogSearchQuery struct {
	Scope      LogScope  `json:"scope"`
	TimeFrom   time.Time `json:"timeFrom"`
	TimeTo     time.Time `json:"timeTo"`
	Query      string    `json:"query,omitempty"`
	TraceID    string    `json:"traceId,omitempty"`
	SpanID     string    `json:"spanId,omitempty"`
	Terms      []string  `json:"terms,omitempty"`
	Severities []string  `json:"severities,omitempty"`
	Limit      int       `json:"limit"`
	Direction  string    `json:"direction"`
	PageToken  string    `json:"pageToken,omitempty"`
}

type LogSearchResult struct {
	SourceID      string         `json:"sourceId"`
	Records       []LogRecord    `json:"records"`
	NextPageToken string         `json:"nextPageToken,omitempty"`
	Truncated     bool           `json:"truncated"`
	QueryCost     map[string]any `json:"queryCost,omitempty"`
	ErrorKind     string         `json:"errorKind,omitempty"`
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
	TraceID  string
	SpanID   string
	Limit    int
}

type LogRecord struct {
	Timestamp  time.Time      `json:"timestamp"`
	Severity   string         `json:"severity,omitempty"`
	Message    string         `json:"message"`
	Service    string         `json:"service,omitempty"`
	Workload   string         `json:"workload,omitempty"`
	Namespace  string         `json:"namespace,omitempty"`
	ClusterID  string         `json:"clusterId,omitempty"`
	Pod        string         `json:"pod,omitempty"`
	Container  string         `json:"container,omitempty"`
	TraceID    string         `json:"traceId,omitempty"`
	SpanID     string         `json:"spanId,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
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
	TraceID     string
	TimeFrom    time.Time
	TimeTo      time.Time
	MinDuration time.Duration
	Limit       int
}

type TraceSpan struct {
	TraceID      string            `json:"traceId"`
	SpanID       string            `json:"spanId"`
	ParentSpanID string            `json:"parentSpanId,omitempty"`
	Operation    string            `json:"operation"`
	Service      string            `json:"service"`
	DurationMS   float64           `json:"durationMs"`
	StartTime    time.Time         `json:"startTime"`
	Tags         map[string]string `json:"tags,omitempty"`
	Error        bool              `json:"error"`
}

type TraceResult struct {
	SourceID     string           `json:"sourceId"`
	Summary      string           `json:"summary"`
	Spans        []TraceSpan      `json:"spans"`
	Hotspots     []map[string]any `json:"hotspots,omitempty"`
	QueryCost    map[string]any   `json:"queryCost,omitempty"`
	SampleWindow map[string]any   `json:"sampleWindow,omitempty"`
}
