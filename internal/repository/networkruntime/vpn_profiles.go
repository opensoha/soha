package networkruntime

import (
	"context"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	networkrepo "github.com/opensoha/soha/internal/repository/networkaccess"
)

func (r *Repository) GetVPNProfile(ctx context.Context, id string) (domain.VPNProfile, error) {
	return networkrepo.New(r.db).GetVPNProfile(ctx, id)
}
func (r *Repository) GetVPNSelectionPolicy(ctx context.Context, id string) (domain.VPNSelectionPolicy, error) {
	return networkrepo.New(r.db).GetVPNSelectionPolicy(ctx, id)
}
