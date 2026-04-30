package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
)

const (
	brandingUploadDir   = "data/branding"
	brandingMaxFileSize = 2 << 20 // 2MB
	brandingURLPathBase = "/branding-assets/"
)

var allowedExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".svg":  true,
	".ico":  true,
	".webp": true,
}

// UploadBrandingAsset handles branding image upload, saves to disk and returns the served URL.
func (h *SettingsHandler) UploadBrandingAsset(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := appaccess.AuthorizeRuntimePermission(c.Request.Context(), h.permissions, principal, appaccess.PermSettingsBrandingManage); err != nil {
		apiresponse.Error(c, http.StatusForbidden, "access_denied", "missing branding manage permission")
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "missing file field")
		return
	}
	defer file.Close()

	if header.Size > brandingMaxFileSize {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "file size exceeds 2MB limit")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !allowedExtensions[ext] {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "unsupported file type; allowed: jpg, png, svg, ico, webp")
		return
	}

	if err := os.MkdirAll(brandingUploadDir, 0o755); err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "internal", "failed to prepare upload directory")
		return
	}

	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	savePath := filepath.Join(brandingUploadDir, filename)

	if err := c.SaveUploadedFile(header, savePath); err != nil {
		apiresponse.Error(c, http.StatusInternalServerError, "internal", "failed to save uploaded file")
		return
	}

	// Return the URL path that will be served by the static file server
	urlPath := brandingURLPathBase + filename
	apiresponse.Item(c, http.StatusOK, map[string]string{
		"url":      urlPath,
		"filename": filename,
	})
}
