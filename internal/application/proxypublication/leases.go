package proxypublication

import (
	"context"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"time"
)

func credentialRenewalKey(c domain.Credential) string { return domain.CredentialIdentity(c) }
func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
func (b *Bundles) projectionLease(ctx context.Context, r domain.BundleRecord, e domain.EndpointContext) (time.Time, error) {
	until := earlier(b.now().UTC().Add(5*time.Minute), earlier(e.ExpiresAt, e.ConfigurationValidUntil))
	if !until.After(b.now()) {
		return time.Time{}, apperrors.ErrAccessDenied
	}
	protection, err := b.destination.CurrentProtection(ctx)
	if err != nil || protection == nil || protection.PolicyVersion < 1 {
		return time.Time{}, apperrors.ErrAccessDenied
	}
	if r.Public.DeliveryMode != "direct_subscription" {
		return b.servingLease(ctx, r, "", until, protection.PolicyVersion)
	}
	return until, nil
}
func (b *Bundles) servingLease(ctx context.Context, r domain.BundleRecord, requested string, until time.Time, floor int) (time.Time, error) {
	ids, eligible := servingLeaseMembers(r.Pool.Members, requested)
	if len(ids) == 0 {
		return time.Time{}, apperrors.ErrAccessDenied
	}
	states, err := b.desired.ObserveMemberConfigurations(ctx, ids)
	if err != nil {
		return time.Time{}, err
	}
	found := false
	runningFound, runningUntil := false, until
	for _, state := range states {
		member, allowed := eligible[state.InstanceID]
		ready, ok := r.Readbacks[state.InstanceID]
		if allowed && ok && state.PolicyVersion >= floor && b.currentMemberMatches(r, state, ready) {
			if requested == "" && member.Running {
				// A retiring member must not expire the entire employee projection.
				runningFound = true
				runningUntil = earlier(runningUntil, *state.ConfigurationValidUntil)
				continue
			}
			found = true
			until = earlier(until, *state.ConfigurationValidUntil)
			if member.ServingUntil != nil {
				until = earlier(until, *member.ServingUntil)
			}
		}
	}
	if runningFound {
		found, until = true, runningUntil
	}
	if !found || !until.After(b.now()) {
		return time.Time{}, apperrors.ErrAccessDenied
	}
	return until, nil
}

func servingLeaseMembers(members []domain.PoolMember, requested string) ([]string, map[string]domain.PoolMember) {
	ids := []string{}
	eligible := map[string]domain.PoolMember{}
	for _, member := range members {
		if member.Serving && (member.Running || member.ServingUntil != nil) && (requested == "" || member.RuntimeID == requested) {
			ids = append(ids, member.RuntimeID)
			eligible[member.RuntimeID] = member
		}
	}
	return ids, eligible
}

// ServerPublication is called only after runtime authentication, with the already
// certificate-bounded configuration lease. It never grants client eligibility.
func (b *Bundles) ServerPublication(ctx context.Context, member, id string, revision int64, leaseUntil time.Time) (api.NetworkProxyServerPublication, error) {
	var out api.NetworkProxyServerPublication
	r, err := b.store.FindBundle(ctx, id)
	if err != nil {
		return out, err
	}
	if r.Public.Revision != revision || r.Public.DeliveryMode == "direct_subscription" || r.Public.State == "revoked" || r.Public.State == "failed" {
		return out, apperrors.ErrAccessDenied
	}
	until := earlier(b.now().UTC().Add(5*time.Minute), leaseUntil)
	if r.Public.State != "activated" {
		until = earlier(until, r.Public.ValidUntil)
	}
	if !until.After(b.now()) {
		return out, apperrors.ErrGone
	}
	r, pool, err := b.publicationSnapshots(ctx, r)
	if err != nil {
		return out, err
	}
	selected, allowed := publicationMember(pool, member, r.Public.State == "activated")
	if !allowed {
		return out, apperrors.ErrAccessDenied
	}
	protection, err := b.destination.CurrentProtection(ctx)
	if err != nil || protection == nil || protection.PolicyVersion < 1 {
		return out, apperrors.ErrAccessDenied
	}
	states, err := b.desired.ObserveMemberConfigurations(ctx, []string{member})
	if err != nil {
		return out, err
	}
	if len(states) != 1 || states[0].PolicyVersion < protection.PolicyVersion || states[0].ContentHash != r.Public.ServerHash || states[0].BundleID != id || states[0].BundleRevision != revision {
		return out, apperrors.ErrAccessDenied
	}
	until, err = publicationLeaseBound(selected, states[0].ConfigurationValidUntil, until, b.now())
	if err != nil {
		return out, err
	}
	return r.Publication(until), nil
}

func publicationMember(pool domain.ApprovedPool, id string, activated bool) (domain.PoolMember, bool) {
	for _, member := range pool.Members {
		if member.RuntimeID != id {
			continue
		}
		if activated {
			return member, member.Serving
		}
		return member, member.Running
	}
	return domain.PoolMember{}, false
}
func publicationLeaseBound(member domain.PoolMember, issued *time.Time, until, now time.Time) (time.Time, error) {
	if !member.Running {
		if !member.Serving || issued == nil || member.ServingUntil == nil {
			return time.Time{}, apperrors.ErrAccessDenied
		}
		until = earlier(until, *issued)
	}
	if member.ServingUntil != nil {
		until = earlier(until, *member.ServingUntil)
	}
	if !until.After(now) {
		return time.Time{}, apperrors.ErrAccessDenied
	}
	return until, nil
}

func (b *Bundles) publicationSnapshots(ctx context.Context, r domain.BundleRecord) (domain.BundleRecord, domain.ApprovedPool, error) {
	var err error
	r, err = b.currentSourceBundle(ctx, r)
	if err != nil {
		return r, domain.ApprovedPool{}, err
	}
	org, err := b.rules.ReadRules(ctx, r.InstanceID, "")
	if err != nil || org.OrgRevision != r.Public.OrgRevision {
		return r, domain.ApprovedPool{}, apperrors.ErrAccessDenied
	}
	pool, err := b.pool.ApprovedPool(ctx, r.InstanceID)
	if err != nil || pool.OwnerEpoch != r.Pool.OwnerEpoch || pool.MemberSetHash != r.Pool.MemberSetHash {
		return r, domain.ApprovedPool{}, apperrors.ErrAccessDenied
	}

	return r, pool, nil
}
