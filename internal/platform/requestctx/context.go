package requestctx

import "context"

type Metadata struct {
	RequestID string
	Path      string
	Method    string
	SourceIP  string
	Source    string
	UserAgent string
}

type ctxKey string

const metadataKey ctxKey = "request_metadata"

func WithMetadata(ctx context.Context, metadata Metadata) context.Context {
	return context.WithValue(ctx, metadataKey, metadata)
}

func FromContext(ctx context.Context) Metadata {
	metadata, ok := ctx.Value(metadataKey).(Metadata)
	if !ok {
		return Metadata{}
	}
	return metadata
}
