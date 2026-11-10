package aigateway

import (
	"context"
	"fmt"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

// relayExecutionReceipt identifies the attempt that actually supplied the response.
// A handled HTTP error or an interrupted stream does not constitute success.
type relayExecutionReceipt struct {
	Selection relaySelection
	Succeeded bool
	CacheHit  bool
}

func (r relayExecutionReceipt) requireSuccess(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !r.Succeeded {
		return fmt.Errorf("%w: model relay did not complete successfully", apperrors.ErrClusterUnready)
	}
	return nil
}
