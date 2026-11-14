package templateupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"reflect"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type Applications interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, domainidentity.Principal, string) (domainapp.App, error)
	ListServices(context.Context, domainidentity.Principal, string) ([]domainapp.Service, error)
	Update(context.Context, domainidentity.Principal, string, domainapp.UpsertInput) (domainapp.App, error)
}
type Environments interface {
	ListTemplateAdoptionEnvironments(context.Context, domainidentity.Principal, string) ([]domaincatalog.ApplicationEnvironment, error)
	ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error)
	ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error)
	GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error)
	AuthorizeApplicationEnvironmentPermission(context.Context, domainidentity.Principal, string, string) (domaincatalog.ApplicationEnvironment, error)
	UpdateApplicationEnvironment(context.Context, domainidentity.Principal, string, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
}
type DeploymentTemplates interface {
	GetDeploymentTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.DeploymentTemplate, error)
	ListDeploymentTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.DeploymentTemplate, error)
	GetDeploymentTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.DeploymentTemplate, error)
	PreviewDeploymentTemplate(context.Context, domainidentity.Principal, string, domaincatalog.DeploymentTemplatePreviewInput) (domaincatalog.DeploymentTemplatePreview, error)
}
type BuildTemplates interface {
	GetBuildTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.BuildTemplate, error)
	ListBuildTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.BuildTemplate, error)
	GetBuildTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.BuildTemplate, error)
}
type WorkflowTemplates interface {
	GetWorkflowTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.WorkflowTemplate, error)
	ListWorkflowTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.WorkflowTemplate, error)
	GetWorkflowTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.WorkflowTemplate, error)
}
type Manifests interface {
	AuthorizeSensitiveTemplatePreview(context.Context, domainidentity.Principal) error
	GetTemplatePackage(context.Context, domainidentity.Principal, string) (domainmanifest.Package, error)
	ListBindings(context.Context, domainidentity.Principal, string) ([]domainmanifest.EnvironmentBinding, error)
	AdoptTemplate(context.Context, domainidentity.Principal, string, int64, domainmanifest.TemplateSelection) error
	PreviewTemplateAdoption(context.Context, domainidentity.Principal, string, domainmanifest.TemplateSelection) (domainmanifest.RenderResult, error)
}
type Batches interface {
	CreateDeliveryBatch(context.Context, domainidentity.Principal, domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error)
	GetDeliveryBatch(context.Context, domainidentity.Principal, string) (domainworkflow.DeliveryBatch, error)
}
type Baselines interface {
	TemplateMigrationBaseline(context.Context, domainidentity.Principal, string, string, string) (*sohaapi.TemplateVersionReference, error)
}

type Principals interface {
	CurrentExecutionPrincipal(context.Context, string, string) (domainidentity.Principal, error)
}

type Dependencies struct {
	Baselines    Baselines
	Repository   domainupdate.Repository
	Applications Applications
	Environments Environments
	Deployment   DeploymentTemplates
	Build        BuildTemplates
	Workflow     WorkflowTemplates
	Manifests    Manifests
	Batches      Batches
	Principals   Principals
}
type Service struct{ deps Dependencies }

func New(deps Dependencies) (*Service, error) {
	for name, dependency := range map[string]any{"repository": deps.Repository, "applications": deps.Applications, "environments": deps.Environments, "deployment": deps.Deployment, "build": deps.Build, "workflow": deps.Workflow, "manifests": deps.Manifests, "batches": deps.Batches, "baselines": deps.Baselines, "principals": deps.Principals} {
		if dependency == nil || reflect.ValueOf(dependency).Kind() == reflect.Pointer && reflect.ValueOf(dependency).IsNil() {
			return nil, fmt.Errorf("template updates require %s", name)
		}
	}
	return &Service{deps: deps}, nil
}

func digest(value any) string {
	data, _ := json.Marshal(value) // All callers supply JSON-owned typed values.
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}
func invalid(message string) error {
	return fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, message)
}
func conflict(message string) error { return fmt.Errorf("%w: %s", apperrors.ErrConflict, message) }

func validFilter(filter domainupdate.Filter) error {
	if filter.TemplateID == "" || len(filter.TemplateID) > 256 {
		return invalid("templateId is required")
	}
	switch filter.Kind {
	case "deployment", "build", "workflow", "blueprint":
		return nil
	default:
		return invalid("unknown template kind")
	}
}
