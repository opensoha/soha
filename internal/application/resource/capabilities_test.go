package resource

import (
	"reflect"
	"slices"
	"testing"
)

func TestCapabilityFacadesRestrictRootServiceSurface(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		facade  any
		exposes string
		hides   string
	}{
		{name: "workloads", facade: &Workloads{}, exposes: "ListPods", hides: "ListSecrets"},
		{name: "configuration", facade: &Configuration{}, exposes: "ListSecrets", hides: "ListPods"},
		{name: "network", facade: &Network{}, exposes: "ListServices", hides: "ListPods"},
		{name: "storage", facade: &Storage{}, exposes: "ListStorageClasses", hides: "ListPods"},
		{name: "rbac", facade: &RBAC{}, exposes: "ListRoles", hides: "ListPods"},
		{name: "helm", facade: &Helm{}, exposes: "ListHelmReleases", hides: "ListPods"},
		{name: "inventory", facade: &Inventory{}, exposes: "ListNodes", hides: "ListPods"},
		{name: "custom resources", facade: &CustomResources{}, exposes: "ListCRDs", hides: "ListPods"},
		{name: "generic resources", facade: &GenericResources{}, exposes: "GetResourceYAML", hides: "ListPods"},
		{name: "events", facade: &Events{}, exposes: "ListClusterEvents", hides: "ListPods"},
		{name: "port forwards", facade: &PortForwards{}, exposes: "ListPortForwards", hides: "ListPods"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			facadeType := reflect.TypeOf(tt.facade)
			if _, ok := facadeType.MethodByName(tt.exposes); !ok {
				t.Fatalf("facade does not expose %s", tt.exposes)
			}
			if _, ok := facadeType.MethodByName(tt.hides); ok {
				t.Fatalf("facade unexpectedly exposes %s", tt.hides)
			}
		})
	}
}

func TestServiceCapabilityAccessorsReturnRestrictedFacades(t *testing.T) {
	t.Parallel()
	service := New(Dependencies{})
	if service.Workloads() == nil || service.Configuration() == nil || service.Network() == nil {
		t.Fatal("capability accessor returned nil")
	}
	if service.Storage() == nil || service.RBAC() == nil || service.Helm() == nil {
		t.Fatal("capability accessor returned nil")
	}
	if service.Inventory() == nil || service.CustomResources() == nil || service.GenericResources() == nil {
		t.Fatal("capability accessor returned nil")
	}
	if service.Events() == nil || service.PortForwards() == nil || service.Runtime() == nil {
		t.Fatal("capability accessor returned nil")
	}
	workloads := service.Workloads()
	network := service.Network()
	if workloads != service.Workloads() || network != service.Network() {
		t.Fatal("capability accessors must return stable long-lived instances")
	}
}

func TestCapabilitiesDoNotRetainRootService(t *testing.T) {
	t.Parallel()

	rootType := reflect.TypeOf((*Service)(nil))
	capabilities := []any{
		Workloads{}, Configuration{}, Network{}, Storage{}, RBAC{}, Helm{},
		Inventory{}, CustomResources{}, GenericResources{}, Events{}, PortForwards{},
	}
	for _, capability := range capabilities {
		capabilityType := reflect.TypeOf(capability)
		for index := 0; index < capabilityType.NumField(); index++ {
			if capabilityType.Field(index).Type == rootType {
				t.Errorf("%s retains *Service through field %s", capabilityType.Name(), capabilityType.Field(index).Name)
			}
		}
	}
}

func TestRootServiceDoesNotExposeMigratedCapabilityMethods(t *testing.T) {
	t.Parallel()
	rootType := reflect.TypeOf(&Service{})
	forbidden := []string{
		"ListPods",
		"ListConfigMaps",
		"ListServices",
		"ListStorageClasses",
		"ListRoles",
		"ListHelmReleases",
		"ListNodes",
		"ListCRDs",
		"GetResourceYAML",
		"ListClusterEvents",
		"ListPortForwards",
	}
	for _, method := range forbidden {
		if _, ok := rootType.MethodByName(method); ok {
			t.Errorf("root service unexpectedly exposes %s", method)
		}
	}
}

func TestRootServiceExportsOnlyCapabilityAccessors(t *testing.T) {
	t.Parallel()
	rootType := reflect.TypeOf(&Service{})
	got := make([]string, 0, rootType.NumMethod())
	for index := 0; index < rootType.NumMethod(); index++ {
		got = append(got, rootType.Method(index).Name)
	}
	want := []string{
		"Configuration",
		"CustomResources",
		"Events",
		"GenericResources",
		"Helm",
		"Inventory",
		"Network",
		"PortForwards",
		"RBAC",
		"ResourceCreation",
		"Runtime",
		"Storage",
		"Workloads",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("root Service methods = %v, want %v", got, want)
	}
}
