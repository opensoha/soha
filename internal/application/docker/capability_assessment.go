package docker

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ProjectAssessmentInput struct {
	ProjectID        string            `json:"projectId"`
	AfterOperationID string            `json:"afterOperationId"`
	ExpectedServices []string          `json:"expectedServices"`
	ExpectedImages   map[string]string `json:"expectedImages,omitempty"`
	MaxAgeSeconds    int               `json:"maxAgeSeconds,omitempty"`
}

// AssessProject verifies fresh runner inventory after a specific deployment.
// This is runtime evidence, not an application health or network reachability test.
func (s *Service) AssessProject(ctx context.Context, principal domainidentity.Principal, input ProjectAssessmentInput) (domainaigateway.CapabilityAssessment, error) {
	assessment := domainaigateway.CapabilityAssessment{Verdict: "inconclusive", Summary: "fresh post-deployment service inventory is required", Evidence: []domainaigateway.CapabilityEvidence{}}
	if input.ProjectID == "" || input.AfterOperationID == "" || len(input.ExpectedServices) == 0 || len(input.ExpectedServices) > 100 {
		return assessment, apperrors.ErrInvalidArgument
	}
	for _, name := range input.ExpectedServices {
		if strings.TrimSpace(name) == "" {
			return assessment, apperrors.ErrInvalidArgument
		}
	}
	if input.MaxAgeSeconds == 0 {
		input.MaxAgeSeconds = 120
	}
	if input.MaxAgeSeconds < 1 || input.MaxAgeSeconds > 600 {
		return assessment, apperrors.ErrInvalidArgument
	}
	project, err := s.GetProject(ctx, principal, input.ProjectID)
	if err != nil {
		return assessment, err
	}
	if err := checkProjectScope(ctx, project); err != nil {
		return assessment, err
	}
	operation, err := s.GetOperation(ctx, principal, input.AfterOperationID)
	if err != nil {
		return assessment, err
	}
	if !isProjectAssessmentDeployment(operation, project) {
		return assessment, fmt.Errorf("%w: operation is not a deployment of the requested project", apperrors.ErrInvalidArgument)
	}
	if operation.Status != OperationStatusCompleted || operation.FinishedAt == nil {
		return assessment, nil
	}
	services, err := s.ListServices(ctx, principal, domaindocker.ServiceFilter{ProjectID: project.ID, HostID: project.HostID, Page: 1, PageSize: 100, Limit: 100})
	if err != nil {
		return assessment, err
	}
	if services.Total > len(services.Items) {
		return assessment, nil
	}
	return assessProjectInventory(input, project, operation, services.Items, time.Now().UTC()), nil
}

func assessProjectInventory(input ProjectAssessmentInput, project domaindocker.Project, operation domaindocker.Operation, services []domaindocker.Service, now time.Time) domainaigateway.CapabilityAssessment {
	assessment := domainaigateway.CapabilityAssessment{Verdict: "satisfied", Summary: "expected services are running in fresh post-deployment inventory", Evidence: []domainaigateway.CapabilityEvidence{}}
	for _, name := range slices.Compact(slices.Sorted(slices.Values(input.ExpectedServices))) {
		matched := false
		for _, service := range services {
			if service.Name != name || service.ProjectID != project.ID || service.HostID != project.HostID {
				continue
			}
			matched = true
			fresh := service.LastSeenAt != nil && !service.LastSeenAt.Before(*operation.FinishedAt) && now.Sub(*service.LastSeenAt) <= time.Duration(input.MaxAgeSeconds)*time.Second && !service.LastSeenAt.After(now.Add(5*time.Second))
			assessment.Evidence = append(assessment.Evidence, domainaigateway.CapabilityEvidence{Kind: "runtime_inventory", Source: "docker.agent.inventory", ObservedAt: now, DataThrough: service.LastSeenAt, Incomplete: !fresh, Summary: name + " is " + service.Status, Resource: &domainaigateway.CapabilityResourceRef{Kind: "docker.service", ID: service.ID, Scope: map[string]string{"dockerHost": project.HostID, "dockerProject": project.ID}}})
			if !fresh && assessment.Verdict != "unsatisfied" {
				assessment.Verdict = "inconclusive"
			} else if fresh && service.Status != "running" {
				assessment.Verdict = "unsatisfied"
			}
			if expected := input.ExpectedImages[name]; fresh && expected != "" && service.Image != expected {
				assessment.Verdict = "unsatisfied"
			}
		}
		if !matched && assessment.Verdict != "unsatisfied" {
			assessment.Verdict = "inconclusive"
		}
	}
	if assessment.Verdict != "satisfied" {
		assessment.Summary = "runtime verification is " + string(assessment.Verdict)
	}
	return assessment
}

func isProjectAssessmentDeployment(operation domaindocker.Operation, project domaindocker.Project) bool {
	return operation.ProjectID == project.ID && operation.HostID == project.HostID && operation.OperationKind == OperationKindProjectDeploy && operation.Payload["action"] != "validate"
}
