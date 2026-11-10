package templateupdate

import "context"

type adoptionKey struct{}
type adoption struct{ kind, applicationID, planID string }

// WithAdoption is an internal capability issued while applying the frozen Plan.
// It is never decoded from HTTP, template variables, metadata or workflow input.
func WithAdoption(ctx context.Context, kind, applicationID, planID string) context.Context {
	return context.WithValue(ctx, adoptionKey{}, adoption{kind: kind, applicationID: applicationID, planID: planID})
}
func IsAdoption(ctx context.Context, kind, applicationID string) bool {
	value, ok := ctx.Value(adoptionKey{}).(adoption)
	return ok && value.kind == kind && value.applicationID == applicationID && value.planID != ""
}

type previewBatchKey struct{}

// Issued only after the preview digest, selection and history acknowledgement
// have been checked. Ordinary batch/recipe requests cannot supply this context.
func WithPreviewBatch(ctx context.Context, previewID string) context.Context {
	return context.WithValue(ctx, previewBatchKey{}, previewID)
}
func IsPreviewBatch(ctx context.Context, previewID string) bool {
	value, _ := ctx.Value(previewBatchKey{}).(string)
	return previewID != "" && value == previewID
}
