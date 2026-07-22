package runtimeconfig

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainruntimeconfig "github.com/opensoha/soha/internal/domain/runtimeconfig"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const defaultPollInterval = 3 * time.Second

type Service struct {
	store       Store
	registry    *Registry
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	snapshot    snapshotPointer
	desired     snapshotPointer
	applyMu     sync.Mutex
	appliersMu  sync.RWMutex
	appliers    []Applier
}

func New(ctx context.Context, store Store, registry *Registry, permissions *appaccess.PermissionResolver, audit AuditRecorder) (*Service, error) {
	if store == nil || registry == nil || permissions == nil {
		return nil, fmt.Errorf("%w: runtime config dependencies are required", apperrors.ErrInvalidArgument)
	}
	state, err := store.LoadState(ctx)
	if err != nil {
		return nil, err
	}
	service := &Service{store: store, registry: registry, permissions: permissions, audit: audit}
	initial := snapshotFromState(registry, state)
	service.snapshot.Store(initial)
	service.desired.Store(initial)
	return service, nil
}

func (s *Service) RegisterApplier(applier Applier) {
	if applier == nil {
		return
	}
	s.appliersMu.Lock()
	s.appliers = append(s.appliers, applier)
	s.appliersMu.Unlock()
}

func (s *Service) Current() Snapshot {
	return s.snapshot.Load()
}

func (s *Service) ModuleEnabled(id string) bool {
	return s.Current().ModuleEnabled(id)
}

func (s *Service) FeatureEnabled(moduleID, feature string) bool {
	if strings.TrimSpace(moduleID) != "ai" {
		return false
	}
	return s.Current().Bool("modules.ai.features."+strings.TrimSpace(feature), false)
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal) (sohaapi.RuntimeConfigSnapshot, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsRuntimeConfigView); err != nil {
		return sohaapi.RuntimeConfigSnapshot{}, err
	}
	snapshot := s.Current()
	desired := s.desired.Load()
	items := make([]sohaapi.RuntimeConfigItem, 0, len(s.registry.keys))
	pendingRestart := false
	for _, definition := range s.registry.Definitions() {
		item := s.item(snapshot, desired, definition)
		pendingRestart = pendingRestart || item.PendingRestart
		items = append(items, item)
	}
	return sohaapi.RuntimeConfigSnapshot{
		Version: snapshot.Version, ActiveRevisionID: snapshot.ActiveRevisionID,
		Items: items, PendingRestart: pendingRestart,
	}, nil
}

func (s *Service) Validate(ctx context.Context, principal domainidentity.Principal, request sohaapi.RuntimeConfigChangeRequest) (sohaapi.RuntimeConfigValidationResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsRuntimeConfigManage); err != nil {
		return sohaapi.RuntimeConfigValidationResult{}, err
	}
	return s.validate(request), nil
}

func (s *Service) Apply(ctx context.Context, principal domainidentity.Principal, request sohaapi.RuntimeConfigChangeRequest) (sohaapi.RuntimeConfigApplyResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsRuntimeConfigManage); err != nil {
		return sohaapi.RuntimeConfigApplyResult{}, err
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	validation := s.validate(request)
	if !validation.Valid {
		return sohaapi.RuntimeConfigApplyResult{}, validationError(validation)
	}
	return s.commitAndApply(ctx, principal, request, "", 0)
}

func (s *Service) History(ctx context.Context, principal domainidentity.Principal, limit int) ([]sohaapi.RuntimeConfigRevision, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsRuntimeConfigView); err != nil {
		return nil, err
	}
	items, err := s.store.ListRevisions(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]sohaapi.RuntimeConfigRevision, 0, len(items))
	for _, item := range items {
		out = append(out, revisionView(item))
	}
	return out, nil
}

func (s *Service) Rollback(ctx context.Context, principal domainidentity.Principal, request sohaapi.RuntimeConfigRollbackRequest) (sohaapi.RuntimeConfigApplyResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsRuntimeConfigManage); err != nil {
		return sohaapi.RuntimeConfigApplyResult{}, err
	}
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	current := s.desired.Load()
	if current.Version != request.ExpectedVersion {
		return sohaapi.RuntimeConfigApplyResult{}, fmt.Errorf("%w: expected version %d, current version %d", apperrors.ErrConflict, request.ExpectedVersion, current.Version)
	}
	target, err := s.store.GetRevisionByVersion(ctx, request.TargetVersion)
	if errors.Is(err, domainruntimeconfig.ErrNotFound) {
		return sohaapi.RuntimeConfigApplyResult{}, fmt.Errorf("%w: runtime config version %d", apperrors.ErrNotFound, request.TargetVersion)
	}
	if err != nil {
		return sohaapi.RuntimeConfigApplyResult{}, err
	}
	changes := diffChanges(current.Overrides, target.Snapshot)
	if len(changes) == 0 {
		return sohaapi.RuntimeConfigApplyResult{}, fmt.Errorf("%w: target version already matches current configuration", apperrors.ErrConflict)
	}
	changeRequest := sohaapi.RuntimeConfigChangeRequest{ExpectedVersion: current.Version, Changes: changes, Reason: request.Reason}
	return s.commitAndApply(ctx, principal, changeRequest, target.ID, target.Version)
}

func (s *Service) Application(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.RuntimeConfigApplication, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsRuntimeConfigView); err != nil {
		return sohaapi.RuntimeConfigApplication{}, err
	}
	item, err := s.store.GetApplication(ctx, strings.TrimSpace(id))
	if errors.Is(err, domainruntimeconfig.ErrNotFound) {
		return sohaapi.RuntimeConfigApplication{}, fmt.Errorf("%w: runtime config application", apperrors.ErrNotFound)
	}
	if err != nil {
		return sohaapi.RuntimeConfigApplication{}, err
	}
	return applicationView(item), nil
}

func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.reconcile(ctx)
			}
		}
	}()
}

func (s *Service) reconcile(ctx context.Context) {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	state, err := s.store.LoadState(ctx)
	if err != nil || state.Version <= s.desired.Load().Version {
		return
	}
	previous := s.Current()
	previousDesired := s.desired.Load()
	desired := snapshotFromState(s.registry, state)
	keys := changedKeys(previousDesired.Overrides, desired.Overrides)
	next := s.effectiveCandidate(previous, desired, keys)
	s.desired.Store(desired)
	if _, applyErr := s.applyHooks(ctx, previous, next, keys); applyErr == nil {
		s.snapshot.Store(next)
		return
	}
	s.snapshot.Store(snapshotWithRevision(previous, desired))
}

func (s *Service) validate(request sohaapi.RuntimeConfigChangeRequest) sohaapi.RuntimeConfigValidationResult {
	current := s.desired.Load()
	result := sohaapi.RuntimeConfigValidationResult{
		ExpectedVersion: request.ExpectedVersion, CurrentVersion: current.Version,
		Changes: []sohaapi.RuntimeConfigValidatedChange{}, Issues: []sohaapi.RuntimeConfigValidationIssue{},
	}
	if request.ExpectedVersion != current.Version {
		result.Issues = append(result.Issues, issue("version_conflict", "", fmt.Sprintf("expected version %d, current version %d", request.ExpectedVersion, current.Version)))
	}
	if len(request.Changes) == 0 {
		result.Issues = append(result.Issues, issue("changes_required", "", "at least one change is required"))
	}
	seen := map[string]struct{}{}
	proposedAIEnabled := current.Bool(KeyModuleAI, false)
	proposedAssistantEnabled := current.Bool(KeyAssistantGlobal, false)
	aiHierarchyTouched := false
	for _, change := range request.Changes {
		key := strings.TrimSpace(change.Key)
		definition, ok := s.registry.Definition(key)
		if !ok {
			result.Issues = append(result.Issues, issue("unknown_key", key, "configuration key is not registered"))
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			result.Issues = append(result.Issues, issue("duplicate_key", key, "configuration key is duplicated"))
			continue
		}
		seen[key] = struct{}{}
		if !definition.Editable || definition.lockedByEnvironment() {
			result.Issues = append(result.Issues, issue("not_editable", key, "configuration key is managed by another source"))
			continue
		}
		proposed := definition.BaselineValue
		if !change.Reset {
			value, err := normalizeValue(definition.ValueType, change.Value)
			if err != nil {
				result.Issues = append(result.Issues, issue("invalid_type", key, err.Error()))
				continue
			}
			if definition.Validate != nil {
				if err := definition.Validate(value); err != nil {
					result.Issues = append(result.Issues, issue("invalid_value", key, err.Error()))
					continue
				}
			}
			proposed = value
		}
		currentValue, _ := current.Value(key)
		if key == KeyModuleAI || key == KeyAssistantGlobal {
			aiHierarchyTouched = true
			if enabled, ok := proposed.(bool); ok {
				if key == KeyModuleAI {
					proposedAIEnabled = enabled
				} else {
					proposedAssistantEnabled = enabled
				}
			}
		}
		result.Changes = append(result.Changes, sohaapi.RuntimeConfigValidatedChange{
			Key: key, CurrentValue: redacted(definition, currentValue), ProposedValue: redacted(definition, proposed), ApplyMode: definition.ApplyMode,
		})
		result.RequiresRestart = result.RequiresRestart || definition.ApplyMode == sohaapi.RuntimeConfigApplyModeRestart
	}
	if aiHierarchyTouched && proposedAssistantEnabled && !proposedAIEnabled {
		result.Issues = append(result.Issues, issue("dependency_required", KeyAssistantGlobal, "global AI assistant requires the AI workbench to be enabled"))
	}
	result.Valid = len(result.Issues) == 0
	return result
}

func (s *Service) commitAndApply(ctx context.Context, principal domainidentity.Principal, request sohaapi.RuntimeConfigChangeRequest, rollbackRevisionID string, targetVersion int64) (sohaapi.RuntimeConfigApplyResult, error) {
	previous := s.Current()
	previousDesired := s.desired.Load()
	overrides := cloneValues(previousDesired.Overrides)
	changes := make([]sohaapi.RuntimeConfigChange, 0, len(request.Changes))
	for _, change := range request.Changes {
		definition, _ := s.registry.Definition(change.Key)
		if change.Reset {
			delete(overrides, change.Key)
			changes = append(changes, sohaapi.RuntimeConfigChange{Key: change.Key, Reset: true})
			continue
		}
		value, _ := normalizeValue(definition.ValueType, change.Value)
		overrides[change.Key] = value
		changes = append(changes, sohaapi.RuntimeConfigChange{Key: change.Key, Value: redacted(definition, value)})
	}
	now := time.Now().UTC()
	version := previousDesired.Version + 1
	revisionID, applicationID := uuid.NewString(), uuid.NewString()
	actor := firstValue(principal.UserName, principal.UserID, "unknown")
	revision := domainruntimeconfig.Revision{
		ID: revisionID, Version: version, Status: sohaapi.RuntimeConfigApplicationStatusPending,
		Changes: changes, Snapshot: overrides, Actor: actor, Reason: strings.TrimSpace(request.Reason),
		RollbackOfRevisionID: rollbackRevisionID, CreatedAt: now,
	}
	application := domainruntimeconfig.Application{
		ID: applicationID, RevisionID: revisionID, Version: version,
		Status: sohaapi.RuntimeConfigApplicationStatusPending, Items: pendingItems(s.registry, request.Changes),
		CreatedAt: now, UpdatedAt: now,
	}
	state, err := s.store.Commit(ctx, domainruntimeconfig.Commit{ExpectedVersion: request.ExpectedVersion, Revision: revision, Application: application})
	if errors.Is(err, domainruntimeconfig.ErrVersionConflict) {
		return sohaapi.RuntimeConfigApplyResult{}, fmt.Errorf("%w: runtime config version changed", apperrors.ErrConflict)
	}
	if err != nil {
		return sohaapi.RuntimeConfigApplyResult{}, err
	}
	next := snapshotFromState(s.registry, state)
	keys := changeKeys(request.Changes)
	effective := s.effectiveCandidate(previous, next, keys)
	s.desired.Store(next)
	application.Items, err = s.applyHooks(ctx, previous, effective, keys)
	if err == nil {
		s.snapshot.Store(effective)
	} else {
		s.snapshot.Store(snapshotWithRevision(previous, next))
	}
	application.UpdatedAt = time.Now().UTC()
	application.Status, application.Error = applicationStatus(application.Items, err, rollbackRevisionID != "")
	if updateErr := s.store.UpdateApplication(ctx, application); updateErr != nil && err == nil {
		err = updateErr
	}
	revision.Status = application.Status
	s.recordAudit(ctx, principal, revision, targetVersion)
	result := sohaapi.RuntimeConfigApplyResult{Revision: revisionView(revision), Application: applicationView(application)}
	return result, err
}

func (s *Service) applyHooks(ctx context.Context, previous, next Snapshot, keys []string) ([]sohaapi.RuntimeConfigAppliedItem, error) {
	s.appliersMu.RLock()
	appliers := append([]Applier(nil), s.appliers...)
	s.appliersMu.RUnlock()
	items := make([]sohaapi.RuntimeConfigAppliedItem, 0, len(keys))
	var combined error
	for _, key := range keys {
		definition, ok := s.registry.Definition(key)
		if !ok {
			continue
		}
		if definition.ApplyMode == sohaapi.RuntimeConfigApplyModeRestart {
			items = append(items, sohaapi.RuntimeConfigAppliedItem{Key: key, ApplyMode: definition.ApplyMode, Status: sohaapi.RuntimeConfigApplicationStatusRestartRequired})
			continue
		}
		handled := false
		for _, applier := range appliers {
			if !applier.Handles(key) {
				continue
			}
			handled = true
			applied, err := applier.Apply(ctx, previous, next, []string{key})
			items = append(items, applied...)
			combined = errors.Join(combined, err)
		}
		if !handled {
			items = append(items, sohaapi.RuntimeConfigAppliedItem{Key: key, ApplyMode: definition.ApplyMode, Status: sohaapi.RuntimeConfigApplicationStatusApplied})
		}
	}
	return items, combined
}

func (s *Service) effectiveCandidate(previous, desired Snapshot, keys []string) Snapshot {
	next := snapshotWithRevision(previous, desired)
	for _, key := range keys {
		definition, ok := s.registry.Definition(key)
		if !ok || definition.ApplyMode == sohaapi.RuntimeConfigApplyModeRestart {
			continue
		}
		if value, ok := desired.Overrides[key]; ok {
			next.Overrides[key] = value
		} else {
			delete(next.Overrides, key)
		}
	}
	return next
}

func (s *Service) item(snapshot, desired Snapshot, definition Definition) sohaapi.RuntimeConfigItem {
	value, _ := snapshot.Value(definition.Key)
	source := sohaapi.RuntimeConfigSourceConfigFile
	if definition.lockedByEnvironment() {
		source = sohaapi.RuntimeConfigSourceEnvironment
	} else if _, ok := desired.Overrides[definition.Key]; ok {
		source = sohaapi.RuntimeConfigSourceRuntimeOverride
	}
	desiredValue, _ := desired.Value(definition.Key)
	return sohaapi.RuntimeConfigItem{
		Key: definition.Key, Category: definition.Category, Label: definition.Label, Description: definition.Description,
		ValueType: definition.ValueType, EffectiveValue: redacted(definition, value), DefaultValue: redacted(definition, definition.DefaultValue),
		Source: source, ApplyMode: definition.ApplyMode,
		Editable: definition.Editable && !definition.lockedByEnvironment(), Sensitive: definition.Sensitive,
		PendingRestart: definition.ApplyMode == sohaapi.RuntimeConfigApplyModeRestart && !reflect.DeepEqual(value, desiredValue),
	}
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permission string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permission)
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, revision domainruntimeconfig.Revision, targetVersion int64) {
	if s.audit == nil {
		return
	}
	keys := make([]string, 0, len(revision.Changes))
	for _, change := range revision.Changes {
		keys = append(keys, change.Key)
	}
	metadata := map[string]any{"version": revision.Version, "keys": keys, "status": revision.Status}
	if targetVersion > 0 {
		metadata["targetVersion"] = targetVersion
	}
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "RuntimeConfigRevision", ResourceName: revision.ID,
		Action: "settings.runtime_config.apply", Result: string(revision.Status), Summary: "runtime configuration revision applied",
		Metadata: metadata,
	})
}

func snapshotFromState(registry *Registry, state domainruntimeconfig.State) Snapshot {
	return Snapshot{Version: state.Version, ActiveRevisionID: state.ActiveRevisionID, Overrides: cloneValues(state.Overrides), registry: registry}
}

func snapshotWithRevision(effective, desired Snapshot) Snapshot {
	effective.Version = desired.Version
	effective.ActiveRevisionID = desired.ActiveRevisionID
	effective.Overrides = cloneValues(effective.Overrides)
	return effective
}

func normalizeValue(kind sohaapi.RuntimeConfigValueType, value any) (any, error) {
	switch kind {
	case sohaapi.RuntimeConfigValueTypeBoolean:
		result, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("must be a boolean")
		}
		return result, nil
	case sohaapi.RuntimeConfigValueTypeString, sohaapi.RuntimeConfigValueTypeURL, sohaapi.RuntimeConfigValueTypeDuration:
		result, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("must be a string")
		}
		return strings.TrimSpace(result), nil
	case sohaapi.RuntimeConfigValueTypeInteger:
		switch typed := value.(type) {
		case int:
			return int64(typed), nil
		case int64:
			return typed, nil
		case float64:
			if typed != float64(int64(typed)) {
				return nil, fmt.Errorf("must be an integer")
			}
			return int64(typed), nil
		default:
			return nil, fmt.Errorf("must be an integer")
		}
	case sohaapi.RuntimeConfigValueTypeStringList:
		values, ok := value.([]string)
		if ok {
			return append([]string(nil), values...), nil
		}
		raw, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("must be a string list")
		}
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("must be a string list")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return value, nil
	}
}

func issue(code, key, message string) sohaapi.RuntimeConfigValidationIssue {
	return sohaapi.RuntimeConfigValidationIssue{Code: code, Key: key, Message: message, Severity: sohaapi.RuntimeConfigIssueSeverityError}
}

func validationError(result sohaapi.RuntimeConfigValidationResult) error {
	if len(result.Issues) == 0 {
		return fmt.Errorf("%w: runtime configuration is invalid", apperrors.ErrInvalidArgument)
	}
	if result.Issues[0].Code == "version_conflict" {
		return fmt.Errorf("%w: %s", apperrors.ErrConflict, result.Issues[0].Message)
	}
	return fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, result.Issues[0].Message)
}

func pendingItems(registry *Registry, changes []sohaapi.RuntimeConfigChange) []sohaapi.RuntimeConfigAppliedItem {
	items := make([]sohaapi.RuntimeConfigAppliedItem, 0, len(changes))
	for _, change := range changes {
		definition, _ := registry.Definition(change.Key)
		items = append(items, sohaapi.RuntimeConfigAppliedItem{Key: change.Key, ApplyMode: definition.ApplyMode, Status: sohaapi.RuntimeConfigApplicationStatusPending})
	}
	return items
}

func applicationStatus(items []sohaapi.RuntimeConfigAppliedItem, applyErr error, rollback bool) (sohaapi.RuntimeConfigApplicationStatus, string) {
	failed, restart := false, false
	for _, item := range items {
		failed = failed || item.Status == sohaapi.RuntimeConfigApplicationStatusFailed
		restart = restart || item.Status == sohaapi.RuntimeConfigApplicationStatusRestartRequired
	}
	if applyErr != nil || failed {
		return sohaapi.RuntimeConfigApplicationStatusFailed, errorText(applyErr)
	}
	if restart {
		return sohaapi.RuntimeConfigApplicationStatusRestartRequired, ""
	}
	if rollback {
		return sohaapi.RuntimeConfigApplicationStatusRolledBack, ""
	}
	return sohaapi.RuntimeConfigApplicationStatusApplied, ""
}

func errorText(err error) string {
	if err == nil {
		return "configuration application failed"
	}
	return err.Error()
}

func revisionView(item domainruntimeconfig.Revision) sohaapi.RuntimeConfigRevision {
	return sohaapi.RuntimeConfigRevision{
		ID: item.ID, Version: item.Version, Status: item.Status, Changes: item.Changes,
		Actor: item.Actor, Reason: item.Reason, RollbackOfRevisionID: item.RollbackOfRevisionID, CreatedAt: item.CreatedAt,
	}
}

func applicationView(item domainruntimeconfig.Application) sohaapi.RuntimeConfigApplication {
	return sohaapi.RuntimeConfigApplication{
		ID: item.ID, RevisionID: item.RevisionID, Version: item.Version, Status: item.Status,
		Items: item.Items, Error: item.Error, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func redacted(definition Definition, value any) any {
	if definition.Sensitive {
		return nil
	}
	return value
}

func changeKeys(changes []sohaapi.RuntimeConfigChange) []string {
	keys := make([]string, 0, len(changes))
	for _, change := range changes {
		keys = append(keys, strings.TrimSpace(change.Key))
	}
	return keys
}

func changedKeys(previous, next map[string]any) []string {
	keys := map[string]struct{}{}
	for key := range previous {
		keys[key] = struct{}{}
	}
	for key := range next {
		keys[key] = struct{}{}
	}
	out := make([]string, 0, len(keys))
	for key := range keys {
		if !reflect.DeepEqual(previous[key], next[key]) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func diffChanges(previous, next map[string]any) []sohaapi.RuntimeConfigChange {
	keys := changedKeys(previous, next)
	out := make([]sohaapi.RuntimeConfigChange, 0, len(keys))
	for _, key := range keys {
		value, ok := next[key]
		if !ok {
			out = append(out, sohaapi.RuntimeConfigChange{Key: key, Reset: true})
			continue
		}
		out = append(out, sohaapi.RuntimeConfigChange{Key: key, Value: value})
	}
	return out
}
