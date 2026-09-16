package aigateway

import (
	"context"
	"fmt"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) analyzeDeliveryRepository(ctx context.Context, principal domainidentity.Principal, input map[string]any) (any, map[string]any, error) {
	var request sohaapi.RepositoryAnalysisInput
	if err := mapInput(input, &request); err != nil {
		return nil, nil, err
	}
	applicationID := stringInput(input, "applicationId")
	if applicationID == "" {
		return nil, nil, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	analyzer, ok := s.apps.(interface {
		AnalyzeRepository(context.Context, domainidentity.Principal, string, sohaapi.RepositoryAnalysisInput) (sohaapi.RepositoryAnalysis, error)
	})
	if !ok {
		return nil, nil, fmt.Errorf("%w: repository analysis is unavailable", apperrors.ErrInvalidArgument)
	}
	result, err := analyzer.AnalyzeRepository(ctx, principal, applicationID, request)
	return result, map[string]any{"applicationId": applicationID, "repositoryId": request.RepositoryID, "resolvedCommit": result.ResolvedCommit, "status": result.Status}, err
}
