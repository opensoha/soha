package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

type DeliveryRecipe struct {
	Mode           string   `json:"mode"`
	SchemaVersion  int      `json:"schemaVersion"`
	Stages         []string `json:"stages"`
	ExecutionMode  string   `json:"executionMode"`
	StopOnFailure  bool     `json:"stopOnFailure"`
	MaxConcurrency int      `json:"maxConcurrency"`
}

func ParseDeliveryRecipe(definition map[string]any) (DeliveryRecipe, error) {
	var recipe DeliveryRecipe
	for _, field := range []string{"mode", "schemaVersion", "stages", "executionMode", "stopOnFailure", "maxConcurrency"} {
		if definition[field] == nil {
			return recipe, fmt.Errorf("delivery recipe requires %s", field)
		}
	}
	data, err := json.Marshal(definition)
	if err != nil {
		return recipe, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&recipe); err != nil {
		return recipe, err
	}
	if recipe.Mode != "delivery_batch" || recipe.SchemaVersion != 1 || !slices.Equal(recipe.Stages, []string{"build", "plan", "deploy", "health"}) {
		return recipe, fmt.Errorf("delivery recipe requires schema 1 and build, plan, deploy, health stages in that order")
	}
	if recipe.ExecutionMode != "service_serial" && recipe.ExecutionMode != "build_all_then_deploy" || recipe.MaxConcurrency < 1 || recipe.MaxConcurrency > 32 {
		return recipe, fmt.Errorf("delivery recipe requires a supported execution mode and 1-32 concurrent nodes")
	}
	return recipe, nil
}

func BuiltinDeliveryRecipes() []WorkflowTemplateInput {
	result := []WorkflowTemplateInput{}
	for _, item := range []struct{ key, name, mode string }{
		{"delivery-service-serial", "逐服务完整交付", "service_serial"},
		{"delivery-build-all", "先构建后部署", "build_all_then_deploy"},
	} {
		result = append(result, WorkflowTemplateInput{ID: item.key, Key: item.key, Name: item.name, Category: "delivery", Enabled: true,
			Definition: map[string]any{"mode": "delivery_batch", "schemaVersion": 1, "stages": []string{"build", "plan", "deploy", "health"}, "executionMode": item.mode, "stopOnFailure": true, "maxConcurrency": 4},
		})
	}
	return result
}
