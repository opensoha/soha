package copilot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
)

type nativeModelInput struct {
	Messages []appaigateway.WorkbenchRelayMessage
	Snapshot map[string]any
	Metadata domaincopilot.SessionMetadata
	Lineage  chatContextLineage
}

// Each immutable Native step checkpoint retains this revision. Fingerprint the
// Gateway input rather than copying request bodies or endpoint/credential fields.
type nativeModelContextRevision struct {
	Lineage                  chatContextLineage               `json:"lineage"`
	Usage                    *domaincopilot.ModelContextUsage `json:"contextUsage,omitempty"`
	Version                  string                           `json:"version"`
	ID                       string                           `json:"id"`
	RequestHash              string                           `json:"requestHash"`
	RequestHashBasis         string                           `json:"requestHashBasis"`
	Status                   string                           `json:"status"`
	RequestedPublicModel     string                           `json:"requestedPublicModel,omitempty"`
	RequestedRouteID         string                           `json:"requestedRouteId,omitempty"`
	RequestedReasoningEffort string                           `json:"requestedReasoningEffort,omitempty"`
	ReservedInputTokens      int64                            `json:"reservedInputTokens"`
	MaxOutputTokens          int                              `json:"maxOutputTokens"`
	ReservedTokens           int64                            `json:"reservedTokens"`
	BudgetTokens             int64                            `json:"budgetTokens"`
	ReservationBasis         string                           `json:"reservationBasis"`
	Deadline                 time.Time                        `json:"deadline"`
	Snapshot                 map[string]any                   `json:"snapshot,omitempty"`
	Messages                 []nativeModelMessageRef          `json:"messages"`
	ResolvedPublicModel      string                           `json:"resolvedPublicModel,omitempty"`
	ResolvedRouteID          string                           `json:"resolvedRouteId,omitempty"`
	ProviderKind             string                           `json:"providerKind,omitempty"`
	GatewayRequestID         string                           `json:"gatewayRequestId,omitempty"`
	UsageKnown               bool                             `json:"usageKnown"`
	CostKnown                bool                             `json:"costKnown"`
}

type nativeModelMessageRef struct {
	Index      int    `json:"index"`
	Role       string `json:"role"`
	ToolCallID string `json:"toolCallId,omitempty"`
	Hash       string `json:"hash"`
}

func recordNativeModelContext(cp *nativeCheckpoint, request appaigateway.WorkbenchRelayRequest, snapshot map[string]any) error {
	data, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("hash Native model request: %w", err)
	}
	hash := sha256.Sum256(data)
	cp.ModelContext = &nativeModelContextRevision{
		Version: "opensoha.dev/native-model-context/v1",
		ID:      fmt.Sprintf("%s:model:%d", request.AgentRunID, cp.Usage.Steps), RequestHash: hex.EncodeToString(hash[:]), RequestHashBasis: "gateway_input", Status: "prepared",
		RequestedPublicModel: request.PublicModel, RequestedRouteID: request.RouteID, RequestedReasoningEffort: request.ReasoningEffort,
		ReservedInputTokens: cp.ModelReservation - int64(request.MaxOutputTokens), MaxOutputTokens: request.MaxOutputTokens,
		ReservedTokens: cp.ModelReservation, BudgetTokens: cp.BudgetTokens, ReservationBasis: "serialized_bytes_plus_message_overhead",
		Deadline: cp.Budget.Deadline, Snapshot: snapshot,
	}
	for index, message := range request.Messages {
		encoded, err := json.Marshal(message)
		if err != nil {
			return fmt.Errorf("hash Native context message: %w", err)
		}
		digest := sha256.Sum256(encoded)
		cp.ModelContext.Messages = append(cp.ModelContext.Messages, nativeModelMessageRef{Index: index, Role: message.Role, ToolCallID: message.ToolCallID, Hash: hex.EncodeToString(digest[:])})
	}
	return nil
}

func (revision *nativeModelContextRevision) receive(response appaigateway.WorkbenchRelayResponse, cp nativeCheckpoint) {
	revision.Usage = modelContextUsage(response)
	revision.Status = "response_received"
	revision.ResolvedPublicModel, revision.ResolvedRouteID = response.PublicModel, response.RouteID
	revision.ProviderKind, revision.GatewayRequestID = response.ProviderKind, response.RequestID
	revision.BudgetTokens = cp.BudgetTokens
	tokens, known := response.Usage["totalTokens"]
	revision.UsageKnown = known && tokens > 0 && !response.UsageEstimated
}
