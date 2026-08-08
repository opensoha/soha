package handlers

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appsoftware "github.com/opensoha/soha/internal/application/software"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type SoftwarePackageService interface {
	List(context.Context, domainidentity.Principal, appsoftware.Filter) ([]appsoftware.Package, string, error)
	Storage(context.Context, domainidentity.Principal, string, int) (appsoftware.Storage, error)
	Upload(context.Context, domainidentity.Principal, appsoftware.UploadInput, io.Reader) (appsoftware.Package, error)
	ImportURL(context.Context, domainidentity.Principal, appsoftware.URLImportInput) (appsoftware.Package, error)
	Open(context.Context, domainidentity.Principal, string) (appsoftware.Package, io.ReadCloser, error)
	Delete(context.Context, domainidentity.Principal, string) error
}

type SoftwareHandler struct{ service SoftwarePackageService }

type softwarePackageURLImportRequest struct {
	SoftwareID  string `json:"softwareId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Publisher   string `json:"publisher"`
	Category    string `json:"category"`
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	URL         string `json:"url"`
	FileName    string `json:"fileName"`
}

func NewSoftwareHandler(service SoftwarePackageService) *SoftwareHandler {
	return &SoftwareHandler{service: service}
}

func (h *SoftwareHandler) List(c *gin.Context) {
	limit, ok := softwareLimit(c)
	if !ok {
		return
	}
	items, next, err := h.service.List(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), appsoftware.Filter{
		Platform: c.Query("platform"), Arch: c.Query("arch"), Cursor: c.Query("cursor"), Limit: limit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"items": items, "nextCursor": next})
}

func (h *SoftwareHandler) Storage(c *gin.Context) {
	limit, ok := softwareLimit(c)
	if !ok {
		return
	}
	storage, err := h.service.Storage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("cursor"), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, storage)
}

func (h *SoftwareHandler) Upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, appsoftware.MaxPackageBytes+(1<<20))
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apiresponse.Error(c, http.StatusRequestEntityTooLarge, "payload_too_large", "software package exceeds the upload limit")
			return
		}
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid software package upload")
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	header, err := c.FormFile("file")
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "installer file is required")
		return
	}
	file, err := header.Open()
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "cannot read installer file")
		return
	}
	defer file.Close()
	item, err := h.service.Upload(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), appsoftware.UploadInput{
		SoftwareID: c.PostForm("softwareId"), Name: c.PostForm("name"), Description: c.PostForm("description"),
		Publisher: c.PostForm("publisher"), Category: c.PostForm("category"), Version: c.PostForm("version"),
		Platform: c.PostForm("platform"), Arch: c.PostForm("arch"), FileName: header.Filename,
	}, file)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *SoftwareHandler) ImportURL(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var request softwarePackageURLImportRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid software package URL import")
		return
	}
	item, err := h.service.ImportURL(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), appsoftware.URLImportInput{
		UploadInput: appsoftware.UploadInput{
			SoftwareID: request.SoftwareID, Name: request.Name, Description: request.Description,
			Publisher: request.Publisher, Category: request.Category, Version: request.Version,
			Platform: request.Platform, Arch: request.Arch, FileName: request.FileName,
		},
		URL: request.URL,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *SoftwareHandler) Download(c *gin.Context) {
	item, reader, err := h.service.Open(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("packageID"))
	if err != nil {
		writeError(c, err)
		return
	}
	defer reader.Close()
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.FileName}))
	c.Header("Content-Length", strconv.FormatInt(item.SizeBytes, 10))
	c.Header("Content-Type", "application/octet-stream")
	c.Header("ETag", `"sha256-`+item.SHA256+`"`)
	c.Header("X-Checksum-SHA256", item.SHA256)
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, reader); err != nil {
		_ = c.Error(err)
	}
}

func (h *SoftwareHandler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("packageID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func softwareLimit(c *gin.Context) (int, bool) {
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid software package limit")
			return 0, false
		}
		return parsed, true
	}
	return 0, true
}
