package manifest

import (
	"context"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
)

func (r *Repository) HasTargetWriteHistory(ctx context.Context, cluster, namespace string, keys []string, release, ignorePlan string) (bool, error) {
	return repodelivery.TargetWriteHistory(r.db.WithContext(ctx), cluster, namespace, keys, release, ignorePlan)
}
