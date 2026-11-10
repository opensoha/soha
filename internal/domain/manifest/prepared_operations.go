package manifest

import "context"

// PreparedOperations retains only already-authorized operation writes while a
// Plan's bounded live preparation runs. Flush joins the caller's durable transaction.
// It is request-local and used synchronously; it introduces no task lifecycle state.
type PreparedOperations struct{ writes []func(context.Context) error }
type preparedOperationsKey struct{}

func WithPreparedOperations(ctx context.Context) (context.Context, *PreparedOperations) {
	batch := &PreparedOperations{}
	return context.WithValue(ctx, preparedOperationsKey{}, batch), batch
}
func StagePreparedOperation(ctx context.Context, write func(context.Context) error) bool {
	batch, ok := ctx.Value(preparedOperationsKey{}).(*PreparedOperations)
	if !ok {
		return false
	}
	batch.writes = append(batch.writes, write)
	return true
}
func FlushPreparedOperations(ctx context.Context) error {
	batch, ok := ctx.Value(preparedOperationsKey{}).(*PreparedOperations)
	if !ok {
		return nil
	}
	for _, write := range batch.writes {
		if err := write(ctx); err != nil {
			return err
		}
	}
	return nil
}
