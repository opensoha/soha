package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type blockedVolumeReader struct{}

func (blockedVolumeReader) ListProjectVolumes(context.Context, identity.Principal, string, string) ([]domain.ProjectVolume, error) {
	return nil, nil
}
func (blockedVolumeReader) ListProjectVolumeFiles(context.Context, identity.Principal, string, domain.ProjectVolumeFileListInput) (domain.ProjectVolumeFileList, error) {
	return domain.ProjectVolumeFileList{}, nil
}
func (blockedVolumeReader) ReadProjectVolumeFile(context.Context, identity.Principal, string, domain.ProjectVolumeFileReadInput) (domain.ProjectVolumeFileContent, error) {
	return domain.ProjectVolumeFileContent{}, apperrors.NewBusiness(apperrors.ErrAccessDenied, "content_dlp_blocked", "Content transfer blocked. Event: event-1", "内容传输阻断。事件：event-1")
}

func TestVolumeDLPBlockReturnsNoContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &DockerHandler{projectStorage: blockedVolumeReader{}}
	r := gin.New()
	r.GET("/docker/projects/:id/runtime/volume-file", h.ReadProjectVolumeFile)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/docker/projects/p/runtime/volume-file?target=/data&path=/secret.txt", nil))
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), `"content":`) || !strings.Contains(w.Body.String(), "content_dlp_blocked") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected blocked response: %d %s", w.Code, w.Body.String())
	}
}

func TestDLPRejectsUnknownAndMissingFields(t *testing.T) {
	for _, body := range []string{`{"expectedRevision":0,"draft":{"enabled":true,"mode":"block","detectors":[],"rules":[],"bypass":true}}`, `null`, `{}","extra":1`, `{"expectedRevision":0,"draft":{"mode":"block","detectors":[],"rules":[]}}`} {
		r := gin.New()
		r.PUT("/", func(c *gin.Context) {
			var input struct {
				ExpectedRevision int64 `json:"expectedRevision"`
				Draft            struct {
					Enabled   bool     `json:"enabled"`
					Mode      string   `json:"mode"`
					Detectors []string `json:"detectors"`
					Rules     []any    `json:"rules"`
				} `json:"draft"`
			}
			if decodeDLPInput(c, &input) {
				c.Status(http.StatusNoContent)
			}
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid body accepted: %s", body)
		}
	}
}
