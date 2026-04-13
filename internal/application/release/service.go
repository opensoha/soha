package release

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainaudit "github.com/kubecrux/kubecrux/internal/domain/audit"
	domaincluster "github.com/kubecrux/kubecrux/internal/domain/cluster"
	domainevent "github.com/kubecrux/kubecrux/internal/domain/event"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	agentinfra "github.com/kubecrux/kubecrux/internal/infrastructure/agent"
	k8sinfra "github.com/kubecrux/kubecrux/internal/infrastructure/kubernetes"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"github.com/kubecrux/kubecrux/internal/platform/requestctx"
	apprepo "github.com/kubecrux/kubecrux/internal/repository/application"
	clusterrepo "github.com/kubecrux/kubecrux/internal/repository/cluster"
)

type ReleaseRepository interface {
	List(context.Context, domainrelease.Filter) ([]domainrelease.Record, error)
	Create(context.Context, domainrelease.Record) (domainrelease.Record, error)
}

type ApplicationReader interface {
	Get(context.Context, string) (domainapp.App, error)
}

type ConnectionResolver interface {
	GetConnection(context.Context, string) (domaincluster.Connection, error)
}

type EventWriter interface {
	Create(context.Context, domainevent.Envelope) error
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	repo       ReleaseRepository
	apps       ApplicationReader
	resolver   ConnectionResolver
	authorizer domainaccess.Authorizer
	events     EventWriter
	audit      AuditRecorder
	clusters   *k8sinfra.Manager
	agents     *agentinfra.Registry
}

type releasePruner interface {
	DeleteByIDs(context.Context, []string) error
}

func New(repo ReleaseRepository, apps ApplicationReader, resolver ConnectionResolver, authorizer domainaccess.Authorizer, events EventWriter, audit AuditRecorder, clusters *k8sinfra.Manager, agents *agentinfra.Registry) *Service {
	return &Service{repo: repo, apps: apps, resolver: resolver, authorizer: authorizer, events: events, audit: audit, clusters: clusters, agents: agents}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter domainrelease.Filter) ([]domainrelease.Record, error) {
	if strings.TrimSpace(filter.ClusterID) != "" {
		if err := s.authorize(ctx, principal, filter.ClusterID, "", "Release", filter.ApplicationID, "", filter.ApplicationID, domainaccess.ActionList); err != nil {
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
		if err := s.authorize(ctx, principal, item.ClusterID, item.Namespace, "Release", item.DeploymentName, app.BusinessLineID, item.ApplicationID, domainaccess.ActionList); err != nil {
			continue
		}
		allowed = append(allowed, item)
	}
	s.pruneReleaseRecords(ctx, staleIDs)
	return allowed, nil
}

func (s *Service) Trigger(ctx context.Context, principal domainidentity.Principal, input domainrelease.TriggerInput) (domainrelease.Record, error) {
	if strings.TrimSpace(input.ApplicationID) == "" {
		return domainrelease.Record{}, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.ClusterID) == "" {
		return domainrelease.Record{}, fmt.Errorf("%w: clusterId is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.Namespace) == "" {
		return domainrelease.Record{}, fmt.Errorf("%w: namespace is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.DeploymentName) == "" {
		return domainrelease.Record{}, fmt.Errorf("%w: deploymentName is required", apperrors.ErrInvalidArgument)
	}

	app, err := s.apps.Get(ctx, input.ApplicationID)
	if err != nil {
		return domainrelease.Record{}, normalizeApplicationError(err)
	}
	connection, err := s.loadConnection(ctx, input.ClusterID)
	if err != nil {
		return domainrelease.Record{}, err
	}
	if err := s.authorize(ctx, principal, connection.Summary.ID, input.Namespace, "Deployment", input.DeploymentName, app.BusinessLineID, input.ApplicationID, domainaccess.ActionUpdate); err != nil {
		return domainrelease.Record{}, err
	}

	resolvedImage, err := resolveImage(app, input)
	if err != nil {
		return domainrelease.Record{}, err
	}
	containerName, previousImage, err := s.applyDeploymentImage(ctx, connection, input.Namespace, input.DeploymentName, input.ContainerName, resolvedImage)
	status := "deployed"
	if err != nil {
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
			"applicationName": app.Name,
			"deploymentName":  input.DeploymentName,
			"releaseName":     fallbackReleaseName(input.ReleaseName, input.DeploymentName),
			"containerName":   containerName,
			"previousImage":   previousImage,
			"image":           resolvedImage,
			"imageTag":        strings.TrimSpace(input.ImageTag),
		},
		CreatedAt: now,
	}
	if status == "deployed" {
		record.DeployedAt = &now
	}
	record, saveErr := s.repo.Create(ctx, record)
	if saveErr != nil {
		return domainrelease.Record{}, saveErr
	}

	if err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, input.DeploymentName, string(domainaccess.ActionUpdate), "failure", err.Error(), map[string]any{"image": resolvedImage})
		return domainrelease.Record{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}

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
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, input.DeploymentName, string(domainaccess.ActionUpdate), "success", "triggered deployment release", map[string]any{"image": resolvedImage})
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
		return s.updateDirectDeploymentImage(ctx, connection.Summary.ID, namespace, name, containerName, image)
	}
}

func (s *Service) updateDirectDeploymentImage(ctx context.Context, clusterID, namespace, name, containerName, image string) (string, string, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	resolvedContainer, previousImage, err := mutateDeploymentImage(deployment, containerName, image)
	if err != nil {
		return "", "", err
	}
	if _, err := bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{}); err != nil {
		return "", "", err
	}
	return resolvedContainer, previousImage, nil
}

func mutateDeploymentImage(deployment *appsv1.Deployment, containerName, image string) (string, string, error) {
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return "", "", fmt.Errorf("deployment has no containers")
	}
	if strings.TrimSpace(containerName) == "" {
		previous := deployment.Spec.Template.Spec.Containers[0].Image
		deployment.Spec.Template.Spec.Containers[0].Image = image
		return deployment.Spec.Template.Spec.Containers[0].Name, previous, nil
	}
	for index := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[index].Name == containerName {
			previous := deployment.Spec.Template.Spec.Containers[index].Image
			deployment.Spec.Template.Spec.Containers[index].Image = image
			return deployment.Spec.Template.Spec.Containers[index].Name, previous, nil
		}
	}
	return "", "", fmt.Errorf("container %s not found in deployment", containerName)
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

func (s *Service) agentClient(connection domaincluster.Connection) (*agentinfra.Client, error) {
	if s.agents == nil {
		return nil, fmt.Errorf("%w: agent registry is not configured", apperrors.ErrClusterUnready)
	}
	client, err := s.agents.ClientFor(connection)
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
	summary, err := s.clusters.Metadata(clusterID)
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
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apprepo.ErrNotFound) {
		return fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
	}
	return err
}

func normalizeClusterError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, clusterrepo.ErrNotFound) {
		return fmt.Errorf("%w: %v", apperrors.ErrNotFound, err)
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

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, businessLineID, applicationID string, action domainaccess.Action) error {
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
			BusinessLineID: strings.TrimSpace(businessLineID),
			EnvironmentKey: strings.TrimSpace(connection.Summary.Environment),
			ApplicationID:  strings.TrimSpace(applicationID),
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
