package contenttransfer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validToken(v string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(raw) == 32 && len(v) == 43
}
func hashToken(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
func (s *Service) recordScope(ctx context.Context, p identity.Principal, r d.Record, key string) error {
	for _, g := range r.Gates {
		if _, err := s.destination(ctx, p, g.DestinationID, key); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) ownCase(ctx context.Context, p identity.Principal, id string) (d.Record, error) {
	r, err := s.OwnRecord(ctx, p, id)
	if err != nil {
		return r, err
	}
	if r.State != "blocked" || r.Decision == nil || r.Decision.Status != "matched" || r.Decision.RequiredAction == "" || len(r.Private.Key) != 32 || len(r.Private.Tag) != 32 || r.NetworkHoldID == "" && !r.Private.ExpiresAt.After(time.Now().UTC()) {
		return d.Record{}, apperrors.ErrConflict
	}
	return r, nil
}
func WithStepUp(verifier appdlp.StepUpVerifier) Option {
	return func(s *Service) { s.stepUp = verifier }
}

func (s *Service) Authorize(ctx context.Context, p identity.Principal, id, action, sessionID string) (dlp.Authorization, error) {
	r, err := s.ownCase(ctx, p, id)
	if err != nil {
		return dlp.Authorization{}, err
	}
	if err := authorizeReleaseAction(r, action); err != nil {
		return dlp.Authorization{}, err
	}
	for i, g := range r.Gates {
		key := Create
		if r.Action == "download" || r.Action == "copy" && i == 0 {
			key = Download
		}
		if _, err := s.destination(ctx, p, g.DestinationID, key); err != nil {
			return dlp.Authorization{}, err
		}
	}
	if r.Decision.StepUpRequired {
		if err := appdlp.RequireReleaseMFA(ctx, s.stepUp, p.UserID, sessionID); err != nil {
			return dlp.Authorization{}, err
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return dlp.Authorization{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	clear(raw)
	expires := time.Now().UTC().Add(5 * time.Minute)
	if r.Private.ExpiresAt.Before(expires) {
		expires = r.Private.ExpiresAt
	}
	r.Private.TokenHash = appdlp.ReleaseTokenHash(token, sessionID, r.Decision.StepUpRequired)
	r.Private.TokenExpiresAt = expires
	err = s.store.WithCurrent(ctx, r.Gates, func(locked context.Context) error {
		return s.store.Update(locked, r, r.Revision, p.UserID, "authorization_issued")
	})
	if err != nil {
		return dlp.Authorization{}, err
	}
	return dlp.Authorization{Token: token, ExpiresAt: expires}, nil
}

func authorizeReleaseAction(r d.Record, action string) error {
	if !r.Private.ExpiresAt.After(time.Now().UTC()) || r.Private.TokenConsumed || action != "acknowledge" && action != "use_approval" {
		return apperrors.ErrConflict
	}
	if r.Decision.RequiredAction == "appeal" && (action != "use_approval" || r.AppealStatus != "approved") {
		return apperrors.ErrAccessDenied
	}
	if r.Decision.RequiredAction == "acknowledge" && action != "acknowledge" {
		return apperrors.ErrInvalidArgument
	}
	return nil
}
func (s *Service) Appeal(ctx context.Context, p identity.Principal, id, reason string) (d.Record, error) {
	if reason != "false_positive" && reason != "business_exception" {
		return d.Record{}, apperrors.ErrInvalidArgument
	}
	r, err := s.ownCase(ctx, p, id)
	if err != nil {
		return r, err
	}
	if r.Decision.RequiredAction != "appeal" || r.AppealStatus != "none" {
		return d.Record{}, apperrors.ErrConflict
	}
	r.AppealStatus = "pending"
	r.AppealReason = reason
	err = s.store.WithCurrent(ctx, r.Gates, func(locked context.Context) error {
		return s.store.Update(locked, r, r.Revision, p.UserID, "appeal_requested")
	})
	if err != nil {
		return d.Record{}, err
	}
	return s.store.Record(ctx, id)
}
func (s *Service) Review(ctx context.Context, p identity.Principal, id string, in d.ReviewInput) (d.Record, error) {
	if !validReview(id, in) {
		return d.Record{}, apperrors.ErrInvalidArgument
	}
	if err := s.global(ctx, p, Review); err != nil {
		return d.Record{}, err
	}
	r, err := s.store.Record(ctx, id)
	if err != nil {
		return r, err
	}
	if r.ActorID == p.UserID {
		return d.Record{}, apperrors.ErrAccessDenied
	}
	if err := s.recordScope(ctx, p, r, Review); err != nil {
		return d.Record{}, err
	}
	if !reviewable(r, in) {
		return d.Record{}, apperrors.ErrConflict
	}

	r.AppealStatus = "rejected"
	if in.Resolution == "approve" {
		r.AppealStatus = "approved"
	}
	r.ReviewerID = p.UserID
	r.ReviewReason = in.ReasonCode
	if r.NetworkHoldID != "" {
		err = s.reviewLinked(ctx, p, r, in.ExpectedRevision)
	} else {
		err = s.store.WithCurrent(ctx, r.Gates, func(locked context.Context) error {
			return s.store.Update(locked, r, in.ExpectedRevision, p.UserID, "appeal_reviewed")
		})
	}

	if err != nil {
		return d.Record{}, err
	}
	return s.store.Record(ctx, id)
}

func validReview(id string, in d.ReviewInput) bool {
	return validID(id) && in.ExpectedRevision >= 1 && (in.Resolution == "approve" || in.Resolution == "reject") && (in.ReasonCode == "false_positive" || in.ReasonCode == "business_exception" || in.ReasonCode == "insufficient_justification")
}

func reviewable(r d.Record, in d.ReviewInput) bool {
	if r.Revision != in.ExpectedRevision || r.State != "blocked" || r.Decision == nil || r.Decision.Status != "matched" || r.Decision.RequiredAction != "appeal" {
		return false
	}
	if r.NetworkHoldID == "" && !r.Private.ExpiresAt.After(time.Now().UTC()) {
		return false
	}
	return r.AppealStatus == "pending" || r.NetworkHoldID != "" && r.AppealStatus == "rejected" && in.Resolution == "approve"
}
