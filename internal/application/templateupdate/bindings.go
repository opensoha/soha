package templateupdate

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListTargets(ctx context.Context, p domainidentity.Principal, f domainupdate.Filter) (sohaapi.TemplateUpdateTargetPage, error) {
	page := sohaapi.TemplateUpdateTargetPage{Items: []sohaapi.TemplateUpdateTarget{}, Page: int64(max(f.Page, 1)), PageSize: int64(min(max(f.PageSize, 1), 100))}
	if f.PageSize == 0 {
		page.PageSize = 20
	}
	bindings, err := s.bindings(ctx, p, f)
	if err != nil {
		return page, err
	}
	page.Total = int64(len(bindings))
	if page.Page > (page.Total+page.PageSize-1)/page.PageSize {
		return page, nil
	}
	start := (page.Page - 1) * page.PageSize
	if start >= page.Total {
		return page, nil
	}
	for _, binding := range bindings[start:min(start+page.PageSize, page.Total)] {
		page.Items = append(page.Items, binding.Target)
	}
	return page, nil
}

func (s *Service) bindings(ctx context.Context, p domainidentity.Principal, f domainupdate.Filter) ([]domainupdate.Binding, error) {
	if err := validFilter(f); err != nil {
		return nil, err
	}
	if _, err := s.versions(ctx, p, f.Kind, f.TemplateID); err != nil {
		return nil, err
	}
	apps, err := s.bindingApplications(ctx, p, f.ApplicationID)
	if err != nil {
		return nil, err
	}
	envs, err := s.deps.Environments.ListApplicationEnvironments(ctx, p)
	if err != nil {
		return nil, err
	}
	definitions, err := s.deps.Environments.ListEnvironments(ctx, p)
	if err != nil {
		return nil, err
	}
	protected := map[string]bool{}
	for _, env := range definitions {
		protected[env.ID] = env.IsProduction || env.RequiresApproval
	}
	policies, err := s.deps.Repository.ListPolicies(ctx, f)
	if err != nil {
		return nil, err
	}
	items := []domainupdate.Binding{}
	for _, app := range apps {
		found, err := s.applicationBindings(ctx, p, app, envs, f)
		if err != nil {
			return nil, err
		}
		for _, binding := range found {
			if f.ServiceID != "" && binding.Target.ServiceID != f.ServiceID || f.ApplicationEnvironmentID != "" && binding.Target.ApplicationEnvironmentID != f.ApplicationEnvironmentID {
				continue
			}
			applyAdoptionPolicy(&binding, protected, policies)
			binding.Target.BindingRevision = bindingRevision(binding)
			items = append(items, binding)
			if len(items) > 2000 {
				return nil, invalid("affected bindings exceed 2000; filter by application")
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Target.ID < items[j].Target.ID })
	return items, nil
}

func (s *Service) applicationBindings(ctx context.Context, p domainidentity.Principal, app domainapp.App, envs []domaincatalog.ApplicationEnvironment, f domainupdate.Filter) ([]domainupdate.Binding, error) {
	appEnvs := []domaincatalog.ApplicationEnvironment{}
	for _, env := range envs {
		if env.ApplicationID == app.ID {
			appEnvs = append(appEnvs, env)
		}
	}
	if f.Kind == "build" {
		var err error
		appEnvs, err = s.deps.Environments.ListTemplateAdoptionEnvironments(ctx, p, app.ID)
		if err != nil {
			return nil, err
		}
	}
	var found []domainupdate.Binding
	var err error
	switch f.Kind {
	case "build":
		found = s.buildBindings(app, appEnvs, f.TemplateID)
	case "workflow":
		found = s.workflowBindings(app, appEnvs, f.TemplateID)
	case "deployment":
		found, err = s.deploymentBindings(ctx, p, app, appEnvs, f.TemplateID)
	}
	if err != nil {
		return nil, err
	}
	return found, nil
}

func applyAdoptionPolicy(binding *domainupdate.Binding, protected map[string]bool, policies []domainupdate.Policy) {
	for _, env := range binding.Environments {
		if protected[env.EnvironmentID] || environmentNeedsApproval(env) {
			binding.Target.RequiresApproval = true
			if binding.Target.Scope == "application" {
				binding.Environment = env
				binding.Target.ApplicationEnvironmentID = env.ID
				binding.Target.EnvironmentName = env.Alias
			}
		}
	}
	binding.Target.Mode = "fixed"
	for _, policy := range policies {
		if policy.Value.TargetID == binding.Target.ID {
			binding.Target.Mode = policy.Value.Mode
		}
	}
}

func (s *Service) bindingApplications(ctx context.Context, p domainidentity.Principal, id string) ([]domainapp.App, error) {
	if id != "" {
		app, err := s.deps.Applications.Get(ctx, p, id)
		return []domainapp.App{app}, err
	}
	apps, err := s.deps.Applications.List(ctx, p, domainapp.Filter{Limit: 501})
	if len(apps) > 500 {
		return nil, invalid("more than 500 accessible applications; select an application to narrow the impact preview")
	}
	return apps, err
}

func targetBase(kind sohaapi.TemplateUpdateKind, templateID, identity string, app domainapp.App, env domaincatalog.ApplicationEnvironment) sohaapi.TemplateUpdateTarget {
	return sohaapi.TemplateUpdateTarget{ID: strings.TrimPrefix(digest([]string{string(kind), templateID, identity}), "sha256:"), Kind: kind, TemplateID: templateID,
		ApplicationID: app.ID, ApplicationName: app.Name, ApplicationEnvironmentID: env.ID, EnvironmentName: env.Alias,
		Eligible: true, Mode: "fixed", Scope: "environment", IndependentOverrides: []string{}, RequiresApproval: environmentNeedsApproval(env)}
}

func environmentNeedsApproval(env domaincatalog.ApplicationEnvironment) bool {
	return env.ReleasePolicy.RequiresApproval || strings.EqualFold(env.EnvironmentKey, "prod") || strings.EqualFold(env.EnvironmentKey, "production")
}

func (s *Service) buildBindings(app domainapp.App, envs []domaincatalog.ApplicationEnvironment, templateID string) []domainupdate.Binding {
	items := []domainupdate.Binding{}
	for _, source := range app.BuildSources {
		id, version, err := domaincatalog.BuildTemplateReference(source.Config)
		if source.Type != domainapp.BuildSourceTypePlatformTemplate || err != nil || id != templateID {
			continue
		}
		env := domaincatalog.ApplicationEnvironment{}
		for _, item := range envs {
			if env.ID == "" || environmentNeedsApproval(item) {
				env = item
			}
		}
		target := targetBase("build", id, app.ID+":"+source.ID, app, env)
		target.Scope = "application"
		target.ServiceName = source.Name
		target.FromVersion = version
		parameters, _ := source.Config["variables"].(map[string]any)
		target.IndependentOverrides = sortedKeys(parameters)
		items = append(items, domainupdate.Binding{Target: target, Application: app, Environment: env, Environments: envs, BuildSourceID: source.ID, Parameters: maps.Clone(parameters)})
	}
	return items
}

func (s *Service) workflowBindings(app domainapp.App, envs []domaincatalog.ApplicationEnvironment, templateID string) []domainupdate.Binding {
	items := []domainupdate.Binding{}
	for _, env := range envs {
		if env.WorkflowTemplateID != templateID {
			continue
		}
		target := targetBase("workflow", templateID, env.ID, app, env)
		target.FromVersion = env.WorkflowTemplateVersion
		items = append(items, domainupdate.Binding{Target: target, Application: app, Environment: env, Environments: []domaincatalog.ApplicationEnvironment{env}, Parameters: map[string]any{}})
	}
	return items
}

func (s *Service) deploymentBindings(ctx context.Context, p domainidentity.Principal, app domainapp.App, envs []domaincatalog.ApplicationEnvironment, templateID string) ([]domainupdate.Binding, error) {
	services, err := s.deps.Applications.ListServices(ctx, p, app.ID)
	if err != nil {
		return nil, err
	}
	items := []domainupdate.Binding{}
	for _, service := range services {
		ref := service.DeploymentTemplate
		if ref == nil || ref.TemplateID != templateID {
			continue
		}
		for _, env := range envs {
			for _, releaseTarget := range env.Targets {
				if releaseTarget.Metadata["serviceId"] != service.ID || !releaseTarget.Enabled {
					continue
				}
				binding := domainupdate.Binding{Application: app, Service: service, Environment: env, Environments: []domaincatalog.ApplicationEnvironment{env}, ReleaseTarget: releaseTarget, Parameters: maps.Clone(ref.Parameters)}
				binding.Target = targetBase("deployment", templateID, env.ID+":"+releaseTarget.ID, app, env)
				binding.Target.ServiceID, binding.Target.ServiceName, binding.Target.ReleaseTargetID, binding.Target.FromVersion = service.ID, service.Name, releaseTarget.ID, ref.Version
				if ref.Detached {
					binding.Target.Eligible = false
					binding.Target.Reason = "独立副本不参与模板传播"
				}
				if releaseTarget.Helm != nil {
					s.helmBinding(&binding)
				} else if ref.ManifestPackageID != "" {
					if err := s.manifestBinding(ctx, p, &binding); err != nil {
						if errors.Is(err, apperrors.ErrAccessDenied) {
							continue
						}
						return nil, err
					}
				} else {
					binding.Target.Eligible = false
					binding.Target.Reason = "服务尚未关联受管 Manifest 或 Helm 配置"
				}
				binding.Target.IndependentOverrides = sortedKeys(binding.Parameters)
				if releaseTarget.Helm != nil && releaseTarget.Helm.Values != nil {
					for _, key := range sortedKeys(map[string]any(*releaseTarget.Helm.Values)) {
						binding.Target.IndependentOverrides = append(binding.Target.IndependentOverrides, "helm.values."+key)
					}
				}
				for _, key := range sortedKeys(binding.EnvironmentParameters) {
					binding.Target.IndependentOverrides = append(binding.Target.IndependentOverrides, "environment."+key)
				}
				if binding.ManifestBinding.Kustomize != nil {
					binding.Target.IndependentOverrides = append(binding.Target.IndependentOverrides, "render.kustomize")
				}
				items = append(items, binding)
			}
		}
	}
	return items, nil
}

func (s *Service) manifestBinding(ctx context.Context, p domainidentity.Principal, b *domainupdate.Binding) error {
	item, err := s.deps.Manifests.GetTemplatePackage(ctx, p, b.Service.DeploymentTemplate.ManifestPackageID)
	if err != nil {
		return err
	}
	bindings, err := s.deps.Manifests.ListBindings(ctx, p, item.ID)
	if err != nil {
		return err
	}
	b.Package = item
	for _, binding := range bindings {
		if binding.ID != b.ReleaseTarget.ConfigRef || binding.ApplicationEnvironmentID != b.Environment.ID {
			continue
		}
		b.ManifestBinding = binding
		b.EnvironmentParameters = maps.Clone(binding.TemplateParameters)
		if selection := binding.TemplateSelection; selection != nil {
			b.Target.FromVersion = selection.Version
			b.Parameters = maps.Clone(selection.Parameters)
			if selection.TemplateID != b.Target.TemplateID || selection.BaseFilesDigest != digest(item.Files) {
				b.Target.Eligible = false
				b.Target.Reason = "Manifest 来源已独立修改，先核对差异"
			}
		}
		if !binding.Enabled || len(binding.Overlay) > 0 {
			b.Target.Eligible = false
			b.Target.Reason = "环境已禁用或存在独立文件覆盖"
		}
		return nil
	}
	b.Target.Eligible = false
	b.Target.Reason = "部署目标未关联到同一环境的 Manifest 绑定"
	return nil
}

func (s *Service) helmBinding(b *domainupdate.Binding) {
	data, _ := json.Marshal(b.ReleaseTarget.Metadata["templateSelection"])
	var selection domainmanifest.TemplateSelection
	if json.Unmarshal(data, &selection) == nil && selection.TemplateID != "" {
		b.Target.FromVersion = selection.Version
		b.Parameters = selection.Parameters
		if selection.TemplateID != b.Target.TemplateID || selection.BaseFilesDigest != digest(b.ReleaseTarget.Helm.Source) {
			b.Target.Eligible = false
			b.Target.Reason = "Helm 配置已独立修改，先核对差异"
		}
	}
}

func sortedKeys(values map[string]any) []string {
	keys := slices.Collect(maps.Keys(values))
	slices.Sort(keys)
	if keys == nil {
		return []string{}
	}
	return keys
}

func bindingRevision(b domainupdate.Binding) string {
	// Policy labels and display names do not invalidate a frozen configuration.
	environments := []any{}
	for _, env := range b.Environments {
		environments = append(environments, environmentScope(env))
	}
	appScope := []string{b.Application.ID, b.Application.Group, b.Application.BusinessLineID}
	if b.Target.Kind == "build" {
		for _, source := range b.Application.BuildSources {
			if source.ID == b.BuildSourceID {
				return digest([]any{appScope, source, environments})
			}
		}
	}
	if b.Target.Kind == "workflow" {
		return digest([]any{appScope, environments, b.Environment.WorkflowTemplateID, b.Environment.WorkflowTemplateVersion})
	}
	return digest([]any{appScope, b.Service.Version, environments, b.ReleaseTarget, b.Package, b.ManifestBinding, b.Parameters})
}

func environmentScope(env domaincatalog.ApplicationEnvironment) any {
	return struct {
		ID, ApplicationID, EnvironmentID, EnvironmentKey, ClusterID, Namespace string
		Policy                                                                 domaincatalog.ReleasePolicy
	}{env.ID, env.ApplicationID, env.EnvironmentID, env.EnvironmentKey, env.ClusterID, env.Namespace, env.ReleasePolicy}
}

func (s *Service) findBinding(ctx context.Context, p domainidentity.Principal, target sohaapi.TemplateUpdateTarget, write bool) (domainupdate.Binding, error) {
	items, err := s.bindings(ctx, p, domainupdate.Filter{Kind: target.Kind, TemplateID: target.TemplateID, ApplicationID: target.ApplicationID})
	if err != nil {
		return domainupdate.Binding{}, err
	}
	for _, item := range items {
		if item.Target.ID == target.ID {
			if write {
				err = s.authorizeBinding(ctx, p, item, false)
			}
			return item, err
		}
	}
	return domainupdate.Binding{}, apperrors.ErrNotFound
}

func (s *Service) authorizeBinding(ctx context.Context, p domainidentity.Principal, b domainupdate.Binding, approve bool) error {
	permission := appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationEnvManage, "update")
	if approve {
		permission = appaccess.PermDeliveryApplicationEnvApprove
	}
	for _, env := range b.Environments {
		if approve && !b.Target.RequiresApproval && b.Target.Mode != "approval" {
			continue
		}
		if _, err := s.deps.Environments.AuthorizeApplicationEnvironmentPermission(ctx, p, env.ID, permission); err != nil {
			return err
		}
		if approve && len(env.ReleasePolicy.ApproverRoles) > 0 && !slices.ContainsFunc(env.ReleasePolicy.ApproverRoles, func(role string) bool {
			return slices.ContainsFunc(p.Roles, func(actual string) bool { return strings.EqualFold(actual, role) })
		}) {
			return apperrors.ErrAccessDenied
		}
	}
	return nil
}
