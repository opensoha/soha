package directorysync

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type originalSyncService interface {
	SyncOriginal(context.Context, string, string, string, string, appdirectorysync.ConnectorFactory) (domain.Run, error)
	GetOriginalSync(context.Context, string, string, string, string) (domain.Run, error)
}

func (h *Handler) cancelOriginalSync(c *gin.Context, id string) {
	actor, credential, err := originalSyncActor(c)
	if err != nil {
		writeError(c, err)
		return
	}
	service, ok := h.service.(interface {
		RequestOriginalCancellation(context.Context, string, string, string) (domain.Run, error)
	})
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	run, err := service.RequestOriginalCancellation(c.Request.Context(), id, actor, credential)
	if err != nil {
		writeError(c, normalizeError(err))
		return
	}
	h.record(c, apiMiddleware.PrincipalFromContext(c), "access.directory.sync.cancel", "DirectorySyncRun", run.ID, "requested original synchronization cancellation; running work retains its active slot until stopped")
	apiresponse.Item(c, http.StatusOK, runView(run))
}

func originalSyncActor(c *gin.Context) (string, string, error) {
	principal := apiMiddleware.PrincipalFromContext(c)
	credential := apiMiddleware.AccessContextFromContext(c).SessionID
	if credential != "" {
		credential = "session:" + credential
	} else if principal.AccessTokenID != "" {
		credential = "token:" + principal.AccessTokenID
	}
	if principal.UserID == "" || credential == "" {
		return "", "", apperrors.ErrAccessDenied
	}
	return principal.UserID, credential, nil
}

func (h *Handler) CreateSyncRequest(c *gin.Context) {
	actor, credential, err := originalSyncActor(c)
	if err != nil {
		writeError(c, err)
		return
	}
	var input sohaapi.DirectorySyncRequestInput
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	service, ok := h.service.(originalSyncService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	run, err := service.SyncOriginal(c.Request.Context(), c.Param("connectionID"), actor, credential, input.IdempotencyKey.String(), h.connectors)
	if err != nil {
		writeError(c, normalizeError(err))
		return
	}
	h.record(c, apiMiddleware.PrincipalFromContext(c), "access.directory.sync", "DirectorySyncRun", run.ID, "observed original manual directory synchronization request")
	apiresponse.Item(c, http.StatusAccepted, gin.H{"protocol": "directory-sync-request-v1", "idempotencyKey": input.IdempotencyKey, "run": runView(run)})
}

func (h *Handler) GetSyncRequest(c *gin.Context) {
	actor, credential, err := originalSyncActor(c)
	if err != nil {
		writeError(c, err)
		return
	}
	service, ok := h.service.(originalSyncService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	run, err := service.GetOriginalSync(c.Request.Context(), c.Param("connectionID"), actor, credential, c.Param("idempotencyKey"))
	if err != nil {
		writeError(c, normalizeError(err))
		return
	}
	apiresponse.Item(c, http.StatusOK, gin.H{"protocol": "directory-sync-request-v1", "idempotencyKey": c.Param("idempotencyKey"), "run": runView(run)})
}
