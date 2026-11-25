package copilot

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	dshReceptionPrefix    = "heya-reception-"
	dshReceptionProfile   = "heya-receptionist"
	dshReceptionModel     = "soha-workbench"
	dshReceptionCompleted = "reception-complete"
)

// The inquiry owns durability and recovery. An abandoned callback grant must
// never survive the request, and DSH never receives the inquiry's text or facts.
type dshReceptionRounds struct {
	mu     sync.Mutex
	active map[string]*dshReceptionRound
}

type dshReceptionRound struct {
	ctx    context.Context
	start  domain.DSHStart
	invoke func(context.Context) (aigateway.WorkbenchRelayResponse, error)

	mu        sync.Mutex
	assembled bool
	blocked   bool
	called    bool
	answer    *aigateway.WorkbenchRelayResponse
}

func (s *Service) RunDSHReception(ctx context.Context, callerID string, invoke func(context.Context) (aigateway.WorkbenchRelayResponse, error)) (aigateway.WorkbenchRelayResponse, error) {
	if s.dshReception == nil || callerID == "" || invoke == nil {
		return aigateway.WorkbenchRelayResponse{}, apperrors.ErrServiceUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	id := dshReceptionPrefix + uuid.NewString()
	start := domain.DSHStart{
		DSHIdentity: domain.DSHIdentity{RunID: id, Attempt: 1, Fence: 1},
		OwnerKey:    dshHash(uuid.NewString()), SessionID: id, RequestID: id, InputHash: dshHash(id),
		ContextRevision: dshHash(dshReceptionProfile), ToolCatalogRevision: dshHash([]domain.DSHTool{}),
		Profile: "chat", Model: dshReceptionModel, Prompt: "Complete the isolated reception request.",
		ToolGrant: uuid.NewString(), Deadline: deadline.UTC().Format(time.RFC3339Nano), MaxOutputTokens: 650,
		Assembly: &domain.DSHAssembly{ProfileID: dshReceptionProfile, Revision: 1, Assets: []domain.RuntimeAssetRef{}},
	}
	round := &dshReceptionRound{ctx: ctx, start: start, invoke: invoke}
	if !s.dshReception.add(round) {
		return aigateway.WorkbenchRelayResponse{}, apperrors.ErrServiceUnavailable
	}
	defer s.dshReception.remove(id)
	receipt, err := s.dshRuntime.Start(ctx, start)
	if err != nil {
		cancel()
		if !errors.Is(err, ErrDSHStartRejected) {
			s.cancelDSHReception(ctx, start, receipt.DSHSubmission)
		}
		return aigateway.WorkbenchRelayResponse{}, err
	}
	status, err := s.waitDSHReception(ctx, receipt.DSHSubmission)
	if err != nil {
		cancel()
		s.cancelDSHReception(ctx, start, receipt.DSHSubmission)
		return aigateway.WorkbenchRelayResponse{}, err
	}
	return round.result(status)
}

func (s *Service) cancelDSHReception(ctx context.Context, start domain.DSHStart, submission domain.DSHSubmission) {
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer stop()
	if submission.SubmissionID == "" {
		status, err := s.dshRuntime.Lookup(cleanup, start.DSHIdentity, start.InputHash)
		if err != nil || status.Terminal() || status.SubmissionID == "" {
			return
		}
		submission = status.DSHSubmission
	}
	_, _ = s.dshRuntime.Cancel(cleanup, submission, "reception_abandoned")
}

func (s *Service) waitDSHReception(ctx context.Context, submission domain.DSHSubmission) (domain.DSHStatus, error) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return domain.DSHStatus{}, ctx.Err()
		case <-ticker.C:
			status, err := s.dshRuntime.Status(ctx, submission)
			if err != nil || status.Terminal() || status.Status == "unknown" {
				return status, err
			}
		}
	}
}

func (r *dshReceptionRound) result(status domain.DSHStatus) (aigateway.WorkbenchRelayResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil || r.blocked || status.Status != "completed" || status.AssistantText != dshReceptionCompleted || r.answer == nil {
		return aigateway.WorkbenchRelayResponse{}, apperrors.ErrAccessDenied
	}
	return *r.answer, nil
}

func (r *dshReceptionRounds) add(round *dshReceptionRound) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.active) >= 4 {
		return false
	}
	r.active[round.start.RunID] = round
	return true
}

func (r *dshReceptionRounds) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, id)
}

func (s *Service) authorizedReceptionRound(grant string, id domain.DSHIdentity) (*dshReceptionRound, error) {
	if s.dshReception == nil {
		return nil, apperrors.ErrAccessDenied
	}
	s.dshReception.mu.Lock()
	round := s.dshReception.active[id.RunID]
	s.dshReception.mu.Unlock()
	if round == nil || round.start.DSHIdentity != id || subtle.ConstantTimeCompare([]byte(grant), []byte(round.start.ToolGrant)) != 1 || round.ctx.Err() != nil {
		return nil, apperrors.ErrAccessDenied
	}
	return round, nil
}

func (s *Service) confirmDSHReception(grant string, report domain.DSHAssemblyReport) (domain.DSHAssemblyDecision, error) {
	id := domain.DSHIdentity{RunID: report.RunID, Attempt: report.Attempt, Fence: report.Fence}
	round, err := s.authorizedReceptionRound(grant, id)
	if err != nil {
		return domain.DSHAssemblyDecision{}, err
	}
	round.mu.Lock()
	defer round.mu.Unlock()
	if round.blocked || len(round.start.Tools) != 0 || len(report.NativeTools) != 0 {
		round.blocked = true
		return domain.DSHAssemblyDecision{}, apperrors.ErrAccessDenied
	}
	decision, err := decideDSHAssembly(*round.start.Assembly, report)
	if err != nil {
		round.blocked = true
		return domain.DSHAssemblyDecision{}, err
	}
	round.assembled = true
	return decision, nil
}

func (r *dshReceptionRound) admitModel(in DSHModelCall) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.blocked || !r.assembled || r.ctx.Err() != nil {
		return apperrors.ErrAccessDenied
	}
	hash := sha256.Sum256(in.RawBody)
	if in.CallID == "" || len(in.CallID) > 128 || len(in.RawBody) == 0 || in.InputHash != hex.EncodeToString(hash[:]) ||
		in.ContextRevision != r.start.ContextRevision || in.ToolCatalogRevision != r.start.ToolCatalogRevision ||
		in.Request.PublicModel != r.start.Model {
		return apperrors.ErrAccessDenied
	}
	if len(in.Request.Tools) != 0 || slices.ContainsFunc(in.Request.Messages, func(message aigateway.WorkbenchRelayMessage) bool {
		return message.Role == "tool" || message.ToolCallID != "" || len(message.ToolCalls) != 0
	}) {
		r.blocked = true
		return apperrors.ErrAccessDenied
	}
	if r.called {
		return apperrors.ErrConflict
	}
	r.called = true
	return nil
}

func (s *Service) invokeDSHReception(ctx context.Context, grant string, in DSHModelCall) (aigateway.WorkbenchRelayResponse, error) {
	round, err := s.authorizedReceptionRound(grant, in.DSHIdentity)
	if err != nil {
		return aigateway.WorkbenchRelayResponse{}, err
	}
	if err = round.admitModel(in); err != nil {
		return aigateway.WorkbenchRelayResponse{}, err
	}
	modelCtx, cancel := context.WithCancel(round.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	if err = ctx.Err(); err != nil {
		return aigateway.WorkbenchRelayResponse{}, err
	}
	answer, err := round.invoke(modelCtx)
	if err != nil {
		return aigateway.WorkbenchRelayResponse{}, err
	}
	if len(answer.ToolCalls) != 0 {
		return aigateway.WorkbenchRelayResponse{}, apperrors.ErrAccessDenied
	}
	round.mu.Lock()
	defer round.mu.Unlock()
	if modelCtx.Err() != nil || round.blocked {
		return aigateway.WorkbenchRelayResponse{}, apperrors.ErrAccessDenied
	}
	round.answer = &answer
	// DSH must not journal the provider's answer or its token counts.
	return aigateway.WorkbenchRelayResponse{Content: dshReceptionCompleted, PublicModel: round.start.Model}, nil
}

func isDSHReception(runID string) bool {
	return strings.HasPrefix(runID, dshReceptionPrefix)
}
