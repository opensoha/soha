package handlers

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	middleware "github.com/opensoha/soha/internal/api/middleware"
	response "github.com/opensoha/soha/internal/api/response"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"io"
	"net/http"
	"strconv"
)

type CopilotMessageSearchService interface {
	SearchMessages(context.Context, identity.Principal, string, string, int) (domain.MessageSearchPage, error)
	MessageWindow(context.Context, identity.Principal, string, domain.MessageWindowInput) (domain.MessageWindow, error)
}
type CopilotFeedService interface {
	ListPersonalFeed(context.Context, identity.Principal, string, int) (domain.FeedPage, error)
	UpdatePersonalFeedSettings(context.Context, identity.Principal, domain.FeedSettings) (domain.FeedPage, error)
	RefreshPersonalFeed(context.Context, identity.Principal) (domain.FeedPage, error)
	GetPersonalFeedItem(context.Context, identity.Principal, string) (domain.FeedItem, error)
	UpdatePersonalFeedFeedback(context.Context, identity.Principal, string, *bool, *bool) (domain.FeedItem, error)
}
type copilotDiscoveryHandler struct {
	search CopilotMessageSearchService
	feed   CopilotFeedService
}

func discoveryLimit(c *gin.Context, defaultLimit int) (int, error) {
	if c.Query("limit") == "" {
		return defaultLimit, nil
	}
	value, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		return 0, apperrors.ErrInvalidArgument
	}
	return value, nil
}
func discoveryJSON(c *gin.Context, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return apperrors.ErrInvalidArgument
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func (h *copilotDiscoveryHandler) SearchMessages(c *gin.Context) {
	if h.search == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	limit, err := discoveryLimit(c, 20)
	if err != nil {
		writeError(c, err)
		return
	}
	var page domain.MessageSearchPage
	switch c.Query("workspace") {
	case "", "chat":
		page, err = h.search.SearchMessages(c.Request.Context(), middleware.PrincipalFromContext(c), c.Query("q"), c.Query("cursor"), limit)
	case "heya":
		personal, ok := h.search.(interface {
			SearchPersonalMessages(context.Context, identity.Principal, string, string, int) (domain.MessageSearchPage, error)
		})
		if !ok {
			err = apperrors.ErrServiceUnavailable
		} else {
			page, err = personal.SearchPersonalMessages(c.Request.Context(), middleware.PrincipalFromContext(c), c.Query("q"), c.Query("cursor"), limit)
		}
	default:
		err = apperrors.ErrInvalidArgument
	}
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, page)
}
func (h *copilotDiscoveryHandler) MessageWindow(c *gin.Context) {
	if h.search == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	limit, err := discoveryLimit(c, 40)
	if err != nil {
		writeError(c, err)
		return
	}
	page, err := h.search.MessageWindow(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("sessionID"), domain.MessageWindowInput{Around: c.Query("around"), Before: c.Query("before"), After: c.Query("after"), Limit: limit})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, page)
}
func (h *copilotDiscoveryHandler) ListPersonalFeed(c *gin.Context) {
	if h.feed == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	limit, err := discoveryLimit(c, 20)
	if err != nil {
		writeError(c, err)
		return
	}
	page, err := h.feed.ListPersonalFeed(c.Request.Context(), middleware.PrincipalFromContext(c), c.Query("before"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, page)
}
func (h *copilotDiscoveryHandler) UpdatePersonalFeedSettings(c *gin.Context) {
	if h.feed == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	var req struct {
		Instructions string  `json:"instructions"`
		SearchTool   *string `json:"searchTool"`
		AutoUpdate   *bool   `json:"autoUpdate"`
	}
	if err := discoveryJSON(c, &req); err != nil {
		writeError(c, err)
		return
	}
	if req.SearchTool == nil || req.AutoUpdate == nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	page, err := h.feed.UpdatePersonalFeedSettings(c.Request.Context(), middleware.PrincipalFromContext(c), domain.FeedSettings{Instructions: req.Instructions, SearchTool: *req.SearchTool, AutoUpdate: *req.AutoUpdate})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, page)
}
func (h *copilotDiscoveryHandler) RefreshPersonalFeed(c *gin.Context) {
	if h.feed == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	page, err := h.feed.RefreshPersonalFeed(c.Request.Context(), middleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusAccepted, page)
}
func (h *copilotDiscoveryHandler) GetPersonalFeedItem(c *gin.Context) {
	if h.feed == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	item, err := h.feed.GetPersonalFeedItem(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("itemID"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, item)
}
func (h *copilotDiscoveryHandler) UpdatePersonalFeedFeedback(c *gin.Context) {
	if h.feed == nil {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	var req struct {
		Liked  *bool `json:"liked"`
		Hidden *bool `json:"hidden"`
	}
	if err := discoveryJSON(c, &req); err != nil {
		writeError(c, err)
		return
	}
	item, err := h.feed.UpdatePersonalFeedFeedback(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("itemID"), req.Liked, req.Hidden)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, http.StatusOK, item)
}
