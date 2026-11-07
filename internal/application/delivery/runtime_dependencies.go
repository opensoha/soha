package delivery

import (
	"fmt"
	"reflect"
)

// ValidateRuntimeDependencies is called after the composition root binds the
// mutually dependent delivery owners, before any delivery worker can claim work.
func (s *Service) ValidateRuntimeDependencies() error {
	if s == nil {
		return fmt.Errorf("delivery service is required")
	}
	for _, dependency := range []struct {
		name  string
		value any
	}{
		{"applications", s.applications}, {"catalog", s.catalog}, {"builds", s.builds},
		{"workflows", s.workflows}, {"releases", s.releases}, {"repository", s.repository},
		{"execution", s.execution}, {"targets", s.targets}, {"permissions", s.permissions},
		{"execution principals", s.executionPrincipals}, {"Manifest packages", s.manifestPackages},
		{"Manifest reference configuration", s.referenceManifests},
		{"Manifest delivery", s.manifestDelivery}, {"Docker delivery", s.docker},
		{"Helm charts", s.helm.Charts}, {"Helm runtime", s.helm.Runtime},
		{"Helm secrets", s.helm.Secrets}, {"template updates", s.templateUpdates},
	} {
		if dependency.value == nil || reflect.ValueOf(dependency.value).Kind() == reflect.Pointer && reflect.ValueOf(dependency.value).IsNil() {
			return fmt.Errorf("delivery runtime requires %s", dependency.name)
		}
	}
	return nil
}
