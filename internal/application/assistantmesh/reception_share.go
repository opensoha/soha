package assistantmesh

import (
	"context"
	"fmt"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type SharedReplyInput struct {
	Answer        string                `json:"answer"`
	Title         string                `json:"title"`
	Summary       string                `json:"summary"`
	Kind          string                `json:"kind"`
	Audience      knowledge.AccessScope `json:"audience"`
	LifetimeHours int                   `json:"lifetimeHours,omitempty"`
	LifetimeDays  int                   `json:"lifetimeDays,omitempty"`
	WorkStatus    string                `json:"workStatus,omitempty"`
}

func (s *Reception) ReplyAndPublish(ctx context.Context, p identity.Principal, inquiryID string, input SharedReplyInput) (InquiryView, error) {
	p, err := s.prepare(ctx, p)
	if err != nil {
		return InquiryView{}, err
	}
	if !validReceptionID(inquiryID) || (input.Kind != domain.KindExperienceCard && input.Kind != domain.KindWorkPulse) ||
		input.Audience.Visibility == "private" || input.Audience.Visibility == "" {
		return InquiryView{}, apperrors.ErrInvalidArgument
	}
	answer, err := cleanReceptionText(ctx, input.Answer, 4000, false)
	if err != nil {
		return InquiryView{}, err
	}
	var replied domain.Inquiry
	err = s.store.WithReceptionTransaction(ctx, func(txCtx context.Context) error {
		replied, err = s.store.ReplyInquiry(txCtx, p.UserID, inquiryID, answer, nowUTC(), retention)
		if err != nil {
			return err
		}
		asset, err := s.assets.Create(txCtx, p, domain.AssetInput{
			Kind: input.Kind, Title: input.Title, Body: input.Summary, WorkStatus: input.WorkStatus,
		})
		if err != nil {
			return err
		}
		request := ActionRequest{ExpectedDigest: asset.DraftDigest, Audience: input.Audience, LifetimeHours: input.LifetimeHours, LifetimeDays: input.LifetimeDays}
		if _, err := s.assets.Act(txCtx, p, asset.ID, "publish", request); err != nil {
			return fmt.Errorf("publish inquiry summary: %w", err)
		}
		return s.assets.audit(txCtx, p, "inquiry.reply_and_publish", inquiryID, "success", map[string]any{"assetId": asset.ID})
	})
	if err != nil {
		return InquiryView{}, err
	}
	return s.inquiryView(ctx, p, replied)
}
