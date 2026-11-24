package access

import (
	"context"
	"sync"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
)

// SnapshotForRead reuses policy data within one bounded read, while still
// evaluating every resource separately. Do not retain it across requests or writes.
func SnapshotForRead(ctx context.Context, authorizer domainaccess.Authorizer) domainaccess.Authorizer {
	source, ok := authorizer.(*Service)
	if !ok || source == nil {
		return authorizer
	}
	snapshot := &readSnapshot{
		policies: snapshotLoader(ctx, source.loadPolicies),
		roles:    snapshotLoader(ctx, source.loadRoleMatrix),
		grants:   snapshotLoader(ctx, source.loadScopeGrants),
	}
	reader := *source
	reader.repo, reader.grants = snapshot, snapshot
	if source.catalog != nil {
		snapshot.environments = snapshotLoader(ctx, source.catalog.ListEnvironments)
		snapshot.bindings = snapshotLoader(ctx, source.catalog.ListApplicationEnvironments)
		reader.catalog = snapshot
	}
	if source.permissions != nil && source.permissions.roles != nil {
		snapshot.permissions = snapshotLoader(ctx, source.permissions.roles.ListRolePermissions)
		reader.permissions = &PermissionResolver{roles: snapshot, requestSnapshot: true}
	}
	return &reader
}

type readSnapshot struct {
	policies     func(context.Context) ([]domainaccess.Policy, error)
	roles        func(context.Context) (map[string][]domainaccess.Action, error)
	grants       func(context.Context) ([]domainscopegrant.Record, error)
	environments func(context.Context) ([]domaincatalog.Environment, error)
	bindings     func(context.Context) ([]domaincatalog.ApplicationEnvironment, error)
	permissions  func(context.Context) (map[string][]string, error)
}

func (s *readSnapshot) ListEnvironments(ctx context.Context) ([]domaincatalog.Environment, error) {
	return s.environments(ctx)
}

func (s *readSnapshot) ListApplicationEnvironments(ctx context.Context) ([]domaincatalog.ApplicationEnvironment, error) {
	return s.bindings(ctx)
}

func (s *readSnapshot) ListRolePermissions(ctx context.Context) (map[string][]string, error) {
	return s.permissions(ctx)
}

func (s *readSnapshot) ListPolicies(ctx context.Context) ([]domainaccess.Policy, error) {
	return s.policies(ctx)
}

func (s *readSnapshot) ListRoleCapabilities(ctx context.Context) (map[string][]domainaccess.Action, error) {
	return s.roles(ctx)
}

func (s *readSnapshot) List(ctx context.Context) ([]domainscopegrant.Record, error) {
	return s.grants(ctx)
}

// A shared load uses the request context. Each observer may stop waiting at
// its own deadline without canceling the load for the other observers.
func snapshotLoader[T any](request context.Context, load func(context.Context) (T, error)) func(context.Context) (T, error) {
	var once sync.Once
	var item T
	var loadErr error
	ready := make(chan struct{})
	return func(ctx context.Context) (T, error) {
		var zero T
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		once.Do(func() {
			go func() {
				item, loadErr = load(request)
				close(ready)
			}()
		})
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-ready:
			if err := ctx.Err(); err != nil {
				return zero, err
			}
			return item, loadErr
		}
	}
}
