package requestctx

import "go.uber.org/zap"

func LoggerFields(metadata Metadata) []zap.Field {
	fields := make([]zap.Field, 0, 5)
	if metadata.RequestID != "" {
		fields = append(fields, zap.String("request_id", metadata.RequestID))
	}
	if metadata.TraceID != "" {
		fields = append(fields, zap.String("trace_id", metadata.TraceID))
	}
	if metadata.SpanID != "" {
		fields = append(fields, zap.String("span_id", metadata.SpanID))
	}
	if metadata.Path != "" {
		fields = append(fields, zap.String("request_path", metadata.Path))
	}
	if metadata.Method != "" {
		fields = append(fields, zap.String("request_method", metadata.Method))
	}
	return fields
}
