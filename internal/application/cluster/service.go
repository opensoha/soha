package cluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	informerinfra "github.com/opensoha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
	"go.uber.org/zap"
)

const defaultSyncConcurrency = 4

type Repository interface {
	List(context.Context) ([]domaincluster.Summary, error)
	Get(context.Context, string) (domaincluster.Summary, error)
	ListConnections(context.Context) ([]domaincluster.Connection, error)
	GetConnection(context.Context, string) (domaincluster.Connection, error)
	UpsertRegistration(context.Context, domaincluster.Connection) error
	UpdateRegistration(context.Context, domaincluster.Connection) error
	UpsertSnapshot(context.Context, domaincluster.Summary) error
	Delete(context.Context, string) error
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	manager    *k8sinfra.Manager
	cache      *informerinfra.Service
	agents     *agentinfra.Registry
	repo       Repository
	authorizer domainaccess.Authorizer
	audit      AuditRecorder
	operations OperationRecorder
	syncLimit  int
	logger     *zap.Logger
	metrics    *runtimeobs.Registry
}

func New(manager *k8sinfra.Manager, cache *informerinfra.Service, agents *agentinfra.Registry, repo Repository, authorizer domainaccess.Authorizer, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{
		manager:    manager,
		cache:      cache,
		agents:     agents,
		repo:       repo,
		authorizer: authorizer,
		audit:      audit,
		operations: operations,
		syncLimit:  defaultSyncConcurrency,
	}
}

func (s *Service) SetInstrumentation(logger *zap.Logger, metrics *runtimeobs.Registry) {
	s.logger = logger
	s.metrics = metrics
}

func (s *Service) SetSyncLimit(limit int) {
	if limit > 0 {
		s.syncLimit = limit
	}
}

func (s *Service) Start(ctx context.Context) {
	if s.repo == nil {
		return
	}
	go func() {
		s.restoreRuntimeRegistrations(ctx)
		s.runSyncCycle(ctx, "startup")
		ticker := time.NewTicker(45 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runSyncCycle(ctx, "scheduled")
			}
		}
	}()
}

func (s *Service) restoreRuntimeRegistrations(ctx context.Context) {
	connections, err := s.repo.ListConnections(ctx)
	if err != nil {
		s.logWarn("cluster restore registrations failed", zap.Error(err))
		return
	}
	for _, connection := range connections {
		if connection.Summary.ConnectionMode != domaincluster.ConnectionModeDirectKubeconfig && connection.Summary.ConnectionMode != "" {
			continue
		}
		cfg, err := runtimeClusterConfig(connection)
		if err != nil {
			s.logWarn("cluster runtime config invalid", zap.String("clusterID", connection.Summary.ID), zap.Error(err))
			continue
		}
		s.manager.RegisterCluster(*cfg)
		if s.cache != nil {
			if err := s.cache.RegisterCluster(ctx, connection.Summary.ID); err != nil {
				s.logWarn("cluster informer registration failed", zap.String("clusterID", connection.Summary.ID), zap.Error(err))
			}
		}
	}
}

func (s *Service) List(ctx context.Context) ([]domaincluster.Summary, error) {
	if s.repo == nil {
		return s.manager.ListClusters(ctx)
	}
	items, err := s.repo.List(ctx)
	if err == nil && len(items) > 0 {
		return items, nil
	}
	if _, syncErr := s.syncAll(ctx); syncErr != nil && err == nil {
		err = syncErr
	}
	if err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}

func (s *Service) ListAccessible(ctx context.Context, principal domainidentity.Principal) ([]domaincluster.Summary, error) {
	items, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	if s.authorizer == nil {
		return items, nil
	}
	filtered := make([]domaincluster.Summary, 0, len(items))
	for _, item := range items {
		if err := s.authorize(ctx, principal, item, domainaccess.ActionView); err == nil {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *Service) Get(ctx context.Context, id string) (domaincluster.Summary, error) {
	if s.repo == nil {
		return s.manager.GetCluster(ctx, id)
	}
	item, err := s.repo.Get(ctx, id)
	if err == nil {
		return item, nil
	}
	if err := s.syncOne(ctx, id); err != nil {
		return domaincluster.Summary{}, err
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) Describe(ctx context.Context, principal domainidentity.Principal, id string) (domaincluster.Detail, error) {
	if s.repo == nil {
		summary, err := s.manager.GetCluster(ctx, id)
		if err != nil {
			return domaincluster.Detail{}, err
		}
		if err := s.authorize(ctx, principal, summary, domainaccess.ActionView); err != nil {
			return domaincluster.Detail{}, err
		}
		return domaincluster.Detail{
			Summary:          summary,
			CapabilityMatrix: domaincluster.DefaultCapabilityMatrix(),
			Diagnostics: domaincluster.Diagnostics{
				Transport:       "client-go",
				SyncStrategy:    "informer_cache_then_live_fallback",
				CacheStatus:     "warming",
				CacheReady:      false,
				LastChecked:     summary.Health.LastChecked,
				ConnectionState: summary.Health.Status,
				Message:         summary.Health.Message,
			},
			Connection: domaincluster.ConnectionDetail{
				Mode:              domaincluster.ConnectionModeDirectKubeconfig,
				UsesInformerCache: true,
			},
		}, nil
	}

	connection, err := s.repo.GetConnection(ctx, id)
	if err != nil {
		return domaincluster.Detail{}, err
	}
	summary, err := s.repo.Get(ctx, id)
	if err != nil {
		summary = connection.Summary
	}
	if err := s.authorize(ctx, principal, summary, domainaccess.ActionView); err != nil {
		return domaincluster.Detail{}, err
	}

	detail := domaincluster.Detail{
		Summary:          summary,
		CapabilityMatrix: domaincluster.DefaultCapabilityMatrix(),
		Connection: domaincluster.ConnectionDetail{
			Mode:           connection.Summary.ConnectionMode,
			CredentialType: connection.CredentialType,
			SourceType:     connection.SourceType,
			SourceRef:      connection.SourceRef,
		},
		Monitoring: domaincluster.MonitoringDetail{
			Prometheus: domaincluster.PrometheusDetail{
				BaseURL:        strings.TrimSpace(metadataString(connection.Metadata, "prometheus_url")),
				ClusterLabel:   strings.TrimSpace(metadataString(connection.Metadata, "prometheus_cluster_label")),
				GrafanaBaseURL: strings.TrimSpace(metadataString(connection.Metadata, "grafana_base_url")),
				HasBearerToken: strings.TrimSpace(metadataString(connection.Metadata, "prometheus_bearer_token")) != "",
			},
		},
		Diagnostics: domaincluster.Diagnostics{
			LastChecked:     summary.Health.LastChecked,
			ConnectionState: summary.Health.Status,
			Message:         summary.Health.Message,
		},
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		detail.Diagnostics.Transport = "remote-agent-http"
		detail.Diagnostics.SyncStrategy = "agent_summary_and_remote_pull"
		detail.Diagnostics.CacheStatus = "disabled"
		detail.Diagnostics.CacheReady = false
		if endpoint, _ := connection.Metadata["endpoint"].(string); strings.TrimSpace(endpoint) != "" {
			detail.Connection.Endpoint = endpoint
		}
		if token, _ := connection.Metadata["token"].(string); strings.TrimSpace(token) != "" {
			detail.Connection.HasToken = true
		}
	default:
		detail.Diagnostics.Transport = "client-go"
		detail.Diagnostics.SyncStrategy = "informer_cache_then_live_fallback"
		detail.Diagnostics.CacheReady = s.cache != nil && s.cache.Ready(id)
		if detail.Diagnostics.CacheReady {
			detail.Diagnostics.CacheStatus = "ready"
		} else {
			detail.Diagnostics.CacheStatus = "warming"
		}
		detail.Connection.Mode = domaincluster.ConnectionModeDirectKubeconfig
		detail.Connection.UsesInformerCache = true
		if contextName, _ := connection.Metadata["context"].(string); strings.TrimSpace(contextName) != "" {
			detail.Connection.Context = contextName
		}
		if kubeconfig, _ := connection.Metadata["kubeconfig"].(string); strings.TrimSpace(kubeconfig) != "" {
			detail.Connection.HasInlineKubeconfig = true
		}
	}

	if detail.Diagnostics.ConnectionState == "" {
		detail.Diagnostics.ConnectionState = "unknown"
	}

	return detail, nil
}

func (s *Service) CapabilityMatrix(context.Context, domainidentity.Principal) ([]domaincluster.CapabilityMatrixEntry, error) {
	return domaincluster.DefaultCapabilityMatrix(), nil
}

func (s *Service) Register(ctx context.Context, principal domainidentity.Principal, input domaincluster.RegisterInput) (domaincluster.Summary, error) {
	if s.repo == nil {
		return domaincluster.Summary{}, fmt.Errorf("%w: cluster repository is required", apperrors.ErrInvalidArgument)
	}
	connection, cfg, err := s.buildConnection(input)
	if err != nil {
		return domaincluster.Summary{}, err
	}
	if err := s.authorize(ctx, principal, connection.Summary, domainaccess.ActionUpdate); err != nil {
		return domaincluster.Summary{}, err
	}
	if cfg != nil {
		if err := s.manager.ValidateCluster(*cfg); err != nil {
			return domaincluster.Summary{}, fmt.Errorf("%w: invalid kubeconfig: %v", apperrors.ErrInvalidArgument, err)
		}
	}
	if err := s.repo.UpsertRegistration(ctx, connection); err != nil {
		return domaincluster.Summary{}, fmt.Errorf("persist cluster registration: %w", err)
	}
	if cfg != nil {
		s.manager.RegisterCluster(*cfg)
		if s.cache != nil {
			_ = s.cache.RegisterCluster(ctx, connection.Summary.ID)
		}
	}
	if err := s.syncOne(ctx, connection.Summary.ID); err != nil {
		// keep the registration even if the target cluster or agent is temporarily unreachable.
	}
	item, err := s.repo.Get(ctx, connection.Summary.ID)
	if err != nil {
		return domaincluster.Summary{}, err
	}
	if err := s.recordAudit(ctx, principal, connection.Summary.ID, "Cluster", connection.Summary.Name, string(domainaccess.ActionUpdate), "success", "registered cluster connection"); err != nil {
		return domaincluster.Summary{}, fmt.Errorf("record cluster registration audit: %w", err)
	}
	s.recordOperation(ctx, principal, "platform.cluster.register", connection.Summary.ID, connection.Summary.Name, "registered cluster connection")
	return item, nil
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, clusterID string, input domaincluster.UpdateInput) (domaincluster.Summary, error) {
	if s.repo == nil {
		return domaincluster.Summary{}, fmt.Errorf("%w: cluster repository is required", apperrors.ErrInvalidArgument)
	}
	existing, err := s.repo.GetConnection(ctx, clusterID)
	if err != nil {
		return domaincluster.Summary{}, err
	}
	if err := s.authorize(ctx, principal, existing.Summary, domainaccess.ActionUpdate); err != nil {
		return domaincluster.Summary{}, err
	}
	registerInput := domaincluster.RegisterInput{
		ID:                     clusterID,
		Name:                   input.Name,
		Region:                 input.Region,
		Environment:            input.Environment,
		Labels:                 input.Labels,
		ConnectionMode:         input.ConnectionMode,
		Kubeconfig:             input.Kubeconfig,
		Context:                input.Context,
		AgentEndpoint:          input.AgentEndpoint,
		AgentToken:             input.AgentToken,
		PrometheusBaseURL:      input.PrometheusBaseURL,
		PrometheusBearerToken:  input.PrometheusBearerToken,
		PrometheusClusterLabel: input.PrometheusClusterLabel,
		GrafanaBaseURL:         input.GrafanaBaseURL,
	}
	registerInput = mergeClusterUpdateInput(existing, registerInput)
	connection, cfg, err := s.buildConnection(registerInput)
	if err != nil {
		return domaincluster.Summary{}, err
	}
	if cfg != nil {
		if err := s.manager.ValidateCluster(*cfg); err != nil {
			return domaincluster.Summary{}, fmt.Errorf("%w: invalid kubeconfig: %v", apperrors.ErrInvalidArgument, err)
		}
	}
	s.manager.UnregisterCluster(clusterID)
	if s.cache != nil {
		s.cache.UnregisterCluster(clusterID)
	}
	if err := s.repo.UpdateRegistration(ctx, connection); err != nil {
		return domaincluster.Summary{}, fmt.Errorf("update cluster registration: %w", err)
	}
	if cfg != nil {
		s.manager.RegisterCluster(*cfg)
		if s.cache != nil {
			_ = s.cache.RegisterCluster(ctx, connection.Summary.ID)
		}
	}
	if err := s.syncOne(ctx, connection.Summary.ID); err != nil {
		// keep the registration even if the target cluster or agent is temporarily unreachable.
	}
	item, err := s.repo.Get(ctx, connection.Summary.ID)
	if err != nil {
		return domaincluster.Summary{}, err
	}
	if err := s.recordAudit(ctx, principal, connection.Summary.ID, "Cluster", connection.Summary.Name, string(domainaccess.ActionUpdate), "success", "updated cluster connection"); err != nil {
		return domaincluster.Summary{}, fmt.Errorf("record cluster update audit: %w", err)
	}
	s.recordOperation(ctx, principal, "platform.cluster.update", connection.Summary.ID, connection.Summary.Name, "updated cluster connection")
	return item, nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, clusterID string) error {
	if s.repo == nil {
		return fmt.Errorf("%w: cluster repository is required", apperrors.ErrInvalidArgument)
	}
	summary, err := s.repo.Get(ctx, clusterID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, principal, summary, domainaccess.ActionDelete); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, clusterID); err != nil {
		return err
	}
	s.manager.UnregisterCluster(clusterID)
	if s.cache != nil {
		s.cache.UnregisterCluster(clusterID)
	}
	if err := s.recordAudit(ctx, principal, clusterID, "Cluster", summary.Name, string(domainaccess.ActionDelete), "success", "deleted cluster connection"); err != nil {
		return fmt.Errorf("record cluster delete audit: %w", err)
	}
	s.recordOperation(ctx, principal, "platform.cluster.delete", clusterID, summary.Name, "deleted cluster connection")
	return nil
}

func (s *Service) syncAll(ctx context.Context) (int, error) {
	if s.repo == nil {
		return 0, nil
	}
	connections, err := s.repo.ListConnections(ctx)
	if err != nil {
		return 0, err
	}
	if len(connections) == 0 {
		ids := s.manager.ClusterIDs()
		return len(ids), s.runSyncJobs(ctx, len(ids), func(jobIndex int) error {
			return s.syncOne(ctx, ids[jobIndex])
		})
	}
	return len(connections), s.runSyncJobs(ctx, len(connections), func(jobIndex int) error {
		return s.syncConnection(ctx, connections[jobIndex])
	})
}

func (s *Service) runSyncCycle(ctx context.Context, operationID string) {
	startedAt := time.Now()
	if s.metrics != nil {
		s.metrics.RecordStart(runtimeobs.ComponentClusterSync, operationID, 0, 0)
	}
	itemCount, err := s.syncAll(ctx)
	if s.metrics != nil {
		outcome := runtimeobs.OutcomeSucceeded
		if err != nil {
			outcome = runtimeobs.OutcomeFailed
		}
		s.metrics.RecordFinish(runtimeobs.ComponentClusterSync, operationID, time.Since(startedAt), 0, itemCount, outcome, err)
	}
	if err != nil {
		s.logWarn("cluster sync failed", zap.String("operation", operationID), zap.Int("items", itemCount), zap.Duration("duration", time.Since(startedAt)), zap.Error(err))
		return
	}
	s.logDebug("cluster sync completed", zap.String("operation", operationID), zap.Int("items", itemCount), zap.Duration("duration", time.Since(startedAt)))
}

func (s *Service) runSyncJobs(ctx context.Context, total int, run func(jobIndex int) error) error {
	if total == 0 {
		return nil
	}

	workerCount := s.syncLimit
	if workerCount <= 0 {
		workerCount = defaultSyncConcurrency
	}
	if workerCount > total {
		workerCount = total
	}

	jobs := make(chan int)
	errCh := make(chan error, total)
	var wait sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for jobIndex := range jobs {
				if err := ctx.Err(); err != nil {
					errCh <- err
					return
				}
				if err := run(jobIndex); err != nil {
					errCh <- err
				}
			}
		}()
	}

	for i := 0; i < total; i++ {
		select {
		case <-ctx.Done():
			close(jobs)
			wait.Wait()
			return ctx.Err()
		case jobs <- i:
		}
	}
	close(jobs)
	wait.Wait()
	close(errCh)

	var firstErr error
	for err := range errCh {
		if err == nil {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Service) logWarn(message string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Warn(message, fields...)
	}
}

func (s *Service) logDebug(message string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Debug(message, fields...)
	}
}

func (s *Service) syncOne(ctx context.Context, clusterID string) error {
	if s.repo == nil {
		return nil
	}
	connection, err := s.repo.GetConnection(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("load cluster %s connection: %w", clusterID, err)
	}
	return s.syncConnection(ctx, connection)
}

func (s *Service) syncConnection(ctx context.Context, connection domaincluster.Connection) error {
	inspectCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	summary := connection.Summary
	summary.Health = domaincluster.Health{Status: "unknown", LastChecked: time.Now().UTC()}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		if s.agents == nil {
			summary.Health = domaincluster.Health{Status: "degraded", Message: "agent registry is not configured", LastChecked: time.Now().UTC()}
			break
		}
		client, err := s.agents.ClientFor(connection)
		if err != nil {
			summary.Health = domaincluster.Health{Status: "degraded", Message: err.Error(), LastChecked: time.Now().UTC()}
			break
		}
		observed, err := client.GetSummary(inspectCtx)
		if err != nil {
			summary.Health = domaincluster.Health{Status: "degraded", Message: err.Error(), LastChecked: time.Now().UTC()}
			break
		}
		summary.Version = observed.Version
		summary.Capabilities = observed.Capabilities
		if observed.Health.Status != "" {
			summary.Health = observed.Health
		} else {
			summary.Health = domaincluster.Health{Status: "healthy", LastChecked: time.Now().UTC()}
		}
	case domaincluster.ConnectionModeDirectKubeconfig, "":
		cfg, cfgErr := runtimeClusterConfig(connection)
		if cfgErr != nil {
			summary.Health = domaincluster.Health{Status: "degraded", Message: cfgErr.Error(), LastChecked: time.Now().UTC()}
			break
		}
		s.manager.RegisterCluster(*cfg)
		if s.cache != nil {
			_ = s.cache.RegisterCluster(ctx, connection.Summary.ID)
		}
		observed, err := s.manager.GetCluster(inspectCtx, connection.Summary.ID)
		if err != nil {
			summary.Health = domaincluster.Health{Status: "degraded", Message: err.Error(), LastChecked: time.Now().UTC()}
			break
		}
		summary = observed
		summary.ConnectionMode = domaincluster.ConnectionModeDirectKubeconfig
	default:
		summary.Health = domaincluster.Health{Status: "degraded", Message: fmt.Sprintf("unsupported connection mode %s", connection.Summary.ConnectionMode), LastChecked: time.Now().UTC()}
	}
	if summary.ConnectionMode == "" {
		summary.ConnectionMode = connection.Summary.ConnectionMode
	}
	if summary.Name == "" {
		summary.Name = connection.Summary.Name
	}
	if summary.Region == "" {
		summary.Region = connection.Summary.Region
	}
	if summary.Environment == "" {
		summary.Environment = connection.Summary.Environment
	}
	if len(summary.Labels) == 0 {
		summary.Labels = connection.Summary.Labels
	}
	return s.repo.UpsertSnapshot(ctx, summary)
}

func runtimeClusterConfig(connection domaincluster.Connection) (*cfgpkg.ClusterConfig, error) {
	kubeconfigData, _ := connection.Metadata["kubeconfig_data"].(string)
	kubeconfigValue, _ := connection.Metadata["kubeconfig"].(string)
	contextName, _ := connection.Metadata["context"].(string)
	kubeconfigPath := ""

	switch strings.TrimSpace(connection.SourceType) {
	case "api":
		if strings.TrimSpace(kubeconfigData) == "" {
			kubeconfigData = kubeconfigValue
		}
	default:
		kubeconfigPath = kubeconfigValue
		if strings.TrimSpace(kubeconfigData) == "" && strings.Contains(kubeconfigValue, "\n") {
			kubeconfigData = kubeconfigValue
			kubeconfigPath = ""
		}
	}

	if strings.TrimSpace(kubeconfigData) == "" && strings.TrimSpace(kubeconfigPath) == "" {
		return nil, fmt.Errorf("cluster %s has no kubeconfig metadata", connection.Summary.ID)
	}

	return &cfgpkg.ClusterConfig{
		ID:                     connection.Summary.ID,
		Name:                   connection.Summary.Name,
		Kubeconfig:             strings.TrimSpace(kubeconfigPath),
		KubeconfigData:         strings.TrimSpace(kubeconfigData),
		Context:                strings.TrimSpace(contextName),
		Region:                 connection.Summary.Region,
		Environment:            connection.Summary.Environment,
		Labels:                 connection.Summary.Labels,
		PrometheusURL:          strings.TrimSpace(metadataString(connection.Metadata, "prometheus_url")),
		PrometheusBearerToken:  strings.TrimSpace(metadataString(connection.Metadata, "prometheus_bearer_token")),
		PrometheusClusterLabel: strings.TrimSpace(metadataString(connection.Metadata, "prometheus_cluster_label")),
		GrafanaBaseURL:         strings.TrimSpace(metadataString(connection.Metadata, "grafana_base_url")),
	}, nil
}

func (s *Service) buildConnection(input domaincluster.RegisterInput) (domaincluster.Connection, *cfgpkg.ClusterConfig, error) {
	if strings.TrimSpace(input.Name) == "" {
		return domaincluster.Connection{}, nil, fmt.Errorf("%w: cluster name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.ID) == "" {
		input.ID = uuid.NewString()
	}
	mode := input.ConnectionMode
	if mode == "" {
		mode = domaincluster.ConnectionModeDirectKubeconfig
	}
	connection := domaincluster.Connection{
		Summary: domaincluster.Summary{
			ID:             strings.TrimSpace(input.ID),
			Name:           strings.TrimSpace(input.Name),
			Region:         strings.TrimSpace(input.Region),
			Environment:    strings.TrimSpace(input.Environment),
			Labels:         input.Labels,
			ConnectionMode: mode,
			Health:         domaincluster.Health{Status: "unknown", LastChecked: time.Now().UTC()},
		},
	}
	if connection.Summary.Labels == nil {
		connection.Summary.Labels = map[string]string{}
	}

	switch mode {
	case domaincluster.ConnectionModeDirectKubeconfig:
		if strings.TrimSpace(input.Kubeconfig) == "" {
			return domaincluster.Connection{}, nil, fmt.Errorf("%w: kubeconfig is required for direct connection mode", apperrors.ErrInvalidArgument)
		}
		connection.CredentialType = "kubeconfig"
		connection.SourceType = "api"
		connection.SourceRef = "cluster.register"
		connection.Metadata = map[string]any{
			"kubeconfig":               input.Kubeconfig,
			"context":                  input.Context,
			"prometheus_url":           strings.TrimSpace(input.PrometheusBaseURL),
			"prometheus_bearer_token":  strings.TrimSpace(input.PrometheusBearerToken),
			"prometheus_cluster_label": strings.TrimSpace(input.PrometheusClusterLabel),
			"grafana_base_url":         strings.TrimSpace(input.GrafanaBaseURL),
		}
		cfg := &cfgpkg.ClusterConfig{
			ID:                     connection.Summary.ID,
			Name:                   connection.Summary.Name,
			KubeconfigData:         input.Kubeconfig,
			Context:                input.Context,
			Region:                 connection.Summary.Region,
			Environment:            connection.Summary.Environment,
			Labels:                 connection.Summary.Labels,
			PrometheusURL:          strings.TrimSpace(input.PrometheusBaseURL),
			PrometheusBearerToken:  strings.TrimSpace(input.PrometheusBearerToken),
			PrometheusClusterLabel: strings.TrimSpace(input.PrometheusClusterLabel),
			GrafanaBaseURL:         strings.TrimSpace(input.GrafanaBaseURL),
		}
		return connection, cfg, nil
	case domaincluster.ConnectionModeAgent:
		if strings.TrimSpace(input.AgentEndpoint) == "" {
			return domaincluster.Connection{}, nil, fmt.Errorf("%w: agentEndpoint is required for agent connection mode", apperrors.ErrInvalidArgument)
		}
		connection.CredentialType = "bearer"
		connection.SourceType = "agent"
		connection.SourceRef = strings.TrimSpace(input.AgentEndpoint)
		connection.Metadata = map[string]any{
			"endpoint":                 strings.TrimSpace(input.AgentEndpoint),
			"token":                    strings.TrimSpace(input.AgentToken),
			"prometheus_url":           strings.TrimSpace(input.PrometheusBaseURL),
			"prometheus_bearer_token":  strings.TrimSpace(input.PrometheusBearerToken),
			"prometheus_cluster_label": strings.TrimSpace(input.PrometheusClusterLabel),
			"grafana_base_url":         strings.TrimSpace(input.GrafanaBaseURL),
		}
		return connection, nil, nil
	default:
		return domaincluster.Connection{}, nil, fmt.Errorf("%w: unsupported connection mode %s", apperrors.ErrInvalidArgument, mode)
	}
}

func mergeClusterUpdateInput(existing domaincluster.Connection, next domaincluster.RegisterInput) domaincluster.RegisterInput {
	if next.ConnectionMode == "" {
		next.ConnectionMode = existing.Summary.ConnectionMode
	}
	if next.Labels == nil {
		next.Labels = existing.Summary.Labels
	}

	switch next.ConnectionMode {
	case domaincluster.ConnectionModeDirectKubeconfig:
		if strings.TrimSpace(next.Kubeconfig) == "" && existing.Summary.ConnectionMode == domaincluster.ConnectionModeDirectKubeconfig {
			if kubeconfig, _ := existing.Metadata["kubeconfig"].(string); strings.TrimSpace(kubeconfig) != "" {
				next.Kubeconfig = kubeconfig
			}
		}
		if strings.TrimSpace(next.Context) == "" && existing.Summary.ConnectionMode == domaincluster.ConnectionModeDirectKubeconfig {
			if contextName, _ := existing.Metadata["context"].(string); strings.TrimSpace(contextName) != "" {
				next.Context = contextName
			}
		}
	case domaincluster.ConnectionModeAgent:
		if strings.TrimSpace(next.AgentEndpoint) == "" && existing.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
			if endpoint, _ := existing.Metadata["endpoint"].(string); strings.TrimSpace(endpoint) != "" {
				next.AgentEndpoint = endpoint
			}
		}
		if strings.TrimSpace(next.AgentToken) == "" && existing.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
			if token, _ := existing.Metadata["token"].(string); strings.TrimSpace(token) != "" {
				next.AgentToken = token
			}
		}
	}
	if strings.TrimSpace(next.PrometheusBearerToken) == "" {
		if token, _ := existing.Metadata["prometheus_bearer_token"].(string); strings.TrimSpace(token) != "" {
			next.PrometheusBearerToken = token
		}
	}
	if strings.TrimSpace(next.PrometheusClusterLabel) == "" {
		if label, _ := existing.Metadata["prometheus_cluster_label"].(string); strings.TrimSpace(label) != "" {
			next.PrometheusClusterLabel = label
		}
	}
	if strings.TrimSpace(next.GrafanaBaseURL) == "" {
		if baseURL, _ := existing.Metadata["grafana_base_url"].(string); strings.TrimSpace(baseURL) != "" {
			next.GrafanaBaseURL = baseURL
		}
	}

	return next
}

func metadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return value
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, summary domaincluster.Summary, action domainaccess.Action) error {
	if s.authorizer == nil {
		return nil
	}
	decision, err := s.authorizer.Authorize(ctx, domainaccess.Request{
		Principal: principal,
		Action:    action,
		Subject: domainaccess.SubjectAttributes{
			UserID:   principal.UserID,
			Roles:    principal.Roles,
			Teams:    principal.Teams,
			Projects: principal.Projects,
			Tags:     principal.Tags,
		},
		Cluster: domainaccess.ClusterAttributes{
			ClusterID:   summary.ID,
			Region:      summary.Region,
			Environment: summary.Environment,
			Labels:      summary.Labels,
		},
		Resource: domainaccess.ResourceAttributes{Kind: "Cluster", Name: summary.Name},
		Context: domainaccess.ContextAttributes{
			Source:     requestctx.FromContext(ctx).Source,
			OccurredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return nil
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, clusterID, kind, name, action, result, summary string) error {
	if s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ClusterID:     clusterID,
		ResourceKind:  kind,
		ResourceName:  name,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"source": meta.Source,
		},
	})
}

func (s *Service) recordOperation(ctx context.Context, principal domainidentity.Principal, operationType, targetID, targetLabel, summary string) {
	if s.operations == nil {
		return
	}
	_ = s.operations.Record(ctx, operationentry.New(
		ctx,
		principal,
		operationType,
		map[string]any{
			"module":       "platform",
			"resourceKind": "Cluster",
			"targetId":     targetID,
			"targetLabel":  targetLabel,
			"clusterId":    targetID,
		},
		"success",
		summary,
		map[string]any{
			"clusterId": targetID,
		},
	))
}
