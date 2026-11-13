package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func TestDeliveryListAliasesPreservePublishedAndLegacyConsumers(t *testing.T) {
	for _, rows := range [][]map[string]string{nil, {}, {{"id": "authorized-row"}}} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		writeDeliveryList(ctx, rows)
		if response.Code != 200 {
			t.Fatal("list response status changed")
		}
		var legacy struct {
			Items []map[string]string `json:"items"`
		}
		var canonical struct {
			Data []map[string]string `json:"data"`
		}
		if json.Unmarshal(response.Body.Bytes(), &legacy) != nil || json.Unmarshal(response.Body.Bytes(), &canonical) != nil || legacy.Items == nil || canonical.Data == nil || !reflect.DeepEqual(legacy.Items, canonical.Data) || len(canonical.Data) != len(rows) {
			t.Fatal("canonical/legacy views differ or an empty list became null")
		}
		consumers := []any{&sohaapi.ApplicationListEnvelope{}, &sohaapi.ApplicationServiceListEnvelope{}, &sohaapi.ApplicationEnvironmentListEnvelope{}, &sohaapi.DeliveryEnvironmentListEnvelope{}, &sohaapi.DeliveryBlueprintListEnvelope{}, &sohaapi.ExecutionTaskListEnvelope{}, &sohaapi.ExecutionArtifactListEnvelope{}, &sohaapi.ManifestBindingListEnvelope{}, &sohaapi.ManifestRevisionListEnvelope{}}
		for _, consumer := range consumers {
			if err := json.Unmarshal(response.Body.Bytes(), consumer); err != nil {
				t.Fatalf("published DTO cannot decode list: %v", err)
			}
			data := reflect.ValueOf(consumer).Elem().FieldByName("Data")
			items := reflect.ValueOf(consumer).Elem().FieldByName("Items")
			if data.IsNil() || items.IsNil() || data.Len() != len(rows) || items.Len() != len(rows) {
				t.Fatal("generated list DTO silently dropped an authorized row or empty array")
			}
		}
	}
}
