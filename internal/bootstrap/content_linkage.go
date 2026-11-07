package bootstrap

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"

	runtime "github.com/opensoha/soha/internal/repository/networkruntime"
)

type contentNetworkLinkage struct {
	approvals runtime.ContentHoldApprovalReader
	runtime   *runtime.Repository
}

func (l contentNetworkLinkage) Within(ctx context.Context, fn func(context.Context) error) error {
	return l.runtime.WithinContentGovernance(ctx, fn)
}
func (l contentNetworkLinkage) Restrict(ctx context.Context, e domain.Event) (string, error) {
	return l.runtime.RestrictContentSubject(ctx, e)
}
func (l contentNetworkLinkage) Release(ctx context.Context, id, reviewer string) error {
	return l.runtime.ReleaseContentSubject(ctx, id, reviewer, l.approvals)
}
func (l contentNetworkLinkage) State(ctx context.Context, id string) (domain.NetworkHold, error) {
	return l.runtime.ContentSubjectState(ctx, id)
}
