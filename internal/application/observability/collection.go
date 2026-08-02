package observability

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"sigs.k8s.io/yaml"
)

const (
	logCollectionChartRepository = "https://opensoha.github.io/soha-helm"
	logCollectionChartVersion    = "0.1.0"
	logCollectionReleaseName     = "soha-observability"
	logCollectionPlanTTL         = 10 * time.Minute
)

var kubernetesNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type CollectionSettingsStore interface {
	Get(context.Context, string) (map[string]any, bool, error)
	Upsert(context.Context, string, string, map[string]any, string) error
}

type CollectionConnectionResolver interface {
	GetConnection(context.Context, string) (domaincluster.Connection, error)
}

type CollectionHelm interface {
	InstallHelmChart(context.Context, domainidentity.Principal, string, domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error)
	UpdateHelmReleaseValues(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmValuesView, error)
	DeleteHelmRelease(context.Context, domainidentity.Principal, string, string, string) error
}

type CollectionPortForwards interface {
	ListPortForwards(context.Context, domainidentity.Principal, string) ([]domainresource.PortForwardSessionView, error)
	RegisterPortForward(context.Context, domainidentity.Principal, string, domainresource.PortForwardRegisterInput) (domainresource.PortForwardSessionView, error)
}

type CollectionDependencies struct {
	Settings     CollectionSettingsStore
	Connections  CollectionConnectionResolver
	Helm         CollectionHelm
	PortForwards CollectionPortForwards
	Access       domainaccess.Authorizer
}

type collectionPlan struct {
	ClusterID            string                       `json:"clusterId"`
	UserID               string                       `json:"userId"`
	Profile              sohaapi.LogCollectionProfile `json:"profile"`
	Namespace            string                       `json:"namespace"`
	DataSourceID         string                       `json:"dataSourceId,omitempty"`
	CredentialSecretName string                       `json:"credentialSecretName,omitempty"`
	DestinationEndpoint  string                       `json:"destinationEndpoint"`
	DestinationTenantID  string                       `json:"destinationTenantId,omitempty"`
	RetentionDays        int                          `json:"retentionDays"`
	StorageClassName     string                       `json:"storageClassName,omitempty"`
	StorageSize          string                       `json:"storageSize"`
	NamespaceAllowlist   []string                     `json:"namespaceAllowlist,omitempty"`
	QueryEndpoint        string                       `json:"queryEndpoint,omitempty"`
	PortForwardSessionID string                       `json:"portForwardSessionId,omitempty"`
	ExpiresAt            time.Time                    `json:"expiresAt"`
}

type collectionRecord struct {
	State sohaapi.LogCollectionState `json:"state"`
	Plan  collectionPlan             `json:"plan"`
}

func (s *Service) ConfigureCollection(deps CollectionDependencies) {
	s.collection = deps
}

func (s *Service) GetLogCollection(ctx context.Context, principal domainidentity.Principal, clusterID string) (sohaapi.LogCollectionState, error) {
	if err := s.authorize(ctx, principal, appaccess.PermPlatformClustersView); err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	clusterID, _, err := s.collectionConnection(ctx, clusterID)
	if err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	record, err := s.loadCollectionRecord(ctx, clusterID)
	return record.State, err
}

func (s *Service) PreflightLogCollection(ctx context.Context, principal domainidentity.Principal, clusterID string, input sohaapi.LogCollectionPreflightInput) (sohaapi.LogCollectionPlan, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveLogCollectionManage); err != nil {
		return sohaapi.LogCollectionPlan{}, err
	}
	clusterID, connection, err := s.collectionConnection(ctx, clusterID)
	if err != nil {
		return sohaapi.LogCollectionPlan{}, err
	}
	plan, blockers, warnings := s.buildCollectionPlan(ctx, principal, connection, input)
	result := publicCollectionPlan(plan, blockers, warnings)
	if len(blockers) == 0 {
		result.PlanToken, err = s.signCollectionPlan(plan)
		if err != nil {
			return sohaapi.LogCollectionPlan{}, err
		}
		result.CanEnable = true
	}
	s.recordCollectionAudit(ctx, principal, clusterID, plan.Namespace, "observability.log-collection.preflight", "success", fmt.Sprintf("prepared log collection plan with %d blockers", len(blockers)))
	return result, nil
}

func (s *Service) EnableLogCollection(ctx context.Context, principal domainidentity.Principal, clusterID string, input sohaapi.LogCollectionEnableInput) (sohaapi.LogCollectionState, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveLogCollectionManage); err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	s.collectionMu.Lock()
	defer s.collectionMu.Unlock()

	clusterID, plan, resumeRelease, err := s.prepareCollectionEnable(ctx, principal, clusterID, input.PlanToken)
	if err != nil {
		return sohaapi.LogCollectionState{}, err
	}

	now := s.now().UTC()
	state := stateFromCollectionPlan(plan, sohaapi.LogCollectionStatusInstalling, now)
	state.OperationID = uuid.NewString()
	record := collectionRecord{State: state, Plan: plan}
	if err := s.saveCollectionRecord(ctx, principal, record); err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	if err := s.deployCollectionPlan(ctx, principal, clusterID, plan, resumeRelease); err != nil {
		s.failCollectionEnable(ctx, principal, clusterID, &record, err)
		return record.State, nil
	}

	s.finalizeCollectionEnable(ctx, principal, clusterID, plan, &record)
	if err := s.saveCollectionRecord(ctx, principal, record); err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	s.recordCollectionAudit(ctx, principal, clusterID, plan.Namespace, "observability.log-collection.enable", "success", "installed managed log collection")
	return record.State, nil
}

func (s *Service) prepareCollectionEnable(ctx context.Context, principal domainidentity.Principal, clusterID, planToken string) (string, collectionPlan, bool, error) {
	clusterID, connection, err := s.collectionConnection(ctx, clusterID)
	if err != nil {
		return "", collectionPlan{}, false, err
	}
	plan, err := s.verifyCollectionPlan(strings.TrimSpace(planToken), clusterID, principal.UserID)
	if err != nil {
		return "", collectionPlan{}, false, err
	}
	if s.collection.Helm == nil {
		return "", collectionPlan{}, false, fmt.Errorf("%w: Helm collection runtime is unavailable", apperrors.ErrClusterUnready)
	}
	_, blockers, _ := s.buildCollectionPlan(ctx, principal, connection, inputFromCollectionPlan(plan))
	if len(blockers) > 0 {
		return "", collectionPlan{}, false, fmt.Errorf("%w: collection preflight changed: %s", apperrors.ErrConflict, strings.Join(blockers, "; "))
	}
	current, err := s.loadCollectionRecord(ctx, clusterID)
	if err != nil {
		return "", collectionPlan{}, false, err
	}
	if collectionStatusActive(current.State.Status) {
		return "", collectionPlan{}, false, fmt.Errorf("%w: log collection is already active", apperrors.ErrConflict)
	}
	resumeRelease := current.State.Mode == sohaapi.LogCollectionModeSohaManaged &&
		(current.State.Status == sohaapi.LogCollectionStatusDisabled || current.State.Status == sohaapi.LogCollectionStatusDegraded)
	if resumeRelease && current.State.Namespace != plan.Namespace {
		return "", collectionPlan{}, false, fmt.Errorf("%w: uninstall the existing release before changing its namespace", apperrors.ErrConflict)
	}
	return clusterID, plan, resumeRelease, nil
}

func collectionStatusActive(status sohaapi.LogCollectionStatus) bool {
	return status == sohaapi.LogCollectionStatusInstalling || status == sohaapi.LogCollectionStatusStopping || status == sohaapi.LogCollectionStatusHealthy
}

func (s *Service) deployCollectionPlan(ctx context.Context, principal domainidentity.Principal, clusterID string, plan collectionPlan, resumeRelease bool) error {
	values, err := renderCollectionValues(plan, true)
	if err != nil {
		return err
	}
	if resumeRelease {
		_, err = s.collection.Helm.UpdateHelmReleaseValues(ctx, principal, clusterID, plan.Namespace, logCollectionReleaseName, values)
		return err
	}
	_, err = s.collection.Helm.InstallHelmChart(ctx, principal, clusterID, domainresource.HelmChartInstallInput{
		RepositoryName: "opensoha", RepositoryURL: logCollectionChartRepository, ChartName: logCollectionReleaseName,
		Version: logCollectionChartVersion, ReleaseName: logCollectionReleaseName, Namespace: plan.Namespace,
		ValuesYAML: values, CreateNamespace: true, Wait: true, TimeoutSeconds: 600,
	})
	return err
}

func (s *Service) failCollectionEnable(ctx context.Context, principal domainidentity.Principal, clusterID string, record *collectionRecord, installErr error) {
	record.State.Status = sohaapi.LogCollectionStatusFailed
	record.State.Collector = componentHealth(sohaapi.LogCollectionHealthStatusFailed, "collector installation failed")
	record.State.LastError = installErr.Error()
	record.State.UpdatedAt = s.now().UTC()
	_ = s.saveCollectionRecord(ctx, principal, *record)
	s.recordCollectionAudit(ctx, principal, clusterID, record.Plan.Namespace, "observability.log-collection.enable", "failure", "failed to install managed log collection")
}

func (s *Service) finalizeCollectionEnable(ctx context.Context, principal domainidentity.Principal, clusterID string, plan collectionPlan, record *collectionRecord) {
	record.State.Collector = componentHealth(sohaapi.LogCollectionHealthStatusHealthy, "collector release is ready")
	var validationErr error
	if plan.Profile == sohaapi.LogCollectionProfileStarter {
		var queryPlan = plan
		queryPlan.DestinationEndpoint, queryPlan.PortForwardSessionID, validationErr = s.ensureManagedLokiQueryEndpoint(ctx, principal, clusterID, plan.Namespace)
		if validationErr == nil {
			record.Plan.QueryEndpoint = queryPlan.DestinationEndpoint
			record.Plan.PortForwardSessionID = queryPlan.PortForwardSessionID
			record.State.DataSourceID, validationErr = s.ensureManagedLokiDataSource(ctx, clusterID, queryPlan)
		}
	}
	if validationErr == nil && record.State.DataSourceID != "" {
		var validated sohaapi.ObservabilityDataSource
		validated, validationErr = s.ValidateDataSource(ctx, principal, record.State.DataSourceID)
		if validationErr == nil && validated.ValidationStatus == sohaapi.ObservabilityDataSourceValidationStatusHealthy {
			record.State.Backend = componentHealth(sohaapi.LogCollectionHealthStatusHealthy, "log backend query succeeded")
			record.State.EndToEnd = componentHealth(sohaapi.LogCollectionHealthStatusHealthy, "collector and query path are ready")
			record.State.Status = sohaapi.LogCollectionStatusHealthy
		} else if validationErr == nil {
			validationErr = fmt.Errorf("log backend validation failed")
		}
	}
	if validationErr != nil {
		record.State.Status = sohaapi.LogCollectionStatusDegraded
		record.State.Backend = componentHealth(sohaapi.LogCollectionHealthStatusDegraded, "collector is running but the query backend is not reachable")
		record.State.EndToEnd = componentHealth(sohaapi.LogCollectionHealthStatusDegraded, "ingest-to-query verification is incomplete")
		record.State.LastError = validationErr.Error()
	}
	record.State.UpdatedAt = s.now().UTC()
}

func (s *Service) DisableLogCollection(ctx context.Context, principal domainidentity.Principal, clusterID string, input sohaapi.LogCollectionDisableInput) (sohaapi.LogCollectionState, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveLogCollectionManage); err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	if input.Action != sohaapi.LogCollectionDisableActionStop && input.Action != sohaapi.LogCollectionDisableActionUninstall {
		return sohaapi.LogCollectionState{}, fmt.Errorf("%w: disable action must be stop or uninstall", apperrors.ErrInvalidArgument)
	}
	s.collectionMu.Lock()
	defer s.collectionMu.Unlock()

	clusterID, _, err := s.collectionConnection(ctx, clusterID)
	if err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	record, err := s.loadCollectionRecord(ctx, clusterID)
	if err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	if record.State.Mode != sohaapi.LogCollectionModeSohaManaged || record.State.Namespace == "" {
		return record.State, nil
	}
	if s.collection.Helm == nil {
		return sohaapi.LogCollectionState{}, fmt.Errorf("%w: Helm collection runtime is unavailable", apperrors.ErrClusterUnready)
	}
	record.State.Status = sohaapi.LogCollectionStatusStopping
	record.State.OperationID = uuid.NewString()
	record.State.UpdatedAt = s.now().UTC()
	if err := s.saveCollectionRecord(ctx, principal, record); err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	if input.Action == sohaapi.LogCollectionDisableActionStop {
		var values string
		values, err = renderCollectionValues(record.Plan, false)
		if err == nil {
			_, err = s.collection.Helm.UpdateHelmReleaseValues(ctx, principal, clusterID, record.State.Namespace, record.State.ReleaseName, values)
		}
	} else {
		err = s.collection.Helm.DeleteHelmRelease(ctx, principal, clusterID, record.State.Namespace, record.State.ReleaseName)
	}
	if err != nil {
		record.State.Status = sohaapi.LogCollectionStatusFailed
		record.State.LastError = err.Error()
		record.State.UpdatedAt = s.now().UTC()
		_ = s.saveCollectionRecord(ctx, principal, record)
		s.recordCollectionAudit(ctx, principal, clusterID, record.State.Namespace, "observability.log-collection.disable", "failure", "failed to disable managed log collection")
		return record.State, nil
	}
	record.State.Status = sohaapi.LogCollectionStatusDisabled
	record.State.Collector = componentHealth(sohaapi.LogCollectionHealthStatusUnknown, "collector is stopped")
	record.State.EndToEnd = componentHealth(sohaapi.LogCollectionHealthStatusUnknown, "collection is stopped; retained history remains queryable")
	record.State.LastError = ""
	record.State.HistoryPreserved = true
	if input.Action == sohaapi.LogCollectionDisableActionUninstall {
		record.State.Mode = sohaapi.LogCollectionModeRuntimeOnly
	}
	record.State.UpdatedAt = s.now().UTC()
	if err := s.saveCollectionRecord(ctx, principal, record); err != nil {
		return sohaapi.LogCollectionState{}, err
	}
	s.recordCollectionAudit(ctx, principal, clusterID, record.State.Namespace, "observability.log-collection.disable", "success", "disabled managed collection without deleting log history")
	return record.State, nil
}

func (s *Service) buildCollectionPlan(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, input sohaapi.LogCollectionPreflightInput) (collectionPlan, []string, []string) {
	plan := collectionPlan{
		ClusterID: connection.Summary.ID, UserID: principal.UserID, Profile: input.Profile,
		Namespace: strings.TrimSpace(input.Namespace), DataSourceID: strings.TrimSpace(input.DataSourceID),
		CredentialSecretName: strings.TrimSpace(input.CredentialSecretName), RetentionDays: input.RetentionDays,
		StorageClassName: strings.TrimSpace(input.StorageClassName), StorageSize: strings.TrimSpace(input.StorageSize),
		NamespaceAllowlist: normalizeNames(input.NamespaceAllowlist), ExpiresAt: s.now().UTC().Add(logCollectionPlanTTL),
	}
	if plan.StorageSize == "" {
		plan.StorageSize = "10Gi"
	}
	warnings := []string{"The collector mounts /var/log/pods read-only on every selected node.", "Stopping or uninstalling collection does not delete durable history."}
	blockers := validateCollectionPlanBasics(plan)
	blockers = append(blockers, s.validateCollectionPlanAccess(ctx, principal, connection, plan)...)
	destinationBlockers, destinationWarnings := s.configureCollectionDestination(ctx, connection, &plan)
	blockers = append(blockers, destinationBlockers...)
	warnings = append(warnings, destinationWarnings...)
	return plan, dedupeStrings(blockers), dedupeStrings(warnings)
}

func validateCollectionPlanBasics(plan collectionPlan) []string {
	blockers := make([]string, 0)
	if !validKubernetesName(plan.Namespace) {
		blockers = append(blockers, "A valid installation namespace is required.")
	}
	if plan.RetentionDays < 1 || plan.RetentionDays > 365 {
		blockers = append(blockers, "Retention must be between 1 and 365 days.")
	}
	if !supportedCollectionProfile(plan.Profile) {
		blockers = append(blockers, "A supported collection profile is required.")
	}
	for _, namespace := range plan.NamespaceAllowlist {
		if !validKubernetesName(namespace) {
			blockers = append(blockers, fmt.Sprintf("Namespace %q is invalid.", namespace))
		}
	}
	return blockers
}

func supportedCollectionProfile(profile sohaapi.LogCollectionProfile) bool {
	return profile == sohaapi.LogCollectionProfileCollectorOnly || profile == sohaapi.LogCollectionProfileStarter || profile == sohaapi.LogCollectionProfileProductionExternal
}

func (s *Service) validateCollectionPlanAccess(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, plan collectionPlan) []string {
	blockers := make([]string, 0)
	for _, namespace := range plan.NamespaceAllowlist {
		if validKubernetesName(namespace) {
			if err := s.authorizeCollectionScope(ctx, principal, connection, namespace, "Pod", domainaccess.ActionLogs); err != nil {
				blockers = append(blockers, fmt.Sprintf("No log access to namespace %q.", namespace))
			}
		}
	}
	if len(plan.NamespaceAllowlist) == 0 {
		if err := s.authorizeCollectionScope(ctx, principal, connection, "", "Pod", domainaccess.ActionLogs); err != nil {
			blockers = append(blockers, "Cluster-wide collection requires cluster-wide Pod log access; select authorized namespaces instead.")
		}
	}
	if plan.Namespace != "" {
		if err := s.authorizeCollectionScope(ctx, principal, connection, plan.Namespace, "HelmRelease", domainaccess.ActionCreate); err != nil {
			blockers = append(blockers, "No permission to install a Helm release in the selected namespace.")
		}
		if plan.Profile == sohaapi.LogCollectionProfileStarter {
			for _, action := range []domainaccess.Action{domainaccess.ActionList, domainaccess.ActionUpdate} {
				if err := s.authorizeCollectionScope(ctx, principal, connection, plan.Namespace, "PortForward", action); err != nil {
					blockers = append(blockers, "No permission to manage the Loki query tunnel in the selected namespace.")
				}
			}
		}
	}
	return blockers
}

func (s *Service) configureCollectionDestination(ctx context.Context, connection domaincluster.Connection, plan *collectionPlan) ([]string, []string) {
	blockers := make([]string, 0)
	warnings := make([]string, 0)
	if plan.Profile == sohaapi.LogCollectionProfileStarter {
		plan.DataSourceID = "ds:soha-managed:" + connection.Summary.ID
		plan.DestinationEndpoint = managedLokiEndpoint(plan.Namespace)
		warnings = append(warnings, "Starter runs a single-replica Loki backend and is intended for small installations.")
	} else if plan.DataSourceID == "" {
		blockers = append(blockers, "Collector-only and production-external profiles require a Loki data source.")
	} else if item, err := s.dataSources.GetDataSource(ctx, plan.DataSourceID); err != nil {
		blockers = append(blockers, "The selected Loki data source does not exist.")
	} else {
		if item.SourceKind != dataSourceKindLogs || item.BackendType != "loki" || !item.Enabled {
			blockers = append(blockers, "The selected data source must be an enabled Loki source.")
		}
		if !dataSourceCovers(item.Scope, connection.Summary.ID, plan.NamespaceAllowlist) {
			blockers = append(blockers, "The selected data source scope does not cover this cluster and namespace selection.")
		}
		plan.DestinationEndpoint = strings.TrimSpace(fmt.Sprint(item.Config["endpoint"]))
		plan.DestinationTenantID = strings.TrimSpace(fmt.Sprint(item.Config["tenantId"]))
		if strings.TrimSpace(item.CredentialRef) != "" && plan.CredentialSecretName == "" {
			blockers = append(blockers, "The selected data source uses credentials; provide an existing in-cluster credential Secret.")
		}
	}
	return blockers, warnings
}

func publicCollectionPlan(plan collectionPlan, blockers, warnings []string) sohaapi.LogCollectionPlan {
	components := []sohaapi.LogCollectionPlanComponents{sohaapi.LogCollectionPlanComponentsOtelCollector}
	cpu, memory := "200m", "256Mi"
	if plan.Profile == sohaapi.LogCollectionProfileStarter {
		components = append(components, sohaapi.LogCollectionPlanComponentsLoki)
		cpu, memory = "500m", "1Gi"
	} else if plan.Profile == sohaapi.LogCollectionProfileProductionExternal {
		cpu, memory = "300m", "512Mi"
	}
	return sohaapi.LogCollectionPlan{
		ClusterID: plan.ClusterID, Profile: plan.Profile, Namespace: plan.Namespace, Components: components,
		HostPaths: []string{"/var/log/pods"}, RBACRules: []string{},
		Destination:   sohaapi.LogCollectionDestination{DataSourceID: plan.DataSourceID, BackendType: "loki", Endpoint: plan.DestinationEndpoint, TenantID: plan.DestinationTenantID, CredentialSecretName: plan.CredentialSecretName},
		RetentionDays: plan.RetentionDays, StorageClassName: plan.StorageClassName, StorageSize: plan.StorageSize,
		Resources: sohaapi.LogCollectionResourceEstimate{CPU: cpu, Memory: memory}, NamespaceAllowlist: plan.NamespaceAllowlist,
		Warnings: warnings, Blockers: blockers, CanEnable: false, HistoryPreserved: true, ExpiresAt: plan.ExpiresAt,
	}
}

func (s *Service) collectionConnection(ctx context.Context, clusterID string) (string, domaincluster.Connection, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" || s.collection.Connections == nil {
		return "", domaincluster.Connection{}, fmt.Errorf("%w: cluster is required", apperrors.ErrInvalidArgument)
	}
	connection, err := s.collection.Connections.GetConnection(ctx, clusterID)
	if err != nil {
		return "", domaincluster.Connection{}, err
	}
	return connection.Summary.ID, connection, nil
}

func (s *Service) authorizeCollectionScope(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, namespace, kind string, action domainaccess.Action) error {
	if s.collection.Access == nil {
		return fmt.Errorf("%w: collection access policy is unavailable", apperrors.ErrAccessDenied)
	}
	decision, err := s.collection.Access.Authorize(ctx, domainaccess.Request{
		Principal: principal, Action: action,
		Subject:   domainaccess.SubjectAttributes{UserID: principal.UserID, Roles: principal.Roles, Teams: principal.Teams, Projects: principal.Projects, Tags: principal.Tags},
		Cluster:   domainaccess.ClusterAttributes{ClusterID: connection.Summary.ID, Region: connection.Summary.Region, Environment: connection.Summary.Environment, Labels: connection.Summary.Labels},
		Namespace: domainaccess.NamespaceAttributes{Namespace: namespace}, Resource: domainaccess.ResourceAttributes{Kind: kind},
		Context: domainaccess.ContextAttributes{Source: requestctx.FromContext(ctx).Source, OccurredAt: s.now().UTC()},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return nil
}

func (s *Service) signCollectionPlan(plan collectionPlan) (string, error) {
	key := s.keys.Active()
	if key.ID() == "" || key.Secret() == "" {
		return "", fmt.Errorf("%w: collection plan signing key is unavailable", apperrors.ErrClusterUnready)
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(key.Secret()))
	_, _ = mac.Write([]byte(encoded))
	return key.ID() + "." + encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Service) verifyCollectionPlan(token, clusterID, userID string) (collectionPlan, error) {
	if len(token) > 16384 {
		return collectionPlan{}, fmt.Errorf("%w: invalid collection plan token", apperrors.ErrInvalidArgument)
	}
	signatureSeparator := strings.LastIndexByte(token, '.')
	payloadSeparator := strings.LastIndexByte(token[:max(signatureSeparator, 0)], '.')
	if payloadSeparator <= 0 || signatureSeparator <= payloadSeparator+1 || signatureSeparator == len(token)-1 {
		return collectionPlan{}, fmt.Errorf("%w: invalid collection plan token", apperrors.ErrInvalidArgument)
	}
	keyID, encoded, encodedSignature := token[:payloadSeparator], token[payloadSeparator+1:signatureSeparator], token[signatureSeparator+1:]
	key, ok := s.keys.Find(keyID, s.now().UTC())
	if !ok {
		return collectionPlan{}, fmt.Errorf("%w: collection plan signing key expired", apperrors.ErrConflict)
	}
	mac := hmac.New(sha256.New, []byte(key.Secret()))
	_, _ = mac.Write([]byte(encoded))
	signature, err := base64.RawURLEncoding.DecodeString(encodedSignature)
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return collectionPlan{}, fmt.Errorf("%w: invalid collection plan signature", apperrors.ErrInvalidArgument)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return collectionPlan{}, fmt.Errorf("%w: invalid collection plan payload", apperrors.ErrInvalidArgument)
	}
	var plan collectionPlan
	if err := json.Unmarshal(payload, &plan); err != nil || plan.ClusterID != clusterID || plan.UserID != userID {
		return collectionPlan{}, fmt.Errorf("%w: collection plan does not match this user and cluster", apperrors.ErrAccessDenied)
	}
	if !s.now().UTC().Before(plan.ExpiresAt) {
		return collectionPlan{}, fmt.Errorf("%w: collection plan expired; run preflight again", apperrors.ErrConflict)
	}
	return plan, nil
}

func (s *Service) loadCollectionRecord(ctx context.Context, clusterID string) (collectionRecord, error) {
	if s.collection.Settings == nil {
		return collectionRecord{}, fmt.Errorf("%w: collection settings store is unavailable", apperrors.ErrClusterUnready)
	}
	value, found, err := s.collection.Settings.Get(ctx, domainsettings.LogCollectionSettingKeyPrefix+clusterID)
	if err != nil {
		return collectionRecord{}, err
	}
	if !found {
		return collectionRecord{State: defaultCollectionState(clusterID, s.now().UTC())}, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return collectionRecord{}, err
	}
	var record collectionRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return collectionRecord{}, fmt.Errorf("decode log collection state: %w", err)
	}
	return record, nil
}

func (s *Service) saveCollectionRecord(ctx context.Context, principal domainidentity.Principal, record collectionRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	value := map[string]any{}
	if err := json.Unmarshal(payload, &value); err != nil {
		return err
	}
	return s.collection.Settings.Upsert(ctx, domainsettings.LogCollectionSettingKeyPrefix+record.State.ClusterID, "observability", value, principal.UserID)
}

func (s *Service) ensureManagedLokiDataSource(ctx context.Context, clusterID string, plan collectionPlan) (string, error) {
	now := s.now().UTC()
	scope := map[string]any{"clusterIds": []string{clusterID}}
	if len(plan.NamespaceAllowlist) > 0 {
		scope["namespaces"] = plan.NamespaceAllowlist
	}
	item := domainobservability.DataSource{
		ID: plan.DataSourceID, Name: "Soha managed Loki (" + clusterID + ")", SourceKind: dataSourceKindLogs, BackendType: "loki", Enabled: true,
		Scope: scope, QueryBudget: map[string]any{"maxEntries": 1000, "maxRangeSeconds": 86400, "timeoutSeconds": 10},
		RedactionPolicy: map[string]any{}, MCPAdapter: logsAdapterID, Config: map[string]any{
			"endpoint": plan.DestinationEndpoint,
			"labelKeys": map[string]any{
				"cluster": "k8s_cluster_name", "namespace": "k8s_namespace_name", "pod": "k8s_pod_name",
				"container": "k8s_container_name", "service": "service_name", "workload": "k8s_deployment_name", "severity": "severity_text",
			},
		}, CreatedAt: now, UpdatedAt: now,
	}
	if current, err := s.dataSources.GetDataSource(ctx, item.ID); err == nil {
		item.CredentialRef = current.CredentialRef
		_, err = s.dataSources.UpdateDataSource(ctx, item.ID, domainobservability.DataSourceInput{
			ID: item.ID, Name: item.Name, SourceKind: item.SourceKind, BackendType: item.BackendType, Enabled: item.Enabled,
			CredentialRef: item.CredentialRef, Scope: item.Scope, QueryBudget: item.QueryBudget, RedactionPolicy: item.RedactionPolicy, MCPAdapter: item.MCPAdapter, Config: item.Config,
		})
		return item.ID, err
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return "", err
	}
	_, err := s.dataSources.CreateDataSource(ctx, item)
	return item.ID, err
}

func (s *Service) ensureManagedLokiQueryEndpoint(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) (string, string, error) {
	if s.collection.PortForwards == nil {
		return "", "", fmt.Errorf("%w: managed Loki query tunnel is unavailable", apperrors.ErrClusterUnready)
	}
	targetName := logCollectionReleaseName + "-loki"
	sessions, err := s.collection.PortForwards.ListPortForwards(ctx, principal, clusterID)
	if err != nil {
		return "", "", err
	}
	for _, session := range sessions {
		if session.Namespace == namespace && strings.EqualFold(session.TargetKind, "Service") && session.TargetName == targetName && session.RemotePort == 3100 && session.LocalPort > 0 && session.Status == "active" {
			return fmt.Sprintf("http://127.0.0.1:%d", session.LocalPort), session.SessionID, nil
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", fmt.Errorf("allocate managed Loki query port: %w", err)
	}
	localPort := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return "", "", fmt.Errorf("release managed Loki query port: %w", err)
	}
	// ponytail: the existing port-forward API takes a port, so registration detects the narrow bind race.
	session, err := s.collection.PortForwards.RegisterPortForward(ctx, principal, clusterID, domainresource.PortForwardRegisterInput{
		Namespace: namespace, TargetKind: "Service", TargetName: targetName, LocalPort: localPort, RemotePort: 3100,
	})
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("http://127.0.0.1:%d", session.LocalPort), session.SessionID, nil
}

func renderCollectionValues(plan collectionPlan, collectorEnabled bool) (string, error) {
	endpoint := strings.TrimRight(plan.DestinationEndpoint, "/")
	if !strings.HasSuffix(endpoint, "/otlp") {
		endpoint += "/otlp"
	}
	values := map[string]any{
		"profile": string(plan.Profile), "clusterId": plan.ClusterID,
		"collector": map[string]any{"enabled": collectorEnabled, "namespaceAllowlist": plan.NamespaceAllowlist, "destination": map[string]any{
			"endpoint": endpoint, "tenantId": plan.DestinationTenantID, "existingSecret": plan.CredentialSecretName,
		}},
		"loki":        map[string]any{"enabled": plan.Profile == sohaapi.LogCollectionProfileStarter, "retentionDays": plan.RetentionDays},
		"persistence": map[string]any{"enabled": plan.Profile == sohaapi.LogCollectionProfileStarter, "storageClassName": plan.StorageClassName, "storageSize": plan.StorageSize},
	}
	payload, err := yaml.Marshal(values)
	return string(payload), err
}

func defaultCollectionState(clusterID string, now time.Time) sohaapi.LogCollectionState {
	unknown := componentHealth(sohaapi.LogCollectionHealthStatusUnknown, "managed collection is not enabled")
	return sohaapi.LogCollectionState{
		ClusterID: clusterID, Mode: sohaapi.LogCollectionModeRuntimeOnly, Status: sohaapi.LogCollectionStatusDisabled,
		Namespace: "", ReleaseName: "", Collector: unknown, Backend: unknown, EndToEnd: unknown, HistoryPreserved: true, UpdatedAt: now,
	}
}

func stateFromCollectionPlan(plan collectionPlan, status sohaapi.LogCollectionStatus, now time.Time) sohaapi.LogCollectionState {
	unknown := componentHealth(sohaapi.LogCollectionHealthStatusUnknown, "waiting for verification")
	return sohaapi.LogCollectionState{
		ClusterID: plan.ClusterID, Mode: sohaapi.LogCollectionModeSohaManaged, Status: status, Profile: plan.Profile,
		Namespace: plan.Namespace, ReleaseName: logCollectionReleaseName, DataSourceID: plan.DataSourceID,
		RetentionDays: plan.RetentionDays, NamespaceAllowlist: plan.NamespaceAllowlist,
		Collector: unknown, Backend: unknown, EndToEnd: unknown, HistoryPreserved: true, UpdatedAt: now,
	}
}

func inputFromCollectionPlan(plan collectionPlan) sohaapi.LogCollectionPreflightInput {
	return sohaapi.LogCollectionPreflightInput{
		Profile: plan.Profile, Namespace: plan.Namespace, DataSourceID: plan.DataSourceID,
		CredentialSecretName: plan.CredentialSecretName, RetentionDays: plan.RetentionDays,
		StorageClassName: plan.StorageClassName, StorageSize: plan.StorageSize, NamespaceAllowlist: plan.NamespaceAllowlist,
	}
}

func componentHealth(status sohaapi.LogCollectionHealthStatus, message string) sohaapi.LogCollectionComponentHealth {
	return sohaapi.LogCollectionComponentHealth{Status: status, Message: message}
}

func managedLokiEndpoint(namespace string) string {
	return fmt.Sprintf("http://%s-loki.%s.svc.cluster.local:3100", logCollectionReleaseName, namespace)
}

func validKubernetesName(value string) bool {
	return len(value) > 0 && len(value) <= 253 && kubernetesNamePattern.MatchString(value)
}

func normalizeNames(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func dedupeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func dataSourceCovers(scope map[string]any, clusterID string, namespaces []string) bool {
	clusters := collectionStringSlice(scope["clusterIds"])
	if len(clusters) > 0 && !slices.Contains(clusters, clusterID) {
		return false
	}
	allowedNamespaces := collectionStringSlice(scope["namespaces"])
	for _, namespace := range namespaces {
		if !slices.Contains(allowedNamespaces, namespace) {
			return false
		}
	}
	return true
}

func collectionStringSlice(value any) []string {
	result := make([]string, 0)
	switch typed := value.(type) {
	case []string:
		return normalizeNames(typed)
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
	}
	return normalizeNames(result)
}

func (s *Service) recordCollectionAudit(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, action, result, summary string) {
	if s.audit == nil {
		return
	}
	metadata := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: "LogCollection", ResourceName: clusterID, ClusterID: clusterID, Namespace: namespace,
		Action: action, Result: result, Summary: summary, RequestPath: metadata.Path, RequestMethod: metadata.Method,
		RequestID: metadata.RequestID, SourceIP: metadata.SourceIP, CreatedAt: s.now().UTC(),
	})
}
