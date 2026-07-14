package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	PermKnowledgeConnectorsView   = appaccess.PermAIKnowledgeConnectorsView
	PermKnowledgeConnectorsManage = appaccess.PermAIKnowledgeConnectorsManage
	PermKnowledgeIngestionOperate = appaccess.PermAIKnowledgeIngestionOperate
	defaultIngestionAttempts      = 3
)

func (s *Service) ListConnectors(
	ctx context.Context,
	principal domainidentity.Principal,
	limit int,
) ([]domainknowledge.ConnectorDefinition, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeConnectorsView); err != nil {
		return nil, err
	}
	if s.productionRepo == nil {
		return nil, fmt.Errorf("%w: production knowledge repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	return s.productionRepo.ListConnectors(ctx, principalScope(principal), boundedLimit(limit))
}

func (s *Service) validateConnectorInput(
	ctx context.Context,
	principal domainidentity.Principal,
	input domainknowledge.ConnectorInput,
) (domainknowledge.ConnectorValidationResult, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeConnectorsManage); err != nil {
		return domainknowledge.ConnectorValidationResult{}, err
	}
	if s.connectorValidator == nil {
		return domainknowledge.ConnectorValidationResult{}, fmt.Errorf("%w: connector validator is not configured", apperrors.ErrUnsupportedOperation)
	}
	return s.connectorValidator.Validate(ctx, input)
}

func (s *Service) ValidateConnector(
	ctx context.Context,
	principal domainidentity.Principal,
	connectorID string,
) (domainknowledge.ConnectorValidationResult, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeConnectorsManage); err != nil {
		return domainknowledge.ConnectorValidationResult{}, err
	}
	if s.productionRepo == nil || s.connectorValidator == nil {
		return domainknowledge.ConnectorValidationResult{}, fmt.Errorf("%w: connector validation is not configured", apperrors.ErrUnsupportedOperation)
	}
	connector, err := s.productionRepo.GetConnector(ctx, principalScope(principal), strings.TrimSpace(connectorID))
	if err != nil {
		return domainknowledge.ConnectorValidationResult{}, err
	}
	return s.connectorValidator.Validate(ctx, domainknowledge.ConnectorInput{
		KnowledgeBaseID: connector.KnowledgeBaseID,
		Name:            connector.Name,
		Kind:            connector.Kind,
		Version:         connector.Version,
		SecretRef:       connector.SecretRef,
		Config:          connector.Config,
		SyncPolicy:      connector.SyncPolicy,
	})
}

func (s *Service) CreateConnector(
	ctx context.Context,
	principal domainidentity.Principal,
	input domainknowledge.ConnectorInput,
) (domainknowledge.ConnectorDefinition, error) {
	version := strings.TrimSpace(input.Version)
	if version != "" && version != "v1" {
		return domainknowledge.ConnectorDefinition{}, fmt.Errorf("%w: connector version must be v1", apperrors.ErrInvalidArgument)
	}
	if _, err := s.validateConnectorInput(ctx, principal, input); err != nil {
		return domainknowledge.ConnectorDefinition{}, err
	}
	source, err := s.CreateSource(ctx, principal, input.KnowledgeBaseID, domainknowledge.SourceInput{
		Name:       input.Name,
		Kind:       input.Kind,
		ConfigRef:  input.SecretRef,
		Config:     input.Config,
		SyncPolicy: input.SyncPolicy,
	})
	if err != nil {
		return domainknowledge.ConnectorDefinition{}, err
	}
	return connectorDefinitionFromSource(source, "v1"), nil
}

func (s *Service) CreateIngestionJob(
	ctx context.Context,
	principal domainidentity.Principal,
	baseID string,
	sourceID string,
) (domainknowledge.IngestionJob, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeIngestionOperate); err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	if s.productionRepo == nil {
		return domainknowledge.IngestionJob{}, fmt.Errorf("%w: production knowledge repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	baseID = strings.TrimSpace(baseID)
	sourceID = strings.TrimSpace(sourceID)
	if _, err := s.repo.GetSource(ctx, principalScope(principal), baseID, sourceID); err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	now := s.now()
	job := domainknowledge.IngestionJob{
		ID:              uuid.NewString(),
		KnowledgeBaseID: baseID,
		SourceID:        sourceID,
		TargetRevision:  now.UnixNano(),
		Stage:           domainknowledge.IngestionStageDiscovering,
		Status:          domainknowledge.IngestionJobQueued,
		MaxAttempts:     defaultIngestionAttempts,
		Checkpoint: domainknowledge.IngestionCheckpoint{
			Stage:      domainknowledge.IngestionStageDiscovering,
			RecordedAt: now,
		},
		PrincipalSnapshot: domainknowledge.IngestionPrincipalSnapshot{
			UserID:         principal.UserID,
			Roles:          append([]string{}, principal.Roles...),
			PermissionKeys: append([]string{}, principal.PermissionKeys...),
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.productionRepo.CreateIngestionJob(ctx, job); err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	s.record(ctx, principal, "knowledge.ingestion.enqueue", job.ID, sourceID, nil)
	return job, nil
}

func (s *Service) GetIngestionJob(
	ctx context.Context,
	principal domainidentity.Principal,
	jobID string,
) (domainknowledge.IngestionJob, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeConnectorsView); err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	if s.productionRepo == nil {
		return domainknowledge.IngestionJob{}, fmt.Errorf("%w: production knowledge repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	return s.productionRepo.GetIngestionJob(ctx, principalScope(principal), strings.TrimSpace(jobID))
}

func (s *Service) CancelIngestionJob(
	ctx context.Context,
	principal domainidentity.Principal,
	jobID string,
) (domainknowledge.IngestionJob, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeIngestionOperate); err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	if s.productionRepo == nil {
		return domainknowledge.IngestionJob{}, fmt.Errorf("%w: production knowledge repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	job, err := s.productionRepo.RequestIngestionCancel(ctx, principalScope(principal), strings.TrimSpace(jobID), s.now())
	s.record(ctx, principal, "knowledge.ingestion.cancel", jobID, job.SourceID, err)
	return job, err
}

func (s *Service) RetryIngestionJob(
	ctx context.Context,
	principal domainidentity.Principal,
	jobID string,
) (domainknowledge.IngestionJob, error) {
	if err := s.authorize(ctx, principal, PermKnowledgeIngestionOperate); err != nil {
		return domainknowledge.IngestionJob{}, err
	}
	if s.productionRepo == nil {
		return domainknowledge.IngestionJob{}, fmt.Errorf("%w: production knowledge repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	job, err := s.productionRepo.RetryIngestionJob(ctx, principalScope(principal), strings.TrimSpace(jobID), s.now())
	s.record(ctx, principal, "knowledge.ingestion.retry", jobID, job.SourceID, err)
	return job, err
}

func connectorDefinitionFromSource(source domainknowledge.Source, version string) domainknowledge.ConnectorDefinition {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "v1"
	}
	config := make(map[string]any, len(source.Config))
	for key, value := range source.Config {
		config[key] = value
	}
	return domainknowledge.ConnectorDefinition{
		ID:              source.ID,
		KnowledgeBaseID: source.KnowledgeBaseID,
		Name:            source.Name,
		Kind:            source.Kind,
		Version:         version,
		SecretRef:       source.ConfigRef,
		Config:          config,
		SyncPolicy:      source.SyncPolicy,
		Status:          source.Status,
		CreatedAt:       source.CreatedAt,
		UpdatedAt:       source.UpdatedAt,
	}
}

func connectorInputFromSource(source domainknowledge.Source) domainknowledge.ConnectorInput {
	return domainknowledge.ConnectorInput{
		KnowledgeBaseID: source.KnowledgeBaseID,
		Name:            source.Name,
		Kind:            source.Kind,
		SecretRef:       source.ConfigRef,
		Config:          source.Config,
		SyncPolicy:      source.SyncPolicy,
	}
}

func ingestionPrincipal(snapshot domainknowledge.IngestionPrincipalSnapshot) domainidentity.Principal {
	return domainidentity.Principal{
		UserID:         snapshot.UserID,
		Roles:          append([]string{}, snapshot.Roles...),
		PermissionKeys: append([]string{}, snapshot.PermissionKeys...),
	}
}

func nextIngestionRetry(attempt int, now time.Time) time.Time {
	delay := time.Second << min(max(attempt-1, 0), 6)
	return now.Add(delay)
}
