package proxypublication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxypolicy"
	identity "github.com/opensoha/soha/internal/domain/identity"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"net/url"
	"slices"
	"time"
)

// ErrBundleCASConflict identifies only a storage UPDATE CAS miss. It retains
// the existing conflict text and classification at the HTTP boundary.
var ErrBundleCASConflict error = bundleCASConflict{}

type bundleCASConflict struct{}

func (bundleCASConflict) Error() string { return apperrors.ErrConflict.Error() }
func (bundleCASConflict) Unwrap() error { return apperrors.ErrConflict }

type BundleStore interface {
	FindBundle(context.Context, string) (domain.BundleRecord, error)
	GetBundle(context.Context, string, string) (domain.BundleRecord, error)
	ListBundles(context.Context, string) ([]domain.BundleRecord, error)
	SaveBundle(context.Context, domain.BundleRecord, int64) error
	SaveProjectionApplied(context.Context, domain.EndpointContext, string, api.NetworkProxyProjectionApplied) error
	IssueCredential(context.Context, domain.Credential) (domain.Credential, error)
	GetCredential(context.Context, string) (domain.Credential, error)
}
type PoolReader interface {
	ApprovedPool(context.Context, string) (domain.ApprovedPool, error)
}
type DesiredWriter interface {
	CommitDesiredBatch(context.Context, identity.Principal, proxy.DesiredBatch) error
	GetDesiredBatchReceipt(context.Context, string) (proxy.DesiredBatchReceipt, error)
	ObserveMemberConfigurations(context.Context, []string) ([]proxy.MemberConfigurationState, error)
}
type EngineCompiler interface {
	Export(domain.IR) (string, error)
	ExportDirect(domain.IR) (string, error)
}
type RuntimeAuthenticator interface {
	AuthorizeProxyRuntime(context.Context, networkidentity.Identity) error
}
type EndpointAuthorizer interface {
	AuthorizeProjection(context.Context, networkidentity.Identity, string) (domain.EndpointContext, error)
}
type CurrentAuthorizer interface {
	ProjectionContext(context.Context, string, string, time.Time) (domain.EndpointContext, error)
}
type DestinationAuthorizer interface {
	ProtectionReader
	AuthorizeDestination(context.Context, domain.EndpointContext, domain.BundleRecord, string, string, int) error
}
type Bundles struct {
	store       BundleStore
	sources     SourceStore
	rules       RuleStore
	auth        Authorizer
	audit       Audit
	pool        PoolReader
	desired     DesiredWriter
	engine      EngineCompiler
	runtime     RuntimeAuthenticator
	endpoint    EndpointAuthorizer
	current     CurrentAuthorizer
	destination DestinationAuthorizer
	keys        keyring.Ring
	now         func() time.Time
}

func NewBundles(store BundleStore, sources SourceStore, rules RuleStore, auth Authorizer, audit Audit, pool PoolReader, desired DesiredWriter, engine EngineCompiler, runtime RuntimeAuthenticator, endpoint EndpointAuthorizer, current CurrentAuthorizer, destination DestinationAuthorizer, keys keyring.Ring) (*Bundles, error) {
	for _, v := range []any{store, sources, rules, auth, audit, pool, desired, engine, runtime, endpoint, current, destination} {
		if absent(v) {
			return nil, apperrors.ErrServiceUnavailable
		}
	}
	if keys.Active().ID() == "" {
		return nil, apperrors.ErrServiceUnavailable
	}
	return &Bundles{store: store, sources: sources, rules: rules, auth: auth, audit: audit, pool: pool, desired: desired, engine: engine, runtime: runtime, endpoint: endpoint, current: current, destination: destination, keys: keys, now: time.Now}, nil
}
func hash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (b *Bundles) Get(ctx context.Context, p identity.Principal, instance, id string) (api.NetworkProxyBundle, error) {
	if err := b.auth.Authorize(ctx, p, instance, "view"); err != nil {
		return api.NetworkProxyBundle{}, err
	}
	r, err := b.store.GetBundle(ctx, instance, id)
	return r.Public, err
}
func (b *Bundles) List(ctx context.Context, p identity.Principal, instance string) (api.NetworkProxyBundleList, error) {
	out := api.NetworkProxyBundleList{Items: []api.NetworkProxyBundle{}}
	if err := b.auth.Authorize(ctx, p, instance, "view"); err != nil {
		return out, err
	}
	records, err := b.store.ListBundles(ctx, instance)
	for _, record := range records {
		out.Items = append(out.Items, record.Public)
	}
	return out, err
}
func (b *Bundles) Prepare(ctx context.Context, p identity.Principal, instance, id string, input api.NetworkProxyBundleInput) (api.NetworkProxyBundle, error) {
	if err := b.auth.Authorize(ctx, p, instance, "update"); err != nil {
		return api.NetworkProxyBundle{}, err
	}
	if !validID(id) || !validID(input.RequestID) || input.ExpectedRevision < 0 || len(input.Scope.UserIDs) == 0 || len(input.Scope.DeviceIDs) == 0 {
		return api.NetworkProxyBundle{}, domain.Invalid("invalid_bundle", "bundle")
	}
	if input.DeliveryMode != "mapped_nodes" && input.DeliveryMode != "gateway" && input.DeliveryMode != "direct_subscription" {
		return api.NetworkProxyBundle{}, domain.Invalid("unsupported_delivery_mode", "deliveryMode")
	}
	if err := validateScopeIdentifiers(input.Scope); err != nil {
		return api.NetworkProxyBundle{}, err
	}

	intentHash := hash(input)
	ir, err := b.loadBundleIR(ctx, instance, input)
	if err != nil {
		return api.NetworkProxyBundle{}, err
	}
	pool, err := b.publicationPool(ctx, instance, input)
	if err != nil {
		return api.NetworkProxyBundle{}, err
	}
	content, ir, nodes, err := b.compileBundle(&input, ir)
	if err != nil {
		return api.NetworkProxyBundle{}, err
	}
	record, err := b.persistPrepared(ctx, instance, id, input, intentHash, ir, nodes, pool, content)
	if err != nil {
		return record.Public, err
	}
	if record.Public.State != "prepare" {
		return record.Public, nil
	}
	return record.Public, b.submitPrepared(ctx, p, record, content)
}

func (b *Bundles) loadBundleIR(ctx context.Context, instance string, input api.NetworkProxyBundleInput) (domain.IR, error) {
	source, err := b.sources.GetSource(ctx, instance, input.SourceID)
	if err != nil {
		return domain.IR{}, err
	}
	if source.Public.Revision != input.SourceRevision {
		return domain.IR{}, apperrors.ErrConflict
	}
	org, err := b.rules.ReadRules(ctx, instance, "")
	if err != nil {
		return domain.IR{}, err
	}
	if org.OrgRevision != input.OrgRevision {
		return domain.IR{}, apperrors.ErrConflict
	}
	raw, err := secretcrypto.DecryptStringWithKeyring(b.keys, source.EncryptedIR)
	if err != nil {
		return domain.IR{}, apperrors.ErrServiceUnavailable
	}
	var ir domain.IR
	if json.Unmarshal([]byte(raw), &ir) != nil {
		return domain.IR{}, apperrors.ErrServiceUnavailable
	}
	ir.Rules = append(ir.Rules, org.Rules...)
	return ir, nil

}

func (b *Bundles) publicationPool(ctx context.Context, instance string, input api.NetworkProxyBundleInput) (domain.ApprovedPool, error) {
	if input.DeliveryMode == "direct_subscription" {
		if len(input.Endpoints) > 0 {
			return domain.ApprovedPool{}, domain.Invalid("unsupported_field", "endpoints")
		}
		return domain.ApprovedPool{MasterID: instance}, nil
	}
	pool, err := b.pool.ApprovedPool(ctx, instance)
	if err != nil {
		return domain.ApprovedPool{}, err
	}
	if len(pool.Members) == 0 || len(input.Endpoints) == 0 {
		return domain.ApprovedPool{}, apperrors.ErrAccessDenied
	}
	selected := []api.NetworkProxyEmployeeEndpoint{}
	for _, ref := range input.Endpoints {
		found := false
		for _, ep := range pool.Endpoints {
			if ep.EndpointID == ref.EndpointID {
				parsed, parseErr := url.Parse(ep.URL)
				if parseErr != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
					return domain.ApprovedPool{}, apperrors.ErrAccessDenied
				}
				selected = append(selected, ep)
				found = true
				break
			}
		}
		if !found {
			return domain.ApprovedPool{}, apperrors.ErrAccessDenied
		}
	}
	pool.Endpoints = selected

	return pool, nil

}

func (b *Bundles) compileBundle(input *api.NetworkProxyBundleInput, ir domain.IR) (string, domain.IR, []string, error) {
	nodes := []string{}
	for _, node := range ir.Nodes {
		nodes = append(nodes, node.NodeID)
	}
	for _, node := range input.Scope.NodeIDs {
		if !slices.Contains(nodes, node) {
			return "", domain.IR{}, nil, domain.Invalid("invalid_target", "scope.nodeIds")
		}
	}
	if len(input.Scope.NodeIDs) == 0 {
		input.Scope.NodeIDs = nodes
	}
	targets := []proxypolicy.Target{}
	for _, node := range ir.Nodes {
		targets = append(targets, proxypolicy.Target{ID: node.NodeID, Kind: "node"})
	}
	for _, group := range ir.Groups {
		targets = append(targets, proxypolicy.Target{ID: group.GroupID, Kind: "group"})
	}
	composition, composeErr := proxypolicy.Compose(input.OrgRevision, 0, ir.Rules, targets, true)
	if composeErr != nil {
		return "", domain.IR{}, nil, domain.Invalid("invalid_rules", "rules")
	}
	ir.Rules = composition.EffectiveRules
	if err := validateBundleRuleScope(ir.Rules, ir.Groups, input.Scope.NodeIDs); err != nil {
		return "", domain.IR{}, nil, err
	}
	if input.DeliveryMode == "gateway" {
		for _, rule := range ir.Rules {
			if rule.Action.Type == "node" || rule.Action.Type == "group" {
				return "", domain.IR{}, nil, domain.Invalid("unsupported_field", "gateway.rules")
			}
		}
		aggregateNodes := slices.Clone(input.Scope.NodeIDs)
		slices.Sort(aggregateNodes)
		identityHash := secretHash(input.SourceID + ":" + hash(aggregateNodes))[:24]
		gatewayID := "gateway-" + identityHash
		groupID := "aggregate-" + identityHash
		ir.Groups = append(ir.Groups, api.NetworkProxyGroup{GroupID: groupID, Label: "Gateway", Type: "select", NodeIDs: aggregateNodes})
		nodes = []string{gatewayID}
		input.Scope.NodeIDs = slices.Clone(nodes)
	}
	var content string
	var err error
	if input.DeliveryMode == "direct_subscription" {
		content, err = b.engine.ExportDirect(ir)
	} else {
		content, err = b.engine.Export(ir)
	}
	if err != nil {
		return "", domain.IR{}, nil, err
	}

	return content, ir, nodes, nil

}

func (b *Bundles) persistPrepared(ctx context.Context, instance, id string, input api.NetworkProxyBundleInput, intentHash string, ir domain.IR, nodes []string, pool domain.ApprovedPool, content string) (domain.BundleRecord, error) {
	raw, _ := json.Marshal(ir)
	encryptedIR, err := secretcrypto.EncryptStringWithKeyring(b.keys, string(raw))
	if err != nil {
		return domain.BundleRecord{}, apperrors.ErrServiceUnavailable
	}
	version := int64(0)
	if input.ExpectedRevision > 0 {
		old, e := b.store.GetBundle(ctx, instance, id)
		if e != nil {
			return domain.BundleRecord{}, e
		}
		if old.Public.Revision != input.ExpectedRevision {
			return domain.BundleRecord{}, apperrors.ErrConflict
		}
		version = old.Version
	}
	revision := input.ExpectedRevision + 1
	record := domain.BundleRecord{Version: version, InstanceID: instance, RequestID: input.RequestID, IntentHash: intentHash, EncryptedIR: encryptedIR, Pool: pool, Scope: input.Scope, Nodes: nodes, Groups: ir.Groups, Rules: ir.Rules, Readbacks: map[string]api.NetworkProxyBundleReadback{}, Public: api.NetworkProxyBundle{BundleID: id, Revision: revision, SourceID: input.SourceID, SourceRevision: input.SourceRevision, OrgRevision: input.OrgRevision, DeliveryMode: input.DeliveryMode, ConverterVersion: domain.ConverterVersion, CredentialEpoch: revision, NodeMappingRevision: revision, ServerHash: "sha256:" + secretHash(content), ProjectionHash: hash(struct {
		Nodes  []string
		Groups []api.NetworkProxyGroup
		Rules  []api.NetworkProxyRule
		Scope  api.NetworkProxyScope
	}{nodes, ir.Groups, ir.Rules, input.Scope}), State: "prepare", ValidUntil: b.now().UTC().Add(5 * time.Minute)}}
	if input.DeliveryMode == "direct_subscription" {
		record.Public.State = "client_eligible"
	}
	record.SourceNodeIDs = slices.Clone(input.Scope.NodeIDs)
	if input.DeliveryMode == "gateway" {
		record.SourceNodeIDs = slices.Clone(ir.Groups[len(ir.Groups)-1].NodeIDs)
	}
	record.Outbounds = map[string]string{}
	for _, node := range nodes {
		record.Outbounds[node] = node
	}
	if input.DeliveryMode == "gateway" {
		record.Outbounds[nodes[0]] = ir.Groups[len(ir.Groups)-1].GroupID
	}
	existing, lookupErr := b.store.GetBundle(ctx, instance, id)
	if lookupErr == nil && existing.RequestID == input.RequestID {
		if existing.IntentHash != intentHash {
			return domain.BundleRecord{}, apperrors.ErrConflict
		}
		record = existing
		if record.Public.State != "prepare" {
			return record, nil
		}
		if !record.Public.ValidUntil.After(b.now()) {
			return record, apperrors.ErrGone
		}
	} else {
		if lookupErr != nil && lookupErr != apperrors.ErrNotFound {
			return domain.BundleRecord{}, lookupErr
		}
		if err = b.store.SaveBundle(ctx, record, input.ExpectedRevision); err != nil {
			return domain.BundleRecord{}, err
		}
	}

	return record, nil

}

func (b *Bundles) submitPrepared(ctx context.Context, p identity.Principal, record domain.BundleRecord, content string) error {
	batch := proxy.DesiredBatch{PublicationID: "publication-" + secretHash(record.InstanceID + ":" + record.Public.BundleID + ":" + record.RequestID)[:48], MasterID: record.InstanceID, OwnerEpoch: record.Pool.OwnerEpoch, MemberSetHash: record.Pool.MemberSetHash}
	for _, member := range record.Pool.Members {
		batch.Members = append(batch.Members, proxy.DesiredMember{InstanceID: member.RuntimeID, ExpectedRevision: member.ExpectedRevision, Enabled: true, Content: &content, BundleID: record.Public.BundleID, BundleRevision: record.Public.Revision})
	}
	receipt, receiptErr := b.desired.GetDesiredBatchReceipt(ctx, batch.PublicationID)
	if receiptErr == nil {
		if receipt.MasterID != record.InstanceID || receipt.OwnerEpoch != record.Pool.OwnerEpoch || receipt.MemberSetHash != record.Pool.MemberSetHash || len(receipt.Members) != len(record.Pool.Members) {
			return apperrors.ErrConflict
		}
		for _, member := range receipt.Members {
			if member.BundleID != record.Public.BundleID || member.BundleRevision != record.Public.Revision || member.ContentHash != record.Public.ServerHash {
				return apperrors.ErrConflict
			}
		}
	} else {
		if receiptErr != apperrors.ErrNotFound {
			return receiptErr
		}
		if err := b.desired.CommitDesiredBatch(ctx, p, batch); err != nil {
			return err
		}
	}
	return b.audit.RecordPublication(ctx, p, record.InstanceID, "bundle_prepared", record.Public.BundleID, record.Public.Revision)
}

func (b *Bundles) ServerReady(ctx context.Context, id networkidentity.Identity, instance string, input api.NetworkProxyBundleReadback) error {
	if err := b.runtime.AuthorizeProxyRuntime(ctx, id); err != nil {
		return err
	}
	if instance == "" {
		record, lookupErr := b.store.FindBundle(ctx, input.BundleID)
		if lookupErr != nil {
			return lookupErr
		}
		instance = record.InstanceID
	}
	return b.saveServerReadback(ctx, id, instance, input)
}
func (b *Bundles) saveServerReadback(ctx context.Context, id networkidentity.Identity, instance string, input api.NetworkProxyBundleReadback) error {
	var previousVersion int64
	var saveErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			if err := b.runtime.AuthorizeProxyRuntime(ctx, id); err != nil {
				return err
			}
		}
		record, err := b.store.GetBundle(ctx, instance, input.BundleID)
		if err != nil {
			return err
		}
		if attempt > 0 && (record.Version <= previousVersion || record.Public.Revision != input.Revision) {
			return apperrors.ErrConflict
		}
		pool, err := b.validateCurrentReadback(ctx, record, id.ID, input)
		if err != nil {
			return err
		}
		recordServerReadback(&record, pool, id.ID, input)
		saveErr = b.store.SaveBundle(ctx, record, record.Public.Revision)
		if !errors.Is(saveErr, ErrBundleCASConflict) {
			return saveErr
		}
		previousVersion = record.Version
	}
	return saveErr
}
func (b *Bundles) validateCurrentReadback(ctx context.Context, record domain.BundleRecord, runtimeID string, input api.NetworkProxyBundleReadback) (domain.ApprovedPool, error) {
	pool, err := b.pool.ApprovedPool(ctx, record.InstanceID)
	if err != nil || pool.OwnerEpoch != record.Pool.OwnerEpoch || pool.MemberSetHash != record.Pool.MemberSetHash {
		return pool, apperrors.ErrConflict
	}
	member := false
	for _, candidate := range pool.Members {
		if candidate.RuntimeID == runtimeID && candidate.Running {
			member = true
		}
	}
	if err := validateReadbackEnvelope(record, input, member, b.now().UTC()); err != nil {
		return pool, err
	}
	protection, err := b.destination.CurrentProtection(ctx)
	if err != nil || protection == nil || protection.PolicyVersion < 1 {
		return pool, apperrors.ErrAccessDenied
	}
	states, err := b.desired.ObserveMemberConfigurations(ctx, []string{runtimeID})
	if err != nil {
		return pool, err
	}
	for _, state := range states {
		if state.PolicyVersion >= protection.PolicyVersion && readbackStateMatches(state, input, runtimeID) && b.currentMemberMatches(record, state, input) {
			return pool, nil
		}
	}
	return pool, apperrors.ErrConflict
}

func (b *Bundles) Activate(ctx context.Context, p identity.Principal, instance, id string, input api.NetworkProxyBundleActivateInput) (api.NetworkProxyBundle, error) {
	if err := b.auth.Authorize(ctx, p, instance, "update"); err != nil {
		return api.NetworkProxyBundle{}, err
	}
	r, err := b.store.GetBundle(ctx, instance, id)
	if err != nil {
		return r.Public, err
	}
	if r.Public.State == "activated" && r.ActivationRequest == input.RequestID && r.Public.Revision == input.ExpectedRevision {
		return r.Public, nil
	}
	if !validID(input.RequestID) {
		return r.Public, apperrors.ErrInvalidArgument
	}
	if r.Public.Revision != input.ExpectedRevision || r.Public.State != "client_eligible" || !r.Public.ValidUntil.After(b.now()) {
		return r.Public, apperrors.ErrConflict
	}
	if r.Public.DeliveryMode != "direct_subscription" {
		if err = b.verifyRunningMembers(ctx, r); err != nil {
			return r.Public, err
		}
	}
	r.ActivationRequest = input.RequestID
	r.Public.State = "activated"
	r.Public.ActiveRevision = r.Public.Revision
	if err = b.store.SaveBundle(ctx, r, r.Public.Revision); err != nil {
		return r.Public, err
	}
	return r.Public, b.audit.RecordPublication(ctx, p, instance, "bundle_activated", id, r.Public.Revision)
}

func (b *Bundles) verifyRunningMembers(ctx context.Context, r domain.BundleRecord) error {
	pool, err := b.pool.ApprovedPool(ctx, r.InstanceID)
	if err != nil || pool.OwnerEpoch != r.Pool.OwnerEpoch || pool.MemberSetHash != r.Pool.MemberSetHash {
		return apperrors.ErrConflict
	}
	ids := []string{}
	for _, m := range pool.Members {
		if m.Running {
			ids = append(ids, m.RuntimeID)
		}
	}
	if len(ids) == 0 {
		return apperrors.ErrConflict
	}
	states, err := b.desired.ObserveMemberConfigurations(ctx, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		ready, ok := r.Readbacks[id]
		if !ok {
			return apperrors.ErrConflict
		}
		matched := false
		for _, s := range states {
			if s.InstanceID == id && b.currentMemberMatches(r, s, ready) {
				matched = true
			}
		}
		if !matched {
			return apperrors.ErrConflict
		}
	}
	return nil
}

func (b *Bundles) verifyServingMember(ctx context.Context, r domain.BundleRecord, requested string) error {
	_, err := b.servingLease(ctx, r, requested, b.now().Add(5*time.Minute), 1)
	return err
}
func (b *Bundles) currentMemberMatches(r domain.BundleRecord, s proxy.MemberConfigurationState, ready api.NetworkProxyBundleReadback) bool {
	return s.ConfigurationValidUntil != nil && s.ConfigurationValidUntil.After(b.now()) && s.BundleID == r.Public.BundleID && s.BundleRevision == r.Public.Revision && s.DesiredRevision > 0 && s.DesiredRevision == s.AppliedRevision && s.DesiredHash != "" && s.DesiredHash == s.AppliedHash && s.ContentHash == r.Public.ServerHash && s.ProcessEpoch == ready.ProcessEpoch && s.PolicyVersion > 0 && s.PolicyVersion == s.ObservedPolicyVersion && s.LastSeenAt != nil && b.now().Sub(*s.LastSeenAt) <= 30*time.Second && !s.LastSeenAt.After(b.now().Add(5*time.Second))
}

func validateScopeIdentifiers(scope api.NetworkProxyScope) error {
	for _, layer := range [][]string{scope.UserIDs, scope.DeviceIDs, scope.NodeIDs} {
		seen := map[string]bool{}
		for _, id := range layer {
			if !validID(id) || seen[id] {
				return domain.Invalid("invalid_scope", "scope")
			}
			seen[id] = true
		}
	}
	return nil
}

func validateBundleRuleScope(rules []api.NetworkProxyRule, groups []api.NetworkProxyGroup, nodeIDs []string) error {
	for _, rule := range rules {
		if rule.Action.Type == "node" && !slices.Contains(nodeIDs, rule.Action.TargetID) {
			return domain.Invalid("rule_target_scope", "rules")
		}
		if rule.Action.Type == "group" {
			found := false
			for _, group := range groups {
				if group.GroupID == rule.Action.TargetID {
					for _, node := range group.NodeIDs {
						found = found || slices.Contains(nodeIDs, node)
					}
				}
			}
			if !found {
				return domain.Invalid("rule_target_scope", "rules")
			}
		}
	}

	return nil
}

func validateReadbackEnvelope(r domain.BundleRecord, input api.NetworkProxyBundleReadback, member bool, now time.Time) error {
	switch r.Public.State {
	case "prepare", "server_ready", "client_eligible", "activated":
	default:
		return apperrors.ErrConflict
	}
	if !member || input.BundleID != r.Public.BundleID || input.Revision != r.Public.Revision || input.ServerHash != r.Public.ServerHash || !input.IngressReady || !input.ProtectionReady || input.ProcessEpoch == "" || now.Sub(input.ObservedAt) > 30*time.Second || input.ObservedAt.After(now.Add(5*time.Second)) || (r.Public.State != "activated" && !r.Public.ValidUntil.After(now)) {
		return apperrors.ErrConflict
	}

	return nil
}

func readbackStateMatches(s proxy.MemberConfigurationState, input api.NetworkProxyBundleReadback, runtimeID string) bool {
	return s.InstanceID == runtimeID && s.BundleID == input.BundleID && s.BundleRevision == input.Revision && s.DesiredRevision > 0 && s.DesiredRevision == input.ConfigurationRevision && s.AppliedRevision == s.DesiredRevision && s.DesiredHash == s.AppliedHash && s.ContentHash == input.ServerHash && s.ProcessEpoch == input.ProcessEpoch && s.PolicyVersion > 0 && s.PolicyVersion == s.ObservedPolicyVersion
}

func recordServerReadback(r *domain.BundleRecord, pool domain.ApprovedPool, runtimeID string, input api.NetworkProxyBundleReadback) {
	if r.Readbacks == nil {
		r.Readbacks = map[string]api.NetworkProxyBundleReadback{}
	}
	input.ReasonCode = ""
	r.Readbacks[runtimeID] = input
	r.Pool.Members = pool.Members
	ready := true
	for _, m := range pool.Members {
		if m.Running {
			_, ok := r.Readbacks[m.RuntimeID]
			ready = ready && ok
		}
	}
	if ready && r.Public.State != "activated" {
		r.Public.State = "client_eligible"
	}

}
