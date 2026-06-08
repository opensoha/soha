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
