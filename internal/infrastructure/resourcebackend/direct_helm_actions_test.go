package resourcebackend

import (
	"errors"
	"strings"
	"testing"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	helmchartpkg "helm.sh/helm/v4/pkg/chart/v2"
	helmreleasecommon "helm.sh/helm/v4/pkg/release/common"
	helmreleasepkg "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
)

func TestMapHelmChartInstallResultIncludesManifestResources(t *testing.T) {
	t.Parallel()

	result := mapHelmChartInstallResult(&helmreleasepkg.Release{
		Name: "prometheus", Namespace: "monitoring", Version: 1,
		Manifest: `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prometheus-operator
  namespace: monitoring
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: prometheus-operator
  namespace: monitoring
`,
	})

	if len(result.Resources) != 2 {
		t.Fatalf("Resources length = %d, want 2", len(result.Resources))
	}
	if result.Resources[0].Kind != "Deployment" || result.Resources[0].Name != "prometheus-operator" || result.Resources[0].Namespace != "monitoring" {
		t.Fatalf("first resource = %#v", result.Resources[0])
	}
	if result.Resources[1].Kind != "ServiceAccount" || result.Resources[1].APIVersion != "v1" {
		t.Fatalf("second resource = %#v", result.Resources[1])
	}
}

func TestHelmSDKReleaseSatisfiesInstallRequiresDeployedSameChartVersion(t *testing.T) {
	t.Parallel()

	input := domainresource.HelmChartInstallInput{
		ChartName: "kube-prometheus-stack", Version: "86.1.0",
		ReleaseName: "kube-prometheus-stack", Namespace: "default",
	}
	release := &helmreleasepkg.Release{
		Name: "kube-prometheus-stack", Namespace: "default", Version: 1,
		Info: &helmreleasepkg.Info{Status: helmreleasecommon.StatusDeployed},
		Chart: &helmchartpkg.Chart{Metadata: &helmchartpkg.Metadata{
			Name: "kube-prometheus-stack", Version: "86.1.0",
		}},
	}

	if !helmSDKReleaseSatisfiesInstall(release, input) {
		t.Fatal("expected deployed matching SDK release to satisfy install")
	}
	release.Info.Status = helmreleasecommon.StatusPendingInstall
	if helmSDKReleaseSatisfiesInstall(release, input) {
		t.Fatal("pending SDK release satisfied install")
	}
	release.Info.Status = helmreleasecommon.StatusDeployed
	release.Chart.Metadata.Version = "86.0.0"
	if helmSDKReleaseSatisfiesInstall(release, input) {
		t.Fatal("different SDK chart version satisfied install")
	}
}

func TestHelmReleaseNameUnavailableErrorIsInvalidArgument(t *testing.T) {
	t.Parallel()

	err := helmReleaseNameUnavailableError("prometheus", "monitoring", "pending-install", "2")
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("error does not wrap ErrInvalidArgument: %v", err)
	}
	if !strings.Contains(err.Error(), "pending-install") || !strings.Contains(err.Error(), "revision 2") {
		t.Fatalf("error message = %q", err.Error())
	}
}

func TestIsHelmReleaseNotFoundErrorUsesHelmSentinels(t *testing.T) {
	t.Parallel()

	if !isHelmReleaseNotFoundError(driver.ErrReleaseNotFound) {
		t.Fatal("ErrReleaseNotFound should be recognized as not found")
	}
	if !isHelmReleaseNotFoundError(driver.ErrNoDeployedReleases) {
		t.Fatal("ErrNoDeployedReleases should be recognized as not found")
	}
	if !isHelmReleaseNotFoundError(errors.New("release foo not found")) {
		t.Fatal("legacy string match should still be recognized")
	}
	if isHelmReleaseNotFoundError(errors.New("backend connection failed")) {
		t.Fatal("unrelated error should not be recognized as not found")
	}
}
