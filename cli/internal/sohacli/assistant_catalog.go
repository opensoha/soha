package sohacli

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func runAssistantCatalog(ctx context.Context, args []string, rt Runtime) error {
	if len(args) < 2 {
		return usageError{message: "assistant environment/runtime/feed requires an action"}
	}
	resource, action := args[0], args[1]
	return runCatalogRequest(ctx, "ai assistant "+resource+" "+action, args[2:], rt, func(ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
		return assistantCatalogRequest(resource, action, ids, opt)
	})
}

func assistantCatalogRequest(resource, action string, ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
	named := resource == "runtime" && action == "operation" || resource == "feed" && (action == "get" || action == "feedback")
	id, err := aiWorkbenchID(ids, named)
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: copilotPath + "/assistant/"}
	switch resource + "/" + action {
	case "environment/get":
		req.path += "environment"
		req.out = &sohaapi.PersonalEnvironmentEnvelope{}
	case "environment/templates":
		req.path += "environment/templates"
		req.out = &sohaapi.PersonalEnvironmentTemplateListEnvelope{}
	case "environment/change":
		req.path += "environment"
		req.method = http.MethodPost
		req.body = &sohaapi.PersonalEnvironmentActionRequest{}
		req.out = &sohaapi.PersonalEnvironmentEnvelope{}
	case "runtime/get":
		req.path += "environment/runtime"
		req.out = &sohaapi.PersonalWorkspaceRuntimeEnvelope{}
	case "runtime/operation":
		req.path += "environment/runtime/" + id
		req.out = &sohaapi.PersonalGuestRuntimeEnvelope{}
	case "runtime/execute":
		req.path += "environment/runtime"
		req.method = http.MethodPost
		req.body = &sohaapi.PersonalGuestRuntimeInput{}
		req.out = &sohaapi.PersonalGuestRuntimeEnvelope{}
	case "feed/list":
		if opt.limit < 1 || opt.limit > 50 {
			return req, usageError{message: "feed limit must be from 1 to 50"}
		}
		req.path += "feed?" + url.Values{"limit": {strconv.Itoa(opt.limit)}, "before": {opt.before}}.Encode()
		req.allowed = []string{"limit", "before"}
		req.out = &sohaapi.PersonalFeedEnvelope{}
	case "feed/get":
		req.path += "feed/items/" + id
		req.out = &sohaapi.PersonalFeedItemEnvelope{}
	case "feed/feedback":
		req.path += "feed/items/" + id
		req.method = http.MethodPatch
		req.body = &sohaapi.PersonalFeedFeedback{}
		req.out = &sohaapi.PersonalFeedItemEnvelope{}
	case "feed/settings":
		req.path += "feed/settings"
		req.method = http.MethodPut
		req.body = &sohaapi.PersonalFeedSettings{}
		req.out = &sohaapi.PersonalFeedEnvelope{}
	case "feed/refresh":
		req.path += "feed/refresh"
		req.method = http.MethodPost
		req.out = &sohaapi.PersonalFeedEnvelope{}
	default:
		return req, usageError{message: "unsupported assistant environment, runtime or feed action"}
	}
	return req, nil
}
