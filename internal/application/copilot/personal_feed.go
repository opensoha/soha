package copilot

import (
	"context"
	"fmt"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	gateway "github.com/opensoha/soha/internal/domain/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
	"strings"
	"time"
	"unicode/utf8"
)

type FeedStore interface {
	ReadFeed(context.Context, string, string, int) (domain.FeedRecord, []domain.FeedItem, string, error)
	SaveFeedSettings(context.Context, string, domain.FeedSettings, time.Time) error
	GetFeedItem(context.Context, string, string) (domain.FeedItem, error)
	SetFeedFeedback(context.Context, string, string, *bool, *bool) (domain.FeedItem, error)
	QueueFeedRefresh(context.Context, string, time.Time) error
	ClaimFeedRefresh(context.Context, time.Time) (domain.FeedRecord, bool, error)
	FinishFeedRefresh(context.Context, domain.FeedRecord, []domain.FeedItem, string, time.Time) error
}

type feedToolInvoker interface {
	WorkbenchCapabilities(context.Context, identity.Principal, gateway.ManifestRequest) (gateway.Manifest, error)
	InvokeTool(context.Context, identity.Principal, gateway.ToolInvocationRequest) (gateway.ToolInvocationResult, error)
}

func feedQueryField(tool gateway.ToolCapability) string {
	name := strings.ToLower(tool.Name + " " + tool.Action)
	if !strings.Contains(name, "search") || strings.HasPrefix(tool.Name, "knowledge.") || strings.HasPrefix(tool.Name, "memory.") || tool.RiskLevel != gateway.RiskLevelRead || tool.RequiresApproval || len(tool.Effects) > 0 || tool.Execution != nil && tool.Execution.Mode != "sync" {
		return ""
	}
	properties, _ := tool.InputSchema["properties"].(map[string]any)
	field := ""
	for _, key := range []string{"query", "q"} {
		property, _ := properties[key].(map[string]any)
		if property["type"] == "string" {
			field = key
			break
		}
	}
	if field == "" {
		return ""
	}
	required, _ := tool.InputSchema["required"].([]any)
	for _, value := range required {
		key, _ := value.(string)
		if key != field && key != "limit" && key != "count" && key != "maxResults" {
			return ""
		}
	}
	return field
}

func (s *Service) feedTools(ctx context.Context, principal identity.Principal) ([]gateway.ToolCapability, error) {
	invoker, ok := s.workbenchInvoker.(feedToolInvoker)
	if !ok {
		return []gateway.ToolCapability{}, nil
	}
	tools := []gateway.ToolCapability{}
	cursor := ""
	for page := 0; page < 10; page++ {
		manifest, err := invoker.WorkbenchCapabilities(ctx, principal, gateway.ManifestRequest{Query: "search", Limit: 100, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		for _, tool := range manifest.Tools {
			if feedQueryField(tool) != "" {
				tools = append(tools, tool)
			}
		}
		if manifest.NextCursor == "" {
			break
		}
		cursor = manifest.NextCursor
	}
	return tools, nil
}

func (s *Service) requireFeed(ctx context.Context, p identity.Principal) error {
	if err := s.requirePersonalAgent(ctx, p); err != nil {
		return err
	}
	if s.feed == nil {
		return apperrors.ErrUnsupportedOperation
	}
	return nil
}

func (s *Service) ListPersonalFeed(ctx context.Context, p identity.Principal, before string, limit int) (domain.FeedPage, error) {
	page := domain.FeedPage{Items: []domain.FeedItem{}, SearchTools: []sohaapi.PersonalFeedSearchTool{}}
	if err := s.requireFeed(ctx, p); err != nil {
		return page, err
	}
	if len(before) > 128 || limit < 1 || limit > 50 {
		return page, apperrors.ErrInvalidArgument
	}
	record, items, next, err := s.feed.ReadFeed(ctx, p.UserID, before, limit)
	if err != nil {
		return page, err
	}
	page.Items = items
	page.Settings = record.Settings
	page.State = sohaapi.PersonalFeedPageState(record.State)
	page.Error = record.Error
	page.LastUpdatedAt = record.LastUpdatedAt
	page.NextCursor = next
	tools, err := s.feedTools(ctx, p)
	if err != nil {
		page.Error = "搜索工具暂不可用"
		return page, nil
	}
	for _, tool := range tools {
		title := tool.Title
		if title == "" {
			title = tool.Name
		}
		page.SearchTools = append(page.SearchTools, sohaapi.PersonalFeedSearchTool{Name: tool.Name, Title: title})
	}
	return page, nil
}

func (s *Service) UpdatePersonalFeedSettings(ctx context.Context, p identity.Principal, settings domain.FeedSettings) (domain.FeedPage, error) {
	if err := s.requireFeed(ctx, p); err != nil {
		return domain.FeedPage{}, err
	}
	settings.Instructions = strings.TrimSpace(settings.Instructions)
	if !utf8.ValidString(settings.Instructions) || settings.Instructions == "" || utf8.RuneCountInString(settings.Instructions) > 2000 || len(settings.SearchTool) > 256 {
		return domain.FeedPage{}, apperrors.ErrInvalidArgument
	}
	if settings.SearchTool != "" || settings.AutoUpdate {
		tools, err := s.feedTools(ctx, p)
		if err != nil {
			return domain.FeedPage{}, err
		}
		found := false
		for _, tool := range tools {
			found = found || tool.Name == settings.SearchTool
		}
		if !found {
			return domain.FeedPage{}, fmt.Errorf("%w: select an available read-only web search tool", apperrors.ErrInvalidArgument)
		}
	}
	if err := s.feed.SaveFeedSettings(ctx, p.UserID, settings, time.Now().UTC()); err != nil {
		return domain.FeedPage{}, err
	}
	return s.ListPersonalFeed(ctx, p, "", 20)
}

func (s *Service) RefreshPersonalFeed(ctx context.Context, p identity.Principal) (domain.FeedPage, error) {
	page, err := s.ListPersonalFeed(ctx, p, "", 20)
	if err != nil {
		return page, err
	}
	found := false
	for _, tool := range page.SearchTools {
		found = found || tool.Name == page.Settings.SearchTool
	}
	if !found {
		return page, fmt.Errorf("%w: configure a read-only web search tool first", apperrors.ErrInvalidArgument)
	}
	if err := s.feed.QueueFeedRefresh(ctx, p.UserID, time.Now().UTC()); err != nil {
		return page, err
	}
	return s.ListPersonalFeed(ctx, p, "", 20)
}

func (s *Service) GetPersonalFeedItem(ctx context.Context, p identity.Principal, id string) (domain.FeedItem, error) {
	if err := s.requireFeed(ctx, p); err != nil {
		return domain.FeedItem{}, err
	}
	if id == "" || len(id) > 128 {
		return domain.FeedItem{}, apperrors.ErrInvalidArgument
	}
	return s.feed.GetFeedItem(ctx, p.UserID, id)
}

func (s *Service) UpdatePersonalFeedFeedback(ctx context.Context, p identity.Principal, id string, liked, hidden *bool) (domain.FeedItem, error) {
	if err := s.requireFeed(ctx, p); err != nil {
		return domain.FeedItem{}, err
	}
	if id == "" || len(id) > 128 || liked == nil && hidden == nil {
		return domain.FeedItem{}, apperrors.ErrInvalidArgument
	}
	return s.feed.SetFeedFeedback(ctx, p.UserID, id, liked, hidden)
}

// The worker shares the AI module lifecycle; reads never enqueue paid work.
func (s *Service) runFeedWorker(ctx context.Context) {
	if s.feed == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runFeedJob(ctx)
		}
	}
}

func (s *Service) runFeedJob(ctx context.Context) {
	if !s.nativeReady.Load() {
		return
	}
	record, ok, err := s.feed.ClaimFeedRefresh(ctx, time.Now().UTC())
	if err != nil || !ok {
		return
	}
	jobCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	items, err := s.generateFeed(jobCtx, record)
	failure := ""
	if err != nil {
		items = nil
		failure = "更新未完成，请检查模型和搜索工具后重试"
		if s.logger != nil {
			s.logger.Warn("personal feed refresh failed", zap.String("job_id", record.JobID))
		}
	}
	// A short independent finalization persists terminal state even when the model deadline expired.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	if err := s.feed.FinishFeedRefresh(finishCtx, record, items, failure, time.Now().UTC()); err != nil && s.logger != nil {
		s.logger.Warn("personal feed finalization failed", zap.String("job_id", record.JobID))
	}
}
