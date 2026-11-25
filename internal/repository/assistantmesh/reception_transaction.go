package assistantmesh

import (
	"context"

	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) WithReceptionTransaction(ctx context.Context, apply func(context.Context) error) error {
	return dbtx.Within(ctx, r.db, apply)
}
