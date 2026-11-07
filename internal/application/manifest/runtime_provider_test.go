package manifest

import (
	"context"
	"errors"
	"testing"

	"github.com/opensoha/soha-contracts/helmrelease"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type manifestCapabilityCluster struct{ domaincluster.Summary }

func (c manifestCapabilityCluster) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return domaincluster.Connection{Summary: c.Summary}, nil
}

func TestManifestTaskProviderRequiresClaimCapability(t *testing.T) {
	directAPI := resourceruntime.ManifestAgentCapability
	manifest := []string{directAPI, resourceruntime.RootCRAgentCapability, domaindelivery.ManifestAdmissionCapability, resourceruntime.ManifestInitializationCapability}
	for _, tc := range []struct {
		name         string
		mode         domaincluster.ConnectionMode
		capabilities []string
		wantProvider string
	}{
		{name: "Legacy", mode: domaincluster.ConnectionModeAgent},
		{name: "ControlPlaneDisabled", mode: domaincluster.ConnectionModeAgent, capabilities: []string{directAPI}},
		{name: "HelmOnly", mode: domaincluster.ConnectionModeAgent, capabilities: []string{directAPI, helmrelease.InitializationCapability}},
		{name: "ManifestOnly", mode: domaincluster.ConnectionModeAgent, capabilities: manifest, wantProvider: "manifest_agent_v3.cluster-1"},
		{name: "Combined", mode: domaincluster.ConnectionModeAgent, capabilities: append(manifest, helmrelease.InitializationCapability), wantProvider: "manifest_agent_v3.cluster-1"},
		{name: "MissingControllerAwareCapability", mode: domaincluster.ConnectionModeAgent, capabilities: []string{resourceruntime.RootCRAgentCapability}},
		{name: "DirectKubeconfig", mode: domaincluster.ConnectionModeDirectKubeconfig, wantProvider: domainmanifest.TaskProviderDirect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary := domaincluster.Summary{ConnectionMode: tc.mode, Capabilities: tc.capabilities}
			s := &DeclarativeService{base: &Service{clusters: manifestCapabilityCluster{summary}}}
			provider, err := s.taskProvider(t.Context(), "cluster-1")
			if tc.wantProvider == "" {
				if provider != "" || !errors.Is(err, apperrors.ErrUnsupportedOperation) {
					t.Fatalf("provider = %q, error = %v, want unsupported", provider, err)
				}
				return
			}
			if err != nil || provider != tc.wantProvider {
				t.Fatalf("provider = %q, error = %v, want %q", provider, err, tc.wantProvider)
			}
		})
	}
}

func TestDirectAgentRolloutDoesNotRequireTaskRunner(t *testing.T) {
	summary := domaincluster.Summary{
		ConnectionMode: domaincluster.ConnectionModeAgent,
		Capabilities:   []string{resourceruntime.ManifestAgentCapability},
	}
	runtime := &rolloutTaskFixture{}
	s := &DeclarativeService{
		base:   &Service{clusters: manifestCapabilityCluster{summary}},
		agents: func(domaincluster.Connection) (ManifestRuntime, error) { return runtime, nil },
	}
	status, err := s.executeTaskRollout(t.Context(), domainmanifest.TaskPayload{ClusterID: "cluster-1", IdempotencyKey: "operation"})
	if err != nil || status.OperationID != "operation" || len(runtime.calls) != 1 {
		t.Fatalf("rollout status = %+v, error = %v, runtime calls = %d", status, err, len(runtime.calls))
	}
}

func TestManifestPlanProviderStillRequiresAdmissionCapability(t *testing.T) {
	summary := domaincluster.Summary{
		ConnectionMode: domaincluster.ConnectionModeAgent,
		Capabilities:   []string{resourceruntime.ManifestAgentCapability, resourceruntime.RootCRAgentCapability},
	}
	s := &DeclarativeService{base: &Service{clusters: manifestCapabilityCluster{summary}}}
	binding := domainmanifest.EnvironmentBinding{ClusterID: "cluster-1"}
	payload := domainmanifest.TaskPayload{DeliveryContext: &domainmanifest.DeliveryContext{}}
	if provider, err := s.operationProvider(t.Context(), binding, payload); provider != "" || !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("provider = %q, error = %v, want unsupported without admission", provider, err)
	}

	summary.Capabilities = append(summary.Capabilities, domaindelivery.ManifestAdmissionCapability)
	s.base.clusters = manifestCapabilityCluster{summary}
	if provider, err := s.operationProvider(t.Context(), binding, payload); provider != "manifest_delivery_v1.manifest_agent_v3.cluster-1" || err != nil {
		t.Fatalf("provider = %q, error = %v, want protected Manifest provider", provider, err)
	}
}

func TestRootCRTasksRequireDedicatedAgentCapability(t *testing.T) {
	for _, capable := range []bool{false, true} {
		summary := domaincluster.Summary{ConnectionMode: domaincluster.ConnectionModeAgent, Capabilities: []string{resourceruntime.ManifestAgentCapability}}
		if capable {
			summary.Capabilities = append(summary.Capabilities, resourceruntime.RootCRAgentCapability)
		}
		s := &DeclarativeService{base: &Service{clusters: manifestCapabilityCluster{summary}}}
		provider, err := s.rootTaskProvider(t.Context(), "cluster", true)
		if !capable && !errors.Is(err, apperrors.ErrUnsupportedOperation) || capable && (err != nil || provider != "manifest_root_cr_v1.cluster") {
			t.Fatalf("provider=%s error=%v", provider, err)
		}
	}
}

func TestInitializationRequiresDedicatedAgentCapability(t *testing.T) {
	for _, capable := range []bool{false, true} {
		summary := domaincluster.Summary{ConnectionMode: domaincluster.ConnectionModeAgent, Capabilities: []string{resourceruntime.ManifestAgentCapability}}
		if capable {
			summary.Capabilities = append(summary.Capabilities, resourceruntime.ManifestInitializationCapability)
		}
		s := &DeclarativeService{base: &Service{clusters: manifestCapabilityCluster{summary}}}
		provider, err := s.initializationProvider(t.Context(), "cluster")
		if !capable && !errors.Is(err, apperrors.ErrUnsupportedOperation) || capable && (err != nil || provider != "manifest_initialize_v1.cluster") {
			t.Fatalf("provider=%s error=%v", provider, err)
		}
	}
}

func TestDirectInitializationUsesDedicatedProvider(t *testing.T) {
	s := &DeclarativeService{base: &Service{clusters: manifestCapabilityCluster{domaincluster.Summary{ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}}
	provider, err := s.initializationProvider(t.Context(), "cluster")
	if err != nil || provider != domainmanifest.TaskProviderDirectInitialization || provider == domainmanifest.TaskProviderDirect {
		t.Fatalf("initialization provider = %s %v", provider, err)
	}
}
