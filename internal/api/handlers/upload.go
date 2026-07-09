package handlers

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaccess "github.com/opensoha/soha/internal/application/access"
)

const (
	brandingMaxFileSize = 2 << 20 // 2MB
)

var (
	allowedExtensions = map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".ico":  "image/x-icon",
		".webp": "image/webp",
	}
	alternateContentTypes = map[string]map[string]bool{
		".ico": {
			"image/vnd.microsoft.icon": true,
			"application/octet-stream": true,
		},
		".webp": {
			"application/octet-stream": true,
		},
	}
	contentSniffSize = 512
)

type readSeeker interface {
	io.Reader
	io.Seeker
}

// UploadBrandingAsset handles branding image upload and returns a data URL for settings storage.
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
	expectedContentType, ok := allowedExtensions[ext]
	if !ok {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "unsupported file type; allowed: jpg, png, ico, webp")
		return
	}

	readSeekFile, ok := file.(readSeeker)
	if !ok {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "uploaded file is not readable")
		return
	}
	contentType, err := detectUploadContentType(readSeekFile)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid branding asset content")
		return
	}
	if !brandingContentTypeAllowed(ext, expectedContentType, contentType) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "file content does not match allowed image types")
		return
	}

	content, err := io.ReadAll(io.LimitReader(file, brandingMaxFileSize+1))
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid branding asset content")
		return
	}
	if len(content) > brandingMaxFileSize {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "file size exceeds 2MB limit")
		return
	}

	apiresponse.Item(c, http.StatusOK, map[string]string{
		"url": "data:" + expectedContentType + ";base64," + base64.StdEncoding.EncodeToString(content),
	})
}

func detectUploadContentType(file readSeeker) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	buffer := make([]byte, contentSniffSize)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if n == 0 {
		return "", fmt.Errorf("empty upload")
	}
	return http.DetectContentType(buffer[:n]), nil
}

func brandingContentTypeAllowed(ext string, expected string, actual string) bool {
	actual = strings.TrimSpace(strings.ToLower(actual))
	expected = strings.TrimSpace(strings.ToLower(expected))
	if actual == expected {
		return true
	}
	return alternateContentTypes[ext][actual]
}
