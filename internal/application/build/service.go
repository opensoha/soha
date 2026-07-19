package build

import (
	"context"
	"fmt"
	"sort"
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
	input.RefType = strings.ToLower(strings.TrimSpace(input.RefType))
	if input.RefType != "branch" && input.RefType != "tag" && input.RefType != "commit" {
		return domainbuild.Record{}, fmt.Errorf("%w: refType must be branch, tag, or commit", apperrors.ErrInvalidArgument)
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
	metadata := s.buildTriggerMetadata(ctx, app, buildSource, input, effectiveImageTag, imageRef)
	metadata["serviceId"] = strings.TrimSpace(input.ServiceID)
	metadata["repositoryId"] = strings.TrimSpace(input.RepositoryID)
	metadata["resolvedCommit"] = strings.TrimSpace(input.ResolvedCommit)
	record, err := s.repo.Create(ctx, input, metadata)
	if err != nil {
		return domainbuild.Record{}, err
	}
	if s.execution == nil {
		return s.failRecord(ctx, record, "execution plane is not configured")
	}
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
	if execErr != nil {
		failed, _ := s.failRecord(ctx, record, execErr.Error())
		return failed, execErr
	}
	metadata = mergeBuildMetadata(record.Metadata, task.Result)
	metadata = mergeBuildMetadata(metadata, map[string]any{
		"releaseBundleId":       bundle.ID,
		"executionTaskId":       task.ID,
		"executionProviderKind": task.ProviderKind,
		"executionTaskStatus":   task.Status,
	})
	record.Metadata = metadata
	switch task.Status {
	case "running", "dispatching":
		record.Status = "running"
		if task.StartedAt != nil {
			record.StartedAt = task.StartedAt
		}
	case "completed":
		record.Status = "completed"
		record.StartedAt = task.StartedAt
		record.FinishedAt = task.FinishedAt
	case "failed", "canceled", "callback_timeout":
		record.Status = "failed"
		record.StartedAt = task.StartedAt
		record.FinishedAt = task.FinishedAt
	}
	updated, updateErr := s.repo.Update(ctx, record)
	if updateErr != nil {
		return domainbuild.Record{}, fmt.Errorf("link build record to execution task: %w", updateErr)
	}
	record = updated
	s.recordTriggeredBuild(ctx, principal, app, input, record)
	return record, nil
}

func (s *Service) buildTriggerMetadata(ctx context.Context, app domainapp.App, buildSource *domainapp.BuildSource, input domainbuild.TriggerInput, effectiveImageTag, imageRef string) map[string]any {
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
			{"name": "checkout", "status": "queued"},
			{"name": "prepare", "status": "queued"},
			{"name": "build", "status": "queued"},
			{"name": "push", "status": "queued"},
			{"name": "publish", "status": "queued"},
		},
		"logs": []map[string]any{
			{"level": "info", "message": fmt.Sprintf("Manual build requested for %s on %s:%s", app.Name, input.RefType, input.RefName), "timestamp": time.Now().UTC().Format(time.RFC3339)},
			{"level": "info", "message": "Build execution task created; waiting for runner claim", "timestamp": time.Now().UTC().Format(time.RFC3339)},
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
	if commands := buildExecutionCommands(buildSource, metadata, imageRef); len(commands) > 0 {
		metadata["commands"] = commands
	}
	return metadata
}

func (s *Service) recordTriggeredBuild(ctx context.Context, principal domainidentity.Principal, app domainapp.App, input domainbuild.TriggerInput, record domainbuild.Record) {
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
}

func (s *Service) Execute(ctx context.Context, principal domainidentity.Principal, input domainbuild.TriggerInput) (domainbuild.Record, error) {
	return s.Trigger(ctx, principal, input)
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
	if configured := strings.TrimSpace(fmt.Sprint(source.Config["providerKind"])); configured != "" {
		return configured
	}
	switch source.Type {
	case domainapp.BuildSourceTypeExternalPipeline:
		return "external_pipeline_adapter"
	case domainapp.BuildSourceTypePlatformTemplate:
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

func buildExecutionCommands(source *domainapp.BuildSource, metadata map[string]any, imageRef string) []string {
	if source == nil {
		return nil
	}
	switch source.Type {
	case domainapp.BuildSourceTypePlatformTemplate:
		return platformTemplateExecutionCommands(source, metadata, imageRef)
	case domainapp.BuildSourceTypeExternalPipeline:
		return externalPipelineExecutionCommands(source)
	default:
		return containerBuildExecutionCommands(source, imageRef, metadataMap(metadata, "buildArgs"))
	}
}

func platformTemplateExecutionCommands(source *domainapp.BuildSource, metadata map[string]any, imageRef string) []string {
	if raw, ok := metadata["buildTemplateCommands"].([]string); ok && len(raw) > 0 {
		return renderCommands(raw, source, imageRef)
	}
	raw, ok := metadata["buildTemplateCommands"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	return renderCommands(nonEmptyStrings(raw), source, imageRef)
}

func externalPipelineExecutionCommands(source *domainapp.BuildSource) []string {
	value, ok := source.Config["triggerConfig"].(map[string]any)
	if !ok {
		return nil
	}
	commands, ok := value["commands"].([]any)
	if !ok {
		return nil
	}
	return nonEmptyStrings(commands)
}

func nonEmptyStrings(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func containerBuildExecutionCommands(source *domainapp.BuildSource, imageRef string, buildArgs map[string]any) []string {
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
		return []string{fmt.Sprintf("executor --dockerfile=%s --context=%s --destination=%s --digest-file=.soha-image-digest%s", shellQuote(dockerfilePath), shellQuote(contextDir), shellQuote(imageRef), renderBuildArgs(buildArgs))}
	case "buildx":
		return []string{fmt.Sprintf("docker buildx build --push -f %s -t %s%s %s", shellQuote(dockerfilePath), shellQuote(imageRef), renderBuildArgs(buildArgs), shellQuote(contextDir))}
	default:
		return []string{
			fmt.Sprintf("docker build -f %s -t %s%s %s", shellQuote(dockerfilePath), shellQuote(imageRef), renderBuildArgs(buildArgs), shellQuote(contextDir)),
			fmt.Sprintf("docker push %s", shellQuote(imageRef)),
			fmt.Sprintf("docker image inspect --format='{{index .RepoDigests 0}}' %s > .soha-image-digest", shellQuote(imageRef)),
		}
	}
}

func renderBuildArgs(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(" --build-arg=")
		builder.WriteString(shellQuote(strings.TrimSpace(key) + "=" + fmt.Sprint(values[key])))
	}
	return builder.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
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
	if resolveBuildProviderKind(source) != "external_pipeline_adapter" {
		workspace["artifactFiles"] = appendUniqueString(valueStringSlice(workspace["artifactFiles"]), ".soha-image-digest")
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

func (s *Service) failRecord(ctx context.Context, record domainbuild.Record, reason string) (domainbuild.Record, error) {
	now := time.Now().UTC()
	record.Status = "failed"
	record.FinishedAt = &now
	record.Metadata = mergeBuildMetadata(record.Metadata, map[string]any{
		"executionTaskStatus": "failed",
		"failureStage":        "prepare",
		"error":               strings.TrimSpace(reason),
	})
	updated, err := s.repo.Update(ctx, record)
	if err != nil {
		return record, err
	}
	return updated, fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, strings.TrimSpace(reason))
}

func mergeBuildMetadata(base, overlay map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range overlay {
		base[key] = value
	}
	return base
}

func metadataMap(metadata map[string]any, key string) map[string]any {
	value, _ := metadata[key].(map[string]any)
	return value
}

func valueStringSlice(value any) []string {
	items, _ := value.([]string)
	return items
}

func appendUniqueString(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
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
