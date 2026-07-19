// Package deliverygovernance contains the release-candidate governance gate.
// It is deliberately independent from HTTP and the delivery orchestration
// service so plan confirmation and runner callbacks can share one decision.
package deliverygovernance

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

const (
	DecisionAllowed = "allowed"
	DecisionBlocked = "blocked"
	DecisionPending = "pending"
)

// EvidenceReader is the small read surface needed by the governance gate.
// Keeping it narrow lets delivery, callbacks, and tests share the same policy.
type EvidenceReader interface {
	GetReleaseBundle(context.Context, string) (domaindelivery.ReleaseBundle, error)
	ListExecutionTasks(context.Context, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error)
	ListArtifacts(context.Context, domaindelivery.ArtifactFilter) ([]domaindelivery.ExecutionArtifact, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Service struct {
	evidence EvidenceReader
	audit    AuditRecorder
}

func New(evidence EvidenceReader, audit AuditRecorder) (*Service, error) {
	if isNil(evidence) {
		return nil, fmt.Errorf("%w: governance evidence reader is required", apperrors.ErrInvalidArgument)
	}
	if isNil(audit) {
		return nil, fmt.Errorf("%w: governance audit recorder is required", apperrors.ErrInvalidArgument)
	}
	return &Service{evidence: evidence, audit: audit}, nil
}

// Request describes the candidate and control state at confirmation time.
// ApprovalStatus is only required when RequiresApproval is true.
type Request struct {
	PlanID                   string
	ApplicationID            string
	ApplicationEnvironmentID string
	Action                   string
	ReleaseBundleID          string
	RequiresValidation       bool
	RequiresApproval         bool
	ApprovalStatus           string
	AIStatus                 string
}

type Decision struct {
	Status          string    `json:"status"`
	Allowed         bool      `json:"allowed"`
	PlanID          string    `json:"planId,omitempty"`
	ReleaseBundleID string    `json:"releaseBundleId,omitempty"`
	Reasons         []string  `json:"reasons,omitempty"`
	Evidence        Evidence  `json:"evidence"`
	EvaluatedAt     time.Time `json:"evaluatedAt"`
}

type Evidence struct {
	ValidationTaskIDs []string `json:"validationTaskIds,omitempty"`
	FailedTaskIDs     []string `json:"failedTaskIds,omitempty"`
	ArtifactIDs       []string `json:"artifactIds,omitempty"`
	CandidateDigest   string   `json:"candidateDigest,omitempty"`
	AIStatus          string   `json:"aiStatus,omitempty"`
}

func (s *Service) Evaluate(ctx context.Context, principal domainidentity.Principal, req Request) (Decision, error) {
	if strings.TrimSpace(req.ApplicationID) == "" || strings.TrimSpace(req.ApplicationEnvironmentID) == "" {
		return Decision{}, fmt.Errorf("%w: applicationId and applicationEnvironmentId are required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.PlanID) == "" {
		return Decision{}, fmt.Errorf("%w: planId is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Action) == "" {
		return Decision{}, fmt.Errorf("%w: action is required", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	decision := Decision{
		Status:      DecisionAllowed,
		Allowed:     true,
		PlanID:      strings.TrimSpace(req.PlanID),
		EvaluatedAt: now,
		Evidence:    Evidence{AIStatus: strings.TrimSpace(req.AIStatus)},
	}

	if req.RequiresApproval && !strings.EqualFold(strings.TrimSpace(req.ApprovalStatus), "approved") {
		decision.block("approval is required before execution")
	}
	if req.RequiresValidation {
		if strings.TrimSpace(req.ReleaseBundleID) == "" {
			decision.block("releaseBundleId is required for a governed release")
		} else if err := s.evaluateCandidate(ctx, &decision, req); err != nil {
			return Decision{}, err
		}
	}
	decision.ReleaseBundleID = strings.TrimSpace(req.ReleaseBundleID)
	if err := s.recordDecision(ctx, principal, req, decision); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

func (s *Service) evaluateCandidate(ctx context.Context, decision *Decision, req Request) error {
	bundle, err := s.evidence.GetReleaseBundle(ctx, strings.TrimSpace(req.ReleaseBundleID))
	if err != nil {
		return fmt.Errorf("get release bundle evidence: %w", err)
	}
	if strings.TrimSpace(bundle.ApplicationID) != strings.TrimSpace(req.ApplicationID) || strings.TrimSpace(bundle.ApplicationEnvironmentID) != strings.TrimSpace(req.ApplicationEnvironmentID) {
		decision.block("release bundle scope does not match delivery plan")
	}
	if !isCompletedStatus(bundle.Status) {
		decision.block(fmt.Sprintf("release bundle is not complete: %s", firstNonEmpty(bundle.Status, "unknown")))
	}
	decision.Evidence.CandidateDigest = strings.TrimSpace(bundle.ArtifactDigest)
	if decision.Evidence.CandidateDigest == "" {
		decision.block("release bundle has no immutable artifact digest")
	}

	tasks, err := s.evidence.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ReleaseBundleID: req.ReleaseBundleID, Limit: 500})
	if err != nil {
		return fmt.Errorf("list release validation tasks: %w", err)
	}
	validationCount := 0
	for _, task := range tasks {
		if !isValidationTask(task.TaskKind) {
			continue
		}
		validationCount++
		if task.ID != "" {
			decision.Evidence.ValidationTaskIDs = append(decision.Evidence.ValidationTaskIDs, task.ID)
		}
		if isCompletedStatus(task.Status) {
			continue
		}
		if task.ID != "" {
			decision.Evidence.FailedTaskIDs = append(decision.Evidence.FailedTaskIDs, task.ID)
		}
		if isTerminalFailure(task.Status) {
			decision.block(fmt.Sprintf("validation task %s failed: %s", task.ID, firstNonEmpty(task.Status, "failed")))
		} else {
			decision.pending(fmt.Sprintf("validation task %s is still %s", task.ID, firstNonEmpty(task.Status, "pending")))
		}
	}
	if validationCount == 0 {
		decision.block("no completed validation task is attached to the release bundle")
	}

	artifacts, err := s.evidence.ListArtifacts(ctx, domaindelivery.ArtifactFilter{ReleaseBundleID: req.ReleaseBundleID, Limit: 500})
	if err != nil {
		return fmt.Errorf("list release validation artifacts: %w", err)
	}
	for _, artifact := range artifacts {
		if !isValidationArtifact(artifact.Kind) {
			continue
		}
		if artifact.ID != "" {
			decision.Evidence.ArtifactIDs = append(decision.Evidence.ArtifactIDs, artifact.ID)
		}
		if isArtifactFailure(artifact) {
			decision.block(fmt.Sprintf("validation artifact %s reports failure", firstNonEmpty(artifact.Name, artifact.ID, artifact.Kind)))
		}
	}
	return nil
}

func (d *Decision) block(reason string) {
	d.Allowed = false
	d.Status = DecisionBlocked
	d.Reasons = appendReason(d.Reasons, reason)
}

func (d *Decision) pending(reason string) {
	if d.Status != DecisionBlocked {
		d.Status = DecisionPending
		d.Allowed = false
	}
	d.Reasons = appendReason(d.Reasons, reason)
}

func appendReason(reasons []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

func (s *Service) recordDecision(ctx context.Context, principal domainidentity.Principal, req Request, decision Decision) error {
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ResourceKind:  "delivery_plan",
		ResourceName:  req.PlanID,
		Action:        "delivery.governance.evaluate",
		Result:        decision.Status,
		Summary:       strings.Join(decision.Reasons, "; "),
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"planId":                   req.PlanID,
			"applicationId":            req.ApplicationID,
			"applicationEnvironmentId": req.ApplicationEnvironmentID,
			"action":                   req.Action,
			"releaseBundleId":          req.ReleaseBundleID,
			"requiresValidation":       req.RequiresValidation,
			"requiresApproval":         req.RequiresApproval,
			"approvalStatus":           req.ApprovalStatus,
			"aiStatus":                 req.AIStatus,
			"evidence":                 decision.Evidence,
		},
		CreatedAt: decision.EvaluatedAt,
	})
}

func isValidationTask(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "verify", "check", "check_http", "check_k8s_event", "smoke_test", "test", "validation":
		return true
	default:
		return false
	}
}

func isValidationArtifact(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "test_report", "junit", "scan_report":
		return true
	default:
		return false
	}
}

func isCompletedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "succeeded", "success", "passed":
		return true
	default:
		return false
	}
}

func isTerminalFailure(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed", "canceled", "cancelled", "callback_timeout", "timeout", "error":
		return true
	default:
		return false
	}
}

func isArtifactFailure(artifact domaindelivery.ExecutionArtifact) bool {
	if isTerminalFailure(artifact.Status) {
		return true
	}
	for _, key := range []string{"status", "conclusion", "result", "outcome"} {
		if value, ok := artifact.Metadata[key]; ok && isTerminalFailure(fmt.Sprint(value)) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
