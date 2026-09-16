package build

import (
	"context"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const ExternalPipelineProvider = "external_pipeline.gitlab"

type PipelineRequest struct {
	TaskID, Image string
	Spec          sohaapi.ExternalPipelineExecutionSpec
	Variables     map[string]string
	CreatedAt     time.Time
}

type PipelineInspection struct {
	Run         sohaapi.ExternalPipelineRun
	Artifact    []byte
	ImageDigest string
}

// The provider adapter owns remote protocol details; persistence and dispatch
// authorization remain in the execution service and repository transaction.
type PipelineProvider interface {
	ResolvePipelineTag(context.Context, string, string) (string, error)
	StartPipeline(context.Context, PipelineRequest) (sohaapi.ExternalPipelineRun, error)
	FindPipeline(context.Context, PipelineRequest) (sohaapi.ExternalPipelineRun, error)
	InspectPipeline(context.Context, PipelineRequest, string, bool) (PipelineInspection, error)
}
