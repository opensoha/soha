package sohacli

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

var workflowRequestKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func deliveryExecutionRequest(action string, ids []string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	id, err := aiWorkbenchID(ids, action != "list" && action != "trigger")
	if err != nil {
		return deliveryWorkbenchRequest{}, err
	}
	req := deliveryWorkbenchRequest{method: http.MethodGet, path: "/api/v1/workflows", id: id, out: &sohaapi.WorkflowRunEnvelope{}}
	switch action {
	case "list":
		if opt.limit < 1 || opt.limit > 200 {
			return req, usageError{message: "limit must be from 1 to 200"}
		}
		query := url.Values{"limit": {strconv.Itoa(opt.limit)}}
		for key, value := range map[string]string{"applicationId": opt.application, "applicationEnvironmentId": opt.binding} {
			if value != "" {
				query.Set(key, value)
			}
		}
		req.path += "?" + query.Encode()
		req.out = &struct {
			Items []sohaapi.WorkflowRun `json:"items"`
			Data  []sohaapi.WorkflowRun `json:"data,omitempty"`
		}{}
		req.allowed = []string{"limit", "application-id", "application-environment-id"}
	case "get":
		req.path += "/" + id
	case "trigger":
		req.path = "/api/v1/workflow-requests"
		req.method = http.MethodPost
		req.body = &sohaapi.WorkflowRequestInput{}
		req.out = &sohaapi.WorkflowRequestReceiptEnvelope{}
	case "request":
		if !workflowRequestKeyPattern.MatchString(ids[0]) {
			return req, usageError{message: "request requires the original 8..128 character request key"}
		}
		req.path = "/api/v1/workflow-requests/" + id
		req.out = &sohaapi.WorkflowRequestReceiptEnvelope{}
	case "approve", "reject":
		req.path += "/" + id + "/" + action
		req.method = http.MethodPost
		req.body = &sohaapi.OperationCommentInput{}
	case "runtime":
		req.path = "/api/v1/delivery/runtime/workflows/" + id
		req.out = &sohaapi.GenericDataEnvelope{}
	default:
		return req, usageError{message: "executions support list/get/trigger/request/approve/reject/runtime"}
	}
	return req, nil
}
