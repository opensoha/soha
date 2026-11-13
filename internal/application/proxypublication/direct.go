package proxypublication

import (
	"context"
	"encoding/json"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxypolicy"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"slices"
)

func (b *Bundles) DirectProjection(ctx context.Context, id networkidentity.Identity, profile, processEpoch string) (api.NetworkProxyDirectProjection, error) {
	var out api.NetworkProxyDirectProjection
	if !validID(processEpoch) {
		return out, apperrors.ErrInvalidArgument
	}
	endpoint, err := b.endpoint.AuthorizeProjection(ctx, id, profile)
	if err != nil {
		return out, err
	}
	r, err := b.currentBundle(ctx, "", endpoint.BundleID)
	if err != nil {
		return out, err
	}
	if r.Public.DeliveryMode != "direct_subscription" || endpoint.DeliveryMode != "direct_subscription" || !endpointInScope(endpoint, r) {
		return out, apperrors.ErrAccessDenied
	}
	plain, err := secretcrypto.DecryptStringWithKeyring(b.keys, r.EncryptedIR)
	if err != nil {
		return out, apperrors.ErrServiceUnavailable
	}
	var ir domain.IR
	if json.Unmarshal([]byte(plain), &ir) != nil {
		return out, apperrors.ErrServiceUnavailable
	}
	expires, leaseErr := b.projectionLease(ctx, r, endpoint)
	if leaseErr != nil {
		return out, leaseErr
	}
	// A receipt may only reference a delivered endpoint boot, just as mapped mode.
	marker, err := token()
	if err != nil {
		return api.NetworkProxyDirectProjection{}, apperrors.ErrServiceUnavailable
	}
	credential := domain.Credential{ID: "delivery-" + marker, InstanceID: r.InstanceID, EndpointCredentialID: endpoint.CredentialID, ProcessEpoch: processEpoch, UserID: endpoint.UserID, DeviceID: endpoint.DeviceID, RuntimeID: id.ID, ProfileID: profile, BundleID: r.Public.BundleID, NodeIDs: slices.Clone(r.Scope.NodeIDs), Epoch: r.Public.CredentialEpoch, Revision: r.Public.Revision, ExpiresAt: expires}
	credential.RenewalKey = credentialRenewalKey(credential)
	credential, err = b.store.IssueCredential(ctx, credential)
	if err != nil {
		return out, err
	}
	ir, err = scopedDirectIR(ir, credential.NodeIDs, r.Groups, r.Rules)
	if err != nil {
		return out, err
	}
	content, err := b.engine.ExportDirect(ir)
	if err != nil {
		return out, err
	}
	out = api.NetworkProxyDirectProjection{SchemaVersion: "proxy-direct/v1", RuntimeID: id.ID, ProfileID: profile, BundleID: r.Public.BundleID, Revision: r.Public.Revision, ActiveRevision: r.Public.ActiveRevision, SourceRevision: r.Public.SourceRevision, OrgRevision: r.Public.OrgRevision, ProjectionHash: r.Public.ProjectionHash, ProcessEpoch: processEpoch, ExpiresAt: credential.ExpiresAt, DeliveryMode: "direct_subscription", UpstreamDisclosure: "upstream_parameters", Format: "mihomo_yaml", Content: content, Groups: ir.Groups, Rules: ir.Rules}

	return out, nil
}

func scopedDirectIR(ir domain.IR, allowed []string, groups []api.NetworkProxyGroup, rules []api.NetworkProxyRule) (domain.IR, error) {
	out := domain.IR{Nodes: []api.NetworkProxyNode{}}
	seen := map[string]bool{}
	for _, node := range ir.Nodes {
		if slices.Contains(allowed, node.NodeID) {
			out.Nodes = append(out.Nodes, node)
			seen[node.NodeID] = true
		}
	}
	if len(allowed) == 0 || len(seen) != len(allowed) {
		return out, domain.Invalid("invalid_target", "scope.nodeIds")
	}
	narrowed, err := proxypolicy.NarrowTargets(groups, rules, allowed)
	if err != nil {
		return domain.IR{}, domain.Invalid("invalid_rules", "rules")
	}
	out.Groups = narrowed.Groups
	out.Rules = narrowed.Rules
	return out, nil
}
