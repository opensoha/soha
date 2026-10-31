package bootstrap

import (
	"context"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
)

type managedContentNetworkLinkage struct{ contentNetworkLinkage }

func (l managedContentNetworkLinkage) Restrict(ctx context.Context, r d.Record) (string, error) {
	return l.runtime.RestrictManagedContentSubject(ctx, r)
}
func (l managedContentNetworkLinkage) Release(ctx context.Context, id, reviewer string) error {
	return l.runtime.ReleaseManagedContentSubject(ctx, id, reviewer)
}
func (l managedContentNetworkLinkage) State(ctx context.Context, id string) (d.NetworkHold, error) {
	return l.runtime.ManagedContentState(ctx, id)
}
