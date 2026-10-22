package templateupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
)

type version struct {
	Number                   int64
	Digest                   string
	Enabled                  bool
	Source, Schema, Defaults any
	Deployment               *domaincatalog.DeploymentTemplate
	Build                    *domaincatalog.BuildTemplate
}

func (s *Service) versions(ctx context.Context, p domainidentity.Principal, kind sohaapi.TemplateUpdateKind, id string) ([]version, error) {
	items := []version{}
	switch kind {
	case "deployment":
		var err error
		items, err = s.deploymentVersions(ctx, p, id)
		if err != nil {
			return nil, err
		}
	case "build":
		head, headErr := s.deps.Build.GetBuildTemplate(ctx, p, id)
		if headErr != nil {
			return nil, headErr
		}
		active := head.Enabled && head.PublicationState != "deprecated"
		values, err := s.deps.Build.ListBuildTemplateVersions(ctx, p, id)
		if err != nil {
			return nil, err
		}
		for _, item := range values {
			items = append(items, version{Number: item.PublishedVersion, Digest: item.ContentDigest, Enabled: item.Enabled && active, Source: map[string]any{"builderKind": item.BuilderKind, "dockerfileTemplate": item.DockerfileTemplate, "buildCommands": item.BuildCommands}, Schema: item.VariableSchema, Defaults: item.DefaultVariables, Build: &item})
		}
	case "workflow":
		head, headErr := s.deps.Workflow.GetWorkflowTemplate(ctx, p, id)
		if headErr != nil {
			return nil, headErr
		}
		active := head.Enabled && head.PublicationState != "deprecated"
		values, err := s.deps.Workflow.ListWorkflowTemplateVersions(ctx, p, id)
		if err != nil {
			return nil, err
		}
		for _, item := range values {
			items = append(items, version{Number: item.PublishedVersion, Digest: item.ContentDigest, Enabled: item.Enabled && active, Source: item.Definition, Schema: map[string]any{}, Defaults: map[string]any{}})
		}
	case "blueprint":
		return nil, invalid("应用蓝图仅用于创建应用；已复制的构建、部署和工作流绑定分别管理版本，不重放初始化")
	default:
		return nil, invalid("unknown template kind")
	}
	if len(items) > 200 {
		return nil, invalid("version history exceeds 200 published entries")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Number < items[j].Number })
	return items, nil
}

func (s *Service) deploymentVersions(ctx context.Context, p domainidentity.Principal, id string) ([]version, error) {
	items := []version{}
	head, headErr := s.deps.Deployment.GetDeploymentTemplate(ctx, p, id)
	if headErr != nil {
		return nil, headErr
	}
	active := head.Enabled && head.PublicationState != "deprecated"
	values, err := s.deps.Deployment.ListDeploymentTemplateVersions(ctx, p, id)
	if err != nil {
		return nil, err
	}
	for _, item := range values {
		items = append(items, version{Number: item.PublishedVersion, Digest: item.ContentDigest, Enabled: item.Enabled && active, Source: item.Source, Schema: item.ParameterSchema, Defaults: item.Defaults, Deployment: &item})
	}
	return items, nil
}

func findVersion(items []version, number int64) (version, bool) {
	for _, item := range items {
		if item.Number == number {
			return item, true
		}
	}
	return version{}, false
}

func selectedParameters(binding domainupdate.Binding, selection sohaapi.TemplateUpdateSelection) map[string]any {
	values := maps.Clone(binding.Parameters)
	if values == nil {
		values = map[string]any{}
	}
	for _, key := range selection.UseNewDefaults {
		delete(values, key)
	}
	if selection.Parameters != nil {
		for key, value := range *selection.Parameters {
			values[key] = value
		}
	}
	return values
}

func resolveParameters(v version, b domainupdate.Binding, parameters map[string]any) (map[string]any, error) {
	if v.Deployment != nil {
		return v.Deployment.ParameterSchema.ResolveParameters(v.Deployment.Defaults, parameters, b.EnvironmentParameters, v.Deployment.EnvironmentOverrides)
	}
	if v.Build != nil {
		return domaincatalog.BuildVariables(v.Build.VariableSchema, v.Build.DefaultVariables, parameters, true)
	}
	if len(parameters) > 0 {
		return nil, invalid("workflow template versions do not accept deployment parameters")
	}
	return map[string]any{}, nil
}

func (s *Service) compare(ctx context.Context, p domainidentity.Principal, b domainupdate.Binding, from, to version, parameters map[string]any) sohaapi.TemplateUpdateDiff {
	diff := sohaapi.TemplateUpdateDiff{FromVersion: from.Number, ToVersion: to.Number, Changes: []sohaapi.TemplateUpdateChange{}, Diagnostics: []string{}, RenderStatus: "available"}
	appendChanges(&diff, "source", from.Source, to.Source)
	appendChanges(&diff, "schema", from.Schema, to.Schema)
	appendChanges(&diff, "defaults", from.Defaults, to.Defaults)
	before, beforeErr := resolveParameters(from, b, b.Parameters)
	after, afterErr := resolveParameters(to, b, parameters)
	appendChanges(&diff, "parameters", before, after)
	if beforeErr != nil || afterErr != nil {
		diff.RenderStatus = "invalid"
		diff.Diagnostics = append(diff.Diagnostics, "当前参数无法在该版本渲染；仍可查看模板、Schema 和默认值差异")
	}
	if from.Deployment != nil && to.Deployment != nil {
		if b.ManifestBinding.ID != "" {
			left, leftErr := s.deps.Manifests.PreviewTemplateAdoption(ctx, p, b.ManifestBinding.ID, templateSelection(b, from, b.Parameters))
			right, rightErr := s.deps.Manifests.PreviewTemplateAdoption(ctx, p, b.ManifestBinding.ID, templateSelection(b, to, parameters))
			if leftErr != nil || rightErr != nil {
				diff.RenderStatus = "invalid"
			} else {
				appendChanges(&diff, "resources", left.Documents, right.Documents)
			}
			return diff
		}
		left, leftErr := s.renderPreview(ctx, p, b, from, b.Parameters)
		right, rightErr := s.renderPreview(ctx, p, b, to, parameters)
		if leftErr != nil || rightErr != nil {
			diff.RenderStatus = "invalid"
		} else {
			appendChanges(&diff, "resources", left.Source, right.Source)
			if from.Deployment.Source.Git != nil || to.Deployment.Source.Git != nil || from.Deployment.Source.Helm != nil || to.Deployment.Source.Helm != nil {
				diff.RenderStatus = "unavailable"
				diff.Diagnostics = append(diff.Diagnostics, "展示固定来源、参数和 Values 差异；远端文件或 Chart 的资源差异须在部署 Plan 预检中确认")
			}
		}
	}
	return diff
}

func templateSelection(b domainupdate.Binding, v version, parameters map[string]any) domainmanifest.TemplateSelection {
	return domainmanifest.TemplateSelection{TemplateID: b.Target.TemplateID, Version: v.Number, Parameters: parameters, BaseFilesDigest: digest(b.Package.Files), AdoptionID: "configuration-preview"}
}

func (s *Service) renderPreview(ctx context.Context, p domainidentity.Principal, b domainupdate.Binding, v version, parameters map[string]any) (domaincatalog.DeploymentTemplatePreview, error) {
	return s.deps.Deployment.PreviewDeploymentTemplate(ctx, p, b.Application.ID, domaincatalog.DeploymentTemplatePreviewInput{TemplateID: b.Target.TemplateID, Version: v.Number, ServiceKey: b.Service.Key, ApplicationEnvironmentID: b.Environment.ID, Parameters: parameters, Overrides: b.EnvironmentParameters})
}

func appendChanges(diff *sohaapi.TemplateUpdateDiff, section string, before, after any) {
	left, right := normalizedObject(before), normalizedObject(after)
	keys := map[string]bool{}
	for k := range left {
		keys[k] = true
	}
	for k := range right {
		keys[k] = true
	}
	paths := slices.Collect(maps.Keys(keys))
	slices.Sort(paths)
	for _, key := range paths {
		a, aok := left[key]
		b, bok := right[key]
		if aok == bok && digest(a) == digest(b) {
			continue
		}
		diff.Changes = append(diff.Changes, sohaapi.TemplateUpdateChange{Section: sohaapi.TemplateUpdateChangeSection(section), Path: key, Before: displayJSON(a, aok), After: displayJSON(b, bok)})
	}
}

func normalizedObject(value any) map[string]any {
	data, _ := json.Marshal(value)
	var object map[string]any
	if json.Unmarshal(data, &object) != nil || object == nil {
		return map[string]any{"$": value}
	}
	return object
}

func displayJSON(value any, present bool) string {
	if !present {
		return ""
	}
	data, _ := json.MarshalIndent(value, "", "  ")
	if len(data) > 32700 {
		return string(data[:32700]) + "\n… diff value truncated; inspect the published source"
	}
	return string(data)
}

func versionDiffs(items []version, from, to int64, compare func(version, version) sohaapi.TemplateUpdateDiff) ([]sohaapi.TemplateUpdateDiff, bool) {
	selected := []version{}
	low, high := min(from, to), max(from, to)
	for _, item := range items {
		if item.Number >= low && item.Number <= high {
			selected = append(selected, item)
		}
	}
	if from > to {
		slices.Reverse(selected)
	}
	diffs := []sohaapi.TemplateUpdateDiff{}
	gap := false
	for i := 1; i < len(selected); i++ {
		diff := compare(selected[i-1], selected[i])
		diff.HistoryGap = abs(selected[i].Number-selected[i-1].Number) != 1
		if diff.HistoryGap {
			gap = true
			diff.Diagnostics = append(diff.Diagnostics, fmt.Sprintf("版本 %d 与 %d 之间的历史缺失", diff.FromVersion, diff.ToVersion))
		}
		diffs = append(diffs, diff)
	}
	return diffs, gap
}
func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func safeReason(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace("参数或模板配置未通过目标版本校验，请检查变量与源文件")
}
