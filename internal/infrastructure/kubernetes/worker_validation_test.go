package kubernetes

import (
	"context"
	"errors"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestInspectWorkerClusterRejectsInvalidSyntaxBeforeLiveRead(t *testing.T) {
	spec := sohaapi.VirtualizationWorkerPoolSpec{
		Name: "workers", ConnectionID: "pve", ClusterID: "target", Owner: "soha-kubeadm", ImageID: "image",
		ProviderNode: "pve-a", Storage: "disk", Bridge: "vmbr0", SnippetStorage: "local",
		OsProfile: "ubuntu-24.04-amd64-containerd", KubernetesVersion: "v1.35.2", CPU: 4, MemoryMiB: 8192, DiskGiB: 80, MaxNodes: 2,
		RequiredDaemonSets: []sohaapi.VirtualizationWorkerDaemonSet{{Namespace: "kube-system", Name: "network"}},
		Labels:             map[string]string{"kubernetes.io/role": ""},
	}
	if err := ValidateWorkerPoolKubernetesSpec(spec); err != nil {
		t.Fatalf("technical validator rejected Kubernetes syntax for a reserved business label: %v", err)
	}
	spec.Labels = nil
	spec.RequiredDaemonSets[0].Name = "invalid/daemon"
	// No manager is needed to reject a syntactically invalid spec.
	var manager *Manager
	if _, err := manager.InspectWorkerCluster(context.Background(), spec); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("InspectWorkerCluster() error = %v, want invalid argument", err)
	}
}
