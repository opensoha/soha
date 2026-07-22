package bootstrap

import (
	"context"
	"sync"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appruntimeconfig "github.com/opensoha/soha/internal/application/runtimeconfig"
	domainruntimeconfig "github.com/opensoha/soha/internal/domain/runtimeconfig"
)

type lifecycleTestStore struct{}

func (lifecycleTestStore) LoadState(context.Context) (domainruntimeconfig.State, error) {
	return domainruntimeconfig.State{Overrides: map[string]any{}}, nil
}
func (lifecycleTestStore) Commit(context.Context, domainruntimeconfig.Commit) (domainruntimeconfig.State, error) {
	return domainruntimeconfig.State{}, nil
}
func (lifecycleTestStore) UpdateApplication(context.Context, domainruntimeconfig.Application) error {
	return nil
}
func (lifecycleTestStore) ListRevisions(context.Context, int) ([]domainruntimeconfig.Revision, error) {
	return nil, nil
}
func (lifecycleTestStore) GetRevisionByVersion(context.Context, int64) (domainruntimeconfig.Revision, error) {
	return domainruntimeconfig.Revision{}, domainruntimeconfig.ErrNotFound
}
func (lifecycleTestStore) GetApplication(context.Context, string) (domainruntimeconfig.Application, error) {
	return domainruntimeconfig.Application{}, domainruntimeconfig.ErrNotFound
}

type fakeRestartableModule struct {
	mu      sync.Mutex
	running bool
	starts  int
	stops   int
}

func (s *fakeRestartableModule) Start(context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		s.starts++
		s.running = true
	}
}
func (s *fakeRestartableModule) Stop(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		s.stops++
	}
	s.running = false
	return nil
}
func (s *fakeRestartableModule) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func runtimeSnapshot(t *testing.T, enabled bool) appruntimeconfig.Snapshot {
	t.Helper()
	service, err := appruntimeconfig.New(context.Background(), lifecycleTestStore{}, appruntimeconfig.NewRegistry(appruntimeconfig.RegistryOptions{ModuleAI: enabled}), appaccess.NewPermissionResolver(nil), nil)
	if err != nil {
		t.Fatalf("build runtime config service: %v", err)
	}
	return service.Current()
}

func TestModuleLifecycleApplierSupportsStartStopStart(t *testing.T) {
	service := &fakeRestartableModule{}
	applier := newModuleLifecycleApplier(context.Background(), map[string]restartableModule{
		appruntimeconfig.KeyModuleAI: service,
	})
	disabled, enabled := runtimeSnapshot(t, false), runtimeSnapshot(t, true)

	for _, transition := range [][2]appruntimeconfig.Snapshot{{disabled, enabled}, {enabled, disabled}, {disabled, enabled}} {
		items, err := applier.Apply(context.Background(), transition[0], transition[1], []string{appruntimeconfig.KeyModuleAI})
		if err != nil {
			t.Fatalf("Apply returned error: %v", err)
		}
		if len(items) != 1 || items[0].Status != sohaapi.RuntimeConfigApplicationStatusApplied {
			t.Fatalf("unexpected application items: %#v", items)
		}
	}
	if service.starts != 2 || service.stops != 1 || !service.Running() {
		t.Fatalf("lifecycle counts = starts %d stops %d running %v", service.starts, service.stops, service.Running())
	}
}
