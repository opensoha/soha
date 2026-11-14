package sohacli

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func runDirectory(ctx context.Context, args []string, rt Runtime) error {
	if len(args) < 2 {
		return usageError{message: "directory requires sync, runs, runtime or events and an action"}
	}
	resource, action := args[0], args[1]
	if resource == "sync" && (action == "start" || action == "request") {
		return runDirectorySyncRequest(ctx, action, args[2:], rt)
	}
	return runCatalogRequest(ctx, "directory "+resource+" "+action, args[2:], rt, func(ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
		return directoryRequest(resource, action, ids, opt)
	})
}

func directoryRequest(resource, action string, ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
	named := resource == "runs" && (action == "get" || action == "cancel") || resource == "events" && action == "retry"
	id, err := aiWorkbenchID(ids, named)
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: "/api/v1/access/", out: &sohaapi.GenericDataEnvelope{}}
	if resource == "runs" && action == "get" {
		req.path += "directory-runs/" + id
		return req, nil
	}
	connection, err := catalogID(opt.connection)
	if err != nil {
		return req, usageError{message: "this operation requires --connection-id"}
	}
	req.path += "directory-connections/" + connection
	req.allowed = []string{"connection-id"}
	switch resource + "/" + action {
	case "sync/preview":
		req.method = http.MethodPost
		req.path += "/sync/preview"
		req.readOnly = true
	case "runs/cancel":
		req.method = http.MethodPost
		req.path += "/runs/" + id + "/cancel"
	case "runs/list":
		req.path += "/runs"
		req.out = &sohaapi.GenericItemsEnvelope{}
	case "events/list":
		if opt.limit < 1 || opt.limit > 200 {
			return req, usageError{message: "limit must be from 1 to 200"}
		}
		query := url.Values{"limit": {strconv.Itoa(opt.limit)}, "status": {opt.status}}
		req.allowed = append(req.allowed, "limit", "status")
		req.out = &sohaapi.DirectoryEventListEnvelope{}
		req.path += "/events?" + query.Encode()

	case "runtime/get":
		req.path += "/runtime-status"
		req.out = &sohaapi.DirectoryRuntimeStatusEnvelope{}
	case "events/retry":
		req.method = http.MethodPost
		req.path += "/events/" + id + "/retry"
		req.out = &sohaapi.DirectoryEventEnvelope{}
	default:
		return req, usageError{message: "unsupported directory operation; deletion and provider callbacks are not CLI management commands"}
	}
	return req, nil
}
