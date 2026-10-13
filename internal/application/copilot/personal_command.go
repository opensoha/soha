package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
)

func personalCommandView(item domain.PersonalCommandRecord) domain.PersonalCommand {
	view := item.PersonalCommand
	if !item.OperationUntil.After(time.Now()) {
		switch view.State {
		case "dispatching":
			view.State = "unknown"
		case "cancelling":
			view.State = "cancellation_unknown"
		}
	}
	if view.State != "succeeded" && view.State != "failed" {
		view.Output, view.OutputDigest = "", ""
	}
	return view
}

func personalCommandCandidate(item domain.PersonalEnvironmentRecord, input domain.PersonalCommandInput) (domain.PersonalCommandRecord, contractenv.Request, error) {
	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey || !utf8.ValidString(input.IdempotencyKey) || strings.ContainsRune(input.IdempotencyKey, '\x00') {
		return domain.PersonalCommandRecord{}, contractenv.Request{}, apperrors.ErrInvalidArgument
	}
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "execute", Lease: item.Lease, TaskID: input.TaskID, Argv: slices.Clone(input.Argv)}
	if input.TaskID == "" || request.Validate() != nil {
		return domain.PersonalCommandRecord{}, contractenv.Request{}, apperrors.ErrInvalidArgument
	}
	if input.TaskDeadline.IsZero() || input.TaskDeadline.After(time.Now().Add(10*time.Minute)) {
		return domain.PersonalCommandRecord{}, request, apperrors.ErrInvalidArgument
	}
	// A new lease does not turn a repeated logical command into new work. The
	// immutable binding, task and argv remain part of its request identity.
	raw, err := json.Marshal(struct {
		Node, Owner, Scope, Mode, Task string
		Quota                          contractenv.Quota
		Argv                           []string
	}{item.AgentClusterID, item.AssistantID, item.Lease.ScopeHash, item.Lease.Mode, input.TaskID, item.Lease.Quota, request.Argv})
	if err != nil {
		return domain.PersonalCommandRecord{}, request, err
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("personal-command\x00"+item.AssistantID+"\x00"+input.IdempotencyKey)).String()
	digest := sha256.Sum256(raw)
	return domain.PersonalCommandRecord{PersonalCommand: domain.PersonalCommand{ID: id, AssistantID: item.AssistantID, TaskID: input.TaskID}, CreatedBy: item.CreatedBy, RequestHash: fmt.Sprintf("sha256:%x", digest), Lease: item.Lease, EnvOperationID: uuid.NewString()}, request, nil
}

func (s *Service) ExecutePersonalCommand(ctx context.Context, principal domainidentity.Principal, input domain.PersonalCommandInput) (domain.PersonalCommand, error) {
	item, err := s.personalEnvironmentForChange(ctx, principal, "reconcile", "", "en-US")
	if err != nil {
		return domain.PersonalCommand{}, err
	}
	candidate, request, err := personalCommandCandidate(item, input)
	if err != nil {
		return domain.PersonalCommand{}, err
	}
	if existing, err := s.personalEnvironment.Store.GetPersonalCommand(ctx, principal.UserID, candidate.ID); err == nil {
		if existing.RequestHash != candidate.RequestHash || existing.AssistantID != item.AssistantID {
			return domain.PersonalCommand{}, apperrors.ErrConflict
		}
		return personalCommandView(existing), nil
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return domain.PersonalCommand{}, err
	}
	if (item.State != "ready" && item.State != "quota_exceeded") || !item.Lease.ExpiresAt.After(time.Now()) || !input.TaskDeadline.After(time.Now()) {
		return domain.PersonalCommand{}, apperrors.ErrConflict
	}
	reserved, command, fresh, err := s.personalEnvironment.Store.ReservePersonalCommand(ctx, item, candidate)
	if err != nil || !fresh {
		return personalCommandView(command), err
	}
	return s.invokePersonalCommand(ctx, principal, item, reserved, command, request, false)
}

func (s *Service) GetPersonalCommand(ctx context.Context, principal domainidentity.Principal, id string) (domain.PersonalCommand, error) {
	_, command, err := s.personalCommandForOwner(ctx, principal, id)
	return personalCommandView(command), err
}

// Recovery only reads the original receipt. A missing or unknown command is
// never executed by this lookup, including after the environment gets a new lease.
func (s *Service) FindPersonalCommand(ctx context.Context, principal domainidentity.Principal, input domain.PersonalCommandInput) (domain.PersonalCommand, error) {
	item, err := s.personalEnvironmentForChange(ctx, principal, "reconcile", "", "en-US")
	if err != nil {
		return domain.PersonalCommand{}, err
	}
	candidate, _, err := personalCommandCandidate(item, input)
	if err != nil {
		return domain.PersonalCommand{}, err
	}
	command, err := s.personalEnvironment.Store.GetPersonalCommand(ctx, principal.UserID, candidate.ID)
	if err == nil && (command.AssistantID != item.AssistantID || command.RequestHash != candidate.RequestHash) {
		err = apperrors.ErrConflict
	}
	return personalCommandView(command), err
}

func (s *Service) cleanupNativePersonalTask(ctx context.Context, lease domain.NativeLease, cp nativeCheckpoint) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 40*time.Second)
	defer cancel()
	principal, err := s.nativePrincipal(ctx, lease.Run)
	if err != nil {
		return false
	}
	parent := domain.NativeTaskParent{RunID: lease.Run.ID, OwnerID: lease.Run.CreatedBy, SessionID: lease.Run.SessionID, RequestHash: lease.RequestHash, Deadline: cp.Budget.Deadline}
	confirmed := false
	err = s.WithNativeTaskParent(ctx, parent, true, func(boundedCtx context.Context) error {
		// At most an original-lease inspection followed by a release. Both use
		// the existing independent cleanup budget; unknown work is never replayed.
		for range 2 {
			item, err := s.personalEnvironmentForChange(boundedCtx, principal, "reconcile", "", "en-US")
			if errors.Is(err, apperrors.ErrNotFound) {
				confirmed = true
				return nil
			}
			if err != nil {
				return err
			}
			if item.ActiveTaskID != domain.PersonalTaskID(parent.RunID) {
				confirmed = true // Never stop an unrelated task or a newer lease.
				return nil
			}
			if item.OperationUntil != nil && item.OperationUntil.After(time.Now()) {
				return apperrors.ErrConflict
			}
			action := "release"
			if item.State == "unknown" || item.State == "pending" {
				action = "inspect"
			}
			if err := s.ReserveNativeTaskToolCall(boundedCtx, parent, true); err != nil {
				return err
			}
			request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: action, Lease: item.Lease}
			view, _, err := s.dispatchPersonalEnvironment(boundedCtx, principal, "task_cleanup", item, request)
			if err != nil {
				return err
			}
			if view.State == "sleeping" || view.State == "missing" || view.State == "expired" || item.Backend == "vm" && action == "release" && view.State == "ready" {
				confirmed = true
				return nil
			}
		}
		return nil
	})
	return err == nil && confirmed
}

func (s *Service) personalCommandForOwner(ctx context.Context, principal domainidentity.Principal, id string) (domain.PersonalEnvironmentRecord, domain.PersonalCommandRecord, error) {
	if value, err := uuid.Parse(id); err != nil || value.String() != id {
		return domain.PersonalEnvironmentRecord{}, domain.PersonalCommandRecord{}, apperrors.ErrInvalidArgument
	}
	item, err := s.personalEnvironmentForChange(ctx, principal, "reconcile", "", "en-US")
	if err != nil {
		return item, domain.PersonalCommandRecord{}, err
	}
	command, err := s.personalEnvironment.Store.GetPersonalCommand(ctx, principal.UserID, id)
	if err == nil && command.AssistantID != item.AssistantID {
		err = apperrors.ErrAccessDenied
	}
	return item, command, err
}

func (s *Service) CancelPersonalCommand(ctx context.Context, principal domainidentity.Principal, id string) (domain.PersonalCommand, error) {
	item, command, err := s.personalCommandForOwner(ctx, principal, id)
	if err != nil {
		return domain.PersonalCommand{}, err
	}
	if command.State == "succeeded" || command.State == "failed" || command.State == "not_dispatched" || command.State == "canceled" || (command.State == "cancelling" && command.OperationUntil.After(time.Now())) {
		return personalCommandView(command), nil
	}
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "release", Lease: command.Lease}
	reserved, cancellation, err := s.personalEnvironment.Store.ReservePersonalCommandCancellation(ctx, item, command, uuid.NewString())
	if err != nil {
		return domain.PersonalCommand{}, err
	}
	return s.invokePersonalCommand(ctx, principal, item, reserved, cancellation, request, true)
}

func (s *Service) invokePersonalCommand(ctx context.Context, principal domainidentity.Principal, previous, reserved domain.PersonalEnvironmentRecord, command domain.PersonalCommandRecord, request contractenv.Request, cancellation bool) (domain.PersonalCommand, error) {
	action := "command"
	if cancellation {
		action = "command_cancel"
	}
	s.auditPersonalEnvironment(ctx, principal, reserved, action, "reserved")
	result, invokeErr := s.invokePersonalRuntime(ctx, principal, reserved, request)
	if invokeErr == nil {
		invokeErr = validatePersonalEnvironmentReceipt(result, request)
	}
	notDispatched := errors.Is(invokeErr, domain.ErrPersonalEnvironmentNotDispatched)
	applyPersonalEnvironmentReceipt(&reserved, result, invokeErr)
	if reserved.Backend == "vm" && cancellation && invokeErr == nil {
		reserved.State, reserved.ActiveTaskID = "ready", ""
		reserved.Usage, reserved.ObservedAt = previous.Usage, previous.ObservedAt
	}
	applyPersonalCommandReceipt(&command, result, invokeErr, cancellation, notDispatched)
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	completed, err := s.personalEnvironment.Store.CompletePersonalCommand(commitCtx, previous, reserved, command, notDispatched)
	if err != nil {
		return domain.PersonalCommand{}, err
	}
	s.auditPersonalEnvironment(commitCtx, principal, reserved, action, completed.State)
	return personalCommandView(completed), nil
}

func applyPersonalCommandReceipt(command *domain.PersonalCommandRecord, result contractenv.Result, err error, cancellation, notDispatched bool) {
	command.State, command.Output, command.OutputDigest, command.ObservedAt = "unknown", "", "", nil
	if cancellation {
		command.State = "cancellation_unknown"
	}
	if err != nil {
		if notDispatched && !cancellation {
			command.State = "not_dispatched"
		}
		return
	}
	command.ObservedAt = &result.ObservedAt
	if cancellation {
		command.State = "canceled"
		return
	}
	command.State = "succeeded"
	if result.ExitCode != nil && *result.ExitCode != 0 {
		command.State = "failed"
	}
	digest := sha256.Sum256([]byte(*result.Output))
	command.OutputDigest = fmt.Sprintf("sha256:%x", digest)
	command.Output = redaction.Text(*result.Output)
	if len(command.Output) > 64<<10 {
		command.Output = "Command output was withheld by the output policy."
	}
}
