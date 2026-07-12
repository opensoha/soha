package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainannouncement "github.com/opensoha/soha/internal/domain/announcement"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type AnnouncementReader interface {
	List(context.Context, domainidentity.Principal, int) ([]domainannouncement.Record, error)
	Get(context.Context, domainidentity.Principal, string) (domainannouncement.Record, error)
	Inbox(context.Context, domainidentity.Principal, int) (domainannouncement.Inbox, error)
	MarkRead(context.Context, domainidentity.Principal, string) error
}

type AnnouncementWriter interface {
	Create(context.Context, domainidentity.Principal, domainannouncement.Input) (domainannouncement.Record, error)
	Update(context.Context, domainidentity.Principal, string, domainannouncement.Input) (domainannouncement.Record, error)
	Publish(context.Context, domainidentity.Principal, string) (domainannouncement.Record, error)
	Withdraw(context.Context, domainidentity.Principal, string) (domainannouncement.Record, error)
	Delete(context.Context, domainidentity.Principal, string) error
}

type AnnouncementService interface {
	AnnouncementReader
	AnnouncementWriter
}

type AnnouncementHandler struct {
	reader AnnouncementReader
	writer AnnouncementWriter
}

func NewAnnouncementHandler(service AnnouncementService) *AnnouncementHandler {
	return NewAnnouncementHandlerWithServices(service, service)
}

func NewAnnouncementHandlerWithServices(reader AnnouncementReader, writer AnnouncementWriter) *AnnouncementHandler {
	return &AnnouncementHandler{reader: reader, writer: writer}
}

func (h *AnnouncementHandler) List(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.reader.List(c.Request.Context(), principal, parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AnnouncementHandler) Get(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.reader.Get(c.Request.Context(), principal, c.Param("announcementID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AnnouncementHandler) Inbox(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.reader.Inbox(c.Request.Context(), principal, parseLimit(c.Query("limit"), 10))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AnnouncementHandler) MarkRead(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.reader.MarkRead(c.Request.Context(), principal, c.Param("announcementID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AnnouncementHandler) Create(c *gin.Context) {
	var req dto.UpsertAnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid announcement payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.writer.Create(c.Request.Context(), principal, announcementInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AnnouncementHandler) Update(c *gin.Context) {
	var req dto.UpsertAnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid announcement payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.writer.Update(c.Request.Context(), principal, c.Param("announcementID"), announcementInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func announcementInput(req dto.UpsertAnnouncementRequest) domainannouncement.Input {
	return domainannouncement.Input{
		ID:       req.ID,
		Title:    req.Title,
		Summary:  req.Summary,
		Content:  req.Content,
		Level:    req.Level,
		Status:   req.Status,
		Audience: req.Audience,
		Sticky:   req.Sticky,
		StartsAt: req.StartsAt,
		EndsAt:   req.EndsAt,
	}
}

func (h *AnnouncementHandler) Publish(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.writer.Publish(c.Request.Context(), principal, c.Param("announcementID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AnnouncementHandler) Withdraw(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.writer.Withdraw(c.Request.Context(), principal, c.Param("announcementID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AnnouncementHandler) Delete(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.writer.Delete(c.Request.Context(), principal, c.Param("announcementID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
