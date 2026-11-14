package proxypublication

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxypolicy"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"log/slog"
	"slices"
	"strconv"
	"time"
)

func token() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func secretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
func (b *Bundles) currentBundle(ctx context.Context, instance, id string) (domain.BundleRecord, error) {
	if instance == "" {
		record, lookupErr := b.store.FindBundle(ctx, id)
		if lookupErr != nil {
			return record, lookupErr
		}
		instance = record.InstanceID
	}
	r, err := b.store.GetBundle(ctx, instance, id)
	if err != nil {
		return r, err
	}
	if r.Public.State != "activated" || r.Public.ActiveRevision != r.Public.Revision {
		return r, apperrors.ErrAccessDenied
	}
	r, err = b.currentSourceBundle(ctx, r)
	if err != nil {
		return r, err
	}
	org, err := b.rules.ReadRules(ctx, instance, "")
	if err != nil || org.OrgRevision != r.Public.OrgRevision {
		return r, apperrors.ErrAccessDenied
	}
	if r.Public.DeliveryMode == "direct_subscription" {
		return r, nil
	}
	pool, err := b.pool.ApprovedPool(ctx, instance)
	if err != nil || pool.OwnerEpoch != r.Pool.OwnerEpoch || pool.MemberSetHash != r.Pool.MemberSetHash {
		return r, apperrors.ErrAccessDenied
	}

	r.Pool.Members = pool.Members
	return r, nil
}
func (b *Bundles) Projection(ctx context.Context, id networkidentity.Identity, instance, profile, processEpoch string) (api.NetworkProxyEmployeeProjection, error) {
	var out api.NetworkProxyEmployeeProjection
	if !validID(processEpoch) {
		return out, apperrors.ErrInvalidArgument
	}
	endpoint, err := b.endpoint.AuthorizeProjection(ctx, id, profile)
	if err != nil {
		return out, err
	}
	r, err := b.currentBundle(ctx, instance, endpoint.BundleID)
	if err != nil {
		return out, err
	}
	if r.Public.DeliveryMode == "direct_subscription" {
		return out, apperrors.ErrAccessDenied
	}
	if err = b.verifyServingMember(ctx, r, ""); err != nil {
		return out, err
	}
	if endpoint.OrgRevision != r.Public.OrgRevision || endpoint.SourceRevision != r.Public.SourceRevision || endpoint.DeliveryMode != string(r.Public.DeliveryMode) || !slices.Contains(r.Scope.UserIDs, endpoint.UserID) || !slices.Contains(r.Scope.DeviceIDs, endpoint.DeviceID) {
		return out, apperrors.ErrAccessDenied
	}
	credential, secret, err := b.issueProjectionCredential(ctx, id, profile, processEpoch, r, endpoint)
	if err != nil {
		return out, err
	}
	r.Scope.NodeIDs = domain.IntersectNodes(r.Scope.NodeIDs, credential.NodeIDs)
	narrowed, narrowErr := proxypolicy.NarrowTargets(r.Groups, r.Rules, credential.NodeIDs)
	if narrowErr != nil {
		return out, apperrors.ErrAccessDenied
	}
	r.Groups = narrowed.Groups
	r.Rules = narrowed.Rules
	expires := credential.ExpiresAt
	out = api.NetworkProxyEmployeeProjection{SchemaVersion: "proxy-projection/v1", RuntimeID: id.ID, ProfileID: profile, ProcessEpoch: processEpoch, BundleID: r.Public.BundleID, Revision: r.Public.Revision, ActiveRevision: r.Public.ActiveRevision, SourceRevision: r.Public.SourceRevision, OrgRevision: r.Public.OrgRevision, CredentialEpoch: r.Public.CredentialEpoch, ProjectionHash: r.Public.ProjectionHash, DeliveryMode: api.NetworkProxyEmployeeProjectionDeliveryMode(r.Public.DeliveryMode), Endpoints: r.Pool.Endpoints, Groups: []api.NetworkProxyGroup{}, Nodes: []api.NetworkProxyEmployeeNode{}, Rules: []api.NetworkProxyRule{}, ExpiresAt: expires, Credential: api.NetworkProxyEmployeeCredential{CredentialID: credential.ID, CredentialEpoch: credential.Epoch, Secret: secret, UserID: credential.UserID, DeviceID: credential.DeviceID, NodeIDs: credential.NodeIDs, ExpiresAt: expires}}
	populateEmployeeTopology(&out, r, credential)
	return out, nil
}
func safeNodeLabel(index int) string { return "Node " + strconv.Itoa(index+1) }
func (b *Bundles) IngressCheck(ctx context.Context, id networkidentity.Identity, instance string, input api.NetworkProxyIngressCheckInput) (api.NetworkProxyIngressCheckResult, error) {
	out := api.NetworkProxyIngressCheckResult{BundleID: input.BundleID, Revision: input.Revision, NodeID: input.NodeID, CredentialEpoch: input.CredentialEpoch, ExpiresAt: b.now().UTC(), ReasonCode: "access_denied"}
	if err := b.runtime.AuthorizeProxyRuntime(ctx, id); err != nil {
		slog.Warn("proxy ingress authorization rejected", "stage", "runtime_authorization")
		return out, err
	}
	if (input.Network != "tcp" && input.Network != "udp") || input.DestinationHost == "" || input.DestinationPort < 1 || input.DestinationPort > 65535 {
		slog.Warn("proxy ingress authorization rejected", "stage", "request_invalid")
		return out, nil
	}
	c, err := b.store.GetCredential(ctx, input.CredentialID)
	if err != nil {
		slog.Warn("proxy ingress authorization rejected", "stage", "credential_lookup")
		return out, nil
	}
	actual := secretHash(input.CredentialSecret)
	if subtle.ConstantTimeCompare([]byte(actual), []byte(c.SecretHash)) != 1 {
		slog.Warn("proxy ingress authorization rejected", "stage", "credential_secret")
		return out, nil
	}
	r, err := b.currentBundle(ctx, instance, input.BundleID)
	if err != nil {
		slog.Warn("proxy ingress authorization rejected", "stage", "current_bundle")
		return out, nil
	}
	if err = b.verifyServingMember(ctx, r, id.ID); err != nil {
		slog.Warn("proxy ingress authorization rejected", "stage", "runtime_member")
		return out, nil
	}
	now := b.now().UTC()
	if !ingressCredentialMatches(c, r, input, now) {
		slog.Warn("proxy ingress authorization rejected", "stage", "credential_scope")
		return out, nil
	}
	ready, ok := r.Readbacks[id.ID]
	if !ok || ready.ProcessEpoch != input.ProcessEpoch || ready.Revision != input.Revision {
		slog.Warn("proxy ingress authorization rejected", "stage", "runtime_readback")
		return out, nil
	}
	endpoint, err := b.current.ProjectionContext(ctx, c.EndpointCredentialID, c.ProfileID, now)
	if err != nil || !ingressEndpointMatches(endpoint, c, r) {
		slog.Warn("proxy ingress authorization rejected", "stage", "endpoint_context")
		return out, nil
	}
	if err = b.destination.AuthorizeDestination(ctx, endpoint, r, input.NodeID, input.DestinationHost, input.DestinationPort); err != nil {
		slog.Warn("proxy ingress authorization rejected", "stage", "destination_authorization")
		return out, nil
	}
	out.Allowed = true
	out.ReasonCode = ""
	out.ExpiresAt, err = b.servingLease(ctx, r, id.ID, now.Add(5*time.Second), 1)
	if err != nil {
		slog.Warn("proxy ingress authorization rejected", "stage", "serving_lease")
		return api.NetworkProxyIngressCheckResult{Allowed: false, ReasonCode: "access_denied", ExpiresAt: now}, nil
	}
	if c.ExpiresAt.Before(out.ExpiresAt) {
		out.ExpiresAt = c.ExpiresAt
	}
	return out, nil
}

func (b *Bundles) ProjectionApplied(ctx context.Context, id networkidentity.Identity, profile string, input api.NetworkProxyProjectionApplied) error {
	endpoint, err := b.endpoint.AuthorizeProjection(ctx, id, profile)
	if err != nil {
		return err
	}
	r, err := b.currentBundle(ctx, "", endpoint.BundleID)
	if err != nil {
		return err
	}
	if input.BundleID != endpoint.BundleID || input.Revision != r.Public.Revision || input.ProjectionHash != r.Public.ProjectionHash || !validID(input.ProcessEpoch) || (input.Status != "applied" && input.Status != "rejected") {
		return apperrors.ErrConflict
	}
	if input.Status == "rejected" {
		input.ReasonCode = "projection_apply_rejected"
	} else {
		input.ReasonCode = ""
	}
	return b.store.SaveProjectionApplied(ctx, endpoint, input.ProcessEpoch, input)
}

func endpointInScope(endpoint domain.EndpointContext, r domain.BundleRecord) bool {
	return endpoint.OrgRevision == r.Public.OrgRevision && endpoint.SourceRevision == r.Public.SourceRevision && slices.Contains(r.Scope.UserIDs, endpoint.UserID) && slices.Contains(r.Scope.DeviceIDs, endpoint.DeviceID)
}

func (b *Bundles) issueProjectionCredential(ctx context.Context, id networkidentity.Identity, profile, processEpoch string, r domain.BundleRecord, endpoint domain.EndpointContext) (domain.Credential, string, error) {
	secret, err := token()
	if err != nil {
		return domain.Credential{}, "", apperrors.ErrServiceUnavailable
	}
	credentialID, err := token()
	if err != nil {
		return domain.Credential{}, "", apperrors.ErrServiceUnavailable
	}
	credentialID = "credential-" + credentialID
	encrypted, err := secretcrypto.EncryptStringWithKeyring(b.keys, secret)
	if err != nil {
		return domain.Credential{}, "", apperrors.ErrServiceUnavailable
	}
	expires, leaseErr := b.projectionLease(ctx, r, endpoint)
	if leaseErr != nil {
		return domain.Credential{}, "", leaseErr
	}
	credential := domain.Credential{ID: credentialID, InstanceID: r.InstanceID, EndpointCredentialID: endpoint.CredentialID, ProcessEpoch: processEpoch, UserID: endpoint.UserID, DeviceID: endpoint.DeviceID, RuntimeID: id.ID, ProfileID: profile, BundleID: r.Public.BundleID, NodeIDs: slices.Clone(r.Scope.NodeIDs), Epoch: r.Public.CredentialEpoch, Revision: r.Public.Revision, EncryptedSecret: encrypted, SecretHash: secretHash(secret), ExpiresAt: expires}
	credential.RenewalKey = credentialRenewalKey(credential)
	credential, err = b.store.IssueCredential(ctx, credential)
	if err != nil {
		return domain.Credential{}, "", err
	}
	secret, err = secretcrypto.DecryptStringWithKeyring(b.keys, credential.EncryptedSecret)
	if err != nil || secretHash(secret) != credential.SecretHash {
		return domain.Credential{}, "", apperrors.ErrServiceUnavailable
	}
	return credential, secret, nil
}

func populateEmployeeTopology(out *api.NetworkProxyEmployeeProjection, r domain.BundleRecord, credential domain.Credential) {
	for i, node := range r.Nodes {
		if slices.Contains(credential.NodeIDs, node) {
			label := safeNodeLabel(i)
			if r.Public.DeliveryMode == "gateway" {
				label = "Gateway"
			}
			out.Nodes = append(out.Nodes, api.NetworkProxyEmployeeNode{NodeID: node, Label: label, EndpointID: r.Pool.Endpoints[0].EndpointID})
		}
	}
	for _, g := range r.Groups {
		filtered := g
		filtered.NodeIDs = nil
		for _, node := range g.NodeIDs {
			if slices.Contains(credential.NodeIDs, node) {
				filtered.NodeIDs = append(filtered.NodeIDs, node)
			}
		}
		if !slices.Contains(filtered.NodeIDs, filtered.SelectedNodeID) {
			filtered.SelectedNodeID = ""
		}
		if len(filtered.NodeIDs) > 0 {
			out.Groups = append(out.Groups, filtered)
		}
	}
	// Rule IR is separately validated; target visibility must not reveal excluded nodes.
	for _, rule := range r.Rules {
		if rule.Action.Type == "node" && !slices.Contains(credential.NodeIDs, rule.Action.TargetID) {
			continue
		}
		out.Rules = append(out.Rules, rule)
	}

}

func ingressCredentialMatches(c domain.Credential, r domain.BundleRecord, input api.NetworkProxyIngressCheckInput, now time.Time) bool {
	return c.InstanceID == r.InstanceID && c.BundleID == input.BundleID && c.Revision == input.Revision && c.Epoch == input.CredentialEpoch && c.Epoch == r.Public.CredentialEpoch && c.ExpiresAt.After(now) && slices.Contains(c.NodeIDs, input.NodeID) && slices.Contains(r.Scope.NodeIDs, input.NodeID) && slices.Contains(r.Scope.UserIDs, c.UserID) && slices.Contains(r.Scope.DeviceIDs, c.DeviceID)
}

func ingressEndpointMatches(endpoint domain.EndpointContext, c domain.Credential, r domain.BundleRecord) bool {
	return endpoint.UserID == c.UserID && endpoint.DeviceID == c.DeviceID && endpoint.RuntimeID == c.RuntimeID && endpoint.BundleID == c.BundleID && endpoint.OrgRevision == r.Public.OrgRevision && endpoint.SourceRevision == r.Public.SourceRevision
}

func (b *Bundles) currentSourceBundle(ctx context.Context, r domain.BundleRecord) (domain.BundleRecord, error) {
	allowed, err := b.sources.AllowedSourceNodes(ctx, r.InstanceID, r.Public.SourceID, r.Public.SourceRevision, r.SourceNodeIDs)
	if err != nil {
		return r, apperrors.ErrAccessDenied
	}
	r, err = domain.NarrowBundle(r, allowed)
	if err != nil {
		return r, apperrors.ErrAccessDenied
	}
	narrowed, err := proxypolicy.NarrowTargets(r.Groups, r.Rules, r.Scope.NodeIDs)
	if err != nil {
		return r, apperrors.ErrAccessDenied
	}
	r.Groups = narrowed.Groups
	r.Rules = narrowed.Rules
	return r, nil
}
