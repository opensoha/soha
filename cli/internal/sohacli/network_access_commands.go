package sohacli

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func runNetworkAccess(ctx context.Context, args []string, rt Runtime) error {
	if len(args) < 2 {
		return usageError{message: "network-access requires sessions, telemetry, enrollments, access-grants, policy or conflicts and an action"}
	}
	resource, action := args[0], args[1]
	if resource == "sessions" && action == "execute" {
		return runNetworkSessionExecute(ctx, args[2:], rt)
	}
	if resource == "sessions" && (action == "command" || action == "request") {
		return runNetworkSessionLookup(ctx, action, args[2:], rt)
	}
	return runCatalogRequest(ctx, "network-access "+resource+" "+action, args[2:], rt, func(ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
		return networkAccessRequest(resource, action, ids, opt)
	})
}

func networkAccessRequest(resource, action string, ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
	named := action == "get" && resource != "telemetry" || action == "revoke" || action == "plan" || action == "execute" || resource == "sessions" && (action == "command" || action == "request")
	id, err := aiWorkbenchID(ids, named)
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: "/api/v1/network-access/"}
	switch resource {
	case "sessions":
		return networkSessionRequest(action, id, opt)
	case "enrollments", "access-grants":
		return networkGrantRequest(resource, action, id, opt)
	case "telemetry":
		if action != "get" {
			return req, usageError{message: "telemetry supports get"}
		}
		req.path += "telemetry/summary"
		req.out = &sohaapi.NetworkTelemetrySummaryEnvelope{}
		return req, nil
	case "policy":
		switch action {
		case "snapshot":
			req.path += "policy/snapshot"
			req.out = &sohaapi.NetworkPolicySnapshotEnvelope{}
		case "preview":
			req.path += "policy/preview"
			req.method = http.MethodPost
			req.body = &sohaapi.NetworkPolicyPreviewRequest{}
			req.out = &sohaapi.NetworkPolicyPreviewEnvelope{}
			req.readOnly = true
		default:
			return req, usageError{message: "policy supports snapshot/preview; configuration remains managed by its owner"}
		}
		return req, nil
	case "conflicts":
		if action != "analyze" {
			return req, usageError{message: "conflicts supports analyze"}
		}
		req.path += "conflicts/analyze"
		req.method = http.MethodPost
		req.body = &sohaapi.NetworkConflictAnalysisRequest{}
		req.out = &sohaapi.NetworkConflictAnalysisEnvelope{}
		req.readOnly = true
		return req, nil
	default:
		return req, usageError{message: "unsupported network-access resource"}
	}
}

func networkGrantRequest(resource, action, id string, opt catalogOptions) (aiWorkbenchRequest, error) {
	req := aiWorkbenchRequest{method: http.MethodGet, path: "/api/v1/network-access/" + resource}
	grant := resource == "access-grants"
	switch action {
	case "list":
		if opt.limit < 1 || opt.limit > 200 {
			return req, usageError{message: "limit must be from 1 to 200"}
		}
		req.path += "?" + url.Values{"limit": {strconv.Itoa(opt.limit)}}.Encode()
		req.allowed = []string{"limit"}
		if grant {
			req.out = &sohaapi.NetworkAccessGrantListEnvelope{}
		} else {
			req.out = &sohaapi.NetworkRuntimeEnrollmentListEnvelope{}
		}
	case "get", "revoke":
		req.path += "/" + id
		if action == "revoke" {
			req.path += "/revoke"
			req.method = http.MethodPost
		}
		if grant {
			req.out = &sohaapi.NetworkAccessGrantEnvelope{}
		} else {
			req.out = &sohaapi.NetworkRuntimeEnrollmentEnvelope{}
		}
	case "create":
		req.method = http.MethodPost
		req.allowed = []string{"show-access-details"}
		if grant {
			req.body = &sohaapi.NetworkAccessGrantInput{}
			req.out = &sohaapi.NetworkAccessGrantSecretEnvelope{}
		} else {
			req.body = &sohaapi.NetworkRuntimeEnrollmentInput{}
			req.out = &sohaapi.NetworkRuntimeEnrollmentSecretEnvelope{}
		}
	default:
		return req, usageError{message: "enrollments/access-grants support list/get/create/revoke"}
	}
	return req, nil
}

func networkSessionRequest(action, id string, opt catalogOptions) (aiWorkbenchRequest, error) {
	req := aiWorkbenchRequest{method: http.MethodGet, path: "/api/v1/network-access/"}
	if action == "command" || action == "request" {
		return networkSessionCommandRequest(action, id, opt)
	}
	req.path += "sessions"
	switch action {
	case "list":
		req.out = &sohaapi.NetworkSessionListEnvelope{}
	case "get":
		req.path += "/" + id
		req.out = &sohaapi.NetworkSessionEnvelope{}
	case "plan":
		req.path += "/" + id + "/actions/plan"
		req.method = http.MethodPost
		req.body = &sohaapi.NetworkSessionActionInput{}
		req.out = &sohaapi.NetworkSessionActionPlanEnvelope{}
		req.readOnly = true
	case "execute":
		req.path += "/" + id + "/actions/execute"
		req.method = http.MethodPost
		req.body = &sohaapi.NetworkSessionActionExecuteInput{}
		req.out = &sohaapi.NetworkSessionCommandEnvelope{}
	default:
		return req, usageError{message: "unsupported network session action"}
	}
	if action == "list" {
		if opt.limit < 1 || opt.limit > 200 {
			return req, usageError{message: "limit must be from 1 to 200"}
		}
		req.path += "?" + url.Values{"limit": {strconv.Itoa(opt.limit)}}.Encode()
		req.allowed = []string{"limit"}
	}
	return req, nil
}

var networkPlanHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func networkSessionCommandRequest(action, id string, opt catalogOptions) (aiWorkbenchRequest, error) {
	req := aiWorkbenchRequest{method: http.MethodGet, out: &sohaapi.NetworkSessionCommandEnvelope{}, allowed: []string{"session-id"}}
	session, err := catalogID(opt.session)
	if err != nil {
		return req, usageError{message: "--session-id requires the owning network session ID"}
	}
	req.path = "/api/v1/network-access/sessions/" + session
	if action == "command" {
		req.path += "/commands/" + id
		return req, nil
	}
	if !networkPlanHashPattern.MatchString(id) {
		return req, usageError{message: "request requires the original reviewed sha256 plan hash"}
	}
	req.path += "/command-request?" + url.Values{"planHash": {id}}.Encode()
	return req, nil
}
