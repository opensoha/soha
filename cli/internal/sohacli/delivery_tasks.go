package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func deliveryTaskRequest(action string, ids []string, opt deliveryWorkbenchOptions) (deliveryWorkbenchRequest, error) {
	id, err := deliveryWorkbenchID(ids, action != "list")
	if err != nil {
		return deliveryWorkbenchRequest{}, err
	}
	req := deliveryWorkbenchRequest{method: http.MethodGet, path: "/api/v1/delivery/execution-tasks", id: id}
	if id != "" {
		req.path += "/" + url.PathEscape(id)
	}
	switch action {
	case "list":
		query, err := deliveryTaskFilters(opt)
		if err != nil {
			return req, err
		}
		req.path, req.out, req.list = deliveryQuery(req.path, query), &sohaapi.ExecutionTaskListEnvelope{}, true
		req.allowed = []string{"application-id", "application-environment-id", "release-bundle-id", "status", "provider-kind", "limit"}
	case "get", "wait":
		req.out = &sohaapi.ExecutionTaskEnvelope{}
		if action == "wait" {
			if err := validateDeliveryWaitOptions(opt); err != nil {
				return req, err
			}
			req.allowed = []string{"wait-timeout", "poll-interval"}
		}
	case "logs":
		if opt.limit < 1 || opt.limit > 1000 {
			return req, usageError{message: "--limit must be 1..1000 for task logs"}
		}
		req.path += "/logs?limit=" + strconv.Itoa(opt.limit)
		req.out, req.allowed = &sohaapi.ExecutionLogListEnvelope{}, []string{"limit"}
	case "artifacts":
		req.path += "/artifacts"
		req.out, req.list = &sohaapi.ExecutionArtifactListEnvelope{}, true
	case "rollout":
		req.path += "/rollout"
		req.out = &sohaapi.ProgressiveRolloutStatusEnvelope{}
	case "rollout-control":
		req.path += "/rollout"
		req.method, req.body, req.out = http.MethodPost, &sohaapi.ProgressiveRolloutControlInput{}, &sohaapi.ProgressiveRolloutStatusEnvelope{}
	case "cancel", "retry":
		if !utf8.ValidString(opt.reason) || utf8.RuneCountInString(opt.reason) > 2048 {
			return req, usageError{message: "--reason must contain at most 2048 UTF-8 characters"}
		}
		req.path += "/control"
		req.method, req.body, req.out = http.MethodPost, &sohaapi.ExecutionTaskControlInput{Action: sohaapi.ExecutionTaskControlInputAction(action), Reason: opt.reason}, &sohaapi.ExecutionTaskEnvelope{}
		req.allowed = []string{"reason"}
	default:
		return req, deliveryUnsupportedAction("tasks", action)
	}
	return req, nil
}

func validateDeliveryWaitOptions(opt deliveryWorkbenchOptions) error {
	if opt.waitTimeout < time.Second || opt.waitTimeout > 24*time.Hour || opt.pollInterval < 100*time.Millisecond || opt.pollInterval > 30*time.Second {
		return usageError{message: "--wait-timeout must be 1s..24h and --poll-interval 100ms..30s"}
	}
	return nil
}

func deliveryTaskFilters(opt deliveryWorkbenchOptions) (url.Values, error) {
	if opt.limit < 1 || opt.limit > 200 {
		return nil, usageError{message: "--limit must be 1..200 for task lists"}
	}
	query := url.Values{"limit": {strconv.Itoa(opt.limit)}}
	for key, value := range map[string]string{"applicationId": opt.application, "applicationEnvironmentId": opt.binding, "releaseBundleId": opt.bundle, "status": opt.status, "providerKind": opt.provider} {
		if err := deliveryFilter(query, key, value); err != nil {
			return nil, err
		}
	}
	return query, nil
}

func validateDeliveryTask(task *sohaapi.ExecutionTask, id string, control bool) error {
	if task.ID == "" || id != "" && task.ID != id || task.Status == "" || task.ProviderKind == "" || task.TaskKind == "" {
		return fmt.Errorf("invalid execution task receipt; inspect the original task ID")
	}
	if control && (task.CallbackToken != "" || task.SecretLease != nil) {
		return fmt.Errorf("task control server returned a runner credential; response withheld")
	}
	// Old Core read routes can include runner fields. Never render those values.
	task.CallbackToken, task.SecretLease = "", nil
	if task.Payload == nil {
		task.Payload = map[string]any{}
	}
	return nil
}

func waitDeliveryTask(ctx context.Context, rt Runtime, client APIClient, req deliveryWorkbenchRequest, opt deliveryWorkbenchOptions, format string) error {
	if rt.StreamingContext != nil {
		var stop context.CancelFunc
		ctx, stop = rt.StreamingContext(ctx)
		defer stop()
	}
	ctx, cancel := context.WithTimeout(ctx, opt.waitTimeout)
	defer cancel()
	for {
		if err := executeDeliveryWorkbench(ctx, client, req); err != nil {
			return fmt.Errorf("%w; detached from original task %s; no task cancellation was requested", err, terminalLabel(req.id))
		}
		if err := validateDeliveryWorkbenchResult(req); err != nil {
			return err
		}
		envelope, ok := req.out.(*sohaapi.ExecutionTaskEnvelope)
		if !ok || envelope == nil {
			return fmt.Errorf("invalid execution task response; inspect the original task before another action")
		}
		task := envelope.Data
		switch task.Status {
		case "completed", "failed", "canceled", "callback_timeout":
			if err := writeWorkbenchOutput(rt, client, format, req.out); err != nil {
				return err
			}
			if task.Status == "canceled" {
				return fmt.Errorf("execution task was canceled")
			}
			return deliveryWorkbenchOutcome(req.out)
		case "queued", "dispatching", "running":
		default:
			if err := writeWorkbenchOutput(rt, client, format, req.out); err != nil {
				return err
			}
			return fmt.Errorf("task has an unsupported status; preserve original task %s and inspect it before another action", terminalLabel(req.id))
		}
		timer := time.NewTimer(opt.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if err := writeWorkbenchOutput(rt, client, format, req.out); err != nil {
				return err
			}
			return fmt.Errorf("%w; detached from original task %s; no task cancellation was requested", ctx.Err(), terminalLabel(req.id))
		case <-timer.C:
		}
	}
}
