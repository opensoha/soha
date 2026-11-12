package networkaccess

import (
	"context"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

// Reserved member IDs resolve only through their managed parent. There is no
// exact-runtime fallback for a stale or unapproved member.
func (r *Repository) findManagedNASBinding(ctx context.Context, runtime, nas string) (domain.NASBinding, error) {
	var id string
	e := r.db.WithContext(ctx).Raw(`SELECT b.id FROM network_radius_members m JOIN network_radius_pools p ON p.pool_id=m.pool_id JOIN network_access_nas_bindings b ON b.id=p.nas_binding_id AND b.runtime_id=p.logical_runtime_id AND b.site_id=p.site_id WHERE m.runtime_id=? AND b.nas_id=? AND b.tenant_id='default' AND b.workspace_id='default' AND b.status='active'`, runtime, nas).Row().Scan(&id)
	if e != nil {
		return domain.NASBinding{}, e
	}
	return r.GetNASBinding(ctx, id)
}
