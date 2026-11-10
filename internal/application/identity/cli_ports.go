package identity

import (
	"context"
	"fmt"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmfa "github.com/opensoha/soha/internal/domain/identitymfa"
)

type CLIStateStore interface {
	WithinCLITransaction(context.Context, func(context.Context) error) error
	CreateCLIGrant(context.Context, domainidentity.CLIGrant, int, int) (bool, error)
	GetCLIGrant(context.Context, string, string) (domainidentity.CLIGrant, error)
	SaveCLIGrant(context.Context, domainidentity.CLIGrant) error
	GetCLIRefresh(context.Context, string) (domainidentity.CLIRefreshCredential, error)
	SaveCLIRefresh(context.Context, domainidentity.CLIRefreshCredential) error
	ConsumeCLIBudget(context.Context, string, int, time.Duration) (bool, error)
}

type CLISessionStore interface {
	GetCLIBrowserSession(context.Context, string) (domainidentity.Session, error)
	UpdateCLISession(context.Context, string, time.Time, time.Time, int64) error
	ListCLISessionRecords(context.Context, string, string, int) ([]domainidentity.SessionRecord, error)
	CleanupCLIAuth(context.Context) error
}

type CLIMFAStore interface {
	ListCredentials(context.Context, string) ([]domainmfa.Credential, error)
	HasRecentStepUp(context.Context, string, string, time.Time) (bool, error)
}

func validateCLIDependencies(deps Dependencies) error {
	if !deps.Config.CLI.Enabled {
		return nil
	}
	if err := deps.Config.CLI.Validate(); err != nil {
		return err
	}
	for _, dep := range []struct {
		name  string
		value any
	}{{"CLI state", deps.CLIState}, {"CLI sessions", deps.CLISessions}, {"CLI MFA", deps.CLIMFA}, {"CLI permissions", deps.Permissions}} {
		if isNilDependency(dep.value) {
			return fmt.Errorf("identity service: %s dependency is required", dep.name)
		}
	}
	if deps.CLIReceiptKeys.Active().ID() == "" {
		return fmt.Errorf("identity service: persistent CLI receipt encryption keys are required")
	}
	return nil
}
