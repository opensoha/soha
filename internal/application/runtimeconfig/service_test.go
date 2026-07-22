package runtimeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainruntimeconfig "github.com/opensoha/soha/internal/domain/runtimeconfig"
)

type memoryStore struct {
	mu           sync.Mutex
	state        domainruntimeconfig.State
	revisions    []domainruntimeconfig.Revision
	applications map[string]domainruntimeconfig.Application
}

func (s *memoryStore) LoadState(context.Context) (domainruntimeconfig.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneState(s.state), nil
}

func (s *memoryStore) Commit(_ context.Context, input domainruntimeconfig.Commit) (domainruntimeconfig.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.ExpectedVersion != s.state.Version {
		return domainruntimeconfig.State{}, domainruntimeconfig.ErrVersionConflict
	}
	s.state = domainruntimeconfig.State{Version: input.Revision.Version, ActiveRevisionID: input.Revision.ID, Overrides: cloneValues(input.Revision.Snapshot)}
	s.revisions = append(s.revisions, input.Revision)
	if s.applications == nil {
		s.applications = map[string]domainruntimeconfig.Application{}
	}
	s.applications[input.Application.ID] = input.Application
	return cloneState(s.state), nil
}

func (s *memoryStore) UpdateApplication(_ context.Context, item domainruntimeconfig.Application) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applications[item.ID] = item
	for i := range s.revisions {
		if s.revisions[i].ID == item.RevisionID {
			s.revisions[i].Status = item.Status
		}
	}
	return nil
}

func (s *memoryStore) ListRevisions(_ context.Context, limit int) ([]domainruntimeconfig.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := append([]domainruntimeconfig.Revision(nil), s.revisions...)
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *memoryStore) GetRevisionByVersion(_ context.Context, version int64) (domainruntimeconfig.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.revisions {
		if item.Version == version {
			return item, nil
		}
	}
	return domainruntimeconfig.Revision{}, domainruntimeconfig.ErrNotFound
}

func (s *memoryStore) GetApplication(_ context.Context, id string) (domainruntimeconfig.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.applications[id]
	if !ok {
		return domainruntimeconfig.Application{}, domainruntimeconfig.ErrNotFound
	}
	return item, nil
}

func cloneState(state domainruntimeconfig.State) domainruntimeconfig.State {
	state.Overrides = cloneValues(state.Overrides)
	return state
}

type runtimePermissions struct{}

func (runtimePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"admin": {appaccess.PermSettingsRuntimeConfigView, appaccess.PermSettingsRuntimeConfigManage}}, nil
}

type captureAudit struct{ entries []domainaudit.Entry }

func (a *captureAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	a.entries = append(a.entries, entry)
	return nil
}

type captureApplier struct {
	key    string
	called chan struct{}
}

type failingApplier struct{ key string }

func (a failingApplier) Handles(key string) bool { return key == a.key }
func (a failingApplier) Apply(_ context.Context, _, _ Snapshot, keys []string) ([]sohaapi.RuntimeConfigAppliedItem, error) {
	items := make([]sohaapi.RuntimeConfigAppliedItem, 0, len(keys))
	for _, key := range keys {
		items = append(items, sohaapi.RuntimeConfigAppliedItem{Key: key, ApplyMode: sohaapi.RuntimeConfigApplyModeHot, Status: sohaapi.RuntimeConfigApplicationStatusFailed})
	}
	return items, errors.New("apply failed")
}

func (a *captureApplier) Handles(key string) bool { return key == a.key }
func (a *captureApplier) Apply(_ context.Context, _, _ Snapshot, keys []string) ([]sohaapi.RuntimeConfigAppliedItem, error) {
	select {
	case a.called <- struct{}{}:
	default:
	}
	items := make([]sohaapi.RuntimeConfigAppliedItem, 0, len(keys))
	for _, key := range keys {
		items = append(items, sohaapi.RuntimeConfigAppliedItem{Key: key, ApplyMode: sohaapi.RuntimeConfigApplyModeHot, Status: sohaapi.RuntimeConfigApplicationStatusApplied})
	}
	return items, nil
}

func newTestService(t *testing.T, store *memoryStore, audit *captureAudit) *Service {
	t.Helper()
	service, err := New(context.Background(), store, NewRegistry(RegistryOptions{ModuleAI: true}), appaccess.NewPermissionResolver(runtimePermissions{}), audit)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return service
}

func adminPrincipal() domainidentity.Principal {
	return domainidentity.Principal{UserID: "admin-1", UserName: "Admin", Roles: []string{"admin"}}
}

func TestComputeRuntimeDefinitionsUseProductTerminology(t *testing.T) {
	registry := NewRegistry(RegistryOptions{})
	tests := []struct {
		key   string
		label string
	}{
		{key: KeyModuleVirtualization, label: "虚拟化资源"},
		{key: KeyModuleDocker, label: "容器运行时"},
	}
	for _, test := range tests {
		definition, ok := registry.Definition(test.key)
		if !ok {
			t.Fatalf("definition %q missing", test.key)
		}
		if definition.Category != "计算资源" || definition.Label != test.label {
			t.Fatalf("definition %q = category %q, label %q", test.key, definition.Category, definition.Label)
		}
	}
}

func TestRegistryDoesNotExposeClusterPrometheusSettings(t *testing.T) {
	registry := NewRegistry(RegistryOptions{})
	for _, key := range []string{"monitoring.prometheus.base_url", "monitoring.prometheus.bearer_token"} {
		if _, ok := registry.Definition(key); ok {
			t.Fatalf("cluster Prometheus setting %q must not be a global runtime configuration", key)
		}
	}
	for _, definition := range registry.Definitions() {
		if strings.HasPrefix(definition.Key, "monitoring.prometheus.") {
			t.Fatalf("registry unexpectedly contains cluster Prometheus setting %q", definition.Key)
		}
	}
}

func TestAIRuntimeDefinitionsDescribeHierarchyAndGatewayIndependence(t *testing.T) {
	registry := NewRegistry(RegistryOptions{})
	for _, key := range []string{KeyModuleAI, KeyAssistantGlobal, KeyModuleAIGateway} {
		definition, ok := registry.Definition(key)
		if !ok {
			t.Fatalf("definition %q missing", key)
		}
		if definition.Category != "模块" || definition.Description == "" {
			t.Fatalf("definition %q = category %q, description %q", key, definition.Category, definition.Description)
		}
	}
}

func TestValidateRejectsEnabledAssistantWhenAIWorkbenchIsDisabled(t *testing.T) {
	service, err := New(
		context.Background(),
		&memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{}}},
		NewRegistry(RegistryOptions{}),
		appaccess.NewPermissionResolver(runtimePermissions{}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	result := service.validate(sohaapi.RuntimeConfigChangeRequest{
		ExpectedVersion: 0,
		Changes:         []sohaapi.RuntimeConfigChange{{Key: KeyAssistantGlobal, Value: true}},
	})
	if result.Valid || len(result.Issues) != 1 || result.Issues[0].Code != "dependency_required" {
		t.Fatalf("validation result = %#v", result)
	}
}

func TestValidateAllowsDisablingAIWorkbenchAndAssistantTogether(t *testing.T) {
	service := newTestService(t, &memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{KeyAssistantGlobal: true}}}, nil)
	result := service.validate(sohaapi.RuntimeConfigChangeRequest{
		ExpectedVersion: 0,
		Changes: []sohaapi.RuntimeConfigChange{
			{Key: KeyModuleAI, Value: false},
			{Key: KeyAssistantGlobal, Value: false},
		},
	})
	if !result.Valid {
		t.Fatalf("validation result = %#v", result)
	}
}

func TestValidateAllowsAIGatewayWithoutAIWorkbench(t *testing.T) {
	service, err := New(
		context.Background(),
		&memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{}}},
		NewRegistry(RegistryOptions{}),
		appaccess.NewPermissionResolver(runtimePermissions{}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	result := service.validate(sohaapi.RuntimeConfigChangeRequest{
		ExpectedVersion: 0,
		Changes:         []sohaapi.RuntimeConfigChange{{Key: KeyModuleAIGateway, Value: true}},
	})
	if !result.Valid {
		t.Fatalf("validation result = %#v", result)
	}
}

func TestApplyUsesCASAndAuditContainsNoValues(t *testing.T) {
	store := &memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{}}}
	audit := &captureAudit{}
	service := newTestService(t, store, audit)
	request := sohaapi.RuntimeConfigChangeRequest{ExpectedVersion: 0, Reason: "enable assistant", Changes: []sohaapi.RuntimeConfigChange{{Key: KeyAssistantGlobal, Value: true}}}

	result, err := service.Apply(context.Background(), adminPrincipal(), request)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if result.Revision.Version != 1 || !service.FeatureEnabled("ai", "assistant.global") {
		t.Fatalf("unexpected applied state: %#v", result)
	}
	if _, err := service.Apply(context.Background(), adminPrincipal(), request); !errors.Is(err, context.Canceled) && !strings.Contains(errorString(err), "version") {
		t.Fatalf("second apply should conflict, got %v", err)
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(audit.entries))
	}
	raw, _ := json.Marshal(audit.entries[0].Metadata)
	if strings.Contains(string(raw), "enable assistant") || strings.Contains(string(raw), "true") {
		t.Fatalf("audit metadata contains configuration values: %s", raw)
	}
}

func TestRollbackCreatesNewRevision(t *testing.T) {
	store := &memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{}}}
	service := newTestService(t, store, &captureAudit{})
	first, err := service.Apply(context.Background(), adminPrincipal(), sohaapi.RuntimeConfigChangeRequest{ExpectedVersion: 0, Changes: []sohaapi.RuntimeConfigChange{{Key: KeyAssistantGlobal, Value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Apply(context.Background(), adminPrincipal(), sohaapi.RuntimeConfigChangeRequest{ExpectedVersion: 1, Changes: []sohaapi.RuntimeConfigChange{{Key: KeyAssistantGlobal, Value: false}}})
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := service.Rollback(context.Background(), adminPrincipal(), sohaapi.RuntimeConfigRollbackRequest{ExpectedVersion: 2, TargetVersion: 1})
	if err != nil {
		t.Fatalf("Rollback returned error: %v", err)
	}
	if rolledBack.Revision.Version != 3 || rolledBack.Revision.RollbackOfRevisionID != first.Revision.ID || !service.FeatureEnabled("ai", "assistant.global") {
		t.Fatalf("unexpected rollback: %#v", rolledBack)
	}
}

func TestPollingReconcilesRemoteRevisionAndRunsHook(t *testing.T) {
	store := &memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{}}}
	service := newTestService(t, store, &captureAudit{})
	applier := &captureApplier{key: KeyAssistantGlobal, called: make(chan struct{}, 1)}
	service.RegisterApplier(applier)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx, 5*time.Millisecond)

	store.mu.Lock()
	store.state = domainruntimeconfig.State{Version: 1, ActiveRevisionID: "remote-1", Overrides: map[string]any{KeyAssistantGlobal: true}}
	store.mu.Unlock()
	select {
	case <-applier.called:
	case <-time.After(time.Second):
		t.Fatal("remote revision was not reconciled")
	}
	if !service.FeatureEnabled("ai", "assistant.global") {
		t.Fatal("remote feature state was not published")
	}
}

func TestApplyFailureKeepsPreviousEffectiveValue(t *testing.T) {
	store := &memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{}}}
	service := newTestService(t, store, &captureAudit{})
	service.RegisterApplier(failingApplier{key: KeyAssistantGlobal})

	_, err := service.Apply(context.Background(), adminPrincipal(), sohaapi.RuntimeConfigChangeRequest{
		ExpectedVersion: 0,
		Changes:         []sohaapi.RuntimeConfigChange{{Key: KeyAssistantGlobal, Value: true}},
	})
	if err == nil {
		t.Fatal("Apply() expected applier error")
	}
	if service.FeatureEnabled("ai", "assistant.global") {
		t.Fatal("failed desired value was published as effective")
	}
	if service.Current().Version != 1 {
		t.Fatalf("current version = %d, want committed desired version 1", service.Current().Version)
	}
	validation := service.validate(sohaapi.RuntimeConfigChangeRequest{
		ExpectedVersion: 1,
		Changes:         []sohaapi.RuntimeConfigChange{{Key: KeyAssistantGlobal, Value: false}},
	})
	if !validation.Valid {
		t.Fatalf("validation should use committed desired version: %#v", validation)
	}
}

func TestRestartOnlyValueBecomesEffectiveOnlyAfterRestart(t *testing.T) {
	store := &memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{}}}
	service := newTestService(t, store, &captureAudit{})

	result, err := service.Apply(context.Background(), adminPrincipal(), sohaapi.RuntimeConfigChangeRequest{
		ExpectedVersion: 0,
		Changes:         []sohaapi.RuntimeConfigChange{{Key: KeyModuleSecurity, Value: true}},
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if result.Application.Status != sohaapi.RuntimeConfigApplicationStatusRestartRequired || service.ModuleEnabled("security") {
		t.Fatalf("restart-only change became effective before restart: %#v", result)
	}
	snapshot, err := service.Get(context.Background(), adminPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.PendingRestart {
		t.Fatal("snapshot should report a pending restart")
	}

	restarted := newTestService(t, store, &captureAudit{})
	if !restarted.ModuleEnabled("security") {
		t.Fatal("restart-only value was not loaded after restart")
	}
	restartedSnapshot, err := restarted.Get(context.Background(), adminPrincipal())
	if err != nil {
		t.Fatal(err)
	}
	if restartedSnapshot.PendingRestart {
		t.Fatal("pending restart was not cleared after restart")
	}
}

func TestRegistryPreservesDisabledSecurityAndCMDBBaselines(t *testing.T) {
	registry := NewRegistry(RegistryOptions{ModuleSecurity: false, ModuleCMDB: false})
	service, err := New(context.Background(), &memoryStore{state: domainruntimeconfig.State{Overrides: map[string]any{}}}, registry, appaccess.NewPermissionResolver(runtimePermissions{}), nil)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	for _, moduleID := range []string{"security", "cmdb"} {
		if service.ModuleEnabled(moduleID) {
			t.Fatalf("%s module ignored disabled config baseline", moduleID)
		}
	}
	for _, key := range []string{KeyModuleSecurity, KeyModuleCMDB} {
		definition, ok := registry.Definition(key)
		if !ok {
			t.Fatalf("missing registry definition %s", key)
		}
		if definition.ApplyMode != sohaapi.RuntimeConfigApplyModeRestart {
			t.Fatalf("%s apply mode = %s, want restart", key, definition.ApplyMode)
		}
		if definition.Description == "" {
			t.Fatalf("%s should explain why restart is required", key)
		}
	}
}

func TestDeliveryModuleUsesRuntimeGateWithoutRestart(t *testing.T) {
	definition, ok := NewRegistry(RegistryOptions{}).Definition(KeyModuleDelivery)
	if !ok {
		t.Fatal("delivery module definition missing")
	}
	if definition.ApplyMode != sohaapi.RuntimeConfigApplyModeHot {
		t.Fatalf("delivery apply mode = %s, want hot", definition.ApplyMode)
	}
}

func TestHomeModuleUsesHotRuntimeGate(t *testing.T) {
	registry := NewRegistry(RegistryOptions{ModuleHome: true})
	definition, ok := registry.Definition(KeyModuleHome)
	if !ok {
		t.Fatal("home module definition missing")
	}
	if definition.ApplyMode != sohaapi.RuntimeConfigApplyModeHot || definition.Label != "首页" {
		t.Fatalf("home definition = %#v", definition)
	}
	if !(Snapshot{registry: registry}).ModuleEnabled("home") {
		t.Fatal("home module must use its enabled baseline")
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
