package templateupdate

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
)

func (s *Service) ListPolicies(ctx context.Context, p domainidentity.Principal, f domainupdate.Filter) ([]sohaapi.TemplateUpdatePolicy, error) {
	bindings, err := s.bindings(ctx, p, f)
	if err != nil {
		return nil, err
	}
	policies, err := s.deps.Repository.ListPolicies(ctx, f)
	if err != nil {
		return nil, err
	}
	items := []sohaapi.TemplateUpdatePolicy{}
	for _, policy := range policies {
		if slices.ContainsFunc(bindings, func(b domainupdate.Binding) bool { return b.Target.ID == policy.Value.TargetID }) {
			items = append(items, policy.Value)
		}
	}
	return items, nil
}

func (s *Service) SavePolicy(ctx context.Context, p domainidentity.Principal, input sohaapi.TemplateUpdatePolicyInput) (sohaapi.TemplateUpdatePolicy, error) {
	var empty sohaapi.TemplateUpdatePolicy
	if !slices.Contains([]sohaapi.TemplateUpdateMode{"fixed", "manual", "automatic", "approval"}, input.Mode) || input.ExpectedVersion < 0 {
		return empty, invalid("invalid adoption policy mode or version")
	}
	bindings, err := s.bindings(ctx, p, domainupdate.Filter{Kind: input.Kind, TemplateID: input.TemplateID})
	if err != nil {
		return empty, err
	}
	index := slices.IndexFunc(bindings, func(b domainupdate.Binding) bool { return b.Target.ID == input.TargetID })
	if index < 0 {
		return empty, invalid("target binding is unavailable")
	}
	b := bindings[index]
	if !b.Target.Eligible {
		return empty, conflict("detached or independently modified bindings cannot subscribe")
	}
	if err := s.authorizeBinding(ctx, p, b, false); err != nil {
		return empty, err
	}
	// Store only identity references. Every dequeue resolves today's user/token
	// state and permissions; publishing authority is never execution authority.
	if _, err := s.deps.Principals.CurrentExecutionPrincipal(ctx, p.UserID, p.AccessTokenID); err != nil {
		return empty, err
	}
	value := sohaapi.TemplateUpdatePolicy{ID: "adoption-" + input.TargetID, Kind: input.Kind, TemplateID: input.TemplateID, TargetID: input.TargetID, Mode: input.Mode, Paused: input.Paused, UpdatedAt: time.Now().UTC()}
	previous, err := s.deps.Repository.ListPolicies(ctx, domainupdate.Filter{Kind: input.Kind, TemplateID: input.TemplateID, ApplicationID: b.Application.ID})
	if err != nil {
		return empty, err
	}
	for _, item := range previous {
		if item.Value.ID == value.ID {
			value.LastBatchID = item.Value.LastBatchID
		}
	}
	policy, err := s.deps.Repository.SavePolicy(ctx, domainupdate.Policy{Value: value, ApplicationID: b.Application.ID, SubjectID: p.UserID, TokenID: p.AccessTokenID}, input.ExpectedVersion)
	return policy.Value, err
}

func (s *Service) validateCandidatePolicy(ctx context.Context, frozen domainupdate.FrozenCandidate) error {
	if frozen.PolicyID == "" {
		return nil
	}
	policies, err := s.deps.Repository.ListPolicies(ctx, domainupdate.Filter{Kind: frozen.Candidate.Target.Kind, TemplateID: frozen.Candidate.Target.TemplateID})
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if policy.Value.ID == frozen.PolicyID {
			if policy.Value.Paused || policy.Value.Version != frozen.PolicyVersion || policy.Value.Mode != "automatic" && policy.Value.Mode != "approval" {
				return conflict("automatic adoption policy was paused or changed")
			}
			return nil
		}
	}
	return conflict("automatic adoption policy no longer exists")
}

func (s *Service) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cycle, cancel := context.WithTimeout(ctx, 25*time.Second)
				_ = s.ReconcilePolicies(cycle)
				cancel()
			}
		}
	}()
}

// ReconcilePolicies claims at most 16 subscriptions per cycle. Existing workflow
// claims, cancellation, approval and retry own all resulting work.
func (s *Service) ReconcilePolicies(ctx context.Context) error {
	policies, err := s.deps.Repository.ListDuePolicies(ctx, 16)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.deps.Repository.WithPolicy(ctx, policy.Value.ID, policy.Value.Version, func(ctx context.Context, item *domainupdate.Policy) error {
			if item.Value.Paused || item.Value.Mode != "automatic" && item.Value.Mode != "approval" {
				return nil
			}
			err := s.reconcilePolicy(ctx, item)
			item.Value.UpdatedAt = time.Now().UTC()
			item.Value.LastError = ""
			if err != nil {
				item.Value.LastError = "自动采用暂未完成：请核对执行身份权限、固定版本、变量与最新批次；修正后恢复策略"
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) reconcilePolicy(ctx context.Context, policy *domainupdate.Policy) error {
	p, err := s.deps.Principals.CurrentExecutionPrincipal(ctx, policy.SubjectID, policy.TokenID)
	if err != nil {
		return err
	}
	if policy.Value.LastBatchID != "" {
		batch, err := s.deps.Batches.GetDeliveryBatch(ctx, p, policy.Value.LastBatchID)
		if err != nil {
			return err
		}
		if !slices.Contains([]string{"completed", "failed", "canceled", "partially_completed"}, batch.Status) {
			return nil
		}
	}
	versions, err := s.versions(ctx, p, policy.Value.Kind, policy.Value.TemplateID)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return nil
	}
	latest := versions[len(versions)-1]
	if !latest.Enabled || latest.Number <= policy.AttemptVersion {
		return nil
	}
	bindings, err := s.bindings(ctx, p, domainupdate.Filter{Kind: policy.Value.Kind, TemplateID: policy.Value.TemplateID, ApplicationID: policy.ApplicationID})
	if err != nil {
		return err
	}
	index := slices.IndexFunc(bindings, func(b domainupdate.Binding) bool { return b.Target.ID == policy.Value.TargetID })
	if index < 0 {
		return conflict("subscribed binding no longer exists")
	}
	b := bindings[index]
	if !b.Target.Eligible || b.Target.Mode == "fixed" || b.Target.FromVersion >= latest.Number {
		return nil
	}
	if err := s.authorizeBinding(ctx, p, b, false); err != nil {
		return err
	}
	preview, err := s.createPreview(ctx, p, sohaapi.TemplateUpdatePreviewInput{ApplicationID: policy.ApplicationID, Kind: policy.Value.Kind, TemplateID: policy.Value.TemplateID, ToVersion: latest.Number, Targets: []sohaapi.TemplateUpdateSelection{{TargetID: b.Target.ID}}}, policy)
	if err != nil {
		return err
	}
	if len(preview.Candidates) != 1 || !preview.Candidates[0].Eligible || preview.Candidates[0].Cumulative.HistoryGap {
		return conflict("automatic adoption requires a compatible final version and complete history")
	}
	batch, err := s.CreateBatch(ctx, p, preview.ID, sohaapi.TemplateUpdateBatchInput{PreviewDigest: preview.Digest, CandidateIDs: []string{preview.Candidates[0].ID}, WaveSize: 1, StopOnFailure: true, IdempotencyKey: strings.TrimPrefix(digest([]any{policy.Value.ID, policy.Value.Version, latest.Number}), "sha256:")})
	if err != nil {
		return err
	}
	policy.AttemptVersion, policy.AttemptPreviewID, policy.Value.LastBatchID = latest.Number, preview.ID, batch.ID
	return nil
}
