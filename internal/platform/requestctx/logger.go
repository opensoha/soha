package requestctx

import "go.uber.org/zap"

func CorrelationFields(metadata Metadata) []zap.Field {
	fields := make([]zap.Field, 0, 3)
	if metadata.RequestID != "" {
		fields = append(fields, zap.String("request_id", metadata.RequestID))
	}
	if metadata.TraceID != "" {
		fields = append(fields, zap.String("trace_id", metadata.TraceID))
	}
	if metadata.SpanID != "" {
		fields = append(fields, zap.String("span_id", metadata.SpanID))
	}
	return fields
}

func LoggerFields(metadata Metadata) []zap.Field {
	fields := CorrelationFields(metadata)
	if metadata.Path != "" {
		fields = append(fields, zap.String("request_path", metadata.Path))
	}
	if metadata.Method != "" {
		fields = append(fields, zap.String("request_method", metadata.Method))
	}
	return fields
}
