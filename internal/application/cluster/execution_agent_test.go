package cluster

import (
	"errors"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestExecutionAgentAssignmentChecksScopeModeAndAuthWithoutPlatformProbe(t *testing.T) {
	for _, test := range []struct {
		name, id, token string
		mode            domaincluster.ConnectionMode
		want            error
	}{
		{"degraded dedicated Agent", "cluster-1", "fixture-token", domaincluster.ConnectionModeAgent, nil},
		{"scope denied", "cluster-2", "fixture-token", domaincluster.ConnectionModeAgent, apperrors.ErrAccessDenied},
		{"direct target", "cluster-1", "fixture-token", domaincluster.ConnectionModeDirectKubeconfig, apperrors.ErrInvalidArgument},
		{"no auth", "cluster-1", "", domaincluster.ConnectionModeAgent, apperrors.ErrServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &stubRepository{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: test.id, ConnectionMode: test.mode}, Metadata: map[string]any{"token": test.token}}}
			s := newTestService(t, repo)
			s.authorizer = stubAuthorizer{}
			// No agent summary client is installed: this validates configuration and
			// scope only, not running-container or Docker quota readiness.
			err := s.ValidateExecutionAgent(t.Context(), domainidentity.Principal{UserID: "operator"}, test.id)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
}
