package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/middleware"
	"github.com/opensoha/soha/internal/api/response"
	dlp "github.com/opensoha/soha/internal/domain/contentdlp"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
)

type ManagedContentDestinationService interface {
	ReviewDestinations(context.Context, identity.Principal) ([]d.Destination, error)
	PreviewDestination(context.Context, identity.Principal, string, dlp.ShadowInput) (dlp.ShadowResult, error)
	Destinations(context.Context, identity.Principal, bool) ([]d.Destination, error)
	GetDestination(context.Context, identity.Principal, string) (d.Destination, error)
	SaveDestination(context.Context, identity.Principal, string, d.SaveInput) (d.Destination, error)
	PublishDestination(context.Context, identity.Principal, string, d.PublishInput) (d.Destination, error)
}
type ManagedContentDestinationsHandler struct {
	service ManagedContentDestinationService
}

func NewManagedContentDestinationsHandler(s ManagedContentDestinationService) *ManagedContentDestinationsHandler {
	return &ManagedContentDestinationsHandler{s}
}
func managedHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
}
func managedInput(c *gin.Context, out any) bool {
	managedHeaders(c)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Error(c, 413, "content_too_large", "Managed content request exceeds the bounded transfer limit.")
		} else {
			response.Error(c, 400, "invalid_argument", "Invalid managed content request.")
		}
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		response.Error(c, 400, "invalid_argument", "Exactly one JSON content request is required.")
		return false
	}
	return true
}
func (h *ManagedContentDestinationsHandler) List(c *gin.Context) {
	managedHeaders(c)
	items, err := h.service.Destinations(c.Request.Context(), middleware.PrincipalFromContext(c), false)
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]d.Summary, 0, len(items))
	for _, item := range items {
		if item.Published == nil {
			continue
		}
		result = append(result, d.Summary{ID: item.ID, Name: item.Published.Name, Kind: item.Published.Kind, PublishedVersion: item.PublishedVersion, Mode: item.Published.Policy.Mode})
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentDestinationsHandler) AdminList(c *gin.Context) {
	managedHeaders(c)
	result, err := h.service.Destinations(c.Request.Context(), middleware.PrincipalFromContext(c), true)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentDestinationsHandler) Get(c *gin.Context) {
	managedHeaders(c)
	result, err := h.service.GetDestination(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("destinationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentDestinationsHandler) Save(c *gin.Context) {
	var in d.SaveInput
	if !managedInput(c, &in) {
		return
	}
	result, err := h.service.SaveDestination(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("destinationID"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentDestinationsHandler) Publish(c *gin.Context) {
	var in d.PublishInput
	if !managedInput(c, &in) {
		return
	}
	result, err := h.service.PublishDestination(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("destinationID"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}

type ManagedContentTransferService interface {
	Upload(context.Context, identity.Principal, d.UploadInput) (d.Result, error)
	Paste(context.Context, identity.Principal, d.PasteInput) (d.Result, error)
	Read(context.Context, identity.Principal, string, d.Request) (d.Result, error)
	Copy(context.Context, identity.Principal, string, d.CopyInput) (d.Result, error)
	Remove(context.Context, identity.Principal, string, d.Request) (d.Result, error)
	OwnRecord(context.Context, identity.Principal, string) (d.Record, error)
	Records(context.Context, identity.Principal, string, string, int, int) (d.Page, error)
}
type ManagedContentTransfersHandler struct{ service ManagedContentTransferService }

func NewManagedContentTransfersHandler(s ManagedContentTransferService) *ManagedContentTransfersHandler {
	return &ManagedContentTransfersHandler{s}
}
func (h *ManagedContentTransfersHandler) Upload(c *gin.Context) {
	var in d.UploadInput
	if !managedInput(c, &in) {
		return
	}
	defer clear(in.Content)
	in.SessionID = middleware.AccessContextFromContext(c).SessionID
	result, err := h.service.Upload(c.Request.Context(), middleware.PrincipalFromContext(c), in)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentTransfersHandler) Paste(c *gin.Context) {
	var in struct {
		RequestID     string           `json:"requestId"`
		DestinationID string           `json:"destinationId"`
		FileName      string           `json:"fileName"`
		Authorization *d.Authorization `json:"authorization,omitempty"`
		Text          *string          `json:"text"`
	}
	if !managedInput(c, &in) {
		return
	}
	if in.Text == nil {
		response.Error(c, 400, "invalid_argument", "Explicit paste text is required.")
		return
	}
	result, err := h.service.Paste(c.Request.Context(), middleware.PrincipalFromContext(c), d.PasteInput{SessionID: middleware.AccessContextFromContext(c).SessionID, RequestID: in.RequestID, DestinationID: in.DestinationID, FileName: in.FileName, Authorization: in.Authorization, Text: *in.Text})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentTransfersHandler) Read(c *gin.Context) {
	var in d.Request
	if !managedInput(c, &in) {
		return
	}
	in.SessionID = middleware.AccessContextFromContext(c).SessionID
	result, err := h.service.Read(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("fileID"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	if result.Content != nil {
		defer clear(*result.Content)
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentTransfersHandler) Copy(c *gin.Context) {
	var in d.CopyInput
	if !managedInput(c, &in) {
		return
	}
	in.SessionID = middleware.AccessContextFromContext(c).SessionID
	result, err := h.service.Copy(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("fileID"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentTransfersHandler) Remove(c *gin.Context) {
	var in d.Request
	if !managedInput(c, &in) {
		return
	}
	result, err := h.service.Remove(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("fileID"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentTransfersHandler) Get(c *gin.Context) {
	managedHeaders(c)
	result, err := h.service.OwnRecord(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("recordID"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentTransfersHandler) list(c *gin.Context, kind string) {
	managedHeaders(c)
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil {
		response.Error(c, 400, "invalid_argument", "Invalid offset.")
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "25"))
	if err != nil {
		response.Error(c, 400, "invalid_argument", "Invalid limit.")
		return
	}
	result, err := h.service.Records(c.Request.Context(), middleware.PrincipalFromContext(c), kind, c.Param("destinationID"), offset, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}
func (h *ManagedContentTransfersHandler) Files(c *gin.Context)   { h.list(c, "files") }
func (h *ManagedContentTransfersHandler) Records(c *gin.Context) { h.list(c, "records") }
func (h *ManagedContentTransfersHandler) Cases(c *gin.Context)   { h.list(c, "cases") }

func (h *ManagedContentDestinationsHandler) Preview(c *gin.Context) {
	var in dlp.ShadowInput
	defer func() {
		for _, sample := range in.Samples {
			clear(sample)
		}
	}()
	if !managedInput(c, &in) {
		return
	}
	result, err := h.service.PreviewDestination(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("destinationID"), in)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Item(c, 200, result)
}

func (h *ManagedContentDestinationsHandler) ReviewList(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	items, err := h.service.ReviewDestinations(c.Request.Context(), middleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]d.Summary, 0, len(items))
	for _, item := range items {
		cfg := item.Draft
		if item.Published != nil {
			cfg = *item.Published
		}
		result = append(result, d.Summary{ID: item.ID, Name: cfg.Name, Kind: cfg.Kind, PublishedVersion: item.PublishedVersion, Mode: cfg.Policy.Mode})
	}
	response.Item(c, 200, result)
}
