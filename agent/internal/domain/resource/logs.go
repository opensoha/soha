package resource

import sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"

type LogCoverage = sohaapi.LogCoverage
type LogEntry = sohaapi.LogEntry
type LogPage = sohaapi.LogPage
type LogQuery = sohaapi.LogQuery
type LogRuntimeOptions = sohaapi.LogRuntimeOptions
type LogSource = sohaapi.LogSource
type LogSourceSelector = sohaapi.LogSourceSelector
type LogWarning = sohaapi.LogWarning

// LogStreamEvent mirrors the WebSocket event schema, which is not emitted by
// the Go generator because stream tickets, rather than events, are the HTTP response.
type LogStreamEvent struct {
	Type   string           `json:"type"`
	Entry  *LogEntry        `json:"entry,omitempty"`
	Status *LogStreamStatus `json:"status,omitempty"`
}

type LogStreamStatus struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}
