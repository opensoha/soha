package observability

import (
	"encoding/json"
	"fmt"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func dataSourceConfigMap(config sohaapi.ObservabilityLogDataSourceConfig) map[string]any {
	return structMap(config)
}

func scopeMap(scope *sohaapi.ObservabilityDataSourceScope) map[string]any {
	if scope == nil {
		return map[string]any{}
	}
	return structMap(*scope)
}

func budgetMap(budget *sohaapi.ObservabilityLogQueryBudget) map[string]any {
	value := sohaapi.ObservabilityLogQueryBudget{MaxEntries: 1000, MaxRangeSeconds: 86400, TimeoutSeconds: 10}
	if budget != nil {
		if budget.MaxEntries > 0 {
			value.MaxEntries = budget.MaxEntries
		}
		if budget.MaxRangeSeconds > 0 {
			value.MaxRangeSeconds = budget.MaxRangeSeconds
		}
		if budget.TimeoutSeconds > 0 {
			value.TimeoutSeconds = budget.TimeoutSeconds
		}
	}
	return structMap(value)
}

func redactionMap(policy *sohaapi.ObservabilityLogRedactionPolicy) map[string]any {
	if policy == nil {
		return map[string]any{}
	}
	return structMap(*policy)
}

func apiConfig(value map[string]any) sohaapi.ObservabilityLogDataSourceConfig {
	var result sohaapi.ObservabilityLogDataSourceConfig
	mapStruct(value, &result)
	return result
}

func apiScope(value map[string]any) sohaapi.ObservabilityDataSourceScope {
	var result sohaapi.ObservabilityDataSourceScope
	mapStruct(value, &result)
	return result
}

func apiBudget(value map[string]any) sohaapi.ObservabilityLogQueryBudget {
	result := sohaapi.ObservabilityLogQueryBudget{MaxEntries: 1000, MaxRangeSeconds: 86400, TimeoutSeconds: 10}
	mapStruct(value, &result)
	if result.MaxEntries <= 0 {
		result.MaxEntries = 1000
	}
	if result.MaxRangeSeconds <= 0 {
		result.MaxRangeSeconds = 86400
	}
	if result.TimeoutSeconds <= 0 {
		result.TimeoutSeconds = 10
	}
	return result
}

func apiRedaction(value map[string]any) sohaapi.ObservabilityLogRedactionPolicy {
	var result sohaapi.ObservabilityLogRedactionPolicy
	mapStruct(value, &result)
	return result
}

func structMap(value any) map[string]any {
	payload, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(payload, &result)
	return result
}

func mapStruct(value map[string]any, target any) {
	payload, _ := json.Marshal(value)
	_ = json.Unmarshal(payload, target)
}

func firstString(value any) string {
	switch values := value.(type) {
	case []string:
		if len(values) > 0 {
			return values[0]
		}
	case []any:
		if len(values) > 0 {
			return fmt.Sprint(values[0])
		}
	}
	return ""
}
