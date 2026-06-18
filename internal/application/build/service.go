package build

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	execution "github.com/opensoha/soha/internal/application/execution"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type BuildRepository interface {
	List(context.Context, domainbuild.Filter) ([]domainbuild.Record, error)
	Get(context.Context, string) (domainbuild.Record, error)
	Create(context.Context, domainbuild.TriggerInput, map[string]any) (domainbuild.Record, error)
	Update(context.Context, domainbuild.Record) (domainbuild.Record, error)
}

type ApplicationReader interface {
	Get(context.Context, string) (domainapp.App, error)
}

type BuildTemplateReader interface {
	GetBuildTemplate(context.Context, string) (domaincatalog.BuildTemplate, error)
}

type ExecutionPlane interface {
	StartBuildExecution(context.Context, execution.BuildPlan) (domaindelivery.ReleaseBundle, domaindelivery.ExecutionTask, error)
	CompleteBuildExecution(context.Context, string, string, string, string, map[string]any) error
}

type EventWriter interface {
	Create(context.Context, domainevent.Envelope) error
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type Service struct {
	repo       BuildRepository
	apps       ApplicationReader
	templates  BuildTemplateReader
	execution  ExecutionPlane
	authorizer domainaccess.Authorizer
	events     EventWriter
	audit      AuditRecorder
	operations OperationRecorder
}

func New(repo BuildRepository, apps ApplicationReader, templates BuildTemplateReader, execution ExecutionPlane, authorizer domainaccess.Authorizer, events EventWriter, audit AuditRecorder, operations OperationRecorder) *Service {
	return &Service{repo: repo, apps: apps, templates: templates, execution: execution, authorizer: authorizer, events: events, audit: audit, operations: operations}
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter domainbuild.Filter) ([]domainbuild.Record, error) {
	if err := s.authorize(ctx, principal, domainaccess.ActionList, filter.ApplicationID); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, buildID string) (domainbuild.Record, error) {
	record, err := s.repo.Get(ctx, strings.TrimSpace(buildID))
	if err != nil {
		return domainbuild.Record{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionView, record.ApplicationID); err != nil {
		return domainbuild.Record{}, err
	}
	return record, nil
}

func (s *Service) Trigger(ctx context.Context, principal domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Record, error) {
	if input.ApplicationID == "" {
		return domainbuild.Record{}, fmt.Errorf("%w: applicationId is required", apperrors.ErrInvalidArgument)
	}
	if input.RefType == "" {
		input.RefType = "branch"
	}
	if input.RefName == "" {
		return domainbuild.Record{}, fmt.Errorf("%w: refName is required", apperrors.ErrInvalidArgument)
	}
	app, err := s.apps.Get(ctx, input.ApplicationID)
	if err != nil {
		return domainbuild.Record{}, err
	}
	buildSource := resolveBuildSource(app, input.BuildSourceID)
	effectiveImageTag := strings.TrimSpace(input.ImageTag)
	if effectiveImageTag == "" {
		if buildSource != nil && strings.TrimSpace(buildSource.DefaultTag) != "" {
			effectiveImageTag = strings.TrimSpace(buildSource.DefaultTag)
		} else {
			effectiveImageTag = strings.TrimSpace(app.DefaultTag)
		}
	}
	imageRef := resolveBuildImageRefForSource(app, buildSource, effectiveImageTag)
	if err := s.authorize(ctx, principal, domainaccess.ActionTrigger, app.ID); err != nil {
		return domainbuild.Record{}, err
	}
	metadata := map[string]any{
		"applicationName":          app.Name,
		"applicationEnvironmentId": strings.TrimSpace(input.ApplicationEnvironmentID),
		"buildSourceId":            strings.TrimSpace(input.BuildSourceID),
		"refType":                  input.RefType,
		"refName":                  input.RefName,
		"imageTag":                 effectiveImageTag,
		"buildArgs":                input.BuildArgs,
		"variables":                input.Variables,
		"repositoryPath":           app.RepositoryPath,
		"pipelineStages": []map[string]any{
			{"name": "queued", "status": "completed", "timestamp": time.Now().UTC().Format(time.RFC3339)},
			{"name": "planning", "status": "running", "timestamp": time.Now().UTC().Format(time.RFC3339)},
		},
		"logs": []map[string]any{
			{"level": "info", "message": fmt.Sprintf("Manual build requested for %s on %s:%s", app.Name, input.RefType, input.RefName), "timestamp": time.Now().UTC().Format(time.RFC3339)},
			{"level": "info", "message": "Build execution worker has not started yet; record is queued for future runner integration", "timestamp": time.Now().UTC().Format(time.RFC3339)},
		},
		"artifacts": []map[string]any{
			{"kind": "image", "ref": imageRef, "status": "planned"},
		},
		"imageDigest": "pending",
		"image":       imageRef,
	}
	appendBuildSourceMetadata(ctx, s.templates, buildSource, metadata)
	if workspace := buildExecutionWorkspace(app, buildSource, input); len(workspace) > 0 {
		metadata["workspace"] = workspace
	}
	metadata["runtime"] = buildExecutionRuntime(buildSource, metadata)
	if commands := buildExecutionCommands(app, buildSource, metadata, imageRef); len(commands) > 0 {
		metadata["commands"] = commands
	}
	if s.execution != nil {
		bundle, task, execErr := s.execution.StartBuildExecution(ctx, execution.BuildPlan{
			ApplicationID:            app.ID,
			ApplicationEnvironmentID: strings.TrimSpace(input.ApplicationEnvironmentID),
			Version:                  effectiveImageTag,
			SourceType:               resolveSourceType(buildSource),
			ProviderKind:             resolveBuildProviderKind(buildSource),
			TargetKind:               "k8s_workload",
			ArtifactRef:              imageRef,
			Metadata:                 metadata,
		})
		if execErr == nil {
			metadata["releaseBundleId"] = bundle.ID
			metadata["executionTaskId"] = task.ID
			metadata["executionProviderKind"] = task.ProviderKind
			metadata["executionTaskStatus"] = task.Status
		}
	}
	record, err := s.repo.Create(ctx, input, metadata)
	if err != nil {
		return domainbuild.Record{}, err
	}
	if s.events != nil {
		_ = s.events.Create(ctx, domainevent.Envelope{
			ID:       "event:" + record.ID,
			Source:   "build",
			Category: "build",
			Severity: "info",
			Summary:  fmt.Sprintf("Build queued for %s on %s:%s", app.Name, input.RefType, input.RefName),
			Payload: map[string]any{
				"buildId":       record.ID,
				"applicationId": app.ID,
				"status":        record.Status,
			},
		})
	}
	_ = s.recordAudit(ctx, principal, app.Name, string(domainaccess.ActionTrigger), "success", "triggered manual build")
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(
			ctx,
			principal,
			"delivery.build.trigger",
			map[string]any{
				"module":        "delivery",
				"resourceKind":  "Build",
				"targetId":      record.ID,
				"targetLabel":   app.Name,
				"applicationId": app.ID,
			},
			"success",
			"triggered manual build",
			map[string]any{
				"buildId":       record.ID,
				"applicationId": app.ID,
			},
		))
	}
	return record, nil
}

func (s *Service) Execute(ctx context.Context, principal domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Record, error) {
	record, err := s.Trigger(ctx, principal, input)
	if err != nil {
		return domainbuild.Record{}, err
	}
	now := time.Now().UTC()
	record.Status = "completed"
	record.StartedAt = &now
	record.FinishedAt = &now
	if record.Metadata == nil {
		record.Metadata = map[string]any{}
	}
	imageRef := resolveBuildImageRef(record.Metadata, input.ImageTag)
	record.Metadata["applicationEnvironmentId"] = strings.TrimSpace(input.ApplicationEnvironmentID)
	record.Metadata["buildSourceId"] = strings.TrimSpace(input.BuildSourceID)
	record.Metadata["artifact"] = map[string]any{
		"kind":   "image",
		"status": "completed",
		"ref":    imageRef,
	}
	record.Metadata["image"] = imageRef
	record.Metadata["variables"] = input.Variables
	record.Metadata["triggeredByWorkflowRunId"] = strings.TrimSpace(input.TriggeredByWorkflowRunID)
	if s.execution != nil {
		if bundleID := strings.TrimSpace(fmt.Sprint(record.Metadata["releaseBundleId"])); bundleID != "" {
			_ = s.execution.CompleteBuildExecution(ctx, bundleID, strings.TrimSpace(fmt.Sprint(record.Metadata["executionTaskId"])), imageRef, strings.TrimSpace(fmt.Sprint(record.Metadata["imageDigest"])), map[string]any{
				"image":       imageRef,
				"artifact":    record.Metadata["artifact"],
				"imageDigest": record.Metadata["imageDigest"],
			})
		}
	}
	if s.repo != nil {
		updated, updateErr := s.repo.Update(ctx, record)
		if updateErr == nil {
			record = updated
		}
	}
	return record, nil
}

func resolveSourceType(source *domainapp.BuildSource) string {
	if source == nil {
		return "repo_dockerfile"
	}
	return string(source.Type)
}

func resolveBuildProviderKind(source *domainapp.BuildSource) string {
	if source == nil {
		return "k8s_job_runner"
	}
	switch source.Type {
	case domainapp.BuildSourceTypeExternalPipeline:
		return "external_pipeline_adapter"
	case domainapp.BuildSourceTypePlatformTemplate:
		if strings.TrimSpace(fmt.Sprint(source.Config["providerKind"])) == "ci_agent_runner" {
			return "ci_agent_runner"
		}
		return "k8s_job_runner"
	default:
		return "k8s_job_runner"
	}
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, resourceName string) error {
	if s.authorizer == nil {
		return nil
	}
	app, _ := s.apps.Get(ctx, resourceName)
	decision, err := s.authorizer.Authorize(ctx, domainaccess.Request{
		Principal: principal,
		Action:    action,
		Subject: domainaccess.SubjectAttributes{
			UserID:   principal.UserID,
			Roles:    principal.Roles,
			Teams:    principal.Teams,
			Projects: principal.Projects,
			Tags:     principal.Tags,
		},
		Resource: domainaccess.ResourceAttributes{
			Kind: "Build",
			Name: resourceName,
		},
		Delivery: domainaccess.DeliveryAttributes{
			BusinessLineID:   app.BusinessLineID,
			ApplicationGroup: app.Group,
			ApplicationID:    app.ID,
		},
		Context: domainaccess.ContextAttributes{
			Source:     requestctx.FromContext(ctx).Source,
			OccurredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, decision.Reason)
	}
	return nil
}

func (s *Service) recordAudit(ctx context.Context, principal domainidentity.Principal, resourceName, action, result, summary string) error {
	if s.audit == nil {
		return nil
	}
	meta := requestctx.FromContext(ctx)
	return s.audit.Record(ctx, domainaudit.Entry{
		ID:            uuid.NewString(),
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         principal.Roles,
		Teams:         principal.Teams,
		ResourceKind:  "Build",
		ResourceName:  resourceName,
		Action:        action,
		Result:        result,
		Summary:       summary,
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata: map[string]any{
			"source": meta.Source,
		},
	})
}

func resolveBuildImageRef(metadata map[string]any, imageTag string) string {
	if len(metadata) == 0 {
		return strings.TrimSpace(imageTag)
	}
	if artifacts, ok := metadata["artifacts"].([]map[string]any); ok {
		for _, item := range artifacts {
			if ref := strings.TrimSpace(fmt.Sprint(item["ref"])); ref != "" {
				return ref
			}
		}
	}
	if artifacts, ok := metadata["artifacts"].([]any); ok {
		for _, item := range artifacts {
			if value, ok := item.(map[string]any); ok {
				if ref := strings.TrimSpace(fmt.Sprint(value["ref"])); ref != "" {
					return ref
				}
			}
		}
	}
	return strings.TrimSpace(imageTag)
}

func resolveBuildSource(app domainapp.App, sourceID string) *domainapp.BuildSource {
	sourceID = strings.TrimSpace(sourceID)
	for _, item := range app.BuildSources {
		if sourceID != "" && item.ID == sourceID {
			copyItem := item
			return &copyItem
		}
	}
	for _, item := range app.BuildSources {
		if item.IsDefault {
			copyItem := item
			return &copyItem
		}
	}
	return nil
}

func resolveBuildImageRefForSource(app domainapp.App, source *domainapp.BuildSource, imageTag string) string {
	base := strings.TrimSpace(app.BuildImage)
	if source != nil && strings.TrimSpace(source.BuildImage) != "" {
		base = strings.TrimSpace(source.BuildImage)
	}
	if base == "" {
		return strings.TrimSpace(imageTag)
	}
	if strings.TrimSpace(imageTag) == "" {
		return base
	}
	return fmt.Sprintf("%s:%s", base, strings.TrimSpace(imageTag))
}

func appendBuildSourceMetadata(ctx context.Context, templates BuildTemplateReader, source *domainapp.BuildSource, metadata map[string]any) {
	if source == nil {
		return
	}
	metadata["buildSourceName"] = source.Name
	metadata["buildSourceType"] = string(source.Type)
	metadata["buildSourceConfig"] = source.Config
	if source.Type != domainapp.BuildSourceTypePlatformTemplate || templates == nil {
		return
	}
	templateID := strings.TrimSpace(fmt.Sprint(source.Config["buildTemplateId"]))
	if templateID == "" {
		return
	}
	template, err := templates.GetBuildTemplate(ctx, templateID)
	if err != nil {
		return
	}
	metadata["buildTemplateId"] = template.ID
	metadata["buildTemplateKey"] = template.Key
	metadata["buildTemplateName"] = template.Name
	metadata["buildTemplateBuilderKind"] = template.BuilderKind
	metadata["buildTemplateCommands"] = template.BuildCommands
	metadata["buildTemplateVariableSchema"] = template.VariableSchema
	metadata["buildTemplateDefaultVariables"] = template.DefaultVariables
	metadata["buildTemplateDockerfileTemplate"] = template.DockerfileTemplate
}

func buildExecutionCommands(app domainapp.App, source *domainapp.BuildSource, metadata map[string]any, imageRef string) []string {
	if source == nil {
		return nil
	}
	switch source.Type {
	case domainapp.BuildSourceTypePlatformTemplate:
		if raw, ok := metadata["buildTemplateCommands"].([]string); ok && len(raw) > 0 {
			return renderCommands(raw, source, imageRef)
		}
		if raw, ok := metadata["buildTemplateCommands"].([]any); ok && len(raw) > 0 {
			items := make([]string, 0, len(raw))
			for _, item := range raw {
				text := strings.TrimSpace(fmt.Sprint(item))
				if text != "" {
					items = append(items, text)
				}
			}
			return renderCommands(items, source, imageRef)
		}
	case domainapp.BuildSourceTypeExternalPipeline:
		if value, ok := source.Config["triggerConfig"].(map[string]any); ok {
			if commands, ok := value["commands"].([]any); ok {
				items := make([]string, 0, len(commands))
				for _, item := range commands {
					text := strings.TrimSpace(fmt.Sprint(item))
					if text != "" {
						items = append(items, text)
					}
				}
				return items
			}
		}
	default:
		contextDir := strings.TrimSpace(fmt.Sprint(source.Config["contextDir"]))
		if contextDir == "" {
			contextDir = "."
		}
		dockerfilePath := strings.TrimSpace(fmt.Sprint(source.Config["dockerfilePath"]))
		if dockerfilePath == "" {
			dockerfilePath = "Dockerfile"
		}
		builderKind := strings.TrimSpace(fmt.Sprint(source.Config["builderKind"]))
		if builderKind == "" {
			builderKind = "docker"
		}
		if resolveBuildProviderKind(source) == "k8s_job_runner" && (builderKind == "docker" || builderKind == "buildx") {
			builderKind = "kaniko"
		}
		switch builderKind {
		case "kaniko":
			return []string{fmt.Sprintf("executor --dockerfile=%s --context=%s --destination=%s", dockerfilePath, contextDir, imageRef)}
		case "buildx":
			return []string{fmt.Sprintf("docker buildx build -f %s -t %s %s", dockerfilePath, imageRef, contextDir)}
		default:
			return []string{fmt.Sprintf("docker build -f %s -t %s %s", dockerfilePath, imageRef, contextDir)}
		}
	}
	_ = app
	return nil
}

func renderCommands(commands []string, source *domainapp.BuildSource, imageRef string) []string {
	items := make([]string, 0, len(commands))
	contextDir := strings.TrimSpace(fmt.Sprint(source.Config["contextDir"]))
	if contextDir == "" {
		contextDir = "."
	}
	for _, command := range commands {
		value := strings.TrimSpace(command)
		if value == "" {
			continue
		}
		value = strings.ReplaceAll(value, "{{IMAGE_REF}}", imageRef)
		value = strings.ReplaceAll(value, "{{CONTEXT_DIR}}", contextDir)
		items = append(items, value)
	}
	return items
}

func buildExecutionWorkspace(app domainapp.App, source *domainapp.BuildSource, input domainbuild.TriggerInput) map[string]any {
	workspace := map[string]any{}
	workspacePath := firstNonEmptyString(
		configString(source, "workspacePath"),
		metadataString(app.Metadata, "workspacePath"),
		strings.TrimSpace(app.RepositoryPath),
		strings.TrimSpace(app.Key),
		strings.TrimSpace(app.ID),
	)
	if workspacePath != "" {
		workspace["path"] = workspacePath
	}
	if commandDir := firstNonEmptyString(
		configString(source, "workingDir"),
		metadataString(app.Metadata, "workingDir"),
	); commandDir != "" {
		workspace["commandDir"] = commandDir
	}
	if artifactFiles := firstNonEmptyStringSlice(
		configStringSlice(source, "artifactFiles"),
		metadataStringSlice(app.Metadata, "artifactFiles"),
	); len(artifactFiles) > 0 {
		workspace["artifactFiles"] = artifactFiles
	}
	checkoutEnabled := source != nil && source.Type != domainapp.BuildSourceTypeExternalPipeline
	if source != nil {
		if value, ok := source.Config["checkoutEnabled"]; ok {
			checkoutEnabled = boolValue(value, checkoutEnabled)
		}
	}
	checkout := map[string]any{
		"enabled":        checkoutEnabled,
		"repositoryPath": strings.TrimSpace(app.RepositoryPath),
		"repositoryURL":  resolveRepositoryURL(app, source),
		"refType":        strings.TrimSpace(input.RefType),
		"refName":        strings.TrimSpace(input.RefName),
		"defaultBranch":  strings.TrimSpace(app.DefaultBranch),
	}
	if checkoutEnabled || strings.TrimSpace(fmt.Sprint(checkout["repositoryURL"])) != "" || strings.TrimSpace(app.RepositoryPath) != "" {
		workspace["checkout"] = checkout
	}
	return workspace
}

func buildExecutionRuntime(source *domainapp.BuildSource, metadata map[string]any) map[string]any {
	runtime := map[string]any{}
	if source == nil {
		return runtime
	}
	if image := configString(source, "runtimeImage"); image != "" {
		runtime["image"] = image
	}
	if checkoutImage := configString(source, "checkoutImage"); checkoutImage != "" {
		runtime["checkoutImage"] = checkoutImage
	}
	if commandDir := firstNonEmptyString(configString(source, "workingDir"), metadataString(nil, "")); commandDir != "" {
		runtime["commandDir"] = commandDir
	}
	switch resolveBuildProviderKind(source) {
	case "k8s_job_runner":
		if strings.TrimSpace(fmt.Sprint(runtime["image"])) == "" {
			runtime["image"] = "gcr.io/kaniko-project/executor:v1.23.2-debug"
		}
		if strings.TrimSpace(fmt.Sprint(runtime["checkoutImage"])) == "" {
			runtime["checkoutImage"] = "alpine/git:2.47.0"
		}
	default:
		if strings.TrimSpace(fmt.Sprint(runtime["image"])) == "" {
			runtime["image"] = "alpine:3.20"
		}
	}
	if workspace, ok := metadata["workspace"].(map[string]any); ok {
		if value := strings.TrimSpace(fmt.Sprint(workspace["commandDir"])); value != "" {
			runtime["commandDir"] = value
		}
		if value := strings.TrimSpace(fmt.Sprint(workspace["path"])); value != "" {
			runtime["workspacePath"] = value
		}
	}
	return runtime
}

func resolveRepositoryURL(app domainapp.App, source *domainapp.BuildSource) string {
	return firstNonEmptyString(
		configString(source, "repositoryURL"),
		configString(source, "repositoryUrl"),
		metadataString(app.Metadata, "repositoryURL"),
		metadataString(app.Metadata, "repositoryUrl"),
	)
}

func configString(source *domainapp.BuildSource, key string) string {
	if source == nil || len(source.Config) == 0 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(source.Config[key]))
}

func configStringSlice(source *domainapp.BuildSource, key string) []string {
	if source == nil || len(source.Config) == 0 {
		return nil
	}
	return valueAsStringSlice(source.Config[key])
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(metadata[key]))
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if len(metadata) == 0 {
		return nil
	}
	return valueAsStringSlice(metadata[key])
}

func valueAsStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	default:
		return nil
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmptyStringSlice(candidates ...[]string) []string {
	for _, candidate := range candidates {
		if len(candidate) > 0 {
			return candidate
		}
	}
	return nil
}

func boolValue(raw any, fallback bool) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		trimmed := strings.TrimSpace(strings.ToLower(value))
		switch trimmed {
		case "true", "1", "yes", "y", "on":
			return true
		case "false", "0", "no", "n", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}
