package contenttransfer

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) existing(ctx context.Context, p identity.Principal, op operation) (*d.Result, error) {
	r, err := s.store.Record(ctx, op.id)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if r.ActorID != p.UserID || r.Action != op.action || r.DestinationID != op.destinationID || r.SourceID != op.sourceID || !hmac.Equal(r.Private.RequestTag, requestTag(r.Private.Key, p, op)) {
		return nil, apperrors.ErrConflict
	}
	for _, g := range op.gates {
		if err := s.authorize(ctx, p, g.permission, g.destination); err != nil {
			return nil, err
		}
	}
	// Replay never repeats an effect or a previous download body.
	return &d.Result{Record: currentRecord(r)}, nil
}
func (s *Service) proof(ctx context.Context, p identity.Principal, op operation, current d.Record) (*d.Record, error) {
	if op.authorization == nil {
		return nil, nil
	}
	auth := op.authorization
	if !validID(auth.AttemptID) || !validToken(auth.Token) {
		return nil, apperrors.ErrInvalidArgument
	}
	old, err := s.store.Record(ctx, auth.AttemptID)
	if err != nil {
		return nil, err
	}
	if !validReleaseRecord(old, p.UserID, auth.Token, op.sessionID) {
		return nil, apperrors.ErrConflict
	}
	if current.Decision == nil || current.Decision.Status != "matched" || current.Decision.RequiredAction == "" || !hmac.Equal(old.Private.Tag, operationTag(old.Private.Key, p, op, current.Gates, current.Private.Backends)) {
		return nil, apperrors.ErrConflict
	}
	if current.Decision.RequiredAction == "appeal" && old.AppealStatus != "approved" {
		return nil, apperrors.ErrAccessDenied
	}
	if old.Decision.StepUpRequired {
		if err := appdlp.RequireReleaseMFA(ctx, s.stepUp, p.UserID, op.sessionID); err != nil {
			return nil, err
		}
	}
	return &old, nil
}

func validReleaseRecord(r d.Record, actor, token, session string) bool {
	now := time.Now().UTC()
	return r.ActorID == actor && r.State == "blocked" && r.Decision != nil && r.Decision.Status == "matched" && r.Private.ExpiresAt.After(now) && !r.Private.TokenConsumed && r.Private.TokenHash == appdlp.ReleaseTokenHash(token, session, r.Decision.StepUpRequired) && r.Private.TokenExpiresAt.After(now)
}
func (s *Service) execute(ctx context.Context, p identity.Principal, op operation) (d.Result, error) {
	if old, err := s.existing(ctx, p, op); err != nil {
		return d.Result{}, err
	} else if old != nil {
		return *old, nil
	}
	var targetErr error
	op, targetErr = s.authorizedTarget(ctx, p, op)
	if targetErr != nil {
		return d.Result{}, targetErr
	}

	record, err := prepareRecord(ctx, p, op)
	if err != nil {
		return d.Result{}, err
	}
	proof, err := s.proof(ctx, p, op, record)
	if err != nil {
		return d.Result{}, err
	}
	if proof != nil {
		record.Decision.Allowed = true
		record.Decision.OriginEventID = proof.ID
		record.Decision.AuthorizationKind = "acknowledge"
		if proof.AppealStatus == "approved" {
			record.Decision.AuthorizationKind = "approved_appeal"
		}
		record.Decision.RequiredAction = ""
		record.State = "prepared"
	}
	if record.State == "blocked" {
		result, err := s.recordDenial(ctx, p, op, record)
		if errors.Is(err, apperrors.ErrConflict) {
			if old, e := s.existing(ctx, p, op); e == nil && old != nil {
				return *old, nil
			}
		}
		return result, err
	}
	if err := s.store.Create(ctx, record); err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			if old, e := s.existing(ctx, p, op); e == nil && old != nil {
				return *old, nil
			}
		}
		return d.Result{}, err
	}
	return s.effect(ctx, p, op, record, proof)
}
func (s *Service) effect(ctx context.Context, p identity.Principal, op operation, record d.Record, proof *d.Record) (d.Result, error) {
	started := false
	err := s.store.WithCurrent(ctx, record.Gates, func(locked context.Context) error {
		if err := s.revalidateOperation(locked, p, &op); err != nil {
			return err
		}
		if proof != nil {
			if err := s.store.Consume(locked, proof.ID, p.UserID, proof.Private.TokenHash); err != nil {
				return err
			}
		}
		record.State = "sending"
		started = true
		backend := op.gates[len(op.gates)-1].backend
		switch op.action {
		case "upload", "paste", "copy":
			if err := backend.Put(locked, op.key, op.content); err != nil {
				return err
			}
		case "delete":
			if err := backend.Delete(locked, op.key); err != nil {
				return err
			}
		case "download":
		}
		record.State = "completed"
		return s.store.Update(locked, record, 1, p.UserID, "transfer_completed")
	})
	if err != nil {
		return s.failed(ctx, p, record, started), nil
	}
	record.State = "completed"
	record.Revision = 2
	record.UpdatedAt = time.Now().UTC()
	result := d.Result{Record: record}
	if op.action == "download" {
		copied := append([]byte{}, op.content...)
		result.Content = &copied
	}
	return result, nil
}
func (s *Service) failed(ctx context.Context, p identity.Principal, r d.Record, started bool) d.Result {
	r.State = "failed"
	r.ErrorCode = "transfer_precondition_changed"
	if started {
		r.State = "unknown"
		r.ErrorCode = "effect_unconfirmed"
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.store.Update(recordCtx, r, 1, p.UserID, "transfer_unconfirmed"); err == nil {
		r.Revision = 2
		r.UpdatedAt = time.Now().UTC()
	} else {
		r.State = "unknown"
		r.ErrorCode = "record_unconfirmed"
	}
	return d.Result{Record: r}
}

func (s *Service) authorizedTarget(ctx context.Context, p identity.Principal, op operation) (operation, error) {
	if op.authorization != nil && (op.action == "upload" || op.action == "paste" || op.action == "copy") {
		origin, err := s.store.Record(ctx, op.authorization.AttemptID)
		if err != nil {
			return op, err
		}
		if origin.ActorID != p.UserID || origin.Action != op.action || origin.DestinationID != op.destinationID || origin.SourceID != op.sourceID || origin.State != "blocked" {
			return op, apperrors.ErrConflict
		}
		owner := sha256.Sum256([]byte(p.UserID))
		expected := "content-transfers/" + op.destinationID + "/" + hex.EncodeToString(owner[:]) + "/" + origin.ID
		if origin.Private.ObjectKey != expected {
			return op, apperrors.ErrConflict
		}
		op.key = origin.Private.ObjectKey
	}
	return op, nil
}

func (s *Service) revalidateOperation(ctx context.Context, p identity.Principal, op *operation) error {
	for i, g := range op.gates {
		current, err := s.destination(ctx, p, g.destination.ID, g.permission)
		if err != nil {
			return err
		}
		if current.Published == nil || op.action != "delete" && !current.Published.Enabled {
			return apperrors.ErrAccessDenied
		}
		fresh, err := s.backends.Prepare(ctx, p, current)
		if err != nil {
			return err
		}
		if fresh.Identity() != g.backend.Identity() || fresh.SnapshotIdentity() != g.backend.SnapshotIdentity() {
			return apperrors.ErrConflict
		}
		op.gates[i].backend = fresh
	}
	if op.sourceID != "" {
		source, err := s.store.Record(ctx, op.sourceID)
		if err != nil {
			return err
		}
		if !fileRecord(source, p) {
			return apperrors.ErrConflict
		}
	}
	return nil
}
