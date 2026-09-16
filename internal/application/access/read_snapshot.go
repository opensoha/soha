package access

import (
	"context"
	"sync"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
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
		policies: sync.OnceValues(func() ([]domainaccess.Policy, error) { return source.loadPolicies(ctx) }),
		roles:    sync.OnceValues(func() (map[string][]domainaccess.Action, error) { return source.loadRoleMatrix(ctx) }),
		grants:   sync.OnceValues(func() ([]domainscopegrant.Record, error) { return source.loadScopeGrants(ctx) }),
	}
	reader := *source
	reader.repo, reader.grants = snapshot, snapshot
	return &reader
}

type readSnapshot struct {
	policies func() ([]domainaccess.Policy, error)
	roles    func() (map[string][]domainaccess.Action, error)
	grants   func() ([]domainscopegrant.Record, error)
}

func (s *readSnapshot) ListPolicies(ctx context.Context) ([]domainaccess.Policy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.policies()
}

func (s *readSnapshot) ListRoleCapabilities(ctx context.Context) (map[string][]domainaccess.Action, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.roles()
}

func (s *readSnapshot) List(ctx context.Context) ([]domainscopegrant.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.grants()
}
