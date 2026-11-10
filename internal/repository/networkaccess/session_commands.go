package networkaccess

import (
	"context"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
)

func (r *Repository) GetSessionCommandForSession(ctx context.Context, sessionID, commandID string) (domainnetworkaccess.SessionCommand, error) {
	return scanSessionCommand(r.db.WithContext(ctx).Raw(sessionCommandSelect+` AND session_id = ? AND id = ? LIMIT 1`, sessionID, commandID).Row())
}

func (r *Repository) GetSessionCommandForPlan(ctx context.Context, sessionID, planHash string) (domainnetworkaccess.SessionCommand, error) {
	return scanSessionCommand(r.db.WithContext(ctx).Raw(sessionCommandSelect+` AND session_id = ? AND plan_hash = ? LIMIT 1`, sessionID, planHash).Row())
}
