package workflow

import (
	"context"
	"sync"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type dagExecutor struct {
	parallelism int
	dispatcher  *dagNodeDispatcher
}

type dagExecutionResult struct {
	nodeID    string
	step      domainworkflow.Step
	status    string
	summary   string
	inputs    map[string]any
	outputs   map[string]any
	artifacts map[string]any
	selectors map[string]any
	events    []map[string]any
}

type dagNodeRun = domainworkflow.NodeRun

type dagExecutorSettings struct {
	mu          sync.Mutex
	parallelism int
}

func (s *dagExecutorSettings) configure(parallelism int) {
	if parallelism <= 0 {
		return
	}
	s.mu.Lock()
	s.parallelism = parallelism
	s.mu.Unlock()
}

func (s *dagExecutorSettings) value() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parallelism <= 0 {
		return defaultDAGNodeConcurrency
	}
	return s.parallelism
}

func newDAGExecutor(service *Service) *dagExecutor {
	parallelism := defaultDAGNodeConcurrency
	if service != nil {
		parallelism = service.executorSettings.value()
	}
	return &dagExecutor{parallelism: parallelism, dispatcher: newDAGNodeDispatcher(service)}
}

func (e *dagExecutor) executeReady(
	ctx context.Context,
	principal domainidentity.Principal,
	app domainapp.App,
	input domainworkflow.Input,
	binding domaincatalog.ApplicationEnvironment,
	ready []dagWorkflowNode,
	run domainworkflow.Run,
	artifactState map[string]any,
) []dagExecutionResult {
	if len(ready) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workerCount := e.parallelism
	if workerCount <= 0 {
		workerCount = defaultDAGNodeConcurrency
	}
	if workerCount > len(ready) {
		workerCount = len(ready)
	}

	jobs := make(chan dagWorkflowNode)
	results := make(chan dagExecutionResult, len(ready))
	var workers sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for node := range jobs {
				results <- e.dispatcher.execute(ctx, principal, app, input, binding, node, run, artifactState)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, node := range ready {
			select {
			case <-ctx.Done():
				return
			case jobs <- node:
			}
		}
	}()
	workers.Wait()
	close(results)

	byNodeID := make(map[string]dagExecutionResult, len(ready))
	for result := range results {
		byNodeID[result.nodeID] = result
	}
	ordered := make([]dagExecutionResult, 0, len(ready))
	for _, node := range ready {
		result, ok := byNodeID[node.ID]
		if !ok {
			summary := "workflow execution canceled"
			if err := ctx.Err(); err != nil {
				summary = err.Error()
			}
			result = newDAGExecutionResult(node, "failed", summary, nil, nil, nil, nil, []map[string]any{{"type": "node_finished", "status": "failed", "summary": summary}})
		}
		ordered = append(ordered, result)
	}
	return ordered
}
