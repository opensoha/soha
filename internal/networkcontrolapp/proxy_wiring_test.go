package networkcontrolapp

import (
	"context"
	"errors"
	audit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

func TestProxyRuntimeCompositionDeniesConsoleWritesAndAudit(t *testing.T) {
	ctx := context.Background()
	p := identity.Principal{}
	deny := proxyRuntimeOnlyConsole{}
	for _, err := range []error{deny.Authorize(ctx, p, "master", "update"), deny.AuthorizeDeployment(ctx, p, "master", true), deny.RecordPublication(ctx, p, "master", "bundle", "activate", 1), deny.Record(ctx, audit.Entry{}), (proxyRuntimeDesiredReader{}).CommitDesiredBatch(ctx, p, proxy.DesiredBatch{})} {
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("console action error=%v", err)
		}
	}
}
