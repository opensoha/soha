package copilot

import (
	"context"
	"encoding/json"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *Repository) AuthorizePersonalGuestRuntime(ctx context.Context, op guest.RuntimeOperation) error {
	item, err := r.GetPersonalEnvironment(ctx, op.Binding.OwnerID, op.Binding.AssistantID)
	if err != nil {
		return err
	}
	if item.Backend != "vm" || item.Provider != op.Binding.Provider || !item.DataInitialized || item.DataRef != op.Binding.DataRef || item.ComputeRef != op.Binding.ComputeRef || item.ComputeGeneration != op.Binding.ComputeGeneration || item.PolicyRevision != op.Binding.PolicyRevision || item.OperationID != op.OperationID || item.OperationUntil == nil || !item.OperationUntil.After(time.Now()) {
		return apperrors.ErrAccessDenied
	}
	actual, _ := json.Marshal(item.Lease)
	frozen, _ := json.Marshal(op.Request.Lease)
	if string(actual) != string(frozen) || op.Request.Action == "execute" && item.ActiveTaskID != op.Request.TaskID {
		return apperrors.ErrAccessDenied
	}
	return r.CheckPersonalBrowserOperation(ctx, op)
}
