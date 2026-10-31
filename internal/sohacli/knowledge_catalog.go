package sohacli

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func runKnowledgeCatalog(ctx context.Context, args []string, rt Runtime) error {
	if len(args) < 2 {
		return usageError{message: "knowledge catalog requires a resource and action"}
	}
	resource, action := args[0], args[1]
	return runCatalogRequest(ctx, "knowledge "+resource+" "+action, args[2:], rt, func(ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
		return knowledgeCatalogRequest(resource, action, ids, opt)
	})
}

func knowledgeCatalogRequest(resource, action string, ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
	id, err := aiWorkbenchID(ids, action == "get" || action == "update" || action == "delete")
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: "/api/v1/ai/knowledge-bases"}
	if resource == "bases" {
		switch action {
		case "list":
			req.out = &sohaapi.KnowledgeBaseListEnvelope{}
		case "get":
			req.path += "/" + id
			req.out = &sohaapi.KnowledgeBaseEnvelope{}
		case "create":
			req.method = http.MethodPost
			req.body = &sohaapi.KnowledgeBaseInput{}
			req.out = &sohaapi.KnowledgeBaseEnvelope{}
		case "update":
			req.method = http.MethodPatch
			req.path += "/" + id
			req.body = &sohaapi.KnowledgeBaseInput{}
			req.out = &sohaapi.KnowledgeBaseEnvelope{}
		case "delete":
			req.method = http.MethodDelete
			req.path += "/" + id
		default:
			return req, usageError{message: "unsupported knowledge base action"}
		}
		return req, nil
	}
	base, err := catalogID(opt.base)
	if err != nil {
		return req, usageError{message: "this operation requires --base-id"}
	}
	req.path += "/" + base + "/" + resource
	req.allowed = []string{"base-id"}
	switch resource + "/" + action {
	case "sources/list":
		req.out = &sohaapi.KnowledgeSourceListEnvelope{}
	case "sources/create":
		req.method = http.MethodPost
		req.body = &sohaapi.KnowledgeSourceInput{}
		req.out = &sohaapi.KnowledgeSourceEnvelope{}
	case "documents/list":
		req.out = &sohaapi.KnowledgeDocumentListEnvelope{}
	case "sync-runs/list":
		req.out = &sohaapi.KnowledgeSyncRunListEnvelope{}
	case "index-revisions/list":
		req.out = &sohaapi.KnowledgeIndexRevisionListEnvelope{}
	default:
		return req, usageError{message: "sources support list/create; documents, sync-runs and index-revisions support list"}
	}
	if resource != "sources" {
		if opt.limit < 1 || opt.limit > 200 {
			return req, usageError{message: "limit must be from 1 to 200"}
		}
		req.path += "?" + url.Values{"limit": {strconv.Itoa(opt.limit)}}.Encode()
		req.allowed = append(req.allowed, "limit")
	}
	return req, nil
}
