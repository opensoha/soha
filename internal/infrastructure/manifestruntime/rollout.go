package manifestruntime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"k8s.io/client-go/dynamic"
)

func isRolloutTask(payload domainmanifest.TaskPayload) bool {
	if payload.RolloutControl != nil || payload.Action == domainmanifest.TaskActionRolloutControl {
		return true
	}
	for _, document := range payload.Documents {
		if document.APIVersion == "argoproj.io/v1alpha1" && document.Kind == "Rollout" {
			return true
		}
	}
	return false
}

func executeRolloutDocuments(ctx context.Context, client dynamic.Interface, payload domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	var wire sohaapi.ManifestExecutionTaskPayload
	encoded, err := json.Marshal(payload)
	if err == nil {
		err = json.Unmarshal(encoded, &wire)
	}
	if err != nil {
		return domainmanifest.TaskResult{}, err
	}
	result, err := resourceruntime.ExecuteRolloutTask(ctx, client, wire)
	var value domainmanifest.TaskResult
	encoded, encodeErr := json.Marshal(result)
	if encodeErr == nil {
		encodeErr = json.Unmarshal(encoded, &value)
	}
	return value, errors.Join(err, encodeErr)
}
