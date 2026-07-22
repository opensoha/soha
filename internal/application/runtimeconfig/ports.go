package runtimeconfig

import (
	"context"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainruntimeconfig "github.com/opensoha/soha/internal/domain/runtimeconfig"
)

type Store interface {
	LoadState(context.Context) (domainruntimeconfig.State, error)
	Commit(context.Context, domainruntimeconfig.Commit) (domainruntimeconfig.State, error)
	UpdateApplication(context.Context, domainruntimeconfig.Application) error
	ListRevisions(context.Context, int) ([]domainruntimeconfig.Revision, error)
	GetRevisionByVersion(context.Context, int64) (domainruntimeconfig.Revision, error)
	GetApplication(context.Context, string) (domainruntimeconfig.Application, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Applier interface {
	Handles(string) bool
	Apply(context.Context, Snapshot, Snapshot, []string) ([]sohaapi.RuntimeConfigAppliedItem, error)
}

type Reader interface {
	Current() Snapshot
	ModuleEnabled(string) bool
	FeatureEnabled(string, string) bool
}
