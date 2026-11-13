package sohacli

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func deliveryCatalogRequest(resource, action string, ids []string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	id, err := deliveryWorkbenchID(ids, action != "list" && action != "create")
	if err != nil {
		return deliveryWorkbenchRequest{}, err
	}
	switch resource {
	case "applications":
		return deliveryApplicationRequest(action, id, opt)
	case "services":
		if _, err := deliveryWorkbenchID([]string{opt.application}, true); err != nil {
			return deliveryWorkbenchRequest{}, usageError{message: "delivery services requires --application-id for every action"}
		}
		req, err := deliveryServiceRequest(action, id, opt.application)
		req.allowed = append(req.allowed, "application-id")
		return req, err
	case "environments":
		return deliveryEnvironmentRequest(action, id, opt)
	case "environment-catalog":
		if action != "list" {
			return deliveryWorkbenchRequest{}, deliveryUnsupportedAction(resource, action)
		}
		return deliveryWorkbenchRequest{method: http.MethodGet, path: "/api/v1/delivery/environments", out: &sohaapi.DeliveryEnvironmentListEnvelope{}, list: true}, nil
	}
	return deliveryWorkbenchRequest{}, deliveryUnsupportedAction(resource, action)
}

func deliveryApplicationRequest(action, id string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	req := deliveryWorkbenchRequest{method: http.MethodGet, path: "/api/v1/applications", id: id, out: &sohaapi.ApplicationEnvelope{}}
	if id != "" {
		req.path += "/" + url.PathEscape(id)
	}
	switch action {
	case "list":
		if opt.limit < 1 || opt.limit > 200 || len(opt.search) > 1024 {
			return req, usageError{message: "application list requires --limit 1..200 and --search at most 1024 bytes"}
		}
		query := url.Values{"limit": {strconv.Itoa(opt.limit)}}
		if opt.search != "" {
			query.Set("search", opt.search)
		}
		req.path, req.out, req.list = deliveryQuery(req.path, query), &sohaapi.ApplicationListEnvelope{}, true
		req.allowed = []string{"search", "limit"}
	case "get":
	case "detail":
		req.path, req.out = req.path+"/detail", &sohaapi.ApplicationDeliveryDetailEnvelope{}
	case "runtime":
		req.path, req.out = req.path+"/runtime", &sohaapi.ApplicationRuntimeDetailEnvelope{}
	case "create", "update":
		req.method, req.body = http.MethodPost, &sohaapi.ApplicationInput{}
		if action == "update" {
			req.method = http.MethodPut
		}
	case "delete":
		req.method, req.out = http.MethodDelete, &sohaapi.OperationStatus{}
	default:
		return req, deliveryUnsupportedAction("applications", action)
	}
	return req, nil
}

func deliveryServiceRequest(action, id, applicationID string) (deliveryWorkbenchRequest, error) {
	req := deliveryWorkbenchRequest{method: http.MethodGet, path: "/api/v1/applications/" + url.PathEscape(applicationID) + "/services", id: id, out: &sohaapi.ApplicationServiceEnvelope{}}
	if id != "" {
		req.path += "/" + url.PathEscape(id)
	}
	switch action {
	case "list":
		req.out, req.list = &sohaapi.ApplicationServiceListEnvelope{}, true
	case "get":
	case "create", "update":
		req.method, req.body = http.MethodPost, &sohaapi.ApplicationServiceInput{}
		if action == "update" {
			req.method = http.MethodPut
		}
	case "delete":
		req.method, req.out = http.MethodDelete, &sohaapi.OperationStatus{}
	default:
		return req, deliveryUnsupportedAction("services", action)
	}
	return req, nil
}

func deliveryEnvironmentRequest(action, id string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	req := deliveryWorkbenchRequest{method: http.MethodGet, path: "/api/v1/application-environments", id: id, out: &sohaapi.ApplicationEnvironmentEnvelope{}}
	if id != "" {
		req.path += "/" + url.PathEscape(id)
	}
	switch action {
	case "list":
		query := url.Values{}
		for key, value := range map[string]string{"applicationId": opt.application, "environmentId": opt.environment} {
			if err := deliveryFilter(query, key, value); err != nil {
				return req, err
			}
		}
		req.path, req.out, req.list = deliveryQuery(req.path, query), &sohaapi.ApplicationEnvironmentListEnvelope{}, true
		req.allowed = []string{"application-id", "environment-id"}
	case "get":
	case "detail":
		req.path, req.out = req.path+"/detail", &sohaapi.ApplicationEnvironmentDeliveryDetailEnvelope{}
	case "create", "update":
		req.method, req.body = http.MethodPost, &sohaapi.ApplicationEnvironmentInput{}
		if action == "update" {
			req.method = http.MethodPut
		}
	case "delete":
		// Current Core returns 204. Successful deletion has no invented body.
		req.method, req.out = http.MethodDelete, nil
	default:
		return req, deliveryUnsupportedAction("environments", action)
	}
	return req, nil
}

func deliveryPlanBlueprintRequest(resource, action string, ids []string) (deliveryWorkbenchRequest, error) {
	id, err := deliveryWorkbenchID(ids, action != "list" && action != "create")
	if err != nil {
		return deliveryWorkbenchRequest{}, err
	}
	req := deliveryWorkbenchRequest{method: http.MethodGet, id: id}
	switch resource + "/" + action {
	case "plans/create":
		req.method, req.path, req.body, req.out = http.MethodPost, "/api/v1/delivery/plans", &sohaapi.DeliveryPlanInput{}, &sohaapi.DeliveryPlanEnvelope{}
	case "plans/confirm":
		req.method, req.path, req.out = http.MethodPost, "/api/v1/delivery/plans/"+url.PathEscape(id)+"/confirm", &sohaapi.DeliveryPlanConfirmResultEnvelope{}
	case "blueprints/list":
		req.path, req.out, req.list = "/api/v1/delivery/blueprints", &sohaapi.DeliveryBlueprintListEnvelope{}, true
	case "blueprints/render":
		req.method, req.path, req.out = http.MethodPost, "/api/v1/delivery/blueprints/"+url.PathEscape(id)+"/render-spec", &sohaapi.RenderedDeliverySpecEnvelope{}
	case "blueprints/usage":
		req.path, req.out = "/api/v1/delivery/blueprints/"+url.PathEscape(id)+"/usage", &sohaapi.DeliveryTemplateUsageSummaryEnvelope{}
	default:
		return req, deliveryUnsupportedAction(resource, action)
	}
	return req, nil
}

func validateDeliveryWorkbenchInput(action, targetID string, input any) error {
	if err := validateDeliveryCatalogShape(action, input); err != nil {
		return err
	}
	id := deliveryCreationID(input)
	if id == "" {
		if action == "create" && (isDeliveryCatalogInput(input)) {
			return usageError{message: "creation input requires a stable resource ID; inspect it after an uncertain response rather than replaying creation"}
		}
		return nil
	}
	if _, err := deliveryWorkbenchID([]string{id}, true); err != nil {
		return err
	}
	if action != "create" && id != targetID {
		return usageError{message: "input id must match the original resource ID"}
	}
	return nil
}

func isDeliveryCatalogInput(input any) bool {
	switch input.(type) {
	case *sohaapi.ApplicationInput, *sohaapi.ApplicationServiceInput, *sohaapi.ApplicationEnvironmentInput, *sohaapi.DeliveryPlanInput:
		return true
	}
	return false
}

func validateDeliveryCatalogShape(action string, input any) error {
	switch value := input.(type) {
	case *sohaapi.ApplicationInput:
		return validateDeliveryApplicationInput(action, value)
	case *sohaapi.ApplicationServiceInput:
		return validateDeliveryServiceInput(action, value)
	case *sohaapi.ApplicationEnvironmentInput:
		return validateDeliveryEnvironmentInput(action, value)
	case *sohaapi.DeliveryPlanInput:
		if value.ApplicationID == "" || value.ApplicationEnvironmentID == "" || !value.Action.Valid() {
			return usageError{message: "plan input requires applicationId/applicationEnvironmentId and a supported action"}
		}
	case *sohaapi.ExecutionTaskControlInput:
		// The command constructs this allowlisted input; no file override.
	case *sohaapi.ProgressiveRolloutControlInput:
		if !value.Action.Valid() || value.UID == "" || value.ResourceVersion == "" {
			return usageError{message: "rollout control requires a supported action and reviewed uid/resourceVersion"}
		}
	case *sohaapi.WorkflowTriggerRequest:
		if !kubernetesSegment(value.ApplicationID) {
			return usageError{message: "workflow trigger requires applicationId"}
		}
	case *sohaapi.WorkflowRequestInput:
		if !workflowRequestKeyPattern.MatchString(value.IdempotencyKey) || !kubernetesSegment(value.Workflow.ApplicationID) {
			return usageError{message: "workflow request requires original idempotencyKey (8..128 ASCII letters/digits/._:-) and workflow.applicationId"}
		}
	case *sohaapi.OperationCommentInput:
		if len([]rune(value.Comment)) > 2048 {
			return usageError{message: "decision comment exceeds 2048 characters"}
		}
	default:
		return validateDeliveryManifestInput(input)
	}
	return nil
}

func validateDeliveryApplicationInput(action string, value *sohaapi.ApplicationInput) error {
	if strings.TrimSpace(value.Key) == "" || strings.TrimSpace(value.Name) == "" || action == "update" && value.ExpectedVersion < 1 {
		return usageError{message: "application input requires key/name; update also requires reviewed expectedVersion > 0"}
	}
	return nil
}

func validateDeliveryServiceInput(action string, value *sohaapi.ApplicationServiceInput) error {
	if strings.TrimSpace(value.Key) == "" || strings.TrimSpace(value.Name) == "" || !value.ServiceKind.Valid() || action == "update" && value.ExpectedVersion < 1 {
		return usageError{message: "service input requires key/name/serviceKind; update also requires reviewed expectedVersion > 0"}
	}
	return nil
}

func validateDeliveryEnvironmentInput(action string, value *sohaapi.ApplicationEnvironmentInput) error {
	if value.ApplicationID == "" || value.EnvironmentID == "" || action == "update" && (value.ExpectedUpdatedAt == nil || value.ExpectedUpdatedAt.IsZero()) {
		return usageError{message: "environment input requires applicationId/environmentId; update also requires reviewed expectedUpdatedAt"}
	}
	return nil
}
