package contentdlp

import (
	"context"
	d "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"slices"
	"time"
)

func shadowDecision(ctx context.Context, c d.Content, sample []byte) d.ShadowDecision {
	x := Inspect(ctx, c, sample)
	if !c.Enabled {
		x.Allowed = true
		x.StepUpRequired = false
	}
	return d.ShadowDecision{StepUpRequired: x.StepUpRequired, Enabled: c.Enabled, Mode: x.Mode, Status: x.Status, Allowed: x.Allowed, DetectorVersion: x.DetectorVersion, MatchedRuleIDs: x.MatchedRuleIDs, DataLevel: x.DataLevel, MinimumDataLevel: x.MinimumDataLevel, RuleSetVersion: x.RuleSetVersion}
}

// Shadow has no storage, authorization-token, publication or network side effects.
func Shadow(ctx context.Context, draft d.Content, published *d.Content, revision, version int64, in d.ShadowInput) (d.ShadowResult, error) {
	defer func() {
		for _, sample := range in.Samples {
			clear(sample)
		}
	}()
	if err := validateShadow(draft, published, revision, in); err != nil {
		return d.ShadowResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := d.ShadowResult{Revision: revision, PublishedVersion: version, DraftRuleSetVersion: RuleSetVersion(draft), Samples: []d.ShadowSample{}}
	for i, sample := range in.Samples {
		if ctx.Err() != nil {
			return d.ShadowResult{}, apperrors.ErrServiceUnavailable
		}
		result := d.ShadowSample{Index: i, SizeBytes: int64(len(sample)), Draft: shadowDecision(ctx, draft, sample)}
		if published != nil {
			previous := shadowDecision(ctx, *published, sample)
			result.Published = &previous
			if result.Draft.StepUpRequired != previous.StepUpRequired || result.Draft.Allowed != previous.Allowed || result.Draft.Status != previous.Status || result.Draft.DataLevel != previous.DataLevel || result.Draft.Mode != previous.Mode || !slices.Equal(result.Draft.MatchedRuleIDs, previous.MatchedRuleIDs) {
				out.Changed++
			}
		}
		countShadow(&out, result)
		out.Samples = append(out.Samples, result)
	}
	if ctx.Err() != nil {
		return d.ShadowResult{}, apperrors.ErrServiceUnavailable
	}
	return out, nil
}
func (s *Service) Preview(ctx context.Context, p identity.Principal, id string, in d.ShadowInput) (d.ShadowResult, error) {
	defer func() {
		for _, sample := range in.Samples {
			clear(sample)
		}
	}()
	project, err := s.project(ctx, p, id, PermissionUpdate)
	if err != nil {
		return d.ShadowResult{}, err
	}
	if err := s.authorize(ctx, p, PermissionView, project); err != nil {
		return d.ShadowResult{}, err
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return d.ShadowResult{}, apperrors.ErrServiceUnavailable
	}
	policy, err := s.store.Policy(ctx, project.ID)
	if err != nil {
		return d.ShadowResult{}, err
	}
	return Shadow(ctx, policy.Draft, policy.Published, policy.Revision, policy.PublishedVersion, in)
}

func validateShadow(draft d.Content, published *d.Content, revision int64, in d.ShadowInput) error {
	if in.ExpectedRevision != revision || revision < 1 {
		return apperrors.ErrConflict
	}
	if len(in.Samples) < 1 || len(in.Samples) > 8 {
		return apperrors.ErrInvalidArgument
	}
	for _, sample := range in.Samples {
		if sample == nil || len(sample) > 32<<10 {
			return apperrors.ErrInvalidArgument
		}
	}
	if err := ValidatePolicy(draft); err != nil {
		return err
	}
	if published != nil {
		if err := ValidatePolicy(*published); err != nil {
			return apperrors.ErrServiceUnavailable
		}
	}
	return nil
}

func countShadow(out *d.ShadowResult, result d.ShadowSample) {
	if !result.Draft.Allowed {
		if result.Draft.Mode == "warn" && result.Draft.Status == "matched" {
			out.WouldWarn++
		} else {
			out.WouldBlock++
		}
	}
	if result.Draft.Status != "clean" && result.Draft.Status != "matched" {
		out.Uninspected++
	}
}
