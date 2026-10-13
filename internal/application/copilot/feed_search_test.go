package copilot

import (
	"context"
	"errors"
	appaccess "github.com/opensoha/soha/internal/application/access"
	relay "github.com/opensoha/soha/internal/application/aigateway"
	gateway "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	settings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"strings"
	"testing"
	"time"
)

type searchReaderFixture struct{ items []domain.MessageCandidate }

func (f searchReaderFixture) SearchMessageCandidates(_ context.Context, _ string, cursor domain.MessageCursor, limit int) ([]domain.MessageCandidate, error) {
	start := 0
	for i, item := range f.items {
		if item.Message.ID == cursor.ID {
			start = i + 1
		}
	}
	return f.items[start:min(len(f.items), start+limit)], nil
}
func (f searchReaderFixture) ReadMessageWindow(context.Context, string, string, domain.MessageWindowInput) (domain.MessageWindow, error) {
	return domain.MessageWindow{}, nil
}
func (f searchReaderFixture) RecentOwnedMessages(context.Context, string, int) ([]domain.Message, error) {
	items := []domain.Message{}
	for _, item := range f.items {
		items = append(items, item.Message)
	}
	return items, nil
}

type feedGatewayFixture struct {
	models   []relay.WorkbenchRelayRequest
	search   gateway.ToolInvocationRequest
	approval bool
}

func (f *feedGatewayFixture) WorkbenchCapabilities(context.Context, identity.Principal, gateway.ManifestRequest) (gateway.Manifest, error) {
	return gateway.Manifest{Tools: []gateway.ToolCapability{{Name: "web.search", Version: "1", RiskLevel: gateway.RiskLevelRead, InputSchema: map[string]any{"properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}}}}}}, nil
}
func (f *feedGatewayFixture) InvokeTool(_ context.Context, _ identity.Principal, input gateway.ToolInvocationRequest) (gateway.ToolInvocationResult, error) {
	f.search = input
	return gateway.ToolInvocationResult{RequiresApproval: f.approval, Output: map[string]any{"structuredContent": map[string]any{"results": []any{map[string]any{"url": "https://example.com/article", "title": "Actual source", "snippet": "A real search result excerpt"}}}}}, nil
}
func (f *feedGatewayFixture) InvokeWorkbenchModel(_ context.Context, _ identity.Principal, input relay.WorkbenchRelayRequest) (relay.WorkbenchRelayResponse, error) {
	f.models = append(f.models, input)
	if len(f.models) == 1 {
		return relay.WorkbenchRelayResponse{Content: `{"query":"文件服务 开源","reason":"最近讨论的兴趣"}`}, nil
	}
	return relay.WorkbenchRelayResponse{Content: `{"posts":[{"title":"文件服务动态","body":"有来源的摘要","reason":"与你近期兴趣相关","sourceIndices":[0]}]}`}, nil
}

func TestFeedGenerationUsesCurrentPrincipalAndBoundedGroundedCalls(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermObserveAIChatUse}})
	owner := identity.Principal{UserID: "owner", Roles: []string{"reader"}}
	service.agentPrincipals = &chatToolPrincipalResolver{principal: owner}
	service.feed = &struct{ FeedStore }{}
	service.conversations = searchReaderFixture{items: []domain.MessageCandidate{{Message: domain.Message{Role: "user", Content: "讨论开源文件服务"}}, {Message: domain.Message{Role: "assistant", Content: "revoked private interest", Metadata: map[string]any{"contextSnapshot": map[string]any{"citations": []map[string]any{{"uri": "memory:revoked"}}}}}}}}
	service.settings = inspectionAuthzSettingsResolver{settings: settings.AISettings{WorkbenchModel: settings.AIWorkbenchModelSettings{Enabled: true, DefaultPublicModel: "configured-model", DefaultEndpoint: "chat/completions"}}}
	invoker := &feedGatewayFixture{}
	service.SetWorkbenchModelInvoker(invoker)
	record := domain.FeedRecord{Owner: "owner", JobID: "bounded-job", Settings: domain.FeedSettings{Instructions: "简短资讯", SearchTool: "web.search"}}
	items, err := service.generateFeed(t.Context(), record)
	if err != nil || len(items) != 1 || items[0].Sources[0].URL != "https://example.com/article" {
		t.Fatalf("grounded generation: %+v %v", items, err)
	}
	if len(invoker.models) != 2 || invoker.models[0].MaxOutputTokens != 600 || invoker.models[1].MaxOutputTokens != 2000 || len(invoker.models[0].Tools) != 0 {
		t.Fatal("model budget or tool boundary")
	}
	if strings.Contains(invoker.models[0].Messages[1].Content, "revoked private interest") {
		t.Fatal("revoked source entered topic prompt")
	}
	if invoker.search.CapabilityVersion != "1" || invoker.search.RequestID != record.JobID || invoker.search.Input["limit"] != 5 {
		t.Fatal("search pin or bound")
	}
	invoker = &feedGatewayFixture{approval: true}
	service.SetWorkbenchModelInvoker(invoker)
	if _, err := service.generateFeed(t.Context(), record); !errors.Is(err, apperrors.ErrUnsupportedOperation) || len(invoker.models) != 1 {
		t.Fatal("approval was followed or writing proceeded")
	}
	service.agentPrincipals = &chatToolPrincipalResolver{principal: identity.Principal{UserID: "owner"}}
	invoker = &feedGatewayFixture{}
	service.SetWorkbenchModelInvoker(invoker)
	if _, err := service.generateFeed(t.Context(), record); err == nil || len(invoker.models) > 0 {
		t.Fatal("revoked current permission invoked the model")
	}
}

func TestMessageSearchCursorAndRevokedSources(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"reader": {appaccess.PermObserveAIChatUse}})
	p := identity.Principal{UserID: "owner", Roles: []string{"reader"}}
	service.conversations = searchReaderFixture{items: []domain.MessageCandidate{
		{Message: domain.Message{ID: "revoked", SessionID: "session", Role: "assistant", Content: "飞牛 TV hidden", CreatedAt: time.Now(), Metadata: map[string]any{"contextSnapshot": map[string]any{"citations": []map[string]any{{"uri": "memory:deleted"}}}}}},
		{Message: domain.Message{ID: "visible1", SessionID: "session", Role: "user", Content: "飞牛 TV question", CreatedAt: time.Now()}},
		{Message: domain.Message{ID: "visible2", SessionID: "session", Role: "assistant", Content: "飞牛tv answer", CreatedAt: time.Now()}},
	}}
	page, err := service.SearchMessages(t.Context(), p, "飞牛tv", "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].MessageID != "visible1" || page.NextCursor == "" {
		t.Fatalf("search: %+v %v", page, err)
	}
	next, err := service.SearchMessages(t.Context(), p, "飞牛tv", page.NextCursor, 1)
	if err != nil || len(next.Items) != 1 || next.Items[0].MessageID != "visible2" {
		t.Fatalf("cursor: %+v %v", next, err)
	}
	if _, err := service.SearchMessages(t.Context(), p, "changed", page.NextCursor, 1); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("cursor query changed")
	}
	if _, err := service.SearchMessages(t.Context(), p, "query", strings.Repeat("a", 10000), 1); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("unbounded cursor")
	}
	if _, err := service.SearchMessages(t.Context(), identity.Principal{UserID: "other"}, "query", "", 1); err == nil {
		t.Fatal("chat permission bypass")
	}
}

func TestFeedSourcesAndGeneratedPostsAreGrounded(t *testing.T) {
	sources := feedSources(map[string]any{"results": []any{map[string]any{"url": "https://example.com/article?utm_source=tracker", "title": "Article", "snippet": "Actual excerpt"}, map[string]any{"url": "https://127.0.0.1/private", "title": "Private", "snippet": "private"}, map[string]any{"url": "javascript:alert(1)", "title": "Unsafe", "snippet": "unsafe"}}})
	if len(sources) != 1 || sources[0].URL != "https://example.com/article" {
		t.Fatalf("source validation: %+v", sources)
	}
	posts, err := parseFeedPosts(`{"posts":[{"title":"Summary","body":"Grounded summary","reason":"Interest","sourceIndices":[0]}]}`, sources, time.Now())
	if err != nil || len(posts) != 1 || posts[0].Sources[0].Excerpt != "Actual excerpt" {
		t.Fatal("grounding", err)
	}
	for _, raw := range []string{`{"posts":[{"title":"Summary","body":"bad","sourceIndices":[9]}]}`, `{"posts":[{"title":"Summary","body":"https://invented.example/link","sourceIndices":[0]}]}`} {
		if _, err := parseFeedPosts(raw, sources, time.Now()); err == nil {
			t.Fatal("invented source accepted")
		}
	}
	for _, query := range []string{"password=x", "https://private.local", "192.168.0.1", "a@company.com"} {
		if validFeedQuery(query) {
			t.Fatal("private query", query)
		}
	}
	tool := gateway.ToolCapability{Name: "web.search", MCPAdapterID: "search", RiskLevel: gateway.RiskLevelRead, InputSchema: map[string]any{"properties": map[string]any{"query": map[string]any{"type": "string"}}}}
	if feedQueryField(tool) != "query" {
		t.Fatal("read search excluded")
	}
	tool.RequiresApproval = true
	if feedQueryField(tool) != "" {
		t.Fatal("approval search allowed")
	}
}
