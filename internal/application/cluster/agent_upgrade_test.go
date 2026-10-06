package cluster

import (
	"context"
	"errors"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type upgradeAgentStub struct {
	agentSummaryStub
	image string
	err   error
}

func (a *upgradeAgentStub) GetAgentUpgradeStatus(context.Context) (domaincluster.AgentUpgradeStatus, error) {
	return domaincluster.AgentUpgradeStatus{Version: "v0.1.6", CanUpgrade: true}, a.err
}

func (a *upgradeAgentStub) UpgradeAgent(_ context.Context, image string) (string, error) {
	a.image = image
	return "ghcr.io/opensoha/soha-agent:v0.1.6", a.err
}

func TestAgentUpgradeValidatesTargetAndAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, version, id string
		mode              domaincluster.ConnectionMode
		want              error
	}{
		{"stable", "v0.1.7", "cluster-1", domaincluster.ConnectionModeAgent, nil},
		{"version without prefix", "0.1.7", "cluster-1", domaincluster.ConnectionModeAgent, nil},
		{"arbitrary image", "evil.example/agent:latest", "cluster-1", domaincluster.ConnectionModeAgent, apperrors.ErrInvalidArgument},
		{"mutable tag", "latest", "cluster-1", domaincluster.ConnectionModeAgent, apperrors.ErrInvalidArgument},
		{"prerelease", "v0.1.7-rc.1", "cluster-1", domaincluster.ConnectionModeAgent, apperrors.ErrInvalidArgument},
		{"direct cluster", "v0.1.7", "cluster-1", domaincluster.ConnectionModeDirectKubeconfig, apperrors.ErrConflict},
		{"scope denied", "v0.1.7", "cluster-2", domaincluster.ConnectionModeAgent, apperrors.ErrAccessDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &stubRepository{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: tc.id, ConnectionMode: tc.mode}}}
			s := newTestService(t, repo)
			s.authorizer = stubAuthorizer{}
			a := &upgradeAgentStub{}
			s.agents = func(domaincluster.Connection) (AgentSummaryClient, error) { return a, nil }
			result, err := s.UpgradeAgent(context.Background(), domainidentity.Principal{}, tc.id, domaincluster.AgentUpgradeInput{Version: tc.version})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want != nil && a.image != "" {
				t.Fatal("denied request reached Agent")
			}
			wantImage := agentImageRepository + ":v0.1.7"
			if tc.want == nil && (a.image != wantImage || result.TargetImage != wantImage || result.PreviousImage == "") {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestAgentUpgradeStatusPreservesRunningVersionAndOfflineError(t *testing.T) {
	repo := &stubRepository{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-1", ConnectionMode: domaincluster.ConnectionModeAgent}}}
	s := newTestService(t, repo)
	a := &upgradeAgentStub{}
	s.agents = func(domaincluster.Connection) (AgentSummaryClient, error) { return a, nil }
	status, err := s.GetAgentUpgradeStatus(context.Background(), domainidentity.Principal{}, "cluster-1")
	if err != nil || status.Version != "v0.1.6" || status.RecommendedVersion != agentVersion {
		t.Fatalf("status = %#v, error = %v", status, err)
	}
	a.err = apperrors.ErrClusterUnready
	_, err = s.GetAgentUpgradeStatus(context.Background(), domainidentity.Principal{}, "cluster-1")
	if !errors.Is(err, apperrors.ErrClusterUnready) {
		t.Fatalf("offline error = %v", err)
	}
}
