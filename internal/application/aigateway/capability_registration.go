package aigateway

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// A trigger has a durable receipt before materialization. Each occurrence gets
// fresh write keys; retries of that receipt reproduce the same keys and plan.
func (s *Service) MaterializeRegisteredCapabilityPlan(ctx context.Context, principal domainidentity.Principal, input domain.CapabilityTaskInput) (domain.CapabilityTaskInput, error) {
	validation, err := s.ValidateCapabilityPlan(ctx, principal, input)
	if err != nil {
		return input, err
	}
	if !validation.Valid {
		return input, fmt.Errorf("%w: registered capability plan is no longer valid", apperrors.ErrInvalidArgument)
	}
	data, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	var result domain.CapabilityTaskInput
	if err := json.Unmarshal(data, &result); err != nil {
		return input, err
	}
	manifest, err := s.Capabilities(ctx, principal, domain.ManifestRequest{AIClientID: input.AIClientID, SkillID: input.SkillID})
	if err != nil {
		return input, err
	}
	tools := map[string]domain.ToolCapability{}
	for _, tool := range manifest.Tools {
		tools[tool.Name] = tool
	}
	for index := range result.Plan.Steps {
		step := &result.Plan.Steps[index]
		tool, ok := tools[step.Call.ToolName]
		if !ok || tool.Execution == nil || !tool.Execution.Idempotent {
			return input, apperrors.ErrAccessDenied
		}
		if field := tool.Execution.IdempotencyKeyField; field != "" {
			step.Call.Input[field] = uuid.NewSHA1(uuid.NameSpaceOID, []byte("inspection/"+input.IdempotencyKey+"/"+step.ID)).String()
		}
	}
	validation, err = s.ValidateCapabilityPlan(ctx, principal, result)
	if err == nil && !validation.Valid {
		err = fmt.Errorf("%w: generated invocation keys do not match the capability contract", apperrors.ErrInvalidArgument)
	}
	return result, err
}
