package manifest

import (
	"context"
	"errors"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

type manifestCapabilityCluster struct{ domaincluster.Summary }

func (c manifestCapabilityCluster) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return domaincluster.Connection{Summary: c.Summary}, nil
}

func TestManifestTasksRequireProtectedAgentProtocol(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeAgent, domaincluster.ConnectionModeDirectKubeconfig} {
		for _, capable := range []bool{false, true} {
			summary := domaincluster.Summary{ConnectionMode: mode}
			if capable {
				summary.Capabilities = []string{resourceruntime.ManifestAgentCapability}
			}
			s := &DeclarativeService{base: &Service{clusters: manifestCapabilityCluster{summary}}}
			provider, err := s.taskProvider(t.Context(), "cluster-1")
			if mode == domaincluster.ConnectionModeAgent && !capable {
				if !errors.Is(err, apperrors.ErrUnsupportedOperation) || provider != "" {
					t.Fatalf("legacy Agent accepted: %s %v", provider, err)
				}
				continue
			}
			want := "manifest_direct"
			if mode == domaincluster.ConnectionModeAgent {
				want = "manifest_agent_v3.cluster-1"
			}
			if err != nil || provider != want {
				t.Fatalf("provider = %s %v, want %s", provider, err, want)
			}
		}
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
