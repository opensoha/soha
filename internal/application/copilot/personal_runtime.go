package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	env "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type personalRuntimeStore interface {
	personalGuestToolsStore
	ReservePersonalRuntime(context.Context, domain.PersonalEnvironmentRecord, guest.RuntimeOperation) (domain.PersonalEnvironmentRecord, error)
	ReservePersonalBrowser(context.Context, domain.PersonalEnvironmentRecord, guest.BrowserAction) (guest.BrowserAction, error)
	GetPersonalEnvironment(context.Context, string, string) (domain.PersonalEnvironmentRecord, error)
}

type personalRuntimeQueue interface {
	QueuePersonalGuestRuntime(context.Context, identity.Principal, guest.RuntimeOperation) (sohaapi.VirtualMachineSSHCommand, error)
	ReadPersonalGuestRuntime(context.Context, identity.Principal, string) (guest.RuntimeOperation, sohaapi.VirtualMachineSSHCommand, error)
}

func (s *PersonalGuestPreparationService) WorkspaceRuntime(ctx context.Context, principal identity.Principal) (domain.PersonalWorkspaceRuntime, error) {
	store, ok := s.store.(interface {
		personalRuntimeStore
		LastPersonalRuntimeID(context.Context, domain.PersonalEnvironmentRecord) (string, error)
		GetPersonalBrowserControl(context.Context, domain.PersonalEnvironmentRecord) (*domain.PersonalRuntimeBrowserControl, error)
	})
	queue, available := s.provisioner.(personalRuntimeQueue)
	var view domain.PersonalWorkspaceRuntime
	if !ok || !available {
		return view, apperrors.ErrServiceUnavailable
	}
	err := store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		item, _, _, err := store.LockPersonalGuestPreparation(txCtx, principal.UserID)
		if err != nil {
			return err
		}
		registered, err := store.CurrentPersonalGuest(txCtx, principal.UserID, item.AssistantID)
		if err != nil {
			return err
		}
		if err = s.guest.store.CheckPersonalGuestBinding(txCtx, registered); err != nil {
			return err
		}
		if err = s.guest.authorizeOwner(txCtx, registered.Binding); err != nil {
			return err
		}
		view.AssistantID, view.EnvironmentVersion = item.AssistantID, item.Version
		view.BrowserControl, err = store.GetPersonalBrowserControl(txCtx, item)
		if err != nil {
			return err
		}
		id, err := store.LastPersonalRuntimeID(txCtx, item)
		if err != nil || id == "" {
			return err
		}
		op, task, err := queue.ReadPersonalGuestRuntime(txCtx, principal, id)
		if err != nil {
			return err
		}
		if !op.Binding.SameWorkspace(registered.Binding) {
			return apperrors.ErrConflict
		}
		latest := personalRuntimeView(item, op, task)
		view.Latest = &latest
		return nil
	})
	return view, err
}

func (s *PersonalGuestPreparationService) Runtime(ctx context.Context, principal identity.Principal, input sohaapi.PersonalGuestRuntimeInput) (domain.PersonalRuntime, error) {
	store, ok := s.store.(personalRuntimeStore)
	queue, available := s.provisioner.(personalRuntimeQueue)
	if !ok || !available {
		return domain.PersonalRuntime{}, apperrors.ErrServiceUnavailable
	}
	if !validPersonalRuntimeRequest(principal.UserID, input.EnvironmentVersion, input.IdempotencyKey) {
		return domain.PersonalRuntime{}, apperrors.ErrInvalidArgument
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return domain.PersonalRuntime{}, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	var receipt domain.PersonalRuntime
	err = store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		item, _, _, err := store.LockPersonalGuestPreparation(txCtx, principal.UserID)
		if err != nil {
			return err
		}
		operation := uuid.NewSHA1(uuid.NameSpaceOID, []byte("personal-runtime/"+item.AssistantID+"/"+input.IdempotencyKey)).String()
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("vm-ssh/"+principal.UserID+"/"+item.ComputeRef+"/personal-runtime-"+operation)).String()
		if previous, task, err := queue.ReadPersonalGuestRuntime(txCtx, principal, id); err == nil {
			if previous.RequestHash != hash {
				return apperrors.ErrConflict
			}
			receipt = personalRuntimeView(item, previous, task)
			return nil
		} else if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		if !availablePersonalRuntimeRecord(item, input.EnvironmentVersion) {
			return apperrors.ErrConflict
		}
		registered, err := store.CurrentPersonalGuest(txCtx, principal.UserID, item.AssistantID)
		if err != nil {
			return err
		}
		if !freshPersonalRuntimeGuest(registered) {
			return apperrors.ErrConflict
		}
		if err := s.guest.store.CheckPersonalGuestBinding(txCtx, registered); err != nil {
			return err
		}
		op, err := personalRuntimeInput(item, input)
		if err != nil {
			return err
		}
		op.SchemaVersion, op.OperationID, op.Binding, op.Source, op.RequestHash = guest.RuntimeOperationVersion, operation, registered.Binding, "user", hash
		op.ArtifactSHA256 = s.artifactSHA
		op.Deadline = slices.MinFunc([]time.Time{time.Now().UTC().Add(50 * time.Second), item.Lease.ExpiresAt, registered.Binding.ExpiresAt}, time.Time.Compare)
		if op.Kind == "browser" {
			*op.Browser, err = store.ReservePersonalBrowser(txCtx, item, *op.Browser)
			if err != nil {
				return err
			}
		}
		if op.Validate(time.Now()) != nil {
			return apperrors.ErrInvalidArgument
		}
		reserved, err := store.ReservePersonalRuntime(txCtx, item, op)
		if err != nil {
			return err
		}
		task, err := queue.QueuePersonalGuestRuntime(txCtx, principal, op)
		if err != nil {
			return err
		}
		receipt = personalRuntimeView(reserved, op, task)
		return nil
	})
	return receipt, err
}

func personalRuntimeInput(item domain.PersonalEnvironmentRecord, input sohaapi.PersonalGuestRuntimeInput) (guest.RuntimeOperation, error) {
	op := guest.RuntimeOperation{Kind: string(input.Kind), Request: env.Request{SchemaVersion: env.RequestVersion, Action: "inspect", Lease: item.Lease}}
	switch input.Kind {
	case "execute", "docker":
		if input.File != nil || input.Browser != nil {
			return op, apperrors.ErrInvalidArgument
		}
		op.Request.Action, op.Request.Argv, op.Request.TaskID = "execute", input.Argv, uuid.NewSHA1(uuid.NameSpaceOID, []byte("personal-manual-task/"+item.AssistantID+"/"+input.IdempotencyKey)).String()
		if input.Kind == "execute" {
			op.Kind = "environment"
		} else {
			op.DockerImage = personalVMImage
		}
	case "file_read", "file_write":
		if input.File == nil || input.Browser != nil || input.Argv != nil {
			return op, apperrors.ErrInvalidArgument
		}
		op.File = &guest.RuntimeFileInput{Path: input.File.Path, ContentBase64: input.File.ContentBase64, ExpectedSHA256: input.File.ExpectedSHA256}
	case "browser":
		if input.Browser == nil || input.File != nil || input.Argv != nil {
			return op, apperrors.ErrInvalidArgument
		}
		b := input.Browser
		op.Browser = &guest.BrowserAction{Action: string(b.Action), Generation: b.Generation, Owner: "user", URL: b.URL, Selector: b.Selector, Text: b.Text}
		if b.ControlID != uuid.Nil {
			op.Browser.ControlID = b.ControlID.String()
		}
		if b.Action == "pointer" {
			op.Browser.X, op.Browser.Y = &b.X, &b.Y
		}
	default:
		return op, apperrors.ErrInvalidArgument
	}
	return op, nil
}

func (s *PersonalGuestPreparationService) GetRuntime(ctx context.Context, principal identity.Principal, id string) (domain.PersonalRuntime, error) {
	store, ok := s.store.(personalRuntimeStore)
	queue, available := s.provisioner.(personalRuntimeQueue)
	if !ok || !available {
		return domain.PersonalRuntime{}, apperrors.ErrServiceUnavailable
	}
	if parsed, err := uuid.Parse(id); err != nil || parsed == uuid.Nil || parsed.String() != id {
		return domain.PersonalRuntime{}, apperrors.ErrInvalidArgument
	}
	op, task, err := queue.ReadPersonalGuestRuntime(ctx, principal, id)
	if err != nil {
		return domain.PersonalRuntime{}, err
	}
	item, err := store.GetPersonalEnvironment(ctx, principal.UserID, op.Binding.AssistantID)
	if err != nil {
		return domain.PersonalRuntime{}, err
	}
	registered, err := store.CurrentPersonalGuest(ctx, principal.UserID, item.AssistantID)
	if err != nil {
		return domain.PersonalRuntime{}, err
	}
	if err := s.guest.store.CheckPersonalGuestBinding(ctx, registered); err != nil {
		return domain.PersonalRuntime{}, err
	}
	return personalRuntimeView(item, op, task), nil
}

func personalRuntimeView(item domain.PersonalEnvironmentRecord, op guest.RuntimeOperation, task sohaapi.VirtualMachineSSHCommand) domain.PersonalRuntime {
	kind := op.Kind
	if kind == "environment" {
		kind = "execute"
	}
	view := domain.PersonalRuntime{ID: task.ID, Kind: kind, Status: "queued", AssistantID: op.Binding.AssistantID, EnvironmentVersion: item.Version}
	if task.Status == "running" || task.Status == "dispatching" {
		view.Status = "running"
	}
	if !task.Terminal {
		return view
	}
	view.Status = "unknown"
	if task.Outcome == "canceled" {
		view.Status = "canceled"
		return view
	}
	if task.Outcome != "succeeded" || task.OutputTruncated {
		return view
	}
	var result guest.RuntimeResult
	if json.Unmarshal([]byte(task.Stdout), &result) != nil || result.ValidateFor(op) != nil {
		return view
	}
	view.Status, view.File, view.Browser, view.ExitCode = "succeeded", result.File, result.Browser, result.Environment.ExitCode
	if result.Environment.Output != nil {
		view.Output = *result.Environment.Output
	}
	if view.ExitCode != nil && *view.ExitCode != 0 {
		view.Status = "failed"
	}
	return view
}

func validPersonalRuntimeRequest(owner string, version int64, key string) bool {
	return owner != "" && version >= 1 && regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`).MatchString(key)
}
func availablePersonalRuntimeRecord(item domain.PersonalEnvironmentRecord, version int64) bool {
	return item.Version == version && item.ActiveTaskID == "" && item.OperationID == "" && item.DataInitialized
}
func freshPersonalRuntimeGuest(item domain.PersonalGuestRecord) bool {
	return item.Status == "registered" && item.LastHeartbeatAt != nil && time.Since(*item.LastHeartbeatAt) <= time.Minute
}
