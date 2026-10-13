package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"time"

	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func (r *Repository) GetOrCreatePersonalAssistant(ctx context.Context, candidate domain.PersonalAssistant) (domain.PersonalAssistant, error) {
	if err := r.db.WithContext(ctx).Exec(`
        INSERT INTO ai_personal_assistants (id, created_by, name, main_session_id, created_at, updated_at)
        SELECT ?, ?, ?, id, ?, ? FROM ai_sessions
        WHERE id = ? AND created_by = ? AND deleted_at IS NULL AND metadata->>'conversationKind' IN ('main','personal_main')
        ON CONFLICT (created_by) DO UPDATE SET main_session_id=EXCLUDED.main_session_id
        WHERE ai_personal_assistants.main_session_id<>EXCLUDED.main_session_id
    `, candidate.ID, candidate.CreatedBy, candidate.Name, candidate.CreatedAt, candidate.UpdatedAt, candidate.MainSessionID, candidate.CreatedBy).Error; err != nil {
		return domain.PersonalAssistant{}, fmt.Errorf("ensure personal assistant: %w", err)
	}
	return r.getPersonalAssistant(ctx, candidate.CreatedBy)
}

func (r *Repository) ListPersonalArtifacts(ctx context.Context, owner string) ([]domain.PersonalArtifact, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
        SELECT m.id, m.session_id, COALESCE(m.metadata->>'agentRunId', ''), a.ordinality, a.value, m.created_at, m.metadata
        FROM ai_messages m JOIN ai_sessions s ON s.id = m.session_id
        CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(m.metadata::jsonb->'analysisArtifacts') = 'array'
            THEN m.metadata::jsonb->'analysisArtifacts' ELSE '[]'::jsonb END) WITH ORDINALITY AS a(value, ordinality)
        WHERE s.created_by = ? AND s.deleted_at IS NULL AND s.metadata->>'conversationKind' IN ('personal_main','personal_side') AND jsonb_typeof(a.value) = 'object'
        ORDER BY m.created_at DESC, m.id DESC, a.ordinality ASC LIMIT 50
    `, owner).Rows()
	if err != nil {
		return nil, fmt.Errorf("query personal artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domain.PersonalArtifact, 0, 50)
	for rows.Next() {
		var item domain.PersonalArtifact
		var ordinal int
		var payload []byte
		var metadata []byte
		if err := rows.Scan(&item.MessageID, &item.SessionID, &item.TaskID, &ordinal, &payload, &item.CreatedAt, &metadata); err != nil {
			return nil, fmt.Errorf("scan personal artifact: %w", err)
		}
		if err := json.Unmarshal(payload, &item.Artifact); err != nil {
			return nil, fmt.Errorf("decode personal artifact: %w", err)
		}
		if err := json.Unmarshal(metadata, &item.SourceMetadata); err != nil {
			return nil, fmt.Errorf("decode personal artifact lineage: %w", err)
		}
		if item.Artifact.Kind == "" {
			continue
		}
		item.ID = fmt.Sprintf("message:%s:%d", item.MessageID, ordinal-1)
		if item.TaskID != "" {
			item.ID = fmt.Sprintf("task:%s:%d", item.TaskID, ordinal-1)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) getPersonalAssistant(ctx context.Context, owner string) (domain.PersonalAssistant, error) {
	var result domain.PersonalAssistant
	err := r.db.WithContext(ctx).Raw(`SELECT id, created_by, name, main_session_id, created_at, updated_at, avatar_url, avatar_poster_url, avatar_motion FROM ai_personal_assistants WHERE created_by = ?`, owner).Row().Scan(&result.ID, &result.CreatedBy, &result.Name, &result.MainSessionID, &result.CreatedAt, &result.UpdatedAt, &result.AvatarURL, &result.AvatarPosterURL, &result.AvatarMotion)
	if err != nil {
		return domain.PersonalAssistant{}, fmt.Errorf("read personal assistant: %w", err)
	}
	return result, nil
}

func (r *Repository) UpdatePersonalAssistantAvatar(ctx context.Context, owner string, input domain.PersonalAssistantAvatar) (domain.PersonalAssistant, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_personal_assistants SET avatar_url = ?, avatar_poster_url = ?, avatar_motion = ?, updated_at = ? WHERE created_by = ?`, input.AvatarURL, input.AvatarPosterURL, input.AvatarMotion, time.Now().UTC(), owner)
	if result.Error != nil {
		return domain.PersonalAssistant{}, fmt.Errorf("update personal assistant avatar: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.PersonalAssistant{}, copilotNotFound("personal assistant", owner)
	}
	return r.getPersonalAssistant(ctx, owner)
}

func (r *Repository) RenamePersonalAssistant(ctx context.Context, owner, name string) (domain.PersonalAssistant, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_personal_assistants SET name = ?, updated_at = ? WHERE created_by = ?`, name, time.Now().UTC(), owner)
	if result.Error != nil {
		return domain.PersonalAssistant{}, fmt.Errorf("rename personal assistant: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.PersonalAssistant{}, copilotNotFound("personal assistant", owner)
	}
	return r.getPersonalAssistant(ctx, owner)
}

func (r *Repository) UpdatePersonalAssistantAvatarIfCurrent(ctx context.Context, owner string, input domain.PersonalAssistantAvatar, expected domain.PersonalAssistant) (domain.PersonalAssistant, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_personal_assistants SET avatar_url = ?, avatar_poster_url = ?, avatar_motion = ?, updated_at = ? WHERE created_by = ? AND avatar_url = ? AND avatar_poster_url = ? AND avatar_motion = ? AND updated_at = ?`, input.AvatarURL, input.AvatarPosterURL, input.AvatarMotion, time.Now().UTC(), owner, expected.AvatarURL, expected.AvatarPosterURL, expected.AvatarMotion, expected.UpdatedAt)
	if result.Error != nil {
		return domain.PersonalAssistant{}, fmt.Errorf("update assistant appearance: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.PersonalAssistant{}, apperrors.ErrConflict
	}
	return r.getPersonalAssistant(ctx, owner)
}

// The saved profile and its undo receipt commit together; stale generation and undo cannot overwrite a newer image.
func (r *Repository) CommitPersonalAssistantAvatar(ctx context.Context, owner string, input domain.PersonalAssistantAvatar, expected domain.PersonalAssistant, receipt domain.Message) (domain.PersonalAssistant, error) {
	var item domain.PersonalAssistant
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		if _, err := repo.GetSession(ctx, owner, receipt.SessionID); err != nil {
			return err
		}
		var err error
		item, err = repo.UpdatePersonalAssistantAvatarIfCurrent(ctx, owner, input, expected)
		if err != nil {
			return err
		}
		receipt.Metadata["assistantProfileAppliedUpdatedAt"] = item.UpdatedAt.Format(time.RFC3339Nano)
		_, err = repo.CreateMessage(ctx, receipt)
		return err
	})
	return item, err
}
