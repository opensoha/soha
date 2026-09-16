package catalog

import (
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
)

func TestValidateHelmTargetPreservesConfiguration(t *testing.T) {
	target := domaincatalog.ReleaseTargetInput{ID: "target", ClusterID: "cluster", Namespace: "app", ExecutorKind: "helm_sdk", TargetKind: "helm_release", WorkloadKind: "HelmRelease", WorkloadName: "app", Helm: &sohaapi.HelmDeliveryConfiguration{
		ReleaseName: "app", Source: sohaapi.DeploymentTemplateHelmSource{RepositoryURL: "https://charts.example.test", Chart: "app", Version: "1.2.3"},
	}}
	if err := validateHelmTargets([]domaincatalog.ReleaseTargetInput{target}, nil); err != nil {
		t.Fatal(err)
	}
	current := []domaincatalog.ReleaseTarget{{ID: target.ID, Helm: target.Helm}}
	target.Helm = nil
	if err := validateHelmTargets([]domaincatalog.ReleaseTargetInput{target}, current); err == nil {
		t.Fatal("old client silently erased Helm configuration")
	}
	target.Helm = current[0].Helm
	target.WorkloadName = "other-release"
	if err := validateHelmTargets([]domaincatalog.ReleaseTargetInput{target}, current); err == nil {
		t.Fatal("accepted a mismatched release identity")
	}
}
