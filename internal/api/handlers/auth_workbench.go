package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"golang.org/x/sync/errgroup"
)

type applicationWorkbenchApplicationReader interface {
	Get(context.Context, domainidentity.Principal, string) (domainapp.App, error)
	ListServiceIdentities(context.Context, domainidentity.Principal, string) ([]domainapp.ServiceIdentity, error)
}

type applicationWorkbenchEnvironmentReader interface {
	ListApplicationEnvironmentNavigation(context.Context, domainidentity.Principal, string) ([]domaindelivery.ApplicationEnvironmentNavigation, error)
}

type AuthApplicationWorkbenchServices struct {
	Tokens       apiMiddleware.AccessTokenParser
	Applications applicationWorkbenchApplicationReader
	Environments applicationWorkbenchEnvironmentReader
	Modules      apiMiddleware.ModuleState
}

func (h *AuthHandler) WithApplicationWorkbench(services AuthApplicationWorkbenchServices) *AuthHandler {
	h.workbench = services
	return h
}

type applicationWorkbenchRefreshResponse struct {
	Session            domainidentity.AuthResult        `json:"session"`
	PermissionSnapshot *domainaccess.PermissionSnapshot `json:"permissionSnapshot,omitempty"`
	Entry              *applicationWorkbenchEntry       `json:"entry,omitempty"`
}

type applicationWorkbenchEntry struct {
	Application  domainapp.App                                     `json:"application"`
	Environments []domaindelivery.ApplicationEnvironmentNavigation `json:"environments"`
	Services     []sohaapi.ApplicationServiceDirectoryItem         `json:"services"`
}

func (h *AuthHandler) RefreshApplicationWorkbench(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !applicationWorkbenchOriginAllowed(c) {
		apiresponse.Error(c, http.StatusForbidden, "access_denied", "browser request origin is not allowed")
		return
	}
	applicationID, ok := readApplicationWorkbenchRefreshRequest(c)
	if !ok {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application workbench refresh payload")
		return
	}
	session, ok := h.refreshSession(c, refreshTokenFromRequest(c, ""))
	if !ok {
		return
	}
	// Refresh owns session success. Optional reads have their own budget so they
	// can never turn a successful cookie refresh into an application error.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 750*time.Millisecond)
	defer cancel()
	response := applicationWorkbenchRefreshResponse{Session: session}
	h.readApplicationWorkbenchStartup(ctx, applicationID, &response)
	apiresponse.Item(c, http.StatusOK, response)
}

func applicationWorkbenchOriginAllowed(c *gin.Context) bool {
	origins := c.Request.Header.Values("Origin")
	if len(origins) != 1 || strings.EqualFold(c.GetHeader("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	origin := origins[0]
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	expected, err := apiMiddleware.RequestOrigin(c)
	return err == nil && (origin == expected || origin == c.Writer.Header().Get("Access-Control-Allow-Origin"))
}

func readApplicationWorkbenchRefreshRequest(c *gin.Context) (string, bool) {
	if c.ContentType() != "application/json" {
		return "", false
	}
	var req struct {
		ApplicationID string `json:"applicationId"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 2<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return "", false
	}
	id := strings.TrimSpace(req.ApplicationID)
	return id, id != "" && utf8.RuneCountInString(req.ApplicationID) <= 200 && errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func (h *AuthHandler) readApplicationWorkbenchStartup(ctx context.Context, applicationID string, response *applicationWorkbenchRefreshResponse) {
	if h.workbench.Tokens == nil || h.access == nil {
		return
	}
	principal, auth, err := h.workbench.Tokens.ParseAccessToken(ctx, response.Session.Tokens.AccessToken)
	if err != nil || auth.TokenKind != "session_access" || auth.SessionID == "" ||
		principal.UserID == "" || principal.UserID != response.Session.User.UserID {
		return
	}
	snapshot, err := h.access.PermissionSnapshot(ctx, principal)
	if err != nil || ctx.Err() != nil {
		return
	}
	response.PermissionSnapshot = &snapshot
	if !slices.Contains(snapshot.PermissionKeys, appaccess.PermWorkbenchDeliveryView) ||
		!slices.Contains(snapshot.PermissionKeys, appaccess.PermDeliveryApplicationsView) {
		return
	}
	response.Entry = h.workbench.readEntry(ctx, principal, applicationID)
}

func (s AuthApplicationWorkbenchServices) readEntry(ctx context.Context, principal domainidentity.Principal, applicationID string) *applicationWorkbenchEntry {
	if s.Applications == nil || s.Environments == nil || s.Modules == nil || !s.Modules.ModuleEnabled("delivery") {
		return nil
	}
	app, err := s.Applications.Get(ctx, principal, applicationID)
	if err != nil || app.ID != applicationID {
		return nil
	}
	entry := &applicationWorkbenchEntry{Application: app}
	readers, readCtx := errgroup.WithContext(ctx)
	readers.Go(func() error {
		environments, err := s.Environments.ListApplicationEnvironmentNavigation(readCtx, principal, app.ID)
		entry.Environments = append([]domaindelivery.ApplicationEnvironmentNavigation{}, environments...)
		return err
	})
	readers.Go(func() error {
		services, err := s.Applications.ListServiceIdentities(readCtx, principal, app.ID)
		entry.Services = applicationServiceDirectory(services)
		return err
	})
	if err := readers.Wait(); err != nil || ctx.Err() != nil {
		return nil
	}
	return entry
}
