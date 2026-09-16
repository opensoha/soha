package virtualization

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/apimachinery/pkg/util/validation"
)

type WorkerPool struct {
	sohaapi.VirtualizationWorkerPool
	Identity WorkerPoolIdentity `json:"-"`
}

// These values come from provider reads, never from the pool registration body.
type WorkerPoolIdentity struct {
	ClusterUID          string            `json:"clusterUid"`
	APIServerEndpoint   string            `json:"apiServerEndpoint"`
	CAHash              string            `json:"caHash"`
	ConnectionIdentity  string            `json:"connectionIdentity"`
	TemplateID          string            `json:"templateId"`
	TemplateFingerprint string            `json:"templateFingerprint"`
	DaemonSetUIDs       map[string]string `json:"daemonSetUids"`
}

type WorkerBootstrap struct {
	CloudInit  string
	SecretName string
	SecretUID  string
	ExpiresAt  time.Time
}

type WorkerObservation struct {
	Ready      bool
	Reason     string
	NodeName   string
	NodeUID    string
	ObservedAt time.Time
	ValidUntil time.Time
}

func WorkerNodeName(operationID string) string {
	return "soha-w-" + strings.ReplaceAll(operationID, "-", "")
}

var workerVersionPattern = regexp.MustCompile(`^v1\.([0-9]+)\.[0-9]+$`)

func ValidateWorkerPoolSpec(spec sohaapi.VirtualizationWorkerPoolSpec) error {
	invalid := func(reason string) error {
		return fmt.Errorf("%w: worker pool %s", apperrors.ErrInvalidArgument, reason)
	}
	if spec.Owner != "soha-kubeadm" || spec.OsProfile != "ubuntu-24.04-amd64-containerd" {
		return invalid("requires the supported Soha kubeadm worker image profile and sole supply owner")
	}
	for _, value := range []string{spec.Name, spec.ConnectionID, spec.ClusterID, spec.ImageID, spec.ProviderNode, spec.Storage, spec.Bridge, spec.SnippetStorage} {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || len(value) > 100 {
			return invalid("identifiers must be nonempty, trimmed and at most 100 bytes")
		}
	}
	version := workerVersionPattern.FindStringSubmatch(spec.KubernetesVersion)
	if len(version) != 2 {
		return invalid("requires an exact Kubernetes release version")
	}
	minor, _ := strconv.Atoi(version[1])
	if minor < 31 {
		return invalid("requires kubeadm v1beta4 (Kubernetes 1.31 or later)")
	}
	if !workerPoolShapeValid(spec) {
		return invalid("requires bounded CPU, memory, root disk and node count")
	}
	if len(spec.Labels) > 32 || len(spec.RequiredDaemonSets) < 1 || len(spec.RequiredDaemonSets) > 16 {
		return invalid("requires 1 to 16 node daemon references and at most 32 labels")
	}
	if err := validateWorkerLabels(spec.Labels); err != nil {
		return invalid(err.Error())
	}
	seen := map[string]bool{}
	for _, daemon := range spec.RequiredDaemonSets {
		key := daemon.Namespace + "/" + daemon.Name
		if len(validation.IsDNS1123Label(daemon.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(daemon.Name)) != 0 || seen[key] {
			return invalid("requires unique valid daemon names and namespaces")
		}
		seen[key] = true
	}
	return nil
}

func workerPoolShapeValid(spec sohaapi.VirtualizationWorkerPoolSpec) bool {
	return spec.CPU >= 2 && spec.CPU <= 256 && spec.MemoryMiB >= 2048 && spec.MemoryMiB <= 1048576 && spec.DiskGiB >= 20 && spec.DiskGiB <= 16384 && spec.MaxNodes >= 1 && spec.MaxNodes <= 1000
}

func validateWorkerLabels(labels map[string]string) error {
	for key, value := range labels {
		prefix, _, _ := strings.Cut(key, "/")
		if len(validation.IsQualifiedName(key)) != 0 || len(validation.IsValidLabelValue(value)) != 0 ||
			prefix == "kubernetes.io" || strings.HasSuffix(prefix, ".kubernetes.io") || prefix == "k8s.io" || strings.HasSuffix(prefix, ".k8s.io") || strings.HasPrefix(key, "node-role.") || strings.HasPrefix(key, "soha.io/") {
			return fmt.Errorf("labels must be valid and must not claim Kubernetes roles or reserved ownership")
		}
	}
	return nil
}
