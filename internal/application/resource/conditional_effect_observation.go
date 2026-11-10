package resource

import (
	"context"
	"encoding/json"
	"fmt"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (g *GatewayConditionalMutations) observeOriginalConditionalEffect(ctx context.Context, principal domainidentity.Principal, input conditionalAssessmentInput, record domainresource.ConditionalMutationRecord) (bool, string) {
	if record.Output["action"] == "delete" {
		uid := conditionalDeletionAcknowledgedUID(input, record.Output)
		if uid == "" {
			return false, "Original deletion acknowledgment is unconfirmed"
		}
		presence, err := g.ObserveResourcePresence(ctx, principal, input.ClusterID, input.Namespace, input.Kind, input.Name)
		if err != nil {
			return false, "Original UID absence could not be confirmed by the versioned metadata collection"
		}
		if presence.State == "absent" || presence.UID != uid {
			return true, fmt.Sprintf("The original acknowledged UID is absent at metadata collection %s (%s); any replacement and related resources were not deleted or assessed", presence.CollectionResourceVersion, presence.Protocol)
		}
		return false, "The original UID is still present; deletion may be pending and finalizers remain intact"
	}
	identity, err := g.GetResourceIdentity(ctx, principal, input.ClusterID, input.Namespace, input.Kind, input.Name)
	if err == nil && conditionalObservedRevisionMatches(identity, input, record.Output) {
		return true, "The original acknowledged resource UID and revision are still current; rollout and business health were not assessed"
	}
	return false, "Original mutation state is unconfirmed"
}

func conditionalDeletionAcknowledgedUID(input conditionalAssessmentInput, output map[string]any) string {
	raw, err := json.Marshal(output)
	var receipt contractresource.ResourceDeletionReceipt
	if err != nil || json.Unmarshal(raw, &receipt) != nil || !receipt.Accepted || receipt.State != "deletion_requested" || receipt.Kind != input.Kind || receipt.Namespace != input.Namespace || receipt.Name != input.Name || receipt.Preconditions.Validate() != nil {
		return ""
	}
	return receipt.Preconditions.ExpectedUID
}
