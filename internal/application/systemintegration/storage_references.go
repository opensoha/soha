package systemintegration

import (
	"context"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
)

func (s *Service) StorageDriverReferenceCount(ctx context.Context, driverID string) (int, error) {
	rows, err := s.repo.List(ctx, domain.Filter{Category: domain.CategoryStorage, ProviderType: driverID})
	return len(rows), err
}

// WithStorageDriverReferences serializes runtime changes against integration writes
// on the single execution host. Disabled connections still refer to the driver.
func (s *Service) WithStorageDriverReferences(ctx context.Context, driverID string, apply func([][]sohaapi.SystemIntegrationConfigurationField) error) error {
	if driverID == "" {
		return apply(nil)
	}
	s.storageMutation.Lock()
	defer s.storageMutation.Unlock()
	rows, err := s.repo.List(ctx, domain.Filter{Category: domain.CategoryStorage, ProviderType: driverID})
	if err != nil {
		return err
	}
	fields := make([][]sohaapi.SystemIntegrationConfigurationField, 0, len(rows))
	for _, row := range rows {
		fields = append(fields, row.Configuration)
	}
	return apply(fields)
}
