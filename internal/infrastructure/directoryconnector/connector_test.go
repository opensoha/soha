package directoryconnector_test

import (
	"errors"
	"testing"

	directoryconnector "github.com/opensoha/soha/internal/infrastructure/directoryconnector"
)

func TestRegistry_DeclaresUnsupportedProvidersWithoutFakeConnectors(t *testing.T) {
	t.Parallel()

	registry := directoryconnector.NewRegistry()
	for _, provider := range []string{"wecom", "dingtalk"} {
		capabilities, ok := registry.Capabilities(provider)
		if !ok {
			t.Fatalf("provider %q capability declaration missing", provider)
		}
		if capabilities.Organizations || capabilities.People || capabilities.Memberships || capabilities.Events {
			t.Fatalf("provider %q unexpectedly declares implemented capability: %#v", provider, capabilities)
		}
		_, err := registry.New(provider)
		if !errors.Is(err, directoryconnector.ErrUnsupported) {
			t.Fatalf("New(%q) error = %v, want ErrUnsupported", provider, err)
		}
	}
}
