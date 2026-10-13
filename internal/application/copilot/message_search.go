package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type ConversationReader interface {
	SearchMessageCandidates(context.Context, string, domain.MessageCursor, int) ([]domain.MessageCandidate, error)
	ReadMessageWindow(context.Context, string, string, domain.MessageWindowInput) (domain.MessageWindow, error)
	RecentOwnedMessages(context.Context, string, int) ([]domain.Message, error)
}

type searchCursor struct {
	Time  time.Time `json:"t"`
	ID    string    `json:"i"`
	Query string    `json:"q"`
}

func searchTerms(query string) []string {
	var terms []string
	var term []rune
	var latin bool
	flush := func() {
		if len(term) > 0 {
			terms = append(terms, string(term))
			term = nil
		}
	}
	for _, r := range strings.ToLower(strings.TrimSpace(query)) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		current := r < 128
		if len(term) > 0 && current != latin {
			flush()
		}
		term = append(term, r)
		latin = current
	}
	flush()
	if len(terms) == 0 {
		return []string{strings.ToLower(strings.TrimSpace(query))}
	}
	return terms
}

func messageMatches(content string, terms []string) bool {
	content = strings.ToLower(content)
	for _, term := range terms {
		if !strings.Contains(content, term) {
			return false
		}
	}
	return true
}

func messageSnippet(content string, terms []string) string {
	content = strings.Join(strings.Fields(content), " ")
	start := 0
	if i := strings.Index(strings.ToLower(content), terms[0]); i >= 0 {
		start = max(0, utf8.RuneCountInString(content[:i])-60)
	}
	runes := []rune(content)
	end := min(len(runes), start+320)
	prefix, suffix := "", ""
	if start > 0 {
		prefix = "…"
	}
	if end < len(runes) {
		suffix = "…"
	}
	return prefix + string(runes[start:end]) + suffix
}

func (s *Service) SearchMessages(ctx context.Context, principal identity.Principal, query, cursor string, limit int) (domain.MessageSearchPage, error) {
	return s.searchWorkspaceMessages(ctx, principal, query, cursor, limit, false)
}
func (s *Service) SearchPersonalMessages(ctx context.Context, principal identity.Principal, query, cursor string, limit int) (domain.MessageSearchPage, error) {
	if err := s.requirePersonalAgent(ctx, principal); err != nil {
		return domain.MessageSearchPage{}, err
	}
	return s.searchWorkspaceMessages(ctx, principal, query, cursor, limit, true)
}
func (s *Service) searchWorkspaceMessages(ctx context.Context, principal identity.Principal, query, cursor string, limit int, personal bool) (domain.MessageSearchPage, error) {
	page := domain.MessageSearchPage{Items: []domain.MessageSearchHit{}}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return page, err
	}
	query = strings.TrimSpace(query)
	if !utf8.ValidString(query) || query == "" || utf8.RuneCountInString(query) > 120 || limit < 1 || limit > 50 {
		return page, apperrors.ErrInvalidArgument
	}
	if s.conversations == nil {
		return page, apperrors.ErrUnsupportedOperation
	}
	position, err := readSearchCursor(fmt.Sprintf("%t:%s", personal, query), cursor)
	if err != nil {
		return page, err
	}
	terms := searchTerms(query)
	// Scan owned rows before matching: a revoked source cannot leak through search counts or snippets.
	for batch := 0; batch < 5; batch++ {
		candidates, err := s.conversations.SearchMessageCandidates(ctx, principal.UserID, domain.MessageCursor{CreatedAt: position.Time, ID: position.ID, Personal: personal}, 200)
		if err != nil {
			return page, err
		}
		for _, candidate := range candidates {
			m := candidate.Message
			position.Time, position.ID = m.CreatedAt, m.ID
			if _, err := s.validateMessageKnowledge(ctx, principal, m); err != nil {
				if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrInvalidArgument) {
					continue
				}
				return page, err
			}
			if !messageMatches(m.Content, terms) {
				continue
			}
			page.Items = append(page.Items, domain.MessageSearchHit{SessionID: m.SessionID, SessionTitle: candidate.SessionTitle, MessageID: m.ID, Role: sohaapi.WorkbenchMessageSearchHitRole(m.Role), Snippet: messageSnippet(m.Content, terms), CreatedAt: m.CreatedAt})
			if len(page.Items) == limit {
				return searchPageCursor(page, position), nil
			}
		}
		if len(candidates) < 200 {
			return page, nil
		}
	}
	return searchPageCursor(page, position), nil
}

func readSearchCursor(query, cursor string) (searchCursor, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(query))))
	position := searchCursor{Query: hash}
	if len(cursor) > 512 {
		return position, apperrors.ErrInvalidArgument
	}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || len(raw) > 512 || json.Unmarshal(raw, &position) != nil || position.ID == "" || position.Time.IsZero() || position.Query != hash {
			return position, apperrors.ErrInvalidArgument
		}
	}
	return position, nil
}

func searchPageCursor(page domain.MessageSearchPage, cursor searchCursor) domain.MessageSearchPage {
	raw, _ := json.Marshal(cursor)
	value := base64.RawURLEncoding.EncodeToString(raw)
	page.NextCursor = value
	return page
}

func (s *Service) MessageWindow(ctx context.Context, principal identity.Principal, sessionID string, input domain.MessageWindowInput) (domain.MessageWindow, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domain.MessageWindow{}, err
	}
	selectors := 0
	for _, id := range []string{input.Around, input.Before, input.After} {
		if id != "" {
			selectors++
			if len(id) > 128 {
				return domain.MessageWindow{}, apperrors.ErrInvalidArgument
			}
		}
	}
	if selectors > 1 || input.Limit < 3 || input.Limit > 100 {
		return domain.MessageWindow{}, apperrors.ErrInvalidArgument
	}
	if _, err := s.ownedSession(ctx, principal, sessionID); err != nil {
		return domain.MessageWindow{}, err
	}
	if s.conversations == nil {
		return domain.MessageWindow{}, apperrors.ErrUnsupportedOperation
	}
	page, err := s.conversations.ReadMessageWindow(ctx, principal.UserID, sessionID, input)
	if err != nil {
		return page, err
	}
	for i, m := range page.Messages {
		if m.ID == input.Around {
			if _, err := s.validateMessageKnowledge(ctx, principal, m); err != nil {
				return domain.MessageWindow{}, apperrors.ErrAccessDenied
			}
		}
		page.Messages[i] = s.visibleKnowledgeMessage(ctx, principal, m)
	}
	return page, nil
}
