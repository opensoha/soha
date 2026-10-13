package copilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// Call inside WithinPersonalGuestTransaction: these locks fence revocation, ownership and guest revisions.
func (r *Repository) LockPersonalGuestPreparation(ctx context.Context, owner string) (domain.PersonalEnvironmentRecord, int64, int64, error) {
	tx := dbtx.DB(ctx, r.db)
	query := `SELECT e.` + strings.ReplaceAll(personalEnvironmentColumns, ",", ",e.") + ` FROM ai_personal_environments e
 JOIN ai_personal_assistants a ON a.id=e.assistant_id AND a.created_by=e.created_by
 JOIN ai_personal_agent_access x ON x.user_id=e.created_by
 WHERE e.created_by=? AND e.backend='vm' AND x.status='approved' FOR UPDATE OF e,a,x`
	environment, err := scanPersonalEnvironment(tx.Raw(query, owner).Row())
	if errors.Is(err, apperrors.ErrNotFound) {
		return environment, 0, 0, apperrors.ErrAccessDenied
	}
	if err != nil {
		return environment, 0, 0, err
	}
	var accessVersion, revision int64
	err = tx.Raw(`SELECT x.version, COALESCE((SELECT MAX(g.binding_version) FROM ai_personal_guests g WHERE g.assistant_id=?),0)+1
 FROM ai_personal_agent_access x WHERE x.user_id=? AND x.status='approved'`, environment.AssistantID, owner).Row().Scan(&accessVersion, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return environment, 0, 0, apperrors.ErrAccessDenied
	}
	if err != nil {
		return environment, 0, 0, fmt.Errorf("read personal guest preparation authority: %w", err)
	}
	if accessVersion < 1 || revision < 1 || revision > guest.MaxCounter {
		return environment, 0, 0, apperrors.ErrConflict
	}
	return environment, accessVersion, revision, nil
}
