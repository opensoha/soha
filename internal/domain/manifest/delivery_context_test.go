package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeliveryContextIsPrivateAndValidated(t *testing.T) {
	context := DeliveryContext{Version: 1, PlanID: "private-plan", TargetID: "private-target", TargetRevision: strings.Repeat("a", 64), ScopeRevision: strings.Repeat("b", 64)}
	if err := context.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{TaskPayload{Action: "apply", DeliveryContext: &context}, OperationRun{Action: "apply", DeliveryContext: &context}} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "scopeRevision") || strings.Contains(string(encoded), "deliveryContext") {
			t.Fatalf("private operation context escaped serialization: %s", encoded)
		}
	}
	for _, change := range []func(*DeliveryContext){
		func(c *DeliveryContext) { c.Version = 0 }, func(c *DeliveryContext) { c.PlanID = "" },
		func(c *DeliveryContext) { c.TargetID = "" }, func(c *DeliveryContext) { c.TargetRevision = "short" },
		func(c *DeliveryContext) { c.ScopeRevision = strings.Repeat("A", 64) },
	} {
		invalid := context
		change(&invalid)
		if invalid.Validate() == nil {
			t.Fatal("incomplete publication identity accepted")
		}
	}
}
