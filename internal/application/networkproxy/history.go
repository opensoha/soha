package networkproxy

import (
	"context"
	"fmt"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type configurationHistoryStore interface {
	ConfigurationHistory(context.Context, string) ([]domain.ConfigurationRevision, error)
	ConfigurationRevision(context.Context, string, int64) (domain.ConfigurationRevision, error)
}

func (s *Management) ConfigurationHistory(ctx context.Context, p domainidentity.Principal, id string) ([]domain.ConfigurationRevision, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return nil, err
	}
	store, ok := s.store.(configurationHistoryStore)
	if !ok {
		return nil, apperrors.ErrServiceUnavailable
	}
	return store.ConfigurationHistory(ctx, id)
}
func (s *Management) RestoreConfiguration(ctx context.Context, p domainidentity.Principal, id string, revision, expected int64) (domain.Instance, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, appaccess.PermNetworkAccessProxyInstancesUpdate); err != nil {
		return domain.Instance{}, err
	}
	if !identifier.MatchString(id) || revision < 1 || expected < 0 {
		return domain.Instance{}, invalid("invalid proxy restore revision")
	}
	current, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.Instance{}, err
	}
	if err := s.authorizeScope(ctx, p, appaccess.PermNetworkAccessProxyInstancesUpdate, current); err != nil {
		return domain.Instance{}, err
	}
	if current.DesiredRevision != expected {
		return domain.Instance{}, apperrors.ErrConflict
	}
	store, ok := s.store.(configurationHistoryStore)
	if !ok {
		return domain.Instance{}, apperrors.ErrServiceUnavailable
	}
	saved, err := store.ConfigurationRevision(ctx, id, revision)
	if err != nil {
		return domain.Instance{}, err
	}
	content, err := secretcrypto.DecryptStringWithKeyring(s.keys, saved.Encrypted)
	if err != nil {
		return domain.Instance{}, fmt.Errorf("decrypt proxy source revision: %w", err)
	}
	// Re-authorize the current binding. Historical policy, lease, enabled state and
	// rendered runtime files are deliberately not restoration inputs.
	return s.UpdateConfiguration(ctx, p, id, domain.ConfigurationInput{ExpectedRevision: expected, Enabled: current.Enabled, Content: &content, Binding: current.Binding})
}
