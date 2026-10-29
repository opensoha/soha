package bootstrap

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/platform/dbtx"
	runtime "github.com/opensoha/soha/internal/repository/networkruntime"
	"gorm.io/gorm"
)

type contentNetworkLinkage struct {
	db      *gorm.DB
	runtime *runtime.Repository
}

func (l contentNetworkLinkage) Within(ctx context.Context, fn func(context.Context) error) error {
	return dbtx.Within(ctx, l.db, func(ctx context.Context) error {
		for _, key := range []string{"network-wireguard-mutations", "network-endpoint-response-policies:default:default", "network-access-policy-snapshot:default:default"} {
			if err := dbtx.DB(ctx, l.db).Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, key).Error; err != nil {
				return err
			}
		}
		return fn(ctx)
	})
}
func (l contentNetworkLinkage) Restrict(ctx context.Context, e domain.Event) (string, error) {
	return l.runtime.RestrictContentSubject(ctx, e)
}
func (l contentNetworkLinkage) Release(ctx context.Context, id, reviewer string) error {
	return l.runtime.ReleaseContentSubject(ctx, id, reviewer)
}
func (l contentNetworkLinkage) State(ctx context.Context, id string) (domain.NetworkHold, error) {
	return l.runtime.ContentSubjectState(ctx, id)
}
