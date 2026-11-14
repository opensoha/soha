package proxypublication

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
)

func (r *Repository) CurrentProtection(ctx context.Context) (*domain.ProtectedDestinations, error) {
	repo := accessrepo.New(r.db)
	snapshot, err := repo.GetPolicySnapshot(ctx)
	if err != nil {
		return nil, err
	}
	return repo.CompileProtectedDestinations(ctx, snapshot.PolicyVersion, snapshot.ProtectedResourceIDs)
}
