package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	appaudit "github.com/opensoha/soha/internal/application/audit"
	appintegration "github.com/opensoha/soha/internal/application/systemintegration"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	storage "github.com/opensoha/soha/internal/infrastructure/softwareobjectstore"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apphostingrepo "github.com/opensoha/soha/internal/repository/apphosting"
)

type identitySessionReader interface {
	GetAuthSessionByID(ctx context.Context, sessionID string) (domainidentity.Session, error)
}

type appHostingSessionValidator struct {
	sessions identitySessionReader
}

func (v appHostingSessionValidator) ValidateBrowserSession(ctx context.Context, sessionID, expectedUserID string) (appapphosting.BrowserSessionState, error) {
	trimmedSession := strings.TrimSpace(sessionID)
	trimmedUser := strings.TrimSpace(expectedUserID)
	if trimmedSession == "" || trimmedUser == "" || v.sessions == nil {
		return appapphosting.BrowserSessionState{Active: false}, nil
	}
	sess, err := v.sessions.GetAuthSessionByID(ctx, trimmedSession)
	if errors.Is(err, apperrors.ErrNotFound) {
		return appapphosting.BrowserSessionState{Active: false}, nil
	}
	if err != nil {
		return appapphosting.BrowserSessionState{}, err
	}
	now := time.Now().UTC()
	active := sess.Status == "active" &&
		sess.UserID == trimmedUser &&
		sess.ProviderType != "cli_v2" &&
		now.Before(sess.ExpiresAt)
	return appapphosting.BrowserSessionState{
		Active:    active,
		UserID:    sess.UserID,
		ExpiresAt: sess.ExpiresAt.UTC(),
	}, nil
}

func newAppHostingService(
	ctx context.Context,
	coreDB cfgpkg.DatabaseConfig,
	infra *infrastructure,
	sessions identitySessionReader,
	visitorSecret string,
	permissions *appaccess.PermissionResolver,
	connections *appintegration.Service,
	audit *appaudit.Service,
) (*appapphosting.Service, error) {
	db := infra.databaseStore.DB()
	objects := storage.NewProvider(connections)
	dynamic, err := configureHostedDynamic(ctx, coreDB, db, objects)
	if err != nil {
		return nil, fmt.Errorf("configure dynamic app hosting: %w", err)
	}
	opts := []appapphosting.Option{
		appapphosting.WithVisitorIDSecret(visitorSecret),
		appapphosting.WithDynamicBackend(dynamic),
	}
	if dynamic != nil {
		opts = append(opts, appapphosting.WithRuntimeConfigKey(dynamic.dataKey))
	}
	if sessions != nil {
		opts = append(opts, appapphosting.WithSessionValidator(appHostingSessionValidator{sessions: sessions}))
	}
	if fallback := os.Getenv("SOHA_APP_HOSTING_GATEWAY_TOKEN"); fallback != "" {
		opts = append(opts, appapphosting.WithFallbackGatewayToken(fallback))
	}
	svc, err := appapphosting.New(
		apphostingrepo.NewRepository(db),
		objects,
		apphostingrepo.NewAudienceDirectory(db),
		permissions,
		audit,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("build app hosting service: %w", err)
	}
	return svc, nil
}
