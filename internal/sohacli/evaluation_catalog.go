package sohacli

import (
	"context"
	"net/http"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func runEvaluationCatalog(ctx context.Context, args []string, rt Runtime) error {
	if len(args) < 2 {
		return usageError{message: "evaluation datasets/runs require an action"}
	}
	resource, action := args[0], args[1]
	return runCatalogRequest(ctx, "ai evaluation "+resource+" "+action, args[2:], rt, func(ids []string, opt catalogOptions) (aiWorkbenchRequest, error) {
		return evaluationCatalogRequest(resource, action, ids)
	})
}

func evaluationCatalogRequest(resource, action string, ids []string) (aiWorkbenchRequest, error) {
	id, err := aiWorkbenchID(ids, action != "list" && action != "create")
	if err != nil {
		return aiWorkbenchRequest{}, err
	}
	req := aiWorkbenchRequest{method: http.MethodGet, path: "/api/v1/ai/evaluations/" + resource}
	switch resource + "/" + action {
	case "datasets/list":
		req.out = &sohaapi.EvaluationDatasetListEnvelope{}
	case "datasets/create":
		req.method = http.MethodPost
		req.body = &sohaapi.EvaluationDataset{}
		req.out = &sohaapi.EvaluationDatasetEnvelope{}
	case "runs/list":
		req.out = &sohaapi.EvaluationRunListEnvelope{}
	case "runs/get":
		req.path += "/" + id
		req.out = &sohaapi.EvaluationRunEnvelope{}
	case "runs/create":
		req.method = http.MethodPost
		req.body = &sohaapi.EvaluationRun{}
		req.out = &sohaapi.EvaluationRunEnvelope{}
	case "runs/results":
		req.path += "/" + id + "/results"
		req.out = &sohaapi.EvaluationResultListEnvelope{}
	case "runs/attempts":
		req.path += "/" + id + "/attempts"
		req.out = &sohaapi.EvaluationSampleAttemptListEnvelope{}
	case "runs/complete":
		req.method = http.MethodPost
		req.path += "/" + id + "/complete"
		req.body = &sohaapi.EvaluationCompleteRunInput{}
		req.out = &sohaapi.EvaluationRunEnvelope{}
	default:
		return req, usageError{message: "unsupported evaluation catalog action"}
	}
	return req, nil
}
