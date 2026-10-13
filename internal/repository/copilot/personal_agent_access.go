package copilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *Repository) GetPersonalAgentAccess(ctx context.Context, owner string) (domain.PersonalAgentAccess, error) {
	item := domain.PersonalAgentAccess{UserID: owner, Status: "not_requested"}
	err := r.db.WithContext(ctx).Raw(`SELECT user_id, status, version, requested_at, decided_at, decided_by FROM ai_personal_agent_access WHERE user_id = ?`, owner).Row().Scan(&item.UserID, &item.Status, &item.Version, &item.RequestedAt, &item.DecidedAt, &item.DecidedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	return item, err
}

func (r *Repository) RequestPersonalAgentAccess(ctx context.Context, owner string) (domain.PersonalAgentAccess, error) {
	err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_personal_agent_access (user_id, status, requested_at) VALUES (?, 'pending', NOW())
		ON CONFLICT (user_id) DO UPDATE SET status='pending', version=ai_personal_agent_access.version+1, requested_at=NOW(), decided_at=NULL, decided_by=''
		WHERE ai_personal_agent_access.status IN ('rejected', 'revoked')`, owner).Error
	if err != nil {
		return domain.PersonalAgentAccess{}, fmt.Errorf("request personal agent access: %w", err)
	}
	return r.GetPersonalAgentAccess(ctx, owner)
}

func (r *Repository) ListPersonalAgentAccess(ctx context.Context) ([]domain.PersonalAgentAccess, error) {
	items := []domain.PersonalAgentAccess{}
	err := r.db.WithContext(ctx).Raw(`SELECT user_id, status, version, requested_at, decided_at, decided_by FROM ai_personal_agent_access ORDER BY requested_at DESC, user_id`).Scan(&items).Error
	return items, err
}

func (r *Repository) DecidePersonalAgentAccess(ctx context.Context, owner, actor, status string, version int64) (domain.PersonalAgentAccess, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_personal_agent_access SET status=?, version=version+1, decided_at=NOW(), decided_by=?
		WHERE user_id=? AND version=? AND ((status='pending' AND ? IN ('approved','rejected')) OR (status='approved' AND ?='revoked'))`, status, actor, owner, version, status, status)
	if result.Error != nil {
		return domain.PersonalAgentAccess{}, result.Error
	}
	if result.RowsAffected != 1 {
		return domain.PersonalAgentAccess{}, apperrors.ErrConflict
	}
	return r.GetPersonalAgentAccess(ctx, owner)
}
