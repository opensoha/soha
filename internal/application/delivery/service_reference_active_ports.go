package delivery

import (
	"context"
	"fmt"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"reflect"
)

type ServiceReferenceScopeRepository interface {
	ListSourceServiceReferences(context.Context, string, string) ([]domaindelivery.ServiceReferenceRecord, error)
	WithServiceReferenceScope(context.Context, string, func(context.Context) error) error
}

type ServiceReferenceActiveRepository interface {
	GetServiceReference(context.Context, string, string) (domaindelivery.ServiceReferenceRecord, error)
	ListServiceReferences(context.Context, string, string, string, int) (domaindelivery.ServiceReferencePage, error)
	SaveServiceReference(context.Context, domaindelivery.ServiceReferenceWrite, func(context.Context) (domaindelivery.ServiceReferenceConfiguration, error)) (domaindelivery.ServiceReferenceRecord, error)
	DeleteServiceReference(context.Context, string, string, domaindelivery.ServiceReferenceDeleteInput, func(context.Context) error) error
}

func (s *Service) referenceActiveStore() (ServiceReferenceActiveRepository, error) {
	store, ok := s.repository.(ServiceReferenceActiveRepository)
	if !ok || (reflect.ValueOf(store).Kind() == reflect.Pointer && reflect.ValueOf(store).IsNil()) {
		return nil, fmt.Errorf("%w: active reference persistence is unavailable", apperrors.ErrUnsupportedOperation)
	}
	return store, nil
}
