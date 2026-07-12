package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	execution "github.com/opensoha/soha/internal/application/execution"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type ReleaseRepository interface {
	List(context.Context, domainrelease.Filter) ([]domainrelease.Record, error)
	Get(context.Context, string) (domainrelease.Record, error)
	Create(context.Context, domainrelease.Record) (domainrelease.Record, error)
}

type ApplicationReader interface {
	Get(context.Context, string) (domainapp.App, error)
}

type BindingReader interface {
	GetApplicationEnvironment(context.Context, string) (domaincatalog.ApplicationEnvironment, error)
}

type ConnectionResolver interface {
	GetConnection(context.Context, string) (domaincluster.Connection, error)
}

type EventWriter interface {
	Create(context.Context, domainevent.Envelope) error
}

type ExecutionPlane interface {
	StartReleaseExecution(context.Context, execution.ReleasePlan) (domaindelivery.ReleaseBundle, domaindelivery.ExecutionTask, error)
	CompleteReleaseExecution(context.Context, string, string, string, map[string]any) error
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type DirectRuntime interface {
	Metadata(string) (domaincluster.Summary, error)
	UpdateDeploymentImage(context.Context, string, string, string, string, string) (string, string, error)
}

type AgentDeploymentClient interface {
	UpdateDeploymentImage(context.Context, string, string, string, string) (string, string, error)
}

type AgentClientFactory func(domaincluster.Connection) (AgentDeploymentClient, error)

type Service struct {
	repo        ReleaseRepository
	apps        ApplicationReader
	bindings    BindingReader
	resolver    ConnectionResolver
	execution   ExecutionPlane
	authorizer  domainaccess.Authorizer
	permissions *appaccess.PermissionResolver
	events      EventWriter
	audit       AuditRecorder
	operations  OperationRecorder
	direct      DirectRuntime
	agents      AgentClientFactory
}

type releasePruner interface {
	DeleteByIDs(context.Context, []string) error
}

func New(repo ReleaseRepository, apps ApplicationReader, bindings BindingReader, resolver ConnectionResolver, execution ExecutionPlane, authorizer domainaccess.Authorizer, permissions *appaccess.PermissionResolver, events EventWriter, audit AuditRecorder, operations OperationRecorder, direct DirectRuntime, agents AgentClientFactory) *Service {
	return &Service{repo: repo, apps: apps, bindings: bindings, resolver: resolver, execution: execution, authorizer: authorizer, permissions: permissions, events: events, audit: audit, operations: operations, direct: direct, agents: agents}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter domainrelease.Filter) ([]domainrelease.Record, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryReleasesView); err != nil {
		return nil, err
	}
	if strings.TrimSpace(filter.ClusterID) != "" {
		businessLineID, applicationGroup := s.lookupApplicationScope(ctx, filter.ApplicationID)
		if err := s.authorize(ctx, principal, filter.ClusterID, "", "Release", filter.ApplicationID, businessLineID, applicationGroup, filter.ApplicationID, domainaccess.ActionList); err != nil {
			return nil, err
		}
	}

	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	allowed := make([]domainrelease.Record, 0, len(items))
	staleIDs := make([]string, 0)
	for _, item := range items {
		if strings.TrimSpace(item.ClusterID) == "" {
			staleIDs = append(staleIDs, item.ID)
			continue
		}
		exists, checkErr := s.applicationExists(ctx, item.ApplicationID)
		if checkErr != nil {
			continue
		}
		if !exists {
			staleIDs = append(staleIDs, item.ID)
			continue
		}
		if _, checkErr := s.loadConnection(ctx, item.ClusterID); checkErr != nil {
			if errors.Is(checkErr, apperrors.ErrNotFound) {
				staleIDs = append(staleIDs, item.ID)
			}
			continue
		}
		app, appErr := s.apps.Get(ctx, item.ApplicationID)
		if appErr != nil {
			continue
		}
		if err := s.authorize(ctx, principal, item.ClusterID, item.Namespace, "Release", item.DeploymentName, app.BusinessLineID, app.Group, item.ApplicationID, domainaccess.ActionList); err != nil {
			continue
		}
		allowed = append(allowed, item)
	}
	s.pruneReleaseRecords(ctx, staleIDs)
	return allowed, nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, releaseID string) (domainrelease.Record, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryReleasesView); err != nil {
		return domainrelease.Record{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(releaseID))
	if err != nil {
		return domainrelease.Record{}, err
	}
	app, appErr := s.apps.Get(ctx, item.ApplicationID)
	if appErr != nil {
		return domainrelease.Record{}, normalizeApplicationError(appErr)
	}
	if err := s.authorize(ctx, principal, item.ClusterID, item.Namespace, "Release", item.DeploymentName, app.BusinessLineID, app.Group, item.ApplicationID, domainaccess.ActionView); err != nil {
		return domainrelease.Record{}, err
	}
	return item, nil
}

func (s *Service) Trigger(ctx context.Context, principal domainidentity.Principal, input domainrelease.TriggerInput) (domainrelease.Record, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryReleasesTrigger); err != nil {
		return domainrelease.Record{}, err
	}
	if err := validateReleaseTriggerInput(input); err != nil {
		return domainrelease.Record{}, err
	}

	prepared, err := s.prepareReleaseTrigger(ctx, principal, input)
	if err != nil {
		return domainrelease.Record{}, err
	}
	app := prepared.app
	target := prepared.target
	connection := prepared.connection

	resolvedImage, err := resolveImage(app, input)
	if err != nil {
		return domainrelease.Record{}, err
	}
	bundleID := strings.TrimSpace(input.ReleaseBundleID)
	providerKind := resolveReleaseProviderKind(target)
	targetKind := resolveReleaseTargetKind(target)
	bundleID, taskID := s.startReleaseExecution(ctx, app, input, target, resolvedImage, bundleID, providerKind, targetKind)
	if !shouldApplyReleaseDirectly(target) {
		return s.createQueuedRelease(ctx, principal, app, connection, target, input, resolvedImage, bundleID, taskID, providerKind, targetKind)
	}
	applied, err := s.applyAndPersistDirectRelease(ctx, app, connection, input, resolvedImage, bundleID, taskID)
	if err != nil {
		return domainrelease.Record{}, err
	}
	if applied.applyErr != nil {
		s.recordFailedReleaseExecution(ctx, principal, connection, input, resolvedImage, bundleID, taskID, applied.applyErr)
		return domainrelease.Record{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, applied.applyErr)
	}
	s.completeReleaseExecution(ctx, connection, input, resolvedImage, bundleID, taskID, applied)
	s.recordTriggeredRelease(ctx, principal, app, input, applied.record, resolvedImage)
	return applied.record, nil
}

type appliedDirectRelease struct {
	record        domainrelease.Record
	containerName string
	previousImage string
	applyErr      error
}

func (s *Service) applyAndPersistDirectRelease(ctx context.Context, app domainapp.App, connection domaincluster.Connection, input domainrelease.TriggerInput, resolvedImage, bundleID, taskID string) (appliedDirectRelease, error) {
	containerName, previousImage, applyErr := s.applyDeploymentImage(ctx, connection, input.Namespace, input.DeploymentName, input.ContainerName, resolvedImage)
	status := "deployed"
	if applyErr != nil {
		status = "failed"
	}
	now := time.Now().UTC()
	record := domainrelease.Record{
		ID:             fmt.Sprintf("release:%s:%d", input.ApplicationID, now.UnixNano()),
		ApplicationID:  input.ApplicationID,
		ClusterID:      connection.Summary.ID,
		Namespace:      input.Namespace,
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         status,
		Metadata: map[string]any{
			"applicationName":          app.Name,
			"applicationEnvironmentId": strings.TrimSpace(input.ApplicationEnvironmentID),
			"releaseBundleId":          bundleID,
			"executionTaskId":          taskID,
			"deploymentName":           input.DeploymentName,
			"releaseName":              fallbackReleaseName(input.ReleaseName, input.DeploymentName),
			"containerName":            containerName,
			"previousImage":            previousImage,
			"image":                    resolvedImage,
			"imageTag":                 strings.TrimSpace(input.ImageTag),
		},
		CreatedAt: now,
	}
	if status == "deployed" {
		record.DeployedAt = &now
	}
	record, saveErr := s.repo.Create(ctx, record)
	if saveErr != nil {
		return appliedDirectRelease{}, saveErr
	}
	return appliedDirectRelease{record: record, containerName: containerName, previousImage: previousImage, applyErr: applyErr}, nil
}

func (s *Service) recordFailedReleaseExecution(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, input domainrelease.TriggerInput, resolvedImage, bundleID, taskID string, applyErr error) {
	if s.execution != nil && bundleID != "" && taskID != "" {
		_ = s.execution.CompleteReleaseExecution(ctx, bundleID, taskID, "failed", map[string]any{"error": applyErr.Error(), "image": resolvedImage})
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, input.DeploymentName, string(domainaccess.ActionTrigger), "failure", applyErr.Error(), map[string]any{"image": resolvedImage})
}

func (s *Service) completeReleaseExecution(ctx context.Context, connection domaincluster.Connection, input domainrelease.TriggerInput, resolvedImage, bundleID, taskID string, applied appliedDirectRelease) {
	if s.execution != nil && bundleID != "" && taskID != "" {
		_ = s.execution.CompleteReleaseExecution(ctx, bundleID, taskID, "completed", map[string]any{
			"image":          resolvedImage,
			"previousImage":  applied.previousImage,
			"containerName":  applied.containerName,
			"clusterId":      connection.Summary.ID,
			"namespace":      input.Namespace,
			"deploymentName": input.DeploymentName,
			"recordId":       applied.record.ID,
		})
	}
}

func (s *Service) recordTriggeredRelease(ctx context.Context, principal domainidentity.Principal, app domainapp.App, input domainrelease.TriggerInput, record domainrelease.Record, resolvedImage string) {
	if s.events != nil {
		_ = s.events.Create(ctx, domainevent.Envelope{
			ID:        "event:" + record.ID,
			Source:    "release",
			Category:  "release",
			Severity:  "info",
			ClusterID: record.ClusterID,
			Namespace: record.Namespace,
			Summary:   fmt.Sprintf("Released %s to %s/%s", app.Name, record.ClusterID, record.Namespace),
			Payload: map[string]any{
				"releaseId":      record.ID,
				"applicationId":  record.ApplicationID,
				"deploymentName": record.DeploymentName,
				"image":          resolvedImage,
			},
		})
	}
	_ = s.recordAudit(ctx, principal, record.ClusterID, input.Namespace, input.DeploymentName, string(domainaccess.ActionTrigger), "success", "triggered deployment release", map[string]any{"image": resolvedImage})
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			"delivery.release.trigger",
			map[string]any{
				"module":       "delivery",
				"clusterId":    record.ClusterID,
				"namespace":    record.Namespace,
				"resourceKind": "Release",
				"resourceName": record.DeploymentName,
				"targetId":     record.ID,
				"targetLabel":  app.Name,
			},
			"success",
			"triggered deployment release",
			map[string]any{
				"releaseId":      record.ID,
				"applicationId":  record.ApplicationID,
				"deploymentName": record.DeploymentName,
				"image":          resolvedImage,
			},
		))
	}
}

type preparedReleaseTrigger struct {
	app        domainapp.App
	target     domaincatalog.ReleaseTarget
	connection domaincluster.Connection
}

func (s *Service) prepareReleaseTrigger(ctx context.Context, principal domainidentity.Principal, input domainrelease.TriggerInput) (preparedReleaseTrigger, error) {
	app, err := s.apps.Get(ctx, input.ApplicationID)
	if err != nil {
		return preparedReleaseTrigger{}, normalizeApplicationError(err)
	}
	target, err := s.resolveTarget(ctx, input)
	if err != nil {
		return preparedReleaseTrigger{}, err
	}
	connection, err := s.loadConnection(ctx, input.ClusterID)
	if err != nil {
		return preparedReleaseTrigger{}, err
	}
	resourceKind := "Deployment"
	if strings.TrimSpace(target.WorkloadKind) != "" {
		resourceKind = strings.TrimSpace(target.WorkloadKind)
	}
	if err := s.authorize(ctx, principal, connection.Summary.ID, input.Namespace, resourceKind, input.DeploymentName, app.BusinessLineID, app.Group, input.ApplicationID, domainaccess.ActionTrigger); err != nil {
		return preparedReleaseTrigger{}, err
	}
	return preparedReleaseTrigger{app: app, target: target, connection: connection}, nil
}

func validateReleaseTriggerInput(input domainrelease.TriggerInput) error {
	switch {
	case strings.TrimSpace(input.ApplicationID) == "":
		return fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	case strings.TrimSpace(input.ClusterID) == "":
		return fmt.Errorf("%w: clusterId is required", apperrors.ErrInvalidArgument)
	case strings.TrimSpace(input.Namespace) == "":
		return fmt.Errorf("%w: namespace is required", apperrors.ErrInvalidArgument)
	case strings.TrimSpace(input.DeploymentName) == "":
		return fmt.Errorf("%w: deploymentName is required", apperrors.ErrInvalidArgument)
	default:
		return nil
	}
}

func (s *Service) startReleaseExecution(ctx context.Context, app domainapp.App, input domainrelease.TriggerInput, target domaincatalog.ReleaseTarget, image, bundleID, providerKind, targetKind string) (string, string) {
	if s.execution == nil {
		return bundleID, ""
	}
	bundle, task, err := s.execution.StartReleaseExecution(ctx, execution.ReleasePlan{
		ApplicationID:            app.ID,
		ApplicationEnvironmentID: strings.TrimSpace(input.ApplicationEnvironmentID),
		ReleaseBundleID:          bundleID,
		Version:                  firstNonEmpty(strings.TrimSpace(input.ReleaseName), strings.TrimSpace(input.ImageTag)),
		SourceType:               firstNonEmpty(strings.TrimSpace(input.ActionKind), "release"),
		ProviderKind:             providerKind,
		TargetKind:               targetKind,
		ArtifactRef:              image,
		Metadata:                 buildReleaseExecutionMetadata(app, input, target, image),
	})
	if err != nil {
		return bundleID, ""
	}
	return bundle.ID, task.ID
}

func (s *Service) createQueuedRelease(ctx context.Context, principal domainidentity.Principal, app domainapp.App, connection domaincluster.Connection, target domaincatalog.ReleaseTarget, input domainrelease.TriggerInput, image, bundleID, taskID, providerKind, targetKind string) (domainrelease.Record, error) {
	now := time.Now().UTC()
	record := domainrelease.Record{
		ID:             fmt.Sprintf("release:%s:%d", input.ApplicationID, now.UnixNano()),
		ApplicationID:  input.ApplicationID,
		ClusterID:      connection.Summary.ID,
		Namespace:      input.Namespace,
		DeploymentName: strings.TrimSpace(input.DeploymentName),
		Status:         "queued",
		Metadata: map[string]any{
			"applicationName":          app.Name,
			"applicationEnvironmentId": strings.TrimSpace(input.ApplicationEnvironmentID),
			"releaseBundleId":          bundleID,
			"executionTaskId":          taskID,
			"clusterId":                input.ClusterID,
			"namespace":                input.Namespace,
			"targetKind":               targetKind,
			"executorKind":             providerKind,
			"deploymentName":           input.DeploymentName,
			"workloadKind":             target.WorkloadKind,
			"workloadName":             target.WorkloadName,
			"containerName":            input.ContainerName,
			"releaseName":              input.ReleaseName,
			"imageTag":                 strings.TrimSpace(input.ImageTag),
			"image":                    image,
		},
		CreatedAt: now,
	}
	record, err := s.repo.Create(ctx, record)
	if err != nil {
		return domainrelease.Record{}, err
	}
	if s.events != nil {
		_ = s.events.Create(ctx, domainevent.Envelope{
			ID:        "event:" + record.ID,
			Source:    "release",
			Category:  "release",
			Severity:  "info",
			ClusterID: record.ClusterID,
			Namespace: record.Namespace,
			Summary:   fmt.Sprintf("Queued %s release task for %s/%s", app.Name, record.ClusterID, record.Namespace),
			Payload: map[string]any{
				"releaseId":       record.ID,
				"applicationId":   record.ApplicationID,
				"executionTaskId": taskID,
				"releaseBundleId": bundleID,
			},
		})
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, input.DeploymentName, string(domainaccess.ActionTrigger), "success", "queued async release task", map[string]any{"image": image})
	return record, nil
}

func (s *Service) applyDeploymentImage(ctx context.Context, connection domaincluster.Connection, namespace, name, containerName, image string) (string, string, error) {
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return "", "", err
		}
		return client.UpdateDeploymentImage(ctx, namespace, name, containerName, image)
	default:
		if s.direct == nil {
			return "", "", fmt.Errorf("%w: direct cluster runtime is not configured", apperrors.ErrClusterUnready)
		}
		return s.direct.UpdateDeploymentImage(ctx, connection.Summary.ID, namespace, name, containerName, image)
	}
}

func resolveImage(app domainapp.App, input domainrelease.TriggerInput) (string, error) {
	if strings.TrimSpace(input.Image) != "" {
		return strings.TrimSpace(input.Image), nil
	}
	base := strings.TrimSpace(app.BuildImage)
	if base == "" {
		return "", fmt.Errorf("%w: image or application buildImage is required", apperrors.ErrInvalidArgument)
	}
	tag := strings.TrimSpace(input.ImageTag)
	if tag == "" {
		tag = strings.TrimSpace(app.DefaultTag)
	}
	if tag == "" {
		return "", fmt.Errorf("%w: imageTag or application defaultTag is required when image is not provided", apperrors.ErrInvalidArgument)
	}
	return fmt.Sprintf("%s:%s", base, tag), nil
}

func fallbackReleaseName(value, deploymentName string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fmt.Sprintf("%s-%d", strings.TrimSpace(deploymentName), time.Now().UTC().Unix())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func resolveReleaseTargetKind(target domaincatalog.ReleaseTarget) string {
	if strings.TrimSpace(target.TargetKind) == "" {
		return "k8s_workload"
	}
	return strings.TrimSpace(target.TargetKind)
}

func resolveReleaseProviderKind(target domaincatalog.ReleaseTarget) string {
	if strings.TrimSpace(target.ExecutorKind) == "" {
		return "k8s_job_runner"
	}
	return strings.TrimSpace(target.ExecutorKind)
}

func shouldApplyReleaseDirectly(target domaincatalog.ReleaseTarget) bool {
	return resolveReleaseTargetKind(target) == "k8s_workload" && resolveReleaseProviderKind(target) == "k8s_job_runner"
}

func buildReleaseExecutionMetadata(app domainapp.App, input domainrelease.TriggerInput, target domaincatalog.ReleaseTarget, image string) map[string]any {
	metadata := map[string]any{
		"clusterId":      input.ClusterID,
		"namespace":      input.Namespace,
		"targetKind":     resolveReleaseTargetKind(target),
		"executorKind":   resolveReleaseProviderKind(target),
		"deploymentName": input.DeploymentName,
		"workloadKind":   target.WorkloadKind,
		"workloadName":   target.WorkloadName,
		"containerName":  input.ContainerName,
		"releaseName":    input.ReleaseName,
		"imageTag":       input.ImageTag,
		"image":          image,
	}
	if len(target.Metadata) > 0 {
		metadata["targetMetadata"] = target.Metadata
	}
	if workspace := buildReleaseExecutionWorkspace(app, input, target); len(workspace) > 0 {
		metadata["workspace"] = workspace
	}
	metadata["runtime"] = buildReleaseExecutionRuntime(target)
	commands := buildReleaseExecutionCommands(target, image)
	if len(commands) > 0 {
		metadata["commands"] = commands
	}
	return metadata
}

func buildReleaseExecutionRuntime(target domaincatalog.ReleaseTarget) map[string]any {
	runtime := map[string]any{}
	if value := strings.TrimSpace(fmt.Sprint(target.Metadata["runtimeImage"])); value != "" {
		runtime["image"] = value
	}
	switch resolveReleaseTargetKind(target) {
	case "helm_release":
		if strings.TrimSpace(fmt.Sprint(runtime["image"])) == "" {
			runtime["image"] = "alpine/helm:3.16.1"
		}
	case "kustomize_overlay", "k8s_workload":
		if strings.TrimSpace(fmt.Sprint(runtime["image"])) == "" {
			runtime["image"] = "bitnami/kubectl:1.31"
		}
	default:
		if strings.TrimSpace(fmt.Sprint(runtime["image"])) == "" {
			runtime["image"] = "alpine:3.20"
		}
	}
	return runtime
}

func buildReleaseExecutionWorkspace(app domainapp.App, input domainrelease.TriggerInput, target domaincatalog.ReleaseTarget) map[string]any {
	workspace := map[string]any{}
	workspacePath := firstNonEmpty(
		strings.TrimSpace(fmt.Sprint(target.Metadata["workspacePath"])),
		strings.TrimSpace(fmt.Sprint(target.Metadata["workspace"])),
		strings.TrimSpace(app.RepositoryPath),
		strings.TrimSpace(app.Key),
		strings.TrimSpace(app.ID),
	)
	if workspacePath != "" {
		workspace["path"] = workspacePath
	}
	if commandDir := firstNonEmpty(
		strings.TrimSpace(fmt.Sprint(target.Metadata["workingDir"])),
		strings.TrimSpace(fmt.Sprint(target.Metadata["commandDir"])),
	); commandDir != "" {
		workspace["commandDir"] = commandDir
	}
	if artifactFiles := valueAsStringSlice(target.Metadata["artifactFiles"]); len(artifactFiles) > 0 {
		workspace["artifactFiles"] = artifactFiles
	}
	checkoutEnabled := boolValue(target.Metadata["checkoutEnabled"], false)
	checkout := map[string]any{
		"enabled":        checkoutEnabled,
		"repositoryPath": strings.TrimSpace(app.RepositoryPath),
		"repositoryURL": firstNonEmpty(
			strings.TrimSpace(fmt.Sprint(target.Metadata["repositoryURL"])),
			strings.TrimSpace(fmt.Sprint(target.Metadata["repositoryUrl"])),
			strings.TrimSpace(fmt.Sprint(app.Metadata["repositoryURL"])),
			strings.TrimSpace(fmt.Sprint(app.Metadata["repositoryUrl"])),
		),
		"refType":       "branch",
		"refName":       firstNonEmpty(strings.TrimSpace(app.DefaultBranch), "main"),
		"defaultBranch": strings.TrimSpace(app.DefaultBranch),
	}
	if checkoutRaw, ok := target.Metadata["checkout"].(map[string]any); ok {
		checkout["enabled"] = boolValue(checkoutRaw["enabled"], checkoutEnabled)
		if value := strings.TrimSpace(fmt.Sprint(checkoutRaw["repositoryPath"])); value != "" {
			checkout["repositoryPath"] = value
		}
		if value := firstNonEmpty(strings.TrimSpace(fmt.Sprint(checkoutRaw["repositoryURL"])), strings.TrimSpace(fmt.Sprint(checkoutRaw["repositoryUrl"]))); value != "" {
			checkout["repositoryURL"] = value
		}
		if value := strings.TrimSpace(fmt.Sprint(checkoutRaw["refType"])); value != "" {
			checkout["refType"] = value
		}
		if value := strings.TrimSpace(fmt.Sprint(checkoutRaw["refName"])); value != "" {
			checkout["refName"] = value
		}
	}
	if boolValue(checkout["enabled"], false) || strings.TrimSpace(fmt.Sprint(checkout["repositoryURL"])) != "" || strings.TrimSpace(fmt.Sprint(checkout["repositoryPath"])) != "" {
		workspace["checkout"] = checkout
	}
	_ = input
	return workspace
}

func buildReleaseExecutionCommands(target domaincatalog.ReleaseTarget, image string) []string {
	if raw, ok := target.Metadata["commands"].([]any); ok && len(raw) > 0 {
		items := make([]string, 0, len(raw))
		for _, item := range raw {
			value := strings.TrimSpace(fmt.Sprint(item))
			if value != "" {
				value = strings.ReplaceAll(value, "{{IMAGE_REF}}", image)
				value = strings.ReplaceAll(value, "{{TARGET_NAME}}", target.WorkloadName)
				value = strings.ReplaceAll(value, "{{CONFIG_REF}}", target.ConfigRef)
				items = append(items, value)
			}
		}
		return items
	}
	switch resolveReleaseTargetKind(target) {
	case "host_service":
		serviceName := strings.TrimSpace(target.WorkloadName)
		if serviceName == "" {
			serviceName = "service"
		}
		unit := strings.TrimSpace(fmt.Sprint(target.Metadata["serviceUnit"]))
		if unit == "" {
			unit = serviceName
		}
		return []string{
			fmt.Sprintf("echo deploying %s with image %s", serviceName, image),
			fmt.Sprintf("systemctl restart %s", unit),
		}
	case "helm_release":
		releaseName := strings.TrimSpace(target.WorkloadName)
		if releaseName == "" {
			releaseName = "release"
		}
		chartRef := strings.TrimSpace(target.ConfigRef)
		if chartRef == "" {
			chartRef = "chart"
		}
		return []string{
			fmt.Sprintf("helm upgrade --install %s %s --set image.repository=%s", releaseName, chartRef, image),
		}
	case "kustomize_overlay":
		overlay := strings.TrimSpace(target.ConfigRef)
		if overlay == "" {
			overlay = "."
		}
		return []string{
			fmt.Sprintf("kustomize build %s > /tmp/%s.yaml", overlay, strings.ReplaceAll(target.WorkloadName, "/", "-")),
			fmt.Sprintf("kubectl apply -f /tmp/%s.yaml", strings.ReplaceAll(target.WorkloadName, "/", "-")),
		}
	default:
		return nil
	}
}

func valueAsStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	default:
		return nil
	}
}

func boolValue(raw any, fallback bool) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		switch strings.TrimSpace(strings.ToLower(value)) {
		case "true", "1", "yes", "y", "on":
			return true
		case "false", "0", "no", "n", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}

func (s *Service) resolveTarget(ctx context.Context, input domainrelease.TriggerInput) (domaincatalog.ReleaseTarget, error) {
	if strings.TrimSpace(input.ApplicationEnvironmentID) == "" {
		return domaincatalog.ReleaseTarget{TargetKind: "k8s_workload", ExecutorKind: "k8s_job_runner", WorkloadKind: "Deployment", WorkloadName: input.DeploymentName}, nil
	}
	if s.bindings == nil {
		return domaincatalog.ReleaseTarget{}, fmt.Errorf("%w: application environment binding reader is not configured", apperrors.ErrInvalidArgument)
	}
	binding, err := s.bindings.GetApplicationEnvironment(ctx, strings.TrimSpace(input.ApplicationEnvironmentID))
	if err != nil {
		return domaincatalog.ReleaseTarget{}, fmt.Errorf("%w: application environment binding not found", apperrors.ErrInvalidArgument)
	}
	for _, target := range binding.Targets {
		if !target.Enabled {
			continue
		}
		if target.ClusterID == strings.TrimSpace(input.ClusterID) &&
			target.Namespace == strings.TrimSpace(input.Namespace) &&
			target.WorkloadName == strings.TrimSpace(input.DeploymentName) {
			return target, nil
		}
	}
	if len(binding.Targets) > 0 {
		return binding.Targets[0], nil
	}
	return domaincatalog.ReleaseTarget{}, fmt.Errorf("%w: no enabled target is configured for application environment", apperrors.ErrInvalidArgument)
}

func (s *Service) agentClient(connection domaincluster.Connection) (AgentDeploymentClient, error) {
	if s.agents == nil {
		return nil, fmt.Errorf("%w: agent registry is not configured", apperrors.ErrClusterUnready)
	}
	client, err := s.agents(connection)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	return client, nil
}

func (s *Service) loadConnection(ctx context.Context, clusterID string) (domaincluster.Connection, error) {
	if s.resolver != nil {
		connection, err := s.resolver.GetConnection(ctx, clusterID)
		if err != nil {
			return domaincluster.Connection{}, normalizeClusterError(err)
		}
		return connection, nil
	}
	if s.direct == nil {
		return domaincluster.Connection{}, fmt.Errorf("%w: direct cluster runtime is not configured", apperrors.ErrClusterUnready)
	}
	summary, err := s.direct.Metadata(clusterID)
	if err != nil {
		return domaincluster.Connection{}, fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
	}
	return domaincluster.Connection{Summary: summary}, nil
}

func (s *Service) applicationExists(ctx context.Context, applicationID string) (bool, error) {
	if strings.TrimSpace(applicationID) == "" {
		return false, nil
	}
	_, err := s.apps.Get(ctx, applicationID)
	if err == nil {
		return true, nil
	}
	normalized := normalizeApplicationError(err)
	if errors.Is(normalized, apperrors.ErrNotFound) {
		return false, nil
	}
	return false, normalized
}

func (s *Service) lookupApplicationScope(ctx context.Context, applicationID string) (string, string) {
	if s.apps == nil || strings.TrimSpace(applicationID) == "" {
		return "", ""
	}
	app, err := s.apps.Get(ctx, strings.TrimSpace(applicationID))
	if err != nil {
		return "", ""
	}
	return app.BusinessLineID, app.Group
}

func (s *Service) pruneReleaseRecords(ctx context.Context, ids []string) {
	if len(ids) == 0 {
		return
	}
	pruner, ok := s.repo.(releasePruner)
	if !ok {
		return
	}
	_ = pruner.DeleteByIDs(ctx, uniqueStrings(ids))
}

func normalizeApplicationError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	return err
}

func normalizeClusterError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	return err
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, businessLineID, applicationGroup, applicationID string, action domainaccess.Action) error {
	if s.authorizer == nil {
		return nil
	}
	connection, err := s.loadConnection(ctx, clusterID)
	if err != nil {
		return err
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
			ClusterID:   connection.Summary.ID,
			Region:      connection.Summary.Region,
			Environment: connection.Summary.Environment,
			Labels:      connection.Summary.Labels,
		},
		Namespace: domainaccess.NamespaceAttributes{Namespace: namespace},
		Resource:  domainaccess.ResourceAttributes{Kind: kind, Name: name, Owner: name},
		Delivery: domainaccess.DeliveryAttributes{
			BusinessLineID:   strings.TrimSpace(businessLineID),
			ApplicationGroup: strings.TrimSpace(applicationGroup),
			EnvironmentKey:   strings.TrimSpace(connection.Summary.Environment),
			ApplicationID:    strings.TrimSpace(applicationID),
		},
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

func (s *Service) authorizePermission(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, deploymentName, action, result, summary string, metadata map[string]any) error {
	if s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ID:            uuid.NewString(),
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ClusterID:     clusterID,
		Namespace:     namespace,
		ResourceKind:  "Release",
		ResourceName:  deploymentName,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata:      metadata,
	})
}
