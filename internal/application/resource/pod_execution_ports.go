package resource

import (
	"context"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type PodExecutionRepository interface {
	Claim(context.Context, domainresource.PodExecutionRecord) (domainresource.PodExecutionRecord, bool, error)
	Get(context.Context, string, string, string) (domainresource.PodExecutionRecord, error)
	Finish(context.Context, domainresource.PodExecutionRecord) error
	Cleanup(context.Context) error
}

type PodExecutionIdentity interface {
	CurrentStreamingPrincipal(context.Context, domainidentity.Principal, domainidentity.AccessContext) (domainidentity.Principal, error)
}

type PodExecutionAgent interface {
	InspectPodExecutionTarget(context.Context, string, string, string, string) (contractresource.PodExecutionTarget, error)
	ExecutePodArgv(context.Context, string, string, contractresource.PodArgvInput) (contractresource.PodArgvResult, error)
}

type PodExecutionDirect interface {
	InspectPodExecutionTarget(context.Context, string, string, string, string, string) (contractresource.PodExecutionTarget, error)
	ExecutePodArgv(context.Context, string, string, string, contractresource.PodArgvInput) (contractresource.PodArgvResult, error)
}
