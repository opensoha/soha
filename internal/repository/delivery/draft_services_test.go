package delivery

import (
	"encoding/json"
	"reflect"
	"testing"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

func TestDraftAndBlueprintKeepServiceTemplateAfterJSONRoundTrip(t *testing.T) {
	version := int64(4)
	want := domaindelivery.DeliveryDraftService{
		ID: "service-1", Key: "api", Name: "API", ServiceKind: "kubernetes_workload", Enabled: true,
		ExpectedVersion: &version, Metadata: map[string]any{},
		DeploymentTemplate: &domaincatalog.DeploymentTemplateBinding{TemplateID: "http", Version: 2, Parameters: map[string]any{"enabled": false, "replicas": float64(0)}},
	}
	services := []domaindelivery.DeliveryDraftService{want}
	for name, normalized := range map[string][]domaindelivery.DeliveryDraftService{
		"draft":     normalizeDeliveryDraftInput(domaindelivery.DeliveryDraftInput{Services: services}, "actor").Services,
		"blueprint": normalizeDeliveryBlueprintInput(domaindelivery.DeliveryBlueprintInput{Services: services}).Services,
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := json.Marshal(normalized)
			if err != nil {
				t.Fatal(err)
			}
			var restored []domaindelivery.DeliveryDraftService
			if err := json.Unmarshal(payload, &restored); err != nil {
				t.Fatal(err)
			}
			if len(restored) != 1 || !reflect.DeepEqual(restored[0].DeploymentTemplate, want.DeploymentTemplate) || restored[0].ExpectedVersion == nil || *restored[0].ExpectedVersion != version {
				t.Fatalf("stored service lost template or version: %s", payload)
			}
		})
	}
}
