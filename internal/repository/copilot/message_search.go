package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"slices"
)

func (r *Repository) SearchMessageCandidates(ctx context.Context, owner string, cursor domain.MessageCursor, limit int) ([]domain.MessageCandidate, error) {
	query := r.db.WithContext(ctx).Table("ai_messages m").Select("m.id,m.session_id,m.role,m.content,m.metadata,m.created_at,s.title").
		Joins("JOIN ai_sessions s ON s.id=m.session_id").Where("s.created_by=? AND s.deleted_at IS NULL AND m.role IN ('user','assistant')", owner)
	query = query.Where("(COALESCE(s.metadata->>'conversationKind','') IN ('personal_main','personal_side')) = ?", cursor.Personal)
	if cursor.ID != "" {
		query = query.Where("(m.created_at,m.id)<(?,?)", cursor.CreatedAt, cursor.ID)
	}
	rows, err := query.Order("m.created_at DESC,m.id DESC").Limit(limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("search message candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []domain.MessageCandidate{}
	for rows.Next() {
		var item domain.MessageCandidate
		var metadata []byte
		m := &item.Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &metadata, &m.CreatedAt, &item.SessionTitle); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metadata, &m.Metadata); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) RecentOwnedMessages(ctx context.Context, owner string, limit int) ([]domain.Message, error) {
	candidates, err := r.SearchMessageCandidates(ctx, owner, domain.MessageCursor{Personal: true}, limit)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Message, 0, len(candidates))
	for _, item := range candidates {
		items = append(items, item.Message)
	}
	return items, nil
}

func (r *Repository) windowRows(ctx context.Context, owner, session string, anchor domain.Message, after bool, limit int) ([]domain.Message, bool, error) {
	query := r.db.WithContext(ctx).Table("ai_messages m").Select("m.id,m.session_id,m.role,m.content,m.metadata,m.created_at").
		Joins("JOIN ai_sessions s ON s.id=m.session_id").Where("s.created_by=? AND s.deleted_at IS NULL AND m.session_id=?", owner, session)
	order := "m.created_at DESC,m.id DESC"
	if after {
		order = "m.created_at ASC,m.id ASC"
	}
	if anchor.ID != "" {
		op := "<"
		if after {
			op = ">"
		}
		query = query.Where("(m.created_at,m.id)"+op+"(?,?)", anchor.CreatedAt, anchor.ID)
	}
	rows, err := query.Order(order).Limit(limit + 1).Rows()
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	items := []domain.Message{}
	for rows.Next() {
		item, err := scanMessage(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	if !after {
		slices.Reverse(items)
	}
	return items, more, nil
}

func (r *Repository) windowAnchor(ctx context.Context, owner, session, id string) (domain.Message, error) {
	rows, err := r.db.WithContext(ctx).Raw("SELECT m.id,m.session_id,m.role,m.content,m.metadata,m.created_at FROM ai_messages m JOIN ai_sessions s ON s.id=m.session_id WHERE m.id=? AND m.session_id=? AND s.created_by=? AND s.deleted_at IS NULL", id, session, owner).Rows()
	if err != nil {
		return domain.Message{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return domain.Message{}, err
		}
		return domain.Message{}, copilotNotFound("message", id)
	}
	return scanMessage(rows)
}

func (r *Repository) ReadMessageWindow(ctx context.Context, owner, session string, input domain.MessageWindowInput) (domain.MessageWindow, error) {
	page := domain.MessageWindow{Messages: []domain.Message{}}
	id := input.Around
	if input.Before != "" {
		id = input.Before
	}
	if input.After != "" {
		id = input.After
	}
	var anchor domain.Message
	if id != "" {
		var err error
		anchor, err = r.windowAnchor(ctx, owner, session, id)
		if err != nil {
			return page, err
		}
	}
	if input.Around != "" {
		left, moreLeft, err := r.windowRows(ctx, owner, session, anchor, false, (input.Limit-1)/2)
		if err != nil {
			return page, err
		}
		right, moreRight, err := r.windowRows(ctx, owner, session, anchor, true, input.Limit-1-len(left))
		if err != nil {
			return page, err
		}
		page.Messages = append(append(left, anchor), right...)
		if moreLeft {
			page.BeforeCursor = page.Messages[0].ID
		}
		if moreRight {
			page.AfterCursor = page.Messages[len(page.Messages)-1].ID
		}
		return page, nil
	}
	items, more, err := r.windowRows(ctx, owner, session, anchor, input.After != "", input.Limit)
	if err != nil {
		return page, err
	}
	page.Messages = items
	if len(items) > 0 {
		if (input.After == "" && more) || input.After != "" {
			page.BeforeCursor = items[0].ID
		}
		if (input.After != "" && more) || input.Before != "" {
			page.AfterCursor = items[len(items)-1].ID
		}
	}
	return page, nil
}
