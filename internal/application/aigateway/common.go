package aigateway

import (
	domaindelivery "github.com/soha/soha/internal/domain/delivery"
)

func optionalExecutionTaskID(item *domaindelivery.ExecutionTask) string {
	if item == nil {
		return ""
	}
	return item.ID
}
