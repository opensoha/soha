package assistantmesh

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	copilot "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/domain/knowledge"
	settings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	receptionCaller = "1c0b7698-ee22-4c2a-96ce-902dbd39bb18"
	receptionOwner  = "2c0b7698-ee22-4c2a-96ce-902dbd39bb18"
	receptionOther  = "3c0b7698-ee22-4c2a-96ce-902dbd39bb18"
	receptionReq    = "4c0b7698-ee22-4c2a-96ce-902dbd39bb18"
	receptionReq2   = "5c0b7698-ee22-4c2a-96ce-902dbd39bb18"
)

type receptionTestStore struct {
	ReceptionStore
	profile   domain.Profile
	inquiries map[string]domain.Inquiry
}

func (s *receptionTestStore) GetProfile(context.Context, string) (domain.Profile, error) {
	return s.profile, nil
}
func (s *receptionTestStore) ListProfiles(context.Context, string, int) ([]domain.Profile, error) {
	return []domain.Profile{s.profile}, nil
}
func (s *receptionTestStore) GetInquiry(_ context.Context, caller, request string) (domain.Inquiry, error) {
	item, ok := s.inquiries[request]
	if !ok || item.CallerID != caller {
		return domain.Inquiry{}, apperrors.ErrNotFound
	}
	return item, nil
}
func (s *receptionTestStore) CreateInquiry(_ context.Context, item domain.Inquiry, _ domain.InquiryLimits) (domain.Inquiry, error) {
	s.inquiries[item.RequestID] = item
	return item, nil
}
func (s *receptionTestStore) CompleteInquiry(_ context.Context, item domain.Inquiry) error {
	s.inquiries[item.RequestID] = item
	return nil
}

func (s *receptionTestStore) FallbackInquiry(_ context.Context, item domain.Inquiry, _ int, _ time.Time, _ time.Duration) (domain.Inquiry, error) {
	item.Status = domain.InquiryPending
	s.inquiries[item.RequestID] = item
	return item, nil
}

type receptionTestIdentities map[string]identity.Principal

func (i receptionTestIdentities) CurrentPrincipal(_ context.Context, id string) (identity.Principal, error) {
	p, ok := i[id]
	if !ok {
		return p, apperrors.ErrUnauthorized
	}
	return p, nil
}

type receptionTestAccess struct{}

func (receptionTestAccess) GetPersonalAgentAccess(context.Context, identity.Principal) (copilot.PersonalAgentAccess, error) {
	return copilot.PersonalAgentAccess{Status: "approved"}, nil
}

type receptionTestModel struct {
	facts        []ReceptionFact
	beforeReturn func()
}

func (m *receptionTestModel) AnswerReception(_ context.Context, _ identity.Principal, _ string, fact ReceptionFact) (string, error) {
	m.facts = append(m.facts, fact)
	if m.beforeReturn != nil {
		m.beforeReturn()
	}
	return "共有排查步骤", nil
}

func publishReceptionFact(t *testing.T, f *fixture, id, title, body string, audience knowledge.AccessScope, expires *time.Time) {
	t.Helper()
	value, err := encodePayload(payload{Published: &Version{
		Version: 2, PublishedAt: nowUTC(), ExpiresAt: expires, Audience: audience,
		Input: domain.AssetInput{Kind: domain.KindExperienceCard, Title: title, Body: body, Files: map[string]string{"private.txt": "never relay files"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.store.records[id] = domain.Record{ID: id, OwnerID: receptionOwner, Kind: domain.KindExperienceCard, Payload: value}
}

func TestReceptionFiltersDiscoveryAndEveryAnswerRead(t *testing.T) {
	f := newFixture(t)
	caller, other := employee(receptionCaller, "team-a"), employee(receptionOther)
	owner := employee(receptionOwner, "team-a")
	for _, p := range []identity.Principal{caller, owner, other} {
		f.service.users.(*fakeUsers).users[p.UserID] = identity.User{ID: p.UserID, Status: "active"}
	}
	ids := receptionTestIdentities{caller.UserID: caller, owner.UserID: owner, other.UserID: other}
	store := &receptionTestStore{
		profile:   domain.Profile{OwnerID: owner.UserID, Open: true},
		inquiries: map[string]domain.Inquiry{},
	}
	model := &receptionTestModel{}
	svc := NewReception(f.service, store, ids, receptionTestAccess{}, model)
	publishReceptionFact(t, f, "shared", "订单同步", "published procedure", knowledge.AccessScope{Visibility: "teams", Teams: []string{"team-a"}}, nil)
	publishReceptionFact(t, f, "private", "保密订单同步", "private content", knowledge.AccessScope{Visibility: "private"}, nil)
	yesterday := nowUTC().Add(-24 * time.Hour)
	publishReceptionFact(t, f, "expired", "过期订单同步", "expired content", knowledge.AccessScope{Visibility: "organization"}, &yesterday)

	ctx := context.Background()
	items, err := svc.Discover(ctx, caller, "订单同步失败怎么办")
	if err != nil || len(items) != 1 || len(items[0].Topics) != 1 || items[0].Topics[0] != "订单同步" {
		t.Fatalf("unexpected admissible discovery: %v, %v", items, err)
	}
	items, err = svc.Discover(ctx, other, "")
	if err != nil || len(items) != 0 {
		t.Fatalf("outside audience sees assistant: %v, %v", items, err)
	}
	ask := InquiryInput{OwnerID: owner.UserID, RequestID: receptionReq, Question: "订单同步失败怎么办"}
	item, err := svc.Ask(ctx, caller, ask)
	if err != nil || item.Status != domain.InquiryAnswered || len(item.Citations) != 1 || len(model.facts) != 1 || model.facts[0].Body != "published procedure" {
		t.Fatalf("unexpected scoped answer: %+v, %v (%+v)", item, err, model.facts)
	}
	if _, err := svc.Ask(ctx, other, ask); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("audience bypassed via guessed recipient: %v", err)
	}
	pat := caller
	pat.AccessTokenID = "unrelated-pat"
	if _, err := svc.Ask(ctx, pat, ask); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("personal token escaped Web session boundary: %v", err)
	}
	miss := InquiryInput{OwnerID: owner.UserID, RequestID: receptionReq2, Question: "请问机房今天要搬迁吗"}
	item, err = svc.Ask(ctx, caller, miss)
	if err != nil || item.Status != domain.InquiryPending || item.Answer != "" || len(model.facts) != 1 {
		t.Fatalf("miss did not become a model-free todo: %+v, %v", item, err)
	}
	ids[caller.UserID] = employee(caller.UserID)
	item, err = svc.Ask(ctx, caller, ask)
	if err != nil || item.Answer != "" || len(item.Citations) != 0 || len(model.facts) != 1 {
		t.Fatalf("idempotent replay disclosed revoked data: %+v, %v", item, err)
	}
	ids[caller.UserID] = caller
	model.beforeReturn = func() { ids[caller.UserID] = employee(caller.UserID) }
	ask.RequestID = "6c0b7698-ee22-4c2a-96ce-902dbd39bb18"
	item, err = svc.Ask(ctx, caller, ask)
	if err != nil || item.Status != domain.InquiryPending || item.Answer != "" {
		t.Fatalf("model-flight revocation disclosed data: %+v, %v", item, err)
	}
}

func TestReceptionFactsIncludeVisibleWorkPulse(t *testing.T) {
	f := newFixture(t)
	caller, owner := employee(receptionCaller, "team-a"), employee(receptionOwner, "team-a")
	f.service.users.(*fakeUsers).users[caller.UserID] = identity.User{ID: caller.UserID, Status: "active"}
	f.service.users.(*fakeUsers).users[owner.UserID] = identity.User{ID: owner.UserID, Status: "active"}
	svc := &Reception{assets: f.service}
	now := nowUTC()
	audience := knowledge.AccessScope{Visibility: "teams", Teams: []string{"team-a"}}
	pulse, err := encodePayload(payload{Published: &Version{
		Version: 3, PublishedAt: now, Audience: audience,
		Input: domain.AssetInput{Kind: domain.KindWorkPulse, Title: "网关迁移", Body: "证书轮换中", WorkStatus: "blocked",
			BlockerReason: "等待运维窗口", Files: map[string]string{"private.txt": "never relay files"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.store.records["pulse"] = domain.Record{ID: "pulse", OwnerID: owner.UserID, Kind: domain.KindWorkPulse, Payload: pulse}
	expired := now.Add(-time.Hour)
	old, err := encodePayload(payload{Published: &Version{
		Version: 1, PublishedAt: expired, ExpiresAt: &expired, Audience: audience,
		Input: domain.AssetInput{Kind: domain.KindWorkPulse, Title: "过期进度", Body: "旧内容", WorkStatus: "done"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.store.records["old-pulse"] = domain.Record{ID: "old-pulse", OwnerID: owner.UserID, Kind: domain.KindWorkPulse, Payload: old}
	facts, err := svc.facts(context.Background(), caller, owner.UserID)
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts = %+v, %v", facts, err)
	}
	fact := facts[0]
	raw, _ := json.Marshal(fact)
	if fact.Status != "blocked" || fact.BlockerReason != "等待运维窗口" || !fact.Citation.ObservedAt.Equal(now) || strings.Contains(string(raw), "private.txt") {
		t.Fatalf("work pulse fact = %s", raw)
	}
	visible, err := svc.citationVisible(context.Background(), caller, owner.UserID, fact.Citation)
	if err != nil || !visible {
		t.Fatalf("current citation visible = %v, %v", visible, err)
	}
	wrong := fact.Citation
	wrong.Version = 2
	if visible, err := svc.citationVisible(context.Background(), caller, owner.UserID, wrong); err != nil || visible {
		t.Fatalf("stale citation visible = %v, %v", visible, err)
	}
}

type receptionGatewayFixture struct {
	request  aigateway.WorkbenchRelayRequest
	response aigateway.WorkbenchRelayResponse
}

func (f *receptionGatewayFixture) InvokeWorkbenchModelStream(_ context.Context, _ identity.Principal, in aigateway.WorkbenchRelayRequest, _ func(aigateway.WorkbenchRelayStreamDelta) bool) (aigateway.WorkbenchRelayResponse, error) {
	f.request = in
	return f.response, nil
}

type receptionModelSettings struct{}

func (receptionModelSettings) ResolveAIWorkbenchSettings(context.Context) (settings.AIWorkbenchModelSettings, error) {
	return settings.AIWorkbenchModelSettings{Enabled: true, DefaultPublicModel: "test"}, nil
}

func TestReceptionModelHasNoSessionOrToolsAndRequiresExactCitation(t *testing.T) {
	ctx := context.Background()
	gateway := &receptionGatewayFixture{}
	model := GatewayReceptionModel{Gateway: gateway, Settings: receptionModelSettings{}}
	fact := ReceptionFact{Citation: domain.Citation{AssetID: "card", Version: 2, Title: "订单同步"}, Body: "published text"}
	gateway.response.Content = `{"assetId":"card","version":2,"answer":"grounded"}`
	got, err := model.AnswerReception(ctx, employee(receptionCaller), "订单同步", fact)
	if err != nil || got != "grounded" {
		t.Fatalf("valid answer: %q, %v", got, err)
	}
	out := gateway.request
	if len(out.Tools) != 0 || out.SessionID != "" || out.AgentRunID != "" || out.Mode != "heya_receptionist" ||
		len(out.Messages) != 2 || !strings.Contains(out.Messages[1].Content, "published text") {
		t.Fatalf("reception payload: %+v", out)
	}
	for _, content := range []string{`{}`, `{"assetId":"hidden","version":2,"answer":"guess"}`, `{"assetId":"card","version":1,"answer":"old"}`} {
		gateway.response.Content = content
		if output, err := model.AnswerReception(ctx, employee(receptionCaller), "订单同步", fact); err == nil || output != "" {
			t.Fatalf("unbound reference accepted: %q", output)
		}
	}
	gateway.response = aigateway.WorkbenchRelayResponse{Content: `{"assetId":"card","version":2,"answer":"grounded"}`, ToolCalls: []aigateway.WorkbenchToolCall{{Name: "invented"}}}
	if output, err := model.AnswerReception(ctx, employee(receptionCaller), "订单同步", fact); !errors.Is(err, apperrors.ErrAccessDenied) || output != "" {
		t.Fatalf("unexpected model tool response accepted: %q, %v", output, err)
	}
}
