package workflow

import (
	"context"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

// dagPlanner owns definition and input resolution. It is stateless apart from
// the narrow application reader needed by service selectors.
type dagPlanner struct {
	apps ApplicationReader
}

type dagWorkflowNode struct {
	ID                  string
	Name                string
	Type                string
	ExecutorKind        string
	TargetKind          string
	CapabilityRef       string
	ProviderRef         string
	TimeoutSeconds      int
	ContinueOnFailure   bool
	Config              map[string]any
	Inputs              []string
	Outputs             []string
	ServiceSelector     map[string]any
	EnvironmentSelector map[string]any
	TargetSelector      map[string]any
	InputMapping        map[string]any
	ArtifactOutputs     []map[string]any
	ArtifactKinds       []string
	RunCondition        string
	FailurePolicy       string
	FanOutStrategy      string
	FanOutBatchSize     int
	FanOutFailurePolicy string
	Observability       map[string]any
}

type dagWorkflowEdge struct {
	ID        string
	Source    string
	Target    string
	Condition string
}

type dagWorkflowDefinition struct {
	SchemaVersion int
	Mode          string
	Nodes         []dagWorkflowNode
	Edges         []dagWorkflowEdge
}

func (s *Service) dagPlanner() dagPlanner {
	if s == nil {
		return dagPlanner{}
	}
	return dagPlanner{apps: s.apps}
}

func (p dagPlanner) parse(definition map[string]any) (dagWorkflowDefinition, bool) {
	return parseDAGWorkflowDefinition(definition)
}

func (p dagPlanner) validate(definition dagWorkflowDefinition, binding domaincatalog.ApplicationEnvironment, app domainapp.App, input domainworkflow.Input) error {
	return validateDAGExecutionDefinition(definition, binding, app, input)
}

func (p dagPlanner) resolveInputs(node dagWorkflowNode, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) map[string]any {
	return resolveDAGNodeInputs(node, app, input, binding, artifactState)
}

func (p dagPlanner) resolveSelectors(ctx context.Context, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode) (map[string]any, error) {
	var services []domainapp.Service
	if p.apps != nil && strings.TrimSpace(app.ID) != "" {
		items, err := p.apps.ListServices(ctx, strings.TrimSpace(app.ID))
		if err == nil {
			services = items
		}
	}
	return resolveDAGNodeSelectorsWithServices(app, input, binding, node, services)
}
