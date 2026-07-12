package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultDAGNodeTimeout = 300 * time.Second
	defaultDAGHTTPTimeout = 10 * time.Second
)

type dagNodeExecutionPolicy struct {
	timeout     time.Duration
	maxAttempts int
	retryDelay  time.Duration
}

func dagNodePolicy(node dagWorkflowNode) dagNodeExecutionPolicy {
	timeoutSeconds := node.TimeoutSeconds
	if configured := toInt(node.Config["timeoutSeconds"], 0); configured > 0 {
		timeoutSeconds = configured
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultDAGNodeTimeout
	}

	retry := toConfigMap(node.Config["retry"])
	maxAttempts := firstPositiveInt(
		toInt(retry["maxAttempts"], 0),
		toInt(node.Config["retryMaxAttempts"], 0),
		1,
	)
	if maxRetries := toInt(node.Config["maxRetries"], -1); maxRetries >= 0 {
		maxAttempts = maxRetries + 1
	}
	delayMilliseconds := firstPositiveInt(
		toInt(retry["delayMilliseconds"], 0),
		toInt(node.Config["retryDelayMilliseconds"], 0),
	)
	retryDelay := time.Duration(delayMilliseconds) * time.Millisecond
	if retryDelay <= 0 {
		retryDelay = time.Duration(firstPositiveInt(
			toInt(retry["delaySeconds"], 0),
			toInt(node.Config["retryDelaySeconds"], 0),
		)) * time.Second
	}
	return dagNodeExecutionPolicy{timeout: timeout, maxAttempts: maxAttempts, retryDelay: retryDelay}
}

func (p dagNodeExecutionPolicy) execute(ctx context.Context, handler dagNodeHandler, input dagNodeExecutionInput) dagNodeHandlerResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if p.timeout <= 0 {
		p.timeout = defaultDAGNodeTimeout
	}
	if p.maxAttempts <= 0 {
		p.maxAttempts = 1
	}

	retryEvents := make([]map[string]any, 0, p.maxAttempts-1)
	for attempt := 1; attempt <= p.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			result := failedDAGNode(err)
			result.events = retryEvents
			return result
		}

		attemptCtx, cancel := context.WithTimeout(ctx, p.timeout)
		result := handler.execute(attemptCtx, input)
		attemptErr := attemptCtx.Err()
		cancel()

		if errors.Is(attemptErr, context.DeadlineExceeded) {
			result = failedDAGNode(fmt.Errorf("node timed out after %s", p.timeout))
		} else if errors.Is(attemptErr, context.Canceled) && ctx.Err() != nil {
			result = failedDAGNode(ctx.Err())
		}
		if result.status == "" {
			result.status = "completed"
		}
		if result.outputs == nil {
			result.outputs = map[string]any{}
		}
		if result.status != "failed" || attempt == p.maxAttempts || ctx.Err() != nil {
			result.events = append(retryEvents, result.events...)
			return result
		}

		retryEvents = append(retryEvents, map[string]any{
			"type":        "node_retry_scheduled",
			"attempt":     attempt,
			"maxAttempts": p.maxAttempts,
			"summary":     result.summary,
		})
		if p.retryDelay > 0 {
			timer := time.NewTimer(p.retryDelay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				canceled := failedDAGNode(ctx.Err())
				canceled.events = retryEvents
				return canceled
			case <-timer.C:
			}
		}
	}

	return failedDAGNode(errors.New("node execution exhausted retry policy"))
}
