package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const conditionalMutationAssessmentTool = "k8s.resources.conditional.assess"

type conditionalAssessmentInput struct {
	ClusterID         string `json:"clusterId"`
	Namespace         string `json:"namespace"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	MutationRequestID string `json:"mutationRequestId"`
}

// Assessment proves only that the exact version acknowledged by the original
// update/action is still current. It does not prove rollout or business health.
// Deletion observes the original UID through a complete versioned metadata
// collection. Generic 404 never proves absence or related-resource cleanup.
func (g *GatewayConditionalMutations) AssessOriginalConditionalMutation(ctx context.Context, principal domainidentity.Principal, call domainai.ToolInvocationRequest) (domainai.CapabilityAssessment, error) {
	return g.assessOriginalConditionalEffect(ctx, principal, call, conditionalMutationAssessmentTool, g.observeOriginalConditionalEffect)
}

type conditionalEffectObserver func(context.Context, domainidentity.Principal, conditionalAssessmentInput, domainresource.ConditionalMutationRecord) (bool, string)

func (g *GatewayConditionalMutations) assessOriginalConditionalEffect(ctx context.Context, principal domainidentity.Principal, call domainai.ToolInvocationRequest, tool string, observe conditionalEffectObserver) (domainai.CapabilityAssessment, error) {
	var empty domainai.CapabilityAssessment
	input, current, err := g.prepareConditionalAssessment(ctx, principal, call, tool)
	if err != nil {
		return empty, err
	}
	key := sha256.Sum256([]byte(input.MutationRequestID))
	record, err := g.receipts.Get(ctx, current.UserID, hex.EncodeToString(key[:]))
	if err != nil {
		return empty, err
	}
	if record.CredentialID != conditionalCredentialID(current, call) || !conditionalAssessmentTargetMatches(input, record.Output) {
		return empty, apperrors.ErrNotFound
	}
	action, _ := record.Output["action"].(string)
	connection, _, err := g.authorize(ctx, current, input.ClusterID, input.Namespace, input.Kind, domainaccess.Action(action))
	if err != nil {
		return empty, err
	}
	fingerprint, err := conditionalConnectionHash(connection)
	if err != nil {
		return empty, err
	}
	assessment := conditionalMutationAssessment(record, input, "inconclusive", "Original mutation state is unconfirmed")
	if tool == conditionalScaleAssessmentTool {
		assessment.Evidence[0].Reference.ToolName = tool
		assessment.Evidence[0].Reference.Input = call.Input
	}
	if record.Status != "succeeded" || record.FinishedAt == nil || record.FinishedAt.After(time.Now().Add(30*time.Second)) || record.ConnectionHash != fingerprint {
		return assessment, nil
	}
	readContext := domaincluster.WithExpectedExecutionConnection(ctx, input.ClusterID, fingerprint)
	observed, summary := observe(readContext, current, input, record)
	_, refreshed, err := g.prepareConditionalAssessment(ctx, current, call, tool)
	if err != nil {
		return empty, err
	}
	latest, _, err := g.authorize(ctx, refreshed, input.ClusterID, input.Namespace, input.Kind, domainaccess.Action(action))
	if err != nil {
		return empty, err
	}
	afterHash, err := conditionalConnectionHash(latest)
	if err != nil {
		return empty, err
	}
	if afterHash != fingerprint {
		summary = "Resource connection changed while observing; the original effect is unconfirmed"
	}
	if observed && afterHash == fingerprint {
		assessment.Verdict, assessment.Summary = "satisfied", summary
		assessment.Evidence[0].Incomplete, assessment.Evidence[0].Summary = false, summary
		return assessment, nil
	}
	assessment.Summary, assessment.Evidence[0].Summary = summary, summary
	return assessment, nil
}

func conditionalObservedRevisionMatches(identity contractresource.ResourceIdentity, input conditionalAssessmentInput, output map[string]any) bool {
	uid, _ := output["uid"].(string)
	version, _ := output["resourceVersion"].(string)
	return uid != "" && version != "" && identity.Kind == input.Kind && identity.Name == input.Name && identity.Namespace == input.Namespace && identity.UID == uid && identity.ResourceVersion == version && identity.DeletionTimestamp == nil
}

func (g *GatewayConditionalMutations) prepareConditionalAssessment(ctx context.Context, principal domainidentity.Principal, call domainai.ToolInvocationRequest, tool string) (conditionalAssessmentInput, domainidentity.Principal, error) {
	var input conditionalAssessmentInput
	if call.ToolName != tool || call.CapabilityVersion != "1" || principal.UserID == "" || conditionalCredentialID(principal, call) == "" {
		return input, principal, apperrors.ErrInvalidArgument
	}
	raw, err := json.Marshal(call.Input)
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &input) != nil || !validConditionalRequestID(input.MutationRequestID) {
		return input, principal, apperrors.ErrInvalidArgument
	}
	kind, err := conditionalResourceKind(input.Namespace, input.Kind, input.Name)
	if err != nil {
		return input, principal, err
	}
	input.Kind = kind
	current, err := g.resolve(ctx, principal, call.SessionID)
	if err != nil {
		return input, current, err
	}
	if current.UserID != principal.UserID || current.AccessTokenID != principal.AccessTokenID || (current.GovernedAI == nil) != (principal.GovernedAI == nil) {
		return input, current, apperrors.ErrAccessDenied
	}
	_, _, err = g.authorize(ctx, current, input.ClusterID, input.Namespace, input.Kind, domainaccess.ActionView)
	return input, current, err
}

func conditionalAssessmentTargetMatches(input conditionalAssessmentInput, output map[string]any) bool {
	return output["clusterId"] == input.ClusterID && output["namespace"] == input.Namespace && output["kind"] == input.Kind && output["name"] == input.Name && output["mutationRequestId"] == input.MutationRequestID
}

func conditionalMutationAssessment(record domainresource.ConditionalMutationRecord, input conditionalAssessmentInput, verdict, summary string) domainai.CapabilityAssessment {
	return domainai.CapabilityAssessment{Verdict: sohaapi.CapabilityAssessmentVerdict(verdict), Summary: summary, Evidence: []domainai.CapabilityEvidence{{
		Kind: "k8s.conditional_mutation", Source: "k8s.conditional_mutation:" + record.ID, ObservedAt: time.Now().UTC(), Incomplete: verdict != "satisfied", Summary: summary,
		Reference: &sohaapi.CapabilityCall{ToolName: conditionalMutationAssessmentTool, CapabilityVersion: "1", Input: map[string]any{"clusterId": input.ClusterID, "namespace": input.Namespace, "kind": input.Kind, "name": input.Name, "mutationRequestId": input.MutationRequestID}},
		Resource:  &domainai.CapabilityResourceRef{Kind: "k8s.conditional_mutation", ID: record.ID},
	}}}
}
