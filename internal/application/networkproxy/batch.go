package networkproxy

import (
	"context"
	appaccess "github.com/opensoha/soha/internal/application/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"slices"
	"time"
)

type DesiredBatchStore interface {
	CommitDesiredBatch(context.Context, domain.DesiredBatch, time.Time) error
	ObserveMemberConfigurations(context.Context, []string) ([]domain.MemberConfigurationState, error)
}

func (s *Management) CommitDesiredBatch(ctx context.Context, p identity.Principal, batch domain.DesiredBatch) error {
	batch.Members = slices.Clone(batch.Members)
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, appaccess.PermNetworkAccessProxyInstancesUpdate); err != nil {
		return err
	}
	if !identifier.MatchString(batch.PublicationID) || !identifier.MatchString(batch.MasterID) || !hashPattern.MatchString(batch.MemberSetHash) || batch.OwnerEpoch < 0 || len(batch.Members) == 0 || len(batch.Members) > 128 {
		return invalid("invalid desired batch")
	}
	if err := s.AuthorizePublication(ctx, p, batch.MasterID, "update"); err != nil {
		return err
	}
	seen := map[string]bool{}
	// Authorize every member before encrypting or writing any member.
	for _, m := range batch.Members {
		if seen[m.InstanceID] {
			return invalid("duplicate desired member")
		}
		seen[m.InstanceID] = true
		if err := s.authorizeDesiredMember(ctx, p, m); err != nil {
			return err
		}
	}
	for i := range batch.Members {
		if err := s.encryptDesiredMember(ctx, p, &batch.Members[i]); err != nil {
			return err
		}
	}

	slices.SortFunc(batch.Members, func(a, b domain.DesiredMember) int {
		if a.InstanceID < b.InstanceID {
			return -1
		}
		if a.InstanceID > b.InstanceID {
			return 1
		}
		return 0
	})
	store, ok := s.store.(DesiredBatchStore)
	if !ok {
		return apperrors.ErrServiceUnavailable
	}
	if err := store.CommitDesiredBatch(ctx, batch, s.now().UTC()); err != nil {
		return err
	}
	for _, m := range batch.Members {
		s.record(ctx, p, "network_access.proxy_instances.update", m.InstanceID)
	}
	return nil
}
func (s *Management) ObserveMemberConfigurations(ctx context.Context, ids []string) ([]domain.MemberConfigurationState, error) {
	if len(ids) > 128 {
		return nil, invalid("too many members")
	}
	for _, id := range ids {
		if !identifier.MatchString(id) {
			return nil, invalid("invalid member")
		}
	}
	store, ok := s.store.(DesiredBatchStore)
	if !ok {
		return nil, apperrors.ErrServiceUnavailable
	}
	return store.ObserveMemberConfigurations(ctx, ids)
}

func (s *Management) AuthorizePublication(ctx context.Context, p identity.Principal, id, action string) error {
	perm := appaccess.PermNetworkAccessProxyInstancesUpdate
	if action == "view" {
		perm = appaccess.PermNetworkAccessProxyInstancesView
	} else if action != "update" {
		return invalid("invalid publication action")
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, perm); err != nil {
		return err
	}
	if !identifier.MatchString(id) {
		return invalid("invalid instance")
	}
	item, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	return s.authorizeScope(ctx, p, perm, item)
}

func (s *Management) GetDesiredBatchReceipt(ctx context.Context, id string) (domain.DesiredBatchReceipt, error) {
	if !identifier.MatchString(id) {
		return domain.DesiredBatchReceipt{}, invalid("invalid publication")
	}
	store, ok := s.store.(interface {
		GetDesiredBatchReceipt(context.Context, string) (domain.DesiredBatchReceipt, error)
	})
	if !ok {
		return domain.DesiredBatchReceipt{}, apperrors.ErrServiceUnavailable
	}
	return store.GetDesiredBatchReceipt(ctx, id)
}

func (s *Management) authorizeDesiredMember(ctx context.Context, p identity.Principal, m domain.DesiredMember) error {
	input := domain.ConfigurationInput{ExpectedRevision: m.ExpectedRevision, Enabled: m.Enabled, Content: m.Content, Binding: m.Binding}
	if err := validateConfigurationInput(m.InstanceID, input); err != nil {
		return err
	}
	item, err := s.store.Get(ctx, m.InstanceID)
	if err != nil {
		return err
	}
	if err = s.authorizeScope(ctx, p, appaccess.PermNetworkAccessProxyInstancesUpdate, item); err != nil {
		return err
	}
	if _, err = s.configurationBinding(ctx, p, input, item); err != nil {
		return err
	}
	return nil
}

func (s *Management) encryptDesiredMember(ctx context.Context, p identity.Principal, m *domain.DesiredMember) error {
	item, err := s.store.Get(ctx, m.InstanceID)
	if err != nil {
		return err
	}
	input := domain.ConfigurationInput{ExpectedRevision: m.ExpectedRevision, Enabled: m.Enabled, Content: m.Content, Binding: m.Binding}
	m.Binding, err = s.configurationBinding(ctx, p, input, item)
	if err != nil {
		return err
	}
	m.Encrypted, m.ContentHash, err = s.configurationContent(input, item)
	if err != nil {
		return err
	}
	m.Content = nil
	return nil
}
