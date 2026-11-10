package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appmemory "github.com/opensoha/soha/internal/application/memory"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
)

type chatMemoryReader interface {
	ListRecords(context.Context, string, string) ([]appmemory.Record, error)
	GetPolicy(context.Context, string, string) (appmemory.Policy, error)
}

func (s *Service) SetChatMemoryReader(reader chatMemoryReader) { s.chatMemory = reader }
func (s *Service) appendChatMemory(ctx context.Context, principal domainidentity.Principal, envelope *domaincopilot.ContextEnvelope, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	ctx = boundedMemoryLineage(ctx)
	if len(ids) > 20 || len(normalizeStringList(ids)) != len(ids) {
		return fmt.Errorf("%w: select at most 20 distinct memories", apperrors.ErrInvalidArgument)
	}
	if s.chatMemory == nil {
		return fmt.Errorf("%w: explicit memory is disabled", apperrors.ErrUnsupportedOperation)
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermAIMemoryView); err != nil {
		return err
	}
	records, err := s.chatMemory.ListRecords(ctx, "user", principal.UserID)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, id := range ids {
		index := slices.IndexFunc(records, func(record appmemory.Record) bool {
			return record.ID == id && chatMemoryAvailable(record, principal.UserID, now)
		})
		if index < 0 {
			return fmt.Errorf("%w: selected memory is unavailable or expired", apperrors.ErrNotFound)
		}
		record := records[index]
		lineage, err := s.authorizeChatMemoryRecord(ctx, principal, record)
		if err != nil {
			return err
		}
		before := len(envelope.Citations)
		appendChatEvidence(envelope, "个人记忆", "memory:"+record.ID, record.Fact)
		if len(envelope.Citations) > before {
			envelope.Citations[len(envelope.Citations)-1].ContentHash = chatMemoryContentHash(record)
			envelope.Citations = append(envelope.Citations, lineage...)
		}
	}
	return nil
}

type memoryLineageDepthKey struct{}
type memoryLineageBudgetKey struct{}

func boundedMemoryLineage(ctx context.Context) context.Context {
	if ctx.Value(memoryLineageBudgetKey{}) != nil {
		return ctx
	}
	remaining := &atomic.Int64{}
	remaining.Store(200)
	return context.WithValue(ctx, memoryLineageBudgetKey{}, remaining)
}

func (s *Service) authorizeChatMemoryRecord(ctx context.Context, principal domainidentity.Principal, record appmemory.Record) ([]domainknowledge.Citation, error) {
	policy, err := s.chatMemory.GetPolicy(ctx, record.PolicyID, record.PolicyVer)
	if err != nil {
		return nil, err
	}
	consented, err := appmemory.LegacyChatConsent(ctx, s.chatMemory, policy, principal.UserID, false)
	if err != nil {
		return nil, err
	}
	if !consented || !appmemory.ReadAllowed(policy) {
		return nil, apperrors.ErrAccessDenied
	}
	return s.authorizeChatMemorySources(ctx, principal, record)
}

func (s *Service) authorizeChatMemorySources(ctx context.Context, principal domainidentity.Principal, record appmemory.Record) ([]domainknowledge.Citation, error) {
	if record.Quarantined {
		return nil, apperrors.ErrAccessDenied
	}
	ctx = boundedMemoryLineage(ctx)
	if len(record.SourceRefs) > 32 {
		return nil, apperrors.ErrAccessDenied
	}
	if err := s.authorizeChatMemorySourcePolicy(ctx, principal, record); err != nil {
		return nil, err
	}
	depth, _ := ctx.Value(memoryLineageDepthKey{}).(int)
	if depth >= 4 {
		return nil, fmt.Errorf("%w: memory source chain exceeds four levels", apperrors.ErrAccessDenied)
	}
	ctx = context.WithValue(ctx, memoryLineageDepthKey{}, depth+1)
	remaining, ok := ctx.Value(memoryLineageBudgetKey{}).(*atomic.Int64)
	if !ok || remaining == nil {
		return nil, apperrors.ErrAccessDenied
	}
	lineage := []domainknowledge.Citation{}
	for _, source := range record.SourceRefs {
		if err := s.authorizeChatMemoryReceipt(ctx, principal, record, source, remaining, &lineage); err != nil {
			return nil, err
		}
	}
	return lineage, nil
}

func chatMemorySourceReference(source string) (domaincopilot.ContextReference, error) {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "soha" || u.Host != "session" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(source) > 2048 {
		return domaincopilot.ContextReference{}, fmt.Errorf("%w: memory source requires an owned message receipt URI", apperrors.ErrAccessDenied)
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "message" || parts[2] == "" {
		return domaincopilot.ContextReference{}, fmt.Errorf("%w: invalid memory source receipt URI", apperrors.ErrAccessDenied)
	}
	return domaincopilot.ContextReference{Kind: "session", SessionID: parts[0], MessageIDs: []string{parts[2]}}, nil
}

func chatMemoryContentHash(record appmemory.Record) string {
	hash := sha256.Sum256([]byte(redaction.Text(record.Fact)))
	return hex.EncodeToString(hash[:])
}

func chatMemoryAvailable(record appmemory.Record, owner string, now time.Time) bool {
	if record.Quarantined {
		return false
	}
	return record.OwnerType == "user" && record.OwnerID == owner && record.Status == "active" && (record.SourceType == "explicit_user" || record.SourceType == "curated_extractor") && record.DeletedAt == nil && !record.ValidFrom.After(now) && record.ExpiresAt != nil && record.ExpiresAt.After(now)
}

// ValidateSource implements memory.SourceValidator for both worker and recall.
// Resolve fresh identity even for background sync: saved role claims are not authority.
func (s *Service) ValidateSource(ctx context.Context, record appmemory.Record) error {
	if record.Quarantined {
		return apperrors.ErrAccessDenied
	}
	if s.chatMemory == nil || s.agentPrincipals == nil || s.permissions == nil {
		return apperrors.ErrServiceUnavailable
	}
	if record.OwnerType != "user" || len(record.SourceRefs) == 0 {
		return apperrors.ErrAccessDenied
	}
	principal, err := s.agentPrincipals.CurrentPrincipal(ctx, record.OwnerID)
	if err != nil {
		return err
	}
	if principal.UserID != record.OwnerID {
		return apperrors.ErrAccessDenied
	}
	if err = s.authorizePrincipal(ctx, principal, appaccess.PermAIMemoryView); err != nil {
		return err
	}
	policy, err := s.chatMemory.GetPolicy(ctx, record.PolicyID, record.PolicyVer)
	if err != nil {
		return err
	}
	mode := policy.Mode
	if mode == "" {
		mode = "chat"
	}
	if mode != "chat" && mode != "heya" {
		return apperrors.ErrAccessDenied
	}
	if record.ScopeHash != appmemory.Scope(record.OwnerID, mode) {
		return apperrors.ErrAccessDenied
	}
	for _, source := range record.SourceRefs {
		if record.SourceHashes[source] == "" {
			return apperrors.ErrAccessDenied
		}
		ref, err := chatMemorySourceReference(source)
		if err != nil {
			return err
		}
		session, err := s.ownedSession(ctx, principal, ref.SessionID)
		if err != nil {
			return err
		}
		if isPersonalSession(session) != (mode == "heya") {
			return apperrors.ErrAccessDenied
		}
	}
	_, err = s.authorizeChatMemorySources(ctx, principal, record)
	return err
}

func memoryMessageHash(m domaincopilot.Message) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(m.Role+"\x00"+m.Content)))
}
func (s *Service) memorySourceMessage(ctx context.Context, session, id string) (domaincopilot.Message, error) {
	if reader, ok := s.messages.(interface {
		GetMessage(context.Context, string, string) (domaincopilot.Message, error)
	}); ok {
		return reader.GetMessage(ctx, session, id)
	}
	messages, err := s.listRecentMessages(ctx, session, 100)
	if err != nil {
		return domaincopilot.Message{}, err
	}
	for _, m := range messages {
		if m.ID == id {
			return m, nil
		}
	}
	return domaincopilot.Message{}, apperrors.ErrNotFound
}

// PrepareMemoryRecord binds provenance and scope on the server before explicit saves.
func (s *Service) PrepareMemoryRecord(ctx context.Context, principal domainidentity.Principal, record appmemory.Record, policy appmemory.Policy) (appmemory.Record, error) {
	if record.SourceType != "explicit_user" || len(record.SourceRefs) > 32 {
		return record, apperrors.ErrAccessDenied
	}
	mode := policy.Mode
	if mode == "" {
		mode = "chat"
	}
	record.OwnerType = "user"
	record.OwnerID = principal.UserID
	record.ScopeHash = appmemory.Scope(principal.UserID, mode)
	record.SourceHashes = map[string]string{}
	record.SourceRefs = slices.Clone(record.SourceRefs)
	for i, source := range record.SourceRefs {
		ref, err := chatMemorySourceReference(source)
		if err != nil {
			return record, err
		}
		session, err := s.ownedSession(ctx, principal, ref.SessionID)
		if err != nil {
			return record, err
		}
		if isPersonalSession(session) != (mode == "heya") {
			return record, apperrors.ErrAccessDenied
		}
		lineage := []domainknowledge.Citation{}
		if _, _, _, err = s.readChatSessionReference(boundedMemoryLineage(ctx), principal, ref, &lineage); err != nil {
			return record, err
		}
		message, err := s.memorySourceMessage(ctx, ref.SessionID, ref.MessageIDs[0])
		if err != nil {
			return record, err
		}
		// Explicit saves still cannot copy retrieved or assistant-generated material into a profile.
		if message.Role != "user" || len(lineage) > 0 {
			return record, apperrors.ErrAccessDenied
		}
		source = "soha://session/" + url.PathEscape(ref.SessionID) + "/message/" + url.PathEscape(ref.MessageIDs[0])
		record.SourceRefs[i] = source
		record.SourceHashes[source] = memoryMessageHash(message)
	}
	return record, nil
}

type chatMemoryAutomation interface {
	GetRecord(context.Context, string) (appmemory.Record, error)
	ListPolicies(context.Context) ([]appmemory.Policy, error)
	Recall(context.Context, string, string, string) ([]appmemory.Record, error)
	PutRecord(context.Context, appmemory.Record, appmemory.Policy) (appmemory.Record, error)
}

func (s *Service) RecallChatMemory(ctx context.Context, principal domainidentity.Principal, envelope *domaincopilot.ContextEnvelope, mode, query string) error {
	memory, ok := s.chatMemory.(chatMemoryAutomation)
	if !ok {
		return nil
	}
	if mode != "heya" {
		mode = "chat"
	}
	policies, err := memory.ListPolicies(ctx)
	if err != nil {
		return err
	}
	enabled := slices.ContainsFunc(policies, func(p appmemory.Policy) bool {
		return p.OwnerID == principal.UserID && p.Mode == mode && appmemory.ReadAllowed(p)
	})
	if !enabled {
		return nil
	}
	if err = s.authorizePrincipal(ctx, principal, appaccess.PermAIMemoryView); err != nil {
		return err
	}
	records, err := memory.Recall(ctx, principal.UserID, mode, query)
	if err != nil {
		return err
	}
	if envelope == nil {
		return fmt.Errorf("memory context envelope required")
	}
	for _, r := range records {
		if !chatMemoryAvailable(r, principal.UserID, time.Now()) {
			continue
		}
		if err := s.ValidateSource(ctx, r); err != nil {
			continue
		}
		lineage, err := s.authorizeChatMemoryRecord(ctx, principal, r)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(envelope.Citations, func(c domainknowledge.Citation) bool { return c.URI == "memory:"+r.ID }) {
			continue
		}
		before := len(envelope.Citations)
		appendChatEvidence(envelope, "个人记忆", "memory:"+r.ID, r.Fact)
		if len(envelope.Citations) > before {
			envelope.Citations[len(envelope.Citations)-1].ContentHash = chatMemoryContentHash(r)
			envelope.Citations = append(envelope.Citations, lineage...)
		}
		if len(envelope.Citations) >= 20 {
			break
		}
	}
	return nil
}
func (s *Service) RememberChatUserMessage(ctx context.Context, principal domainidentity.Principal, sessionID, messageID, mode string) error {
	if mode != "heya" {
		return nil
	}
	memory, ok := s.chatMemory.(chatMemoryAutomation)
	if !ok {
		return nil
	}
	policies, err := memory.ListPolicies(ctx)
	if err != nil {
		return err
	}
	for _, p := range policies {
		if p.OwnerID != principal.UserID || p.Mode != "heya" || p.ExplicitWriteOnly || !appmemory.WriteAllowed(p) {
			continue
		}
		return s.rememberCuratedPreference(ctx, principal, sessionID, messageID, memory, p)
	}
	return nil
}

// ValidateChatMemoryScope also covers explicit selections and memory-derived history.
func (s *Service) ValidateChatMemoryScope(ctx context.Context, principal domainidentity.Principal, envelope *domaincopilot.ContextEnvelope, mode string) error {
	if envelope == nil {
		return nil
	}
	if mode != "heya" {
		mode = "chat"
	}
	for _, c := range envelope.Citations {
		if !strings.HasPrefix(c.URI, "memory:") {
			continue
		}
		if s.chatMemory == nil {
			return apperrors.ErrAccessDenied
		}
		records, err := s.chatMemory.ListRecords(ctx, "user", principal.UserID)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(records, func(r appmemory.Record) bool { return "memory:"+r.ID == c.URI })
		if i < 0 {
			return apperrors.ErrAccessDenied
		}
		r := records[i]
		p, err := s.chatMemory.GetPolicy(ctx, r.PolicyID, r.PolicyVer)
		if err != nil {
			return err
		}
		consented, err := appmemory.LegacyChatConsent(ctx, s.chatMemory, p, principal.UserID, false)
		if err != nil {
			return err
		}
		if !consented || !appmemory.ReadAllowed(p) {
			return apperrors.ErrAccessDenied
		}
		if p.Mode == "" && mode == "chat" {
			continue
		}
		if p.Mode != mode || r.ScopeHash != appmemory.Scope(principal.UserID, mode) {
			return apperrors.ErrAccessDenied
		}
	}
	return nil
}

// These receipts are written only by the server into persisted assistant context.
// The original user selection remains the authority; a citation alone grants nothing.
type memoryShareReceipt struct {
	SelectionMessageID string `json:"selectionMessageId"`
	ContentHash        string `json:"contentHash"`
	SourceHash         string `json:"sourceHash"`
	PolicyID           string `json:"policyId"`
	PolicyVersion      string `json:"policyVersion"`
	ScopeHash          string `json:"scopeHash"`
}

func memoryShareSourceHash(r appmemory.Record) string {
	data, _ := json.Marshal(struct {
		Refs   []string
		Hashes map[string]string
	}{r.SourceRefs, r.SourceHashes})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func memoryShareReceiptFor(r appmemory.Record, selection string) memoryShareReceipt {
	return memoryShareReceipt{SelectionMessageID: selection, ContentHash: chatMemoryContentHash(r), SourceHash: memoryShareSourceHash(r), PolicyID: r.PolicyID, PolicyVersion: r.PolicyVer, ScopeHash: r.ScopeHash}
}
func (s *Service) persistedMemorySelection(ctx context.Context, sessionID, messageID string) ([]string, error) {
	if messageID == "" {
		return nil, nil
	}
	message, err := s.memorySourceMessage(ctx, sessionID, messageID)
	if err != nil {
		return nil, err
	}
	if message.Role != "user" || message.ID != messageID || message.SessionID != sessionID {
		return nil, apperrors.ErrAccessDenied
	}
	ids := agentPrincipalStringList(message.Metadata["_sohaExplicitMemoryIds"])
	if len(ids) > 20 || len(normalizeStringList(ids)) != len(ids) {
		return nil, apperrors.ErrAccessDenied
	}
	return ids, nil
}
func (s *Service) ValidateChatMemoryScopeForTurn(ctx context.Context, principal domainidentity.Principal, envelope *domaincopilot.ContextEnvelope, mode, currentMessageID string, historyIDs []string) error {
	_, err := s.MemoryShareReceiptsForTurn(ctx, principal, envelope, mode, currentMessageID, historyIDs)
	return err
}
func (s *Service) MemoryShareReceiptsForTurn(ctx context.Context, principal domainidentity.Principal, envelope *domaincopilot.ContextEnvelope, mode, currentMessageID string, historyIDs []string) (map[string]memoryShareReceipt, error) {
	receipts := map[string]memoryShareReceipt{}
	if envelope == nil {
		return receipts, nil
	}
	if mode != "heya" {
		mode = "chat"
	}
	citations := slices.DeleteFunc(slices.Clone(envelope.Citations), func(c domainknowledge.Citation) bool { return !strings.HasPrefix(c.URI, "memory:") })
	if len(citations) == 0 {
		return receipts, nil
	}
	if s.chatMemory == nil || len(historyIDs) > 200 {
		return nil, apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermAIMemoryView); err != nil {
		return nil, err
	}
	records, err := s.chatMemory.ListRecords(ctx, "user", principal.UserID)
	if err != nil {
		return nil, err
	}
	var selected []string
	var selectionErr error
	var historical map[string][]memoryShareReceipt
	shareLoaded := false
	for _, citation := range citations {
		id := strings.TrimPrefix(citation.URI, "memory:")
		r, policy, err := s.authorizedMemoryCitation(ctx, principal, records, citation)
		if err != nil {
			return nil, err
		}
		donorMode := policy.Mode
		if donorMode == "" {
			donorMode = "chat"
		}
		if donorMode == mode {
			continue
		}
		if !shareLoaded {
			session, err := s.authorizedMemoryShareSession(ctx, principal, envelope.SessionID, mode)
			if err != nil {
				return nil, err
			}
			selected, selectionErr = s.persistedMemorySelection(ctx, session.ID, currentMessageID)
			historical = s.persistedMemoryShareReceipts(ctx, session.ID, historyIDs)
			shareLoaded = true
		}
		// A recipient's explicit read opt-out wins over a donor's consent.
		if err = s.authorizeMemoryShareRecipient(ctx, principal.UserID, mode); err != nil {
			return nil, err
		}
		if policy.Mode != "" && r.ScopeHash != appmemory.Scope(principal.UserID, donorMode) {
			return nil, apperrors.ErrAccessDenied
		}
		if selectionErr == nil && slices.Contains(selected, id) {
			receipts[id] = memoryShareReceiptFor(r, currentMessageID)
			continue
		}
		receipt, err := s.inheritedMemoryShareReceipt(ctx, envelope.SessionID, r, historical[id])
		if err != nil {
			return nil, err
		}
		receipts[id] = receipt
	}
	return receipts, nil
}
func (s *Service) authorizeMemoryShareRecipient(ctx context.Context, owner, mode string) error {
	reader, ok := s.chatMemory.(interface {
		ListPolicies(context.Context) ([]appmemory.Policy, error)
	})
	if !ok {
		return apperrors.ErrAccessDenied
	}
	policies, err := reader.ListPolicies(ctx)
	if err != nil {
		return err
	}
	for _, p := range policies {
		if p.OwnerID == owner && p.Mode == mode && !appmemory.ReadAllowed(p) {
			return apperrors.ErrAccessDenied
		}
	}
	return nil
}
func (s *Service) persistedMemoryShareReceipts(ctx context.Context, sessionID string, historyIDs []string) map[string][]memoryShareReceipt {
	out := map[string][]memoryShareReceipt{}
	for _, id := range normalizeStringList(historyIDs) {
		message, err := s.memorySourceMessage(ctx, sessionID, id)
		if err != nil || message.Role != "assistant" || message.ID != id || message.SessionID != sessionID {
			continue
		}
		snapshot := mapValue(message.Metadata["contextSnapshot"])
		data, err := json.Marshal(snapshot["_sohaMemoryShareReceipts"])
		if err != nil {
			continue
		}
		receipts := map[string]memoryShareReceipt{}
		if json.Unmarshal(data, &receipts) != nil {
			continue
		}
		citations, err := messageKnowledgeCitations(message)
		if err != nil {
			continue
		}
		for memoryID, receipt := range receipts {
			if receipt.SelectionMessageID == "" {
				continue
			}
			if slices.ContainsFunc(citations, func(c domainknowledge.Citation) bool {
				return c.URI == "memory:"+memoryID && c.ContentHash == receipt.ContentHash
			}) {
				out[memoryID] = append(out[memoryID], receipt)
			}
		}
	}
	return out
}

func (s *Service) authorizeChatMemorySourcePolicy(ctx context.Context, principal domainidentity.Principal, record appmemory.Record) error {
	policy, err := s.chatMemory.GetPolicy(ctx, record.PolicyID, record.PolicyVer)
	if err != nil {
		return err
	}
	if !policy.Enabled || record.Confidence < policy.MinimumConfidence || policy.Version != record.PolicyVer || (policy.OwnerID != "" && policy.OwnerID != principal.UserID) || (record.SourceType == "curated_extractor" && (policy.Mode != "heya" || policy.OwnerID != principal.UserID || policy.ExplicitWriteOnly)) || !slices.Contains(policy.OwnerTypes, "user") {
		return fmt.Errorf("%w: memory policy no longer permits this reference", apperrors.ErrAccessDenied)
	}
	return nil
}

func (s *Service) authorizeChatMemoryReceipt(ctx context.Context, principal domainidentity.Principal, record appmemory.Record, source string, remaining *atomic.Int64, lineage *[]domainknowledge.Citation) error {
	if record.SourceHashes[source] == "" {
		return fmt.Errorf("%w: memory source revision unavailable", apperrors.ErrAccessDenied)
	}
	if remaining.Add(-1) < 0 {
		return fmt.Errorf("%w: memory source chain exceeds 200 receipts", apperrors.ErrAccessDenied)
	}
	ref, err := chatMemorySourceReference(source)
	if err != nil {
		return err
	}
	if _, _, _, err := s.readChatSessionReference(ctx, principal, ref, lineage); err != nil {
		return err
	}
	if hash := record.SourceHashes[source]; hash != "" {
		if remaining.Add(-1) < 0 {
			return fmt.Errorf("%w: memory source chain exceeds 200 receipts", apperrors.ErrAccessDenied)
		}
		message, err := s.memorySourceMessage(ctx, ref.SessionID, ref.MessageIDs[0])
		if err != nil {
			return err
		}
		if hash != memoryMessageHash(message) {
			return apperrors.ErrAccessDenied
		}
	}
	return nil
}

func (s *Service) rememberCuratedPreference(ctx context.Context, principal domainidentity.Principal, sessionID, messageID string, memory chatMemoryAutomation, p appmemory.Policy) error {
	if err := s.authorizePrincipal(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermAIMemoryManage, "update")); err != nil {
		return err
	}
	message, err := s.memorySourceMessage(ctx, sessionID, messageID)
	if err != nil {
		return err
	}
	// Conservative deterministic curation: retain only a short first-person preference,
	// never a complete conversation, tool output or retrieved knowledge.
	fact := strings.TrimSpace(message.Content)
	if len(fact) > 1000 || !(strings.HasPrefix(fact, "我偏好") || strings.HasPrefix(fact, "我喜欢") || strings.HasPrefix(strings.ToLower(fact), "i prefer ")) {
		return nil
	}
	source := "soha://session/" + url.PathEscape(sessionID) + "/message/" + url.PathEscape(messageID)
	sum := sha256.Sum256([]byte(principal.UserID + "\x00" + sessionID + "\x00" + messageID + "\x00" + p.ID + "\x00" + p.Version))
	r := appmemory.Record{ID: fmt.Sprintf("auto-%x", sum), SourceType: "explicit_user", SourceRefs: []string{source}, Fact: fact, PolicyVer: p.Version, Confidence: 1}
	r, err = s.PrepareMemoryRecord(ctx, principal, r, p)
	if err != nil {
		return err
	}
	if old, err := memory.GetRecord(ctx, r.ID); err == nil {
		if old.Status == "deleted" || (old.Fact == r.Fact && maps.Equal(old.SourceHashes, r.SourceHashes) && old.PolicyID == p.ID && old.PolicyVer == p.Version) {
			return nil
		}
	} else if !errors.Is(err, appmemory.ErrNotFound) {
		return err
	}
	r.SourceType = "curated_extractor"
	_, err = memory.PutRecord(ctx, r, p)
	return err
}

func (s *Service) authorizedMemoryCitation(ctx context.Context, principal domainidentity.Principal, records []appmemory.Record, citation domainknowledge.Citation) (appmemory.Record, appmemory.Policy, error) {
	id := strings.TrimPrefix(citation.URI, "memory:")
	i := slices.IndexFunc(records, func(r appmemory.Record) bool {
		return r.ID == id && chatMemoryAvailable(r, principal.UserID, time.Now())
	})
	if i < 0 {
		return appmemory.Record{}, appmemory.Policy{}, apperrors.ErrAccessDenied
	}
	r := records[i]
	if citation.ContentHash != chatMemoryContentHash(r) {
		return appmemory.Record{}, appmemory.Policy{}, apperrors.ErrAccessDenied
	}
	if _, err := s.authorizeChatMemoryRecord(ctx, principal, r); err != nil {
		return appmemory.Record{}, appmemory.Policy{}, err
	}
	policy, err := s.chatMemory.GetPolicy(ctx, r.PolicyID, r.PolicyVer)
	if err != nil {
		return appmemory.Record{}, appmemory.Policy{}, err
	}
	return r, policy, nil
}

func (s *Service) inheritedMemoryShareReceipt(ctx context.Context, sessionID string, r appmemory.Record, historical []memoryShareReceipt) (memoryShareReceipt, error) {
	for _, receipt := range historical {
		if receipt != memoryShareReceiptFor(r, receipt.SelectionMessageID) {
			continue
		}
		ids, err := s.persistedMemorySelection(ctx, sessionID, receipt.SelectionMessageID)
		if err == nil && slices.Contains(ids, r.ID) {
			return receipt, nil
		}
	}
	return memoryShareReceipt{}, apperrors.ErrAccessDenied
}

func (s *Service) authorizedMemoryShareSession(ctx context.Context, principal domainidentity.Principal, sessionID, mode string) (domaincopilot.Session, error) {
	session, err := s.ownedSession(ctx, principal, sessionID)
	if err != nil {
		return domaincopilot.Session{}, err
	}
	if session.ID != sessionID || isPersonalSession(session) != (mode == "heya") {
		return domaincopilot.Session{}, apperrors.ErrAccessDenied
	}
	return session, nil
}
