package outpostruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAuthorizeAccessUsesManagedDrainGate(t *testing.T) {
	repo := &memoryRepository{state: domain.State{Owner: "soha", DesiredReplicas: 0, LatestTaskID: "task", Phase: "converged"}}
	s, _ := New(repo, testPermissions(), nil, nil)
	if err := s.AuthorizeAccess(context.Background(), "edge"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("stopped workload authorized: %v", err)
	}
	repo.state.Owner = "external"
	if err := s.AuthorizeAccess(context.Background(), "edge"); err != nil {
		t.Fatalf("external compatibility changed: %v", err)
	}
}

func TestControlCredentialAndObservationStayPrivate(t *testing.T) {
	credential := domain.InstanceCredential{TokenHash: "private-credential-hash", ConfigurationVersion: 4, ExpiresAt: time.Now().Add(time.Minute)}
	observation := domain.InstanceObservation{CredentialHash: credential.TokenHash, ConfigurationVersion: 4}
	for _, value := range []any{credential, observation} {
		encoded, err := json.Marshal(value)
		if err != nil || strings.Contains(string(encoded), "private-credential-hash") || strings.Contains(string(encoded), "configurationVersion") {
			t.Fatalf("private grant leaked: %s %v", encoded, err)
		}
	}
	s, _ := New(&memoryRepository{}, testPermissions(), nil, nil)
	if _, err := s.ControlForInstance(context.Background(), "edge", "pod", "epoch", domain.InstanceCredential{}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unbound claim accepted: %v", err)
	}
}
