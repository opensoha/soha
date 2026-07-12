package workflow

import (
	"context"
	"errors"
	"sync"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

var (
	errWorkflowRunnerUnavailable = errors.New("workflow runner is not available")
	errWorkflowRunnerStopped     = errors.New("workflow runner stopped")
)

type dagRunTask struct {
	principal   domainidentity.Principal
	app         domainapp.App
	input       domainworkflow.Input
	binding     domaincatalog.ApplicationEnvironment
	definition  dagWorkflowDefinition
	run         domainworkflow.Run
	requestMeta requestctx.Metadata
}

type dagScheduler struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	queue       chan dagRunTask
	isClosed    bool
	isStarted   bool
	workerCount int
	queueSize   int
	workers     sync.WaitGroup
}

func (s *dagScheduler) configure(workerCount, queueSize int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isStarted {
		return
	}
	if workerCount > 0 {
		s.workerCount = workerCount
	}
	if queueSize > 0 {
		s.queueSize = queueSize
	}
}

func (s *dagScheduler) start(ctx context.Context, consume func(context.Context, dagRunTask)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isStarted || s.isClosed {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runnerCtx, cancel := context.WithCancel(ctx)
	s.ctx = runnerCtx
	s.cancel = cancel
	queueSize := s.queueSize
	if queueSize <= 0 {
		queueSize = defaultAsyncWorkflowQueueSize
	}
	s.queue = make(chan dagRunTask, queueSize)
	s.isStarted = true

	workerCount := s.workerCount
	if workerCount <= 0 {
		workerCount = defaultAsyncWorkflowWorkers
	}
	for i := 0; i < workerCount; i++ {
		s.workers.Add(1)
		go s.runWorker(runnerCtx, consume)
	}
}

func (s *dagScheduler) runWorker(ctx context.Context, consume func(context.Context, dagRunTask)) {
	defer s.workers.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-s.queue:
			if consume != nil {
				consume(ctx, task)
			}
		}
	}
}

func (s *dagScheduler) enqueue(ctx context.Context, task dagRunTask) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	queue := s.queue
	runnerCtx := s.ctx
	closed := s.isClosed
	s.mu.Unlock()
	if closed || queue == nil || runnerCtx == nil {
		return 0, errWorkflowRunnerUnavailable
	}
	if runnerCtx.Err() != nil {
		return len(queue), errWorkflowRunnerStopped
	}
	select {
	case queue <- task:
		return len(queue), nil
	case <-runnerCtx.Done():
		return len(queue), errWorkflowRunnerStopped
	case <-ctx.Done():
		return len(queue), ctx.Err()
	}
}

func (s *dagScheduler) shutdown(ctx context.Context) ([]dagRunTask, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if !s.isStarted {
		s.isClosed = true
		s.mu.Unlock()
		return nil, nil
	}
	s.isClosed = true
	cancel := s.cancel
	queue := s.queue
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})
	go func() {
		s.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	pending := make([]dagRunTask, 0, len(queue))
	for {
		select {
		case task := <-queue:
			pending = append(pending, task)
		default:
			return pending, nil
		}
	}
}

func (s *dagScheduler) depth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.queue == nil {
		return 0
	}
	return len(s.queue)
}
