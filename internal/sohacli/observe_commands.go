package sohacli

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func runObserve(ctx context.Context, args []string, rt Runtime) error {
	if len(args) == 0 {
		return usageError{message: "observe requires summary, metrics, traces, alerts, events or healing-runs"}
	}
	resource, action, rest := args[0], "get", args[1:]
	if resource != "summary" {
		if len(rest) == 0 {
			return usageError{message: "observe resource requires an action"}
		}
		action, rest = rest[0], rest[1:]
	}
	if (resource == "events" && action == "heal") || (resource == "healing-runs" && action == "request") {
		return runHealingOriginalRequest(ctx, action, rest, rt)
	}
	return runCatalogRequest(ctx, "observe "+resource+" "+action, rest, rt, func(ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
		return observeRequest(resource, action, ids, opt)
	})
}

func observeRequest(resource, action string, ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
	named := resource != "summary" && action != "list" && action != "query"
	id, err := aiWorkbenchID(ids, named)
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: "/api/v1/", out: &sohaapi.GenericDataEnvelope{}}
	switch resource {
	case "summary":
		req.path += "monitoring/summary"
		return req, nil
	case "metrics", "traces":
		if action != "query" {
			return req, usageError{message: "metrics/traces support query"}
		}
		req.path += "observability/" + resource + "/query"
		req.method = http.MethodPost
		req.readOnly = true
		if resource == "metrics" {
			req.body = &sohaapi.ObservabilityMetricQueryInput{}
			req.out = &sohaapi.ObservabilityMetricQueryEnvelope{}
		} else {
			req.body = &sohaapi.ObservabilityTraceQueryInput{}
			req.out = &sohaapi.ObservabilityTraceQueryEnvelope{}
		}
		return req, nil
	case "alerts":
		req.path += "alerts"
	case "events":
		req.path += "alert-events"
	case "healing-runs":
		req.path += "healing-runs"
	default:
		return req, usageError{message: "unsupported observability resource"}
	}
	if action == "list" {
		return observeListRequest(req, resource, opt)
	}
	req.path += "/" + id
	if action == "get" {
		return req, nil
	}
	req.method = http.MethodPost
	switch resource + "/" + action {
	case "alerts/assign":
		req.method = http.MethodPut
		req.path += "/ownership"
		req.body = &sohaapi.AlertOwnershipInput{}
	case "alerts/acknowledge", "events/acknowledge", "events/resolve", "healing-runs/retry":
		req.path += "/" + action
	case "events/heal":
		_, err := catalogID(opt.policy)
		if err != nil {
			return req, usageError{message: "heal requires --policy-id"}
		}
		req.path += "/heal?" + url.Values{"policyId": {opt.policy}}.Encode()
		req.allowed = []string{"policy-id"}
	case "healing-runs/approve", "healing-runs/reject":
		req.path += "/" + action
		req.body = &sohaapi.OperationCommentInput{}
	default:
		return req, usageError{message: "unsupported observability action"}
	}
	return req, nil
}

func observeListRequest(req aiWorkbenchRequest, resource string, opt catalogOptions) (aiWorkbenchRequest, error) {
	if opt.limit < 1 || opt.limit > 200 {
		return req, usageError{message: "limit must be from 1 to 200"}
	}
	query := url.Values{"limit": {strconv.Itoa(opt.limit)}}
	if resource == "events" {
		req.allowed = append(req.allowed, "rule-id")
		if opt.rule != "" {
			query.Set("ruleId", opt.rule)
		}
	}
	if resource == "healing-runs" {
		req.allowed = append(req.allowed, "event-id")
		if opt.event != "" {
			query.Set("eventId", opt.event)
		}
	}
	req.allowed = append(req.allowed, "limit", "status")
	if opt.status != "" {
		query.Set("status", opt.status)
	}
	if resource == "healing-runs" {
		req.allowed = append(req.allowed, "policy-id")
		if opt.policy != "" {
			query.Set("policyId", opt.policy)
		}
	} else {
		req.allowed = append(req.allowed, "cluster-id")
		if opt.cluster != "" {
			query.Set("clusterId", opt.cluster)
		}
	}
	req.path += "?" + query.Encode()
	req.out = &sohaapi.GenericItemsEnvelope{}
	return req, nil
}
