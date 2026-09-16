package handlers

import (
	"encoding/json"
	"testing"
	"time"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

func TestManifestDeploymentResponsePreservesResourceObservations(t *testing.T) {
	zero, deletingAt := int64(0), time.Now().UTC()
	input := domainmanifest.Deployment{Status: domainmanifest.DeploymentStatus{Inventory: []domainmanifest.ResourceInventory{
		{Kind: "WorkloadCronJob", ResourceGeneration: 3, ObservedResourceGeneration: &zero, DeletingAt: &deletingAt, Finalizers: []string{"test.soha.io/cleanup"}},
		{Kind: "Service"},
	}}}
	output, err := json.Marshal(manifestDeploymentDTO(input))
	if err != nil {
		t.Fatal(err)
	}
	var decoded domainmanifest.Deployment
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatal(err)
	}
	item := decoded.Status.Inventory[0]
	if item.ResourceGeneration != 3 || item.ObservedResourceGeneration == nil || *item.ObservedResourceGeneration != 0 || item.DeletingAt == nil || !item.DeletingAt.Equal(deletingAt) || len(item.Finalizers) != 1 || item.Finalizers[0] != "test.soha.io/cleanup" || decoded.Status.Inventory[1].ObservedResourceGeneration != nil {
		t.Fatalf("resource observations changed in HTTP response: %s", output)
	}
}
