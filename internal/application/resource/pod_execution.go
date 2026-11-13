package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	contractresource "github.com/opensoha/soha/contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (w *Workloads) ExecutePodArgv(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, clusterID, namespace, name, key string, input contractresource.PodArgvInput) (contractresource.PodExecutionReceipt, error) {
	var empty contractresource.PodExecutionReceipt
	if _, err := conditionalResourceKind(namespace, "Pod", name); err != nil {
		return empty, err
	}
	input, err := input.Normalized()
	if err != nil || !validPodExecutionKey(key) || principal.UserID == "" {
		return empty, fmt.Errorf("%w: valid original key, target and bounded argv are required", apperrors.ErrInvalidArgument)
	}
	if err := w.validateGatewayPodExecution(ctx, principal, clusterID, namespace, name, key, input); err != nil {
		return empty, err
	}
	connection, _, err := w.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return empty, err
	}
	if w.executions == nil || w.executionIdentity == nil {
		return empty, fmt.Errorf("%w: durable Pod execution is unavailable", apperrors.ErrUnsupportedOperation)
	}
	ctx, connectionHash, err := bindPodExecutionConnection(ctx, connection)
	if err != nil {
		return empty, err
	}
	principal, err = w.currentPodExecutionPrincipal(ctx, principal, access)
	if err != nil {
		return empty, err
	}
	if _, err := w.authorizePodExecutionConnection(ctx, principal, connection.Summary.ID, namespace, connectionHash); err != nil {
		return empty, err
	}
	clusterID = connection.Summary.ID
	keyHash, inputHash := podExecutionHashes(clusterID, namespace, name, key, input)
	old, err := w.executions.Get(ctx, principal.UserID, "", keyHash)
	if err == nil {
		if old.InputHash != inputHash {
			return empty, fmt.Errorf("%w: execution key is bound to another original request", apperrors.ErrConflict)
		}
		return old.Receipt, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return empty, err
	}
	route, err := w.podExecutionRoute(connection)
	if err != nil {
		return empty, err
	}
	target, err := route.InspectPodExecutionTarget(ctx, namespace, name, input.ExpectedUID, input.Container)
	if err != nil {
		return empty, wrapAgentResourceError(err)
	}
	if !matchesPodExecutionTarget(target, namespace, name, input) {
		return empty, fmt.Errorf("%w: invalid Pod execution target response", apperrors.ErrClusterUnready)
	}
	now := time.Now().UTC()
	record := domainresource.PodExecutionRecord{ActorID: principal.UserID, KeyHash: keyHash, InputHash: inputHash, ConnectionHash: connectionHash, Input: input, Receipt: contractresource.PodExecutionReceipt{
		ID: uuid.NewString(), Status: "running", ClusterID: clusterID, Namespace: namespace, PodName: name, ExpectedUID: input.ExpectedUID, Container: target.Container,
		CreatedAt: now, DeadlineAt: now.Add(time.Duration(input.TimeoutSeconds+10) * time.Second), ExpiresAt: now.Add(7 * 24 * time.Hour),
	}}
	record, created, err := w.executions.Claim(ctx, record)
	if err != nil || !created {
		return record.Receipt, err
	}
	return w.runPodExecution(ctx, principal, access, route, record)
}

func (w *Workloads) GetPodExecution(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, id, key string) (contractresource.PodExecutionReceipt, error) {
	var empty contractresource.PodExecutionReceipt
	if _, err := conditionalResourceKind(namespace, "Pod", name); err != nil {
		return empty, err
	}
	if principal.UserID == "" || id == "" && !validPodExecutionKey(key) || id != "" && !validPodExecutionID(id) {
		return empty, apperrors.ErrInvalidArgument
	}
	connection, _, err := w.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return empty, err
	}
	if w.executions == nil {
		return empty, apperrors.ErrUnsupportedOperation
	}
	digest := sha256.Sum256([]byte(key))
	record, err := w.executions.Get(ctx, principal.UserID, id, hex.EncodeToString(digest[:]))
	if err != nil {
		return empty, err
	}
	item := record.Receipt
	if item.ClusterID != connection.Summary.ID || item.Namespace != namespace || item.PodName != name {
		return empty, apperrors.ErrNotFound
	}
	_ = w.recordAudit(ctx, principal, item.ClusterID, namespace, "Pod", name, "exec.receipt", "success", "read original Pod execution receipt")
	return item, nil
}

// Recovery additionally proves the exact original input. A receipt for another
// argv/stdin under the same actor/key must never satisfy a new task's intent.
func (w *Workloads) FindOriginalPodExecution(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, key string, input contractresource.PodArgvInput) (contractresource.PodExecutionReceipt, error) {
	input, err := input.Normalized()
	if err != nil {
		return contractresource.PodExecutionReceipt{}, apperrors.ErrInvalidArgument
	}
	receipt, err := w.GetPodExecution(ctx, principal, clusterID, namespace, name, "", key)
	if err != nil {
		return receipt, err
	}
	record, err := w.executions.Get(ctx, principal.UserID, receipt.ID, "")
	if err != nil {
		return contractresource.PodExecutionReceipt{}, err
	}
	_, digest := podExecutionHashes(receipt.ClusterID, namespace, name, key, input)
	if record.InputHash != digest {
		return contractresource.PodExecutionReceipt{}, fmt.Errorf("%w: original Pod execution input differs", apperrors.ErrConflict)
	}
	return record.Receipt, nil
}

func validPodExecutionKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, char := range key {
		if char < '!' || char > '~' {
			return false
		}
	}
	return true
}

func validPodExecutionID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id
}

func podExecutionHashes(clusterID, namespace, name, key string, input contractresource.PodArgvInput) (string, string) {
	keyHash := sha256.Sum256([]byte(key))
	data, _ := json.Marshal(struct {
		ClusterID, Namespace, Name string
		Input                      contractresource.PodArgvInput
	}{clusterID, namespace, name, input})
	inputHash := sha256.Sum256(data)
	return hex.EncodeToString(keyHash[:]), hex.EncodeToString(inputHash[:])
}

func matchesPodExecutionTarget(target contractresource.PodExecutionTarget, namespace, name string, input contractresource.PodArgvInput) bool {
	return target.Namespace == namespace && target.PodName == name && target.UID == input.ExpectedUID && target.Container != "" && (input.Container == "" || input.Container == target.Container)
}
