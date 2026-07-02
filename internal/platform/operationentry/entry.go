package operationentry

import (
	"context"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func New(
	ctx context.Context,
	principal domainidentity.Principal,
	operationType string,
	targetScope map[string]any,
	result string,
	summary string,
	metadata map[string]any,
) domainoperation.Entry {
	meta := requestctx.FromContext(ctx)
	if targetScope == nil {
		targetScope = map[string]any{}
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	if meta.Source != "" {
		if _, exists := metadata["source"]; !exists {
			metadata["source"] = meta.Source
		}
	}
	if meta.RequestID != "" {
		if _, exists := metadata["requestId"]; !exists {
			metadata["requestId"] = meta.RequestID
		}
	}
	if meta.TraceID != "" {
		if _, exists := metadata["traceId"]; !exists {
			metadata["traceId"] = meta.TraceID
		}
	}
	if meta.SpanID != "" {
		if _, exists := metadata["spanId"]; !exists {
			metadata["spanId"] = meta.SpanID
		}
	}
	return domainoperation.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		OperationType: operationType,
		TargetScope:   targetScope,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
		CreatedAt:     time.Now().UTC(),
	}
}
