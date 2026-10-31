package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ConditionalMutationRepository interface {
	Claim(context.Context, domainresource.ConditionalMutationRecord) (domainresource.ConditionalMutationRecord, bool, error)
	Get(context.Context, string, string) (domainresource.ConditionalMutationRecord, error)
	Finish(context.Context, domainresource.ConditionalMutationRecord) error
}

// GatewayConditionalMutations adds original-call receipts around the existing
// domain methods. It has no runner, retry or separate provider implementation.
type GatewayConditionalMutations struct {
	*GenericResources
	receipts ConditionalMutationRepository
	resolve  func(context.Context, domainidentity.Principal, string) (domainidentity.Principal, error)
	check    func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error
}

func NewGatewayConditionalMutations(resources *GenericResources, receipts ConditionalMutationRepository,
	resolve func(context.Context, domainidentity.Principal, string) (domainidentity.Principal, error),
	check func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error) (*GatewayConditionalMutations, error) {
	if resources == nil || receipts == nil || (reflect.ValueOf(receipts).Kind() == reflect.Pointer && reflect.ValueOf(receipts).IsNil()) || resolve == nil || check == nil {
		return nil, fmt.Errorf("%w: conditional mutation receipt, identity and authorization dependencies are required", apperrors.ErrInvalidArgument)
	}
	return &GatewayConditionalMutations{GenericResources: resources, receipts: receipts, resolve: resolve, check: check}, nil
}

func (g *GatewayConditionalMutations) ExecuteOriginalConditionalMutation(ctx context.Context, principal domainidentity.Principal, call domainai.ToolInvocationRequest) (map[string]any, error) {
	accepted, ok := domainai.ExecutionAuthorizationFrom(ctx)
	if !ok || accepted.ApprovalID == "" || !sameConditionalCall(accepted.Call, call) || accepted.ActorID != principal.UserID || accepted.ActorTokenID != principal.AccessTokenID {
		return nil, apperrors.ErrAccessDenied
	}
	current, target, err := g.authorizeConditionalMutation(ctx, principal, call)
	if err != nil {
		return nil, err
	}
	if err := g.check(ctx, current, accepted); err != nil {
		return nil, err
	}
	keyHash, inputHash, err := conditionalMutationHashes(current, call)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	// The receipt stores only encrypted provenance and safe output. Original
	// YAML stays in the existing protected intent and PreparedCall storage.
	accepted.Call.Input = nil
	record := domainresource.ConditionalMutationRecord{ID: uuid.NewString(), ActorID: current.UserID, CredentialID: conditionalCredentialID(current, call),
		KeyHash: keyHash, InputHash: inputHash, ConnectionHash: target.ConnectionHash, Status: "running", CreatedAt: now, DeadlineAt: now.Add(2 * time.Minute), ExpiresAt: now.Add(7 * 24 * time.Hour), Authorization: accepted}
	record.Output = conditionalMutationMetadata(record.ID, call.RequestID, target)
	record, created, err := g.receipts.Claim(ctx, record)
	if err != nil {
		return nil, err
	}
	if !created {
		if _, _, err := g.authorizeConditionalMutation(ctx, current, call); err != nil {
			return nil, err
		}
		return conditionalMutationOutput(record)
	}
	return g.dispatchOriginalConditionalMutation(ctx, current, call, target, record)
}

func (g *GatewayConditionalMutations) dispatchOriginalConditionalMutation(ctx context.Context, principal domainidentity.Principal, call domainai.ToolInvocationRequest, target conditionalMutationTarget, record domainresource.ConditionalMutationRecord) (map[string]any, error) {
	bounded, cancel := context.WithDeadline(ctx, record.DeadlineAt)
	defer cancel()
	current, currentTarget, err := g.authorizeConditionalMutation(bounded, principal, call)
	if err == nil && currentTarget.ConnectionHash != record.ConnectionHash {
		err = apperrors.ErrConflict
	}
	if err == nil {
		authorization := record.Authorization
		authorization.Call = call
		err = g.check(bounded, current, authorization)
	}
	var output map[string]any
	if err == nil {
		bounded = domaincluster.WithExpectedExecutionConnection(bounded, target.ClusterID, record.ConnectionHash)
		output, err = g.executeConditionalMutation(bounded, current, target)
	}
	finished := time.Now().UTC()
	record.Status, record.FinishedAt = "unknown", &finished
	if err == nil {
		record.Status, record.Output = "succeeded", output
		for key, value := range conditionalMutationMetadata(record.ID, call.RequestID, target) {
			record.Output[key] = value
		}
	}
	// Preserve the original outcome even if the caller disconnected. A failed
	// finish leaves an occupied claim; it never makes the mutation retryable.
	persist, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if failure := g.receipts.Finish(persist, record); failure != nil {
		return nil, fmt.Errorf("%w: original conditional mutation outcome is unconfirmed; do not replay", apperrors.ErrConflict)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: approved conditional mutation outcome is unknown; do not replay", apperrors.ErrConflict)
	}
	if _, _, err := g.authorizeConditionalMutation(ctx, current, call); err != nil {
		return nil, err
	}
	return conditionalMutationOutput(record)
}

func conditionalMutationMetadata(id, requestID string, target conditionalMutationTarget) map[string]any {
	return map[string]any{"mutationReceiptId": id, "mutationRequestId": requestID, "clusterId": target.ClusterID, "namespace": target.Namespace, "kind": target.Kind, "name": target.Name, "action": string(target.Action)}
}

// FindOriginalConditionalMutation only reads the original receipt. Approval
// expiration must not dispatch a second write or manufacture a fresh approval.
func (g *GatewayConditionalMutations) FindOriginalConditionalMutation(ctx context.Context, principal domainidentity.Principal, call domainai.ToolInvocationRequest) (map[string]any, error) {
	current, _, err := g.authorizeConditionalMutation(ctx, principal, call)
	if err != nil {
		return nil, err
	}
	keyHash, inputHash, err := conditionalMutationHashes(current, call)
	if err != nil {
		return nil, err
	}
	record, err := g.receipts.Get(ctx, current.UserID, keyHash)
	if err != nil {
		return nil, err
	}
	if record.InputHash != inputHash || record.CredentialID != conditionalCredentialID(current, call) {
		return nil, apperrors.ErrConflict
	}
	if _, _, err := g.authorizeConditionalMutation(ctx, current, call); err != nil {
		return nil, err
	}
	return conditionalMutationOutput(record)
}

func conditionalMutationOutput(record domainresource.ConditionalMutationRecord) (map[string]any, error) {
	if record.Status != "succeeded" || len(record.Output) == 0 {
		return nil, fmt.Errorf("%w: original conditional mutation outcome is unknown; do not replay", apperrors.ErrConflict)
	}
	return record.Output, nil
}

func conditionalMutationHashes(principal domainidentity.Principal, call domainai.ToolInvocationRequest) (string, string, error) {
	if principal.UserID == "" || conditionalCredentialID(principal, call) == "" || !validConditionalRequestID(call.RequestID) {
		return "", "", apperrors.ErrInvalidArgument
	}
	raw, err := json.Marshal(struct {
		CredentialID, SessionID string
		Call                    domainai.ToolInvocationRequest
	}{conditionalCredentialID(principal, call), call.SessionID, call})
	if err != nil || len(raw) > 5<<20 {
		return "", "", apperrors.ErrInvalidArgument
	}
	key, input := sha256.Sum256([]byte(call.RequestID)), sha256.Sum256(raw)
	return hex.EncodeToString(key[:]), hex.EncodeToString(input[:]), nil
}

func validConditionalRequestID(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func conditionalCredentialID(principal domainidentity.Principal, call domainai.ToolInvocationRequest) string {
	if principal.AccessTokenID != "" {
		return "token:" + principal.AccessTokenID
	}
	if call.SessionID != "" {
		return "session:" + call.SessionID
	}
	return ""
}

func sameConditionalCall(left, right domainai.ToolInvocationRequest) bool {
	a, err := json.Marshal(left)
	b, otherErr := json.Marshal(right)
	return err == nil && otherErr == nil && string(a) == string(b) && left.SessionID == right.SessionID
}
