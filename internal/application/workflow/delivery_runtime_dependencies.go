package workflow

import (
	"fmt"
	"reflect"
)

// ValidateDeliveryRuntime checks the durable batch consumers before polling is
// started. Capability-only workflow instances do not use this bootstrap gate.
func (s *Service) ValidateDeliveryRuntime() error {
	if s == nil {
		return fmt.Errorf("delivery workflow service is required")
	}
	for _, dependency := range []struct {
		name  string
		value any
	}{
		{"repository", s.repo}, {"runtime", s.deliveryRuntime}, {"principals", s.deliveryPrincipals},
	} {
		if dependency.value == nil || reflect.ValueOf(dependency.value).Kind() == reflect.Pointer && reflect.ValueOf(dependency.value).IsNil() {
			return fmt.Errorf("delivery workflow requires %s", dependency.name)
		}
	}
	_, err := s.deliveryRepository()
	return err
}
