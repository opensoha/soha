package secret

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

const maxSecretValueBytes = 1 << 20
const secretLeaseTTL = 5 * time.Minute

var (
	secretAliasPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	secretRefPattern   = regexp.MustCompile(`^soha://secrets/([A-Za-z0-9._-]+)(?:/versions/([1-9][0-9]*))?$`)
)

type Repository interface {
	List(context.Context, domainsecret.Filter) ([]domainsecret.Secret, error)
	Get(context.Context, string) (domainsecret.Secret, error)
	Create(context.Context, domainsecret.Secret, domainsecret.Version) (domainsecret.Secret, error)
	Update(context.Context, domainsecret.Secret) (domainsecret.Secret, error)
	ListVersions(context.Context, string) ([]domainsecret.Version, error)
	GetVersion(context.Context, string, int) (domainsecret.Version, error)
	Rotate(context.Context, string, domainsecret.Version) (domainsecret.Version, error)
	RevokeVersion(context.Context, string, int, time.Time) (domainsecret.Version, error)
	CreateLease(context.Context, domainsecret.Lease) error
	RedeemLease(context.Context, string, string, string, time.Time) (domainsecret.Lease, error)
	RevokeSubjectLeases(context.Context, string, string, time.Time) error
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	repo        Repository
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
	keys        keyring.Ring
	now         func() time.Time
}

func New(repo Repository, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder, keys keyring.Ring) (*Service, error) {
	if repo == nil || permissions == nil || audit == nil || operations == nil {
		return nil, fmt.Errorf("%w: secret service dependencies are required", apperrors.ErrInvalidArgument)
	}
	if keys.Active().ID() == "" {
		return nil, fmt.Errorf("%w: secret encryption key is required", apperrors.ErrInvalidArgument)
	}
	return &Service{repo: repo, permissions: permissions, audit: audit, operations: operations, keys: keys, now: time.Now}, nil
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter domainsecret.Filter) ([]sohaapi.SecretMetadata, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretView); err != nil {
		return nil, err
	}
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	if filter.ScopeType != "" && !validScopeType(filter.ScopeType) {
		return nil, fmt.Errorf("%w: invalid secret scope type", apperrors.ErrInvalidArgument)
	}
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	result := make([]sohaapi.SecretMetadata, 0, len(items))
	for _, item := range items {
		allowed, err := s.scopeAllowed(ctx, principal, item)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, publicSecret(item))
		}
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.SecretMetadata, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretView); err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	item, err := s.authorizedSecret(ctx, principal, id)
	if err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	return publicSecret(item), nil
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, request sohaapi.SecretCreateRequest) (sohaapi.SecretMetadata, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretManage); err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	name := strings.TrimSpace(request.Name)
	description := strings.TrimSpace(request.Description)
	scopeType := domainsecret.ScopeType(request.ScopeType)
	scopeID := strings.TrimSpace(request.ScopeID)
	if name == "" || len(name) > 128 || len(description) > 1024 || !validScope(scopeType, scopeID) {
		return sohaapi.SecretMetadata{}, fmt.Errorf("%w: invalid secret metadata", apperrors.ErrInvalidArgument)
	}
	if request.Value == "" || len(request.Value) > maxSecretValueBytes {
		return sohaapi.SecretMetadata{}, fmt.Errorf("%w: secret value is required and must not exceed 1 MiB", apperrors.ErrInvalidArgument)
	}
	bindings, err := normalizeBindingsFromAPI(request.Bindings)
	if err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(s.keys, request.Value)
	if err != nil {
		return sohaapi.SecretMetadata{}, fmt.Errorf("encrypt secret value: %w", err)
	}
	if !secretcrypto.Encrypted(ciphertext) {
		return sohaapi.SecretMetadata{}, errorsInvalidCiphertext()
	}
	now := s.now().UTC()
	item := domainsecret.Secret{
		ID: uuid.NewString(), Name: name, Description: description, ScopeType: scopeType, ScopeID: scopeID,
		Status: domainsecret.StatusActive, CurrentVersion: 1, Bindings: bindings, CreatedBy: principal.UserID,
		CreatedAt: now, UpdatedAt: now,
	}
	created, err := s.repo.Create(ctx, item, domainsecret.Version{
		SecretID: item.ID, Version: 1, Ciphertext: ciphertext, Status: domainsecret.VersionActive,
		CreatedBy: principal.UserID, CreatedAt: now,
	})
	if err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	s.record(ctx, principal, created, "secret.create", "success", map[string]any{"version": 1})
	return publicSecret(created), nil
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, id string, input domainsecret.UpdateInput) (sohaapi.SecretMetadata, error) {
	return s.update(ctx, principal, id, input, "secret.update")
}

func (s *Service) update(ctx context.Context, principal domainidentity.Principal, id string, input domainsecret.UpdateInput, action string) (sohaapi.SecretMetadata, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretManage); err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	if input.Name != nil {
		item.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		item.Description = strings.TrimSpace(*input.Description)
	}
	if input.Status != nil {
		item.Status = *input.Status
	}
	if input.Bindings != nil {
		item.Bindings, err = normalizeBindings(*input.Bindings)
		if err != nil {
			return sohaapi.SecretMetadata{}, err
		}
	}
	if item.Name == "" || len(item.Name) > 128 || len(item.Description) > 1024 || !validStatus(item.Status) {
		return sohaapi.SecretMetadata{}, fmt.Errorf("%w: invalid secret metadata", apperrors.ErrInvalidArgument)
	}
	item.UpdatedAt = s.now().UTC()
	updated, err := s.repo.Update(ctx, item)
	if err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	s.record(ctx, principal, updated, action, "success", nil)
	return publicSecret(updated), nil
}

func (s *Service) Disable(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.SecretMetadata, error) {
	status := domainsecret.StatusDisabled
	return s.update(ctx, principal, id, domainsecret.UpdateInput{Status: &status}, "secret.disable")
}

func (s *Service) ListVersions(ctx context.Context, principal domainidentity.Principal, id string) ([]sohaapi.SecretVersionMetadata, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretView); err != nil {
		return nil, err
	}
	if _, err := s.authorizedSecret(ctx, principal, id); err != nil {
		return nil, err
	}
	items, err := s.repo.ListVersions(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	result := make([]sohaapi.SecretVersionMetadata, 0, len(items))
	for _, item := range items {
		result = append(result, publicVersion(item))
	}
	return result, nil
}

func (s *Service) Rotate(ctx context.Context, principal domainidentity.Principal, id string, request sohaapi.SecretRotateRequest) (sohaapi.SecretVersionMetadata, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretManage); err != nil {
		return sohaapi.SecretVersionMetadata{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return sohaapi.SecretVersionMetadata{}, err
	}
	if item.Status != domainsecret.StatusActive {
		return sohaapi.SecretVersionMetadata{}, fmt.Errorf("%w: disabled secret cannot be rotated", apperrors.ErrConflict)
	}
	if request.Value == "" || len(request.Value) > maxSecretValueBytes {
		return sohaapi.SecretVersionMetadata{}, fmt.Errorf("%w: secret value is required and must not exceed 1 MiB", apperrors.ErrInvalidArgument)
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(s.keys, request.Value)
	if err != nil {
		return sohaapi.SecretVersionMetadata{}, fmt.Errorf("encrypt secret value: %w", err)
	}
	if !secretcrypto.Encrypted(ciphertext) {
		return sohaapi.SecretVersionMetadata{}, errorsInvalidCiphertext()
	}
	version, err := s.repo.Rotate(ctx, item.ID, domainsecret.Version{
		SecretID: item.ID, Ciphertext: ciphertext, Status: domainsecret.VersionActive,
		CreatedBy: principal.UserID, CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return sohaapi.SecretVersionMetadata{}, err
	}
	item.CurrentVersion = version.Version
	s.record(ctx, principal, item, "secret.rotate", "success", map[string]any{"version": version.Version})
	return publicVersion(version), nil
}

func (s *Service) RevokeVersion(ctx context.Context, principal domainidentity.Principal, id string, version int) (sohaapi.SecretVersionMetadata, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretManage); err != nil {
		return sohaapi.SecretVersionMetadata{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return sohaapi.SecretVersionMetadata{}, err
	}
	if version < 1 || version == item.CurrentVersion {
		return sohaapi.SecretVersionMetadata{}, fmt.Errorf("%w: current secret version cannot be revoked", apperrors.ErrConflict)
	}
	revoked, err := s.repo.RevokeVersion(ctx, item.ID, version, s.now().UTC())
	if err != nil {
		return sohaapi.SecretVersionMetadata{}, err
	}
	s.record(ctx, principal, item, "secret.version.revoke", "success", map[string]any{"version": version})
	return publicVersion(revoked), nil
}

func (s *Service) PinReferences(ctx context.Context, principal domainidentity.Principal, refs map[string]string, target domainsecret.Target) ([]domainsecret.Reference, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretUse); err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return []domainsecret.Reference{}, nil
	}
	if err := validateTarget(target); err != nil {
		return nil, err
	}
	parsed, err := parseReferences(refs)
	if err != nil {
		return nil, err
	}
	for index := range parsed {
		item, version, err := s.availableReference(ctx, principal, parsed[index], target)
		if err != nil {
			s.recordUse(ctx, principal, target, "failure", len(refs))
			return nil, err
		}
		parsed[index].Version = version.Version
		parsed[index].URI = fmt.Sprintf("soha://secrets/%s/versions/%d", item.ID, version.Version)
	}
	s.recordUse(ctx, principal, target, "validated", len(parsed))
	return parsed, nil
}

func (s *Service) ResolvePinnedReferences(ctx context.Context, principal domainidentity.Principal, refs []domainsecret.Reference, target domainsecret.Target) (map[string]string, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermSecretUse); err != nil {
		return nil, err
	}
	if err := validateTarget(target); err != nil {
		return nil, err
	}
	values := make(map[string]string, len(refs))
	for _, ref := range refs {
		if ref.Version < 1 || !secretAliasPattern.MatchString(ref.Alias) {
			return nil, fmt.Errorf("%w: invalid pinned secret reference", apperrors.ErrInvalidArgument)
		}
		_, version, err := s.availableReference(ctx, principal, ref, target)
		if err != nil {
			s.recordUse(ctx, principal, target, "failure", len(refs))
			return nil, err
		}
		if !secretcrypto.Encrypted(version.Ciphertext) {
			return nil, errorsInvalidCiphertext()
		}
		value, err := secretcrypto.DecryptStringWithKeyring(s.keys, version.Ciphertext)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret reference: %w", err)
		}
		values[ref.Alias] = value
	}
	s.recordUse(ctx, principal, target, "success", len(refs))
	return values, nil
}

func (s *Service) IssueLease(ctx context.Context, principal domainidentity.Principal, refs []domainsecret.Reference, target domainsecret.Target, subjectType, subjectID, agentID string) (*domainsecret.LeaseGrant, error) {
	subjectType = strings.TrimSpace(subjectType)
	subjectID = strings.TrimSpace(subjectID)
	agentID = strings.TrimSpace(agentID)
	if len(refs) == 0 {
		return nil, nil
	}
	if !slices.Contains([]string{"execution_task", "agent_run"}, subjectType) || subjectID == "" || agentID == "" {
		return nil, fmt.Errorf("%w: invalid secret lease binding", apperrors.ErrInvalidArgument)
	}
	pinned, err := s.PinReferences(ctx, principal, ReferencesToMap(refs), target)
	if err != nil {
		return nil, err
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return nil, fmt.Errorf("generate secret lease token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	now := s.now().UTC()
	lease := domainsecret.Lease{
		ID: uuid.NewString(), TokenHash: hashLeaseToken(token), AgentID: agentID, SubjectType: subjectType, SubjectID: subjectID,
		Target: target, References: pinned, Principal: principal, ExpiresAt: now.Add(secretLeaseTTL), CreatedAt: now,
	}
	if err := s.repo.CreateLease(ctx, lease); err != nil {
		return nil, err
	}
	s.recordLease(ctx, principal, lease, "issued")
	return &domainsecret.LeaseGrant{ID: lease.ID, Token: token, ExpiresAt: lease.ExpiresAt}, nil
}

func (s *Service) RedeemLease(ctx context.Context, leaseID, token, agentID string) (sohaapi.SecretLeaseRedemption, error) {
	leaseID = strings.TrimSpace(leaseID)
	token = strings.TrimSpace(token)
	agentID = strings.TrimSpace(agentID)
	if leaseID == "" || len(token) < 16 || agentID == "" {
		return sohaapi.SecretLeaseRedemption{}, fmt.Errorf("%w: invalid secret lease redemption", apperrors.ErrInvalidArgument)
	}
	lease, err := s.repo.RedeemLease(ctx, leaseID, hashLeaseToken(token), agentID, s.now().UTC())
	if err != nil {
		return sohaapi.SecretLeaseRedemption{}, err
	}
	values, err := s.ResolvePinnedReferences(ctx, lease.Principal, lease.References, lease.Target)
	if err != nil {
		s.recordLease(ctx, lease.Principal, lease, "failure")
		return sohaapi.SecretLeaseRedemption{}, err
	}
	s.recordLease(ctx, lease.Principal, lease, "redeemed")
	return sohaapi.SecretLeaseRedemption{LeaseID: lease.ID, Values: values, ExpiresAt: lease.ExpiresAt}, nil
}

func (s *Service) RevokeSubjectLeases(ctx context.Context, subjectType, subjectID string) error {
	return s.repo.RevokeSubjectLeases(ctx, strings.TrimSpace(subjectType), strings.TrimSpace(subjectID), s.now().UTC())
}

func hashLeaseToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func ReferencesToMap(refs []domainsecret.Reference) map[string]string {
	result := make(map[string]string, len(refs))
	for _, ref := range refs {
		result[ref.Alias] = ref.URI
	}
	return result
}

func (s *Service) availableReference(ctx context.Context, principal domainidentity.Principal, ref domainsecret.Reference, target domainsecret.Target) (domainsecret.Secret, domainsecret.Version, error) {
	item, err := s.repo.Get(ctx, ref.SecretID)
	if err != nil {
		return domainsecret.Secret{}, domainsecret.Version{}, unavailableReference()
	}
	allowed, err := s.scopeAllowed(ctx, principal, item)
	if err != nil || !allowed || item.Status != domainsecret.StatusActive || !bindingMatches(item.Bindings, target) {
		return domainsecret.Secret{}, domainsecret.Version{}, unavailableReference()
	}
	versionNumber := ref.Version
	if versionNumber == 0 {
		versionNumber = item.CurrentVersion
	}
	version, err := s.repo.GetVersion(ctx, item.ID, versionNumber)
	if err != nil || version.Status != domainsecret.VersionActive {
		return domainsecret.Secret{}, domainsecret.Version{}, unavailableReference()
	}
	return item, version, nil
}

func (s *Service) authorizedSecret(ctx context.Context, principal domainidentity.Principal, id string) (domainsecret.Secret, error) {
	item, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return domainsecret.Secret{}, err
	}
	allowed, err := s.scopeAllowed(ctx, principal, item)
	if err != nil {
		return domainsecret.Secret{}, err
	}
	if !allowed {
		return domainsecret.Secret{}, unavailableReference()
	}
	return item, nil
}

func (s *Service) scopeAllowed(ctx context.Context, principal domainidentity.Principal, item domainsecret.Secret) (bool, error) {
	manage, err := s.permissions.HasPermission(ctx, principal, appaccess.PermSecretManage)
	if err != nil {
		return false, err
	}
	if manage {
		return true, nil
	}
	switch item.ScopeType {
	case domainsecret.ScopeWorkspace:
		return item.ScopeID == "default", nil
	case domainsecret.ScopeProject:
		return slices.Contains(principal.Projects, item.ScopeID), nil
	case domainsecret.ScopeEnvironment:
		for _, binding := range item.Bindings {
			if binding.TargetType == "project" && slices.Contains(principal.Projects, binding.TargetRef) {
				return true, nil
			}
		}
	}
	return false, nil
}

func parseReferences(refs map[string]string) ([]domainsecret.Reference, error) {
	aliases := make([]string, 0, len(refs))
	for alias := range refs {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	result := make([]domainsecret.Reference, 0, len(aliases))
	for _, alias := range aliases {
		if !secretAliasPattern.MatchString(alias) {
			return nil, fmt.Errorf("%w: invalid secret alias %q", apperrors.ErrInvalidArgument, alias)
		}
		uri := strings.TrimSpace(refs[alias])
		matches := secretRefPattern.FindStringSubmatch(uri)
		if len(matches) == 0 {
			return nil, fmt.Errorf("%w: invalid secret reference for %s", apperrors.ErrInvalidArgument, alias)
		}
		version := 0
		if matches[2] != "" {
			version, _ = strconv.Atoi(matches[2])
		}
		result = append(result, domainsecret.Reference{Alias: alias, SecretID: matches[1], Version: version, URI: uri})
	}
	return result, nil
}

func normalizeBindingsFromAPI(items []sohaapi.SecretBinding) ([]domainsecret.Binding, error) {
	bindings := make([]domainsecret.Binding, 0, len(items))
	for _, item := range items {
		bindings = append(bindings, domainsecret.Binding{TargetType: string(item.TargetType), TargetRef: item.TargetRef})
	}
	return normalizeBindings(bindings)
}

func normalizeBindings(items []domainsecret.Binding) ([]domainsecret.Binding, error) {
	if len(items) > 100 {
		return nil, fmt.Errorf("%w: secret bindings must not exceed 100", apperrors.ErrInvalidArgument)
	}
	result := make([]domainsecret.Binding, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item.TargetType = strings.ToLower(strings.TrimSpace(item.TargetType))
		item.TargetRef = strings.TrimSpace(item.TargetRef)
		if !slices.Contains([]string{"capability", "project", "connection"}, item.TargetType) || item.TargetRef == "" || len(item.TargetRef) > 256 {
			return nil, fmt.Errorf("%w: invalid secret binding", apperrors.ErrInvalidArgument)
		}
		key := item.TargetType + "\x00" + item.TargetRef
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TargetType == result[j].TargetType {
			return result[i].TargetRef < result[j].TargetRef
		}
		return result[i].TargetType < result[j].TargetType
	})
	return result, nil
}

func validScope(scopeType domainsecret.ScopeType, scopeID string) bool {
	if !validScopeType(scopeType) || scopeID == "" || len(scopeID) > 128 {
		return false
	}
	return scopeType != domainsecret.ScopeWorkspace || scopeID == "default"
}

func validScopeType(scopeType domainsecret.ScopeType) bool {
	return slices.Contains([]domainsecret.ScopeType{domainsecret.ScopeWorkspace, domainsecret.ScopeProject, domainsecret.ScopeEnvironment}, scopeType)
}

func validStatus(status domainsecret.Status) bool {
	return status == domainsecret.StatusActive || status == domainsecret.StatusDisabled
}

func validateTarget(target domainsecret.Target) error {
	if !slices.Contains([]string{"capability", "project", "connection"}, target.Type) || strings.TrimSpace(target.Ref) == "" {
		return fmt.Errorf("%w: invalid secret target", apperrors.ErrInvalidArgument)
	}
	return nil
}

func bindingMatches(bindings []domainsecret.Binding, target domainsecret.Target) bool {
	for _, binding := range bindings {
		if binding.TargetType == target.Type && binding.TargetRef == target.Ref {
			return true
		}
	}
	return false
}

func unavailableReference() error {
	return fmt.Errorf("%w: secret reference is unavailable", apperrors.ErrNotFound)
}

func errorsInvalidCiphertext() error {
	return fmt.Errorf("%w: stored secret is not encrypted", apperrors.ErrConflict)
}

func publicSecret(item domainsecret.Secret) sohaapi.SecretMetadata {
	bindings := make([]sohaapi.SecretBinding, 0, len(item.Bindings))
	for _, binding := range item.Bindings {
		bindings = append(bindings, sohaapi.SecretBinding{TargetType: sohaapi.SecretBindingTargetType(binding.TargetType), TargetRef: binding.TargetRef})
	}
	return sohaapi.SecretMetadata{
		ID: item.ID, Name: item.Name, Description: item.Description, ScopeType: sohaapi.SecretScopeType(item.ScopeType),
		ScopeID: item.ScopeID, Status: sohaapi.SecretStatus(item.Status), CurrentVersion: item.CurrentVersion,
		Bindings: bindings, CreatedBy: item.CreatedBy, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func publicVersion(item domainsecret.Version) sohaapi.SecretVersionMetadata {
	return sohaapi.SecretVersionMetadata{
		SecretID: item.SecretID, Version: item.Version, Status: sohaapi.SecretVersionStatus(item.Status),
		CreatedBy: item.CreatedBy, CreatedAt: item.CreatedAt, RevokedAt: item.RevokedAt,
	}
}

func (s *Service) record(ctx context.Context, principal domainidentity.Principal, item domainsecret.Secret, action, result string, metadata map[string]any) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["secretId"] = item.ID
	metadata["scopeType"] = item.ScopeType
	metadata["scopeId"] = item.ScopeID
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: append([]string(nil), principal.Roles...), Teams: append([]string(nil), principal.Teams...),
		ResourceKind: "Secret", ResourceName: item.ID, Action: action, Result: result, Summary: action,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: metadata,
	})
	_ = s.operations.Record(ctx, operationentry.New(ctx, principal, action, map[string]any{
		"resourceKind": "Secret", "resourceName": item.ID, "scopeType": item.ScopeType, "scopeId": item.ScopeID,
	}, result, action, metadata))
}

func (s *Service) recordUse(ctx context.Context, principal domainidentity.Principal, target domainsecret.Target, result string, count int) {
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: append([]string(nil), principal.Roles...), Teams: append([]string(nil), principal.Teams...),
		ResourceKind: "SecretReference", ResourceName: target.Ref, Action: "secret.use", Result: result, Summary: "secret reference use",
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"targetType": target.Type, "targetRef": target.Ref, "referenceCount": count},
	})
}

func (s *Service) recordLease(ctx context.Context, principal domainidentity.Principal, lease domainsecret.Lease, result string) {
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: append([]string(nil), principal.Roles...), Teams: append([]string(nil), principal.Teams...),
		ResourceKind: "SecretLease", ResourceName: lease.ID, Action: "secret.lease." + result, Result: result, Summary: "secret lease " + result,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"leaseId": lease.ID, "agentId": lease.AgentID, "subjectType": lease.SubjectType, "subjectId": lease.SubjectID, "referenceCount": len(lease.References)},
	})
}
