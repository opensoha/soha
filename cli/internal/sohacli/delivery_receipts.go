package sohacli

import (
	"fmt"
	"net/url"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func deliveryCreationID(input any) string {
	switch value := input.(type) {
	case *sohaapi.ApplicationInput:
		return value.ID
	case *sohaapi.ApplicationServiceInput:
		return value.ID
	case *sohaapi.ApplicationEnvironmentInput:
		return value.ID
	case *sohaapi.DeliveryPlanInput:
		return value.ID
	}
	return ""
}

func validateDeliveryWorkbenchResult(req deliveryWorkbenchRequest) error {
	switch result := req.out.(type) {
	case *sohaapi.WorkflowRequestReceiptEnvelope:
		return validateWorkflowRequestReceipt(req, result)
	case *sohaapi.WorkflowRunEnvelope:
		return deliveryReceiptID(result.Data.ID, req.id)
	case *sohaapi.ApplicationEnvelope:
		return deliveryReceiptID(result.Data.ID, req.id)
	case *sohaapi.ApplicationServiceEnvelope:
		return deliveryReceiptID(result.Data.ID, req.id)
	case *sohaapi.ApplicationEnvironmentEnvelope:
		return deliveryReceiptID(result.Data.ID, req.id)
	case *sohaapi.ApplicationDeliveryDetailEnvelope:
		if err := deliveryReceiptID(result.Data.Application.ID, req.id); err != nil {
			return err
		}
		return validateDeliveryLatestTask(result.Data.LatestExecutionTask)
	case *sohaapi.ApplicationEnvironmentDeliveryDetailEnvelope:
		if err := deliveryReceiptID(result.Data.Binding.ID, req.id); err != nil {
			return err
		}
		return validateDeliveryLatestTask(result.Data.LatestExecutionTask)
	case *sohaapi.ApplicationRuntimeDetailEnvelope:
		return deliveryReceiptID(result.Data.Application.ID, req.id)
	case *sohaapi.ApplicationEnvironmentListEnvelope:
		filterDeliveryEnvironments(result, req.path)
	case *sohaapi.DeliveryPlanEnvelope:
		return deliveryReceiptID(result.Data.ID, req.id)
	case *sohaapi.DeliveryPlanConfirmResultEnvelope:
		return deliveryReceiptID(result.Data.Plan.ID, req.id)
	default:
		if err := validateDeliveryTaskResult(req); err != nil {
			return err
		}
		return validateDeliveryManifestResult(req)
	}
	return nil
}

func deliveryReceiptID(actual, expected string) error {
	if actual == "" || expected != "" && expected != actual {
		return fmt.Errorf("delivery receipt does not identify the original resource; inspect its original ID before another action")
	}
	return nil
}

func validateDeliveryLatestTask(task *sohaapi.ExecutionTask) error {
	if task == nil {
		return nil
	}
	return validateDeliveryTask(task, "", false)
}

func validateDeliveryTaskResult(req deliveryWorkbenchRequest) error {
	switch result := req.out.(type) {
	case *sohaapi.ExecutionTaskEnvelope:
		return validateDeliveryTask(&result.Data, req.id, req.method == "POST")
	case *sohaapi.ExecutionTaskListEnvelope:
		for i := range result.Data {
			if err := validateDeliveryTask(&result.Data[i], "", false); err != nil {
				return err
			}
		}
	case *sohaapi.ExecutionLogListEnvelope:
		if result.Items == nil {
			return fmt.Errorf("execution logs response lacks an items array")
		}
		for _, log := range result.Items {
			if log.ID == "" || log.ExecutionTaskID != req.id {
				return fmt.Errorf("execution logs response changed the original task identity")
			}
		}
	case *sohaapi.ProgressiveRolloutStatusEnvelope:
		if result.Data.UID == "" || result.Data.ResourceVersion == "" || result.Data.OperationID == "" {
			return fmt.Errorf("rollout response lacks the observed resource and operation identity")
		}
	}
	return nil
}

// Old servers ignored these declared filters. Narrow the already authorized
// response as well, so no supported server can silently broaden this query.
func filterDeliveryEnvironments(result *sohaapi.ApplicationEnvironmentListEnvelope, path string) {
	u, _ := url.Parse(path)
	application, environment := u.Query().Get("applicationId"), u.Query().Get("environmentId")
	filtered := make([]sohaapi.ApplicationEnvironment, 0, len(result.Data))
	for _, item := range result.Data {
		if (application == "" || item.ApplicationID == application) && (environment == "" || item.EnvironmentID == environment) {
			filtered = append(filtered, item)
		}
	}
	result.Data = filtered
}

func deliveryWorkbenchOutcome(value any) error {
	if receipt, ok := value.(*sohaapi.WorkflowRequestReceiptEnvelope); ok {
		if receipt.Data.SubmissionStatus != "accepted" {
			return fmt.Errorf("workflow submission is %s; preserve original request key %s and inspect it; no automatic replay", receipt.Data.SubmissionStatus, terminalLabel(receipt.Data.RequestKey))
		}
		if receipt.Data.Run != nil && receipt.Data.Run.Status == "failed" {
			return fmt.Errorf("original workflow failed; inspect run %s before another submission", terminalLabel(receipt.Data.RunID))
		}
	}
	if task, ok := value.(*sohaapi.ExecutionTaskEnvelope); ok {
		switch task.Data.Status {
		case "failed", "callback_timeout":
			return fmt.Errorf("execution task is %s; inspect the original task before another action", task.Data.Status)
		}
	}
	if action, ok := value.(*sohaapi.ManifestDeploymentActionEnvelope); ok && (action.Data.Task.Status == "failed" || action.Data.Task.Status == "callback_timeout") {
		return fmt.Errorf("manifest execution task is %s; inspect the returned original task", action.Data.Task.Status)
	}
	return nil
}

func validateWorkflowRequestReceipt(req deliveryWorkbenchRequest, envelope *sohaapi.WorkflowRequestReceiptEnvelope) error {
	item := envelope.Data
	if !kubernetesSegment(item.ID) || !kubernetesSegment(item.RunID) || item.RequestKey != req.id || !item.SubmissionStatus.Valid() {
		return fmt.Errorf("workflow receipt does not match the original request; preserve its key and do not create another run")
	}
	if item.Run != nil && item.Run.ID != item.RunID {
		return fmt.Errorf("workflow receipt changed its original run identity")
	}
	return nil
}

func validateDeliveryManifestResult(req deliveryWorkbenchRequest) error {
	switch result := req.out.(type) {
	case *sohaapi.ManifestPackageEnvelope:
		return deliveryReceiptID(result.Data.ID, req.id)
	case *sohaapi.ManifestBindingEnvelope:
		if req.method == "POST" {
			if err := deliveryReceiptID(result.Data.PackageID, req.id); err != nil {
				return err
			}
			return deliveryReceiptID(result.Data.ID, "")
		}
		return deliveryReceiptID(result.Data.ID, req.id)
	case *sohaapi.ManifestDeploymentEnvelope:
		return deliveryReceiptID(result.Data.ID, req.id)
	case *sohaapi.ManifestDeploymentActionEnvelope:
		return validateDeliveryManifestAction(req, result)
	case *sohaapi.ManifestRevisionListEnvelope:
		for _, revision := range result.Data {
			if revision.PackageID != req.id || revision.ID == "" {
				return fmt.Errorf("manifest revision list changed the original package identity")
			}
		}
	case *sohaapi.ManifestBindingListEnvelope:
		for _, binding := range result.Data {
			if binding.PackageID != req.id || binding.ID == "" {
				return fmt.Errorf("manifest binding list changed the original package identity")
			}
		}
	case *sohaapi.ManifestPackagePageEnvelope:
		return validateDeliveryManifestPage(result.Data.Items != nil, result.Data.Page, result.Data.PageSize, result.Data.Total, len(result.Data.Items))
	case *sohaapi.ManifestDeploymentListEnvelope:
		return validateDeliveryManifestPage(result.Data.Items != nil, result.Data.Page, result.Data.PageSize, result.Data.Total, len(result.Data.Items))
	case *sohaapi.DeliveryTemplateUsageSummaryEnvelope:
		return deliveryReceiptID(result.Data.TemplateID, req.id)
	}
	return nil
}

func validateDeliveryManifestAction(req deliveryWorkbenchRequest, result *sohaapi.ManifestDeploymentActionEnvelope) error {
	if result.Data.Deployment.ID == "" || result.Data.Task.ID == "" || result.Data.Task.Status == "" {
		return fmt.Errorf("manifest action response lacks durable deployment/task identity")
	}
	id := result.Data.Deployment.ID
	if _, desiredRevision := req.body.(*sohaapi.ManifestDesiredRevisionInput); desiredRevision {
		id = result.Data.Deployment.BindingID
	}
	if err := deliveryReceiptID(id, req.id); err != nil {
		return err
	}
	result.Data.Task.CallbackToken, result.Data.Task.SecretLease = "", nil
	return nil
}

func validateDeliveryManifestPage(hasItems bool, page, size, total, count int) error {
	if !hasItems || page < 1 || size < 1 || total < count {
		return fmt.Errorf("manifest response lacks a valid page and items array")
	}
	return nil
}
