package aigateway

import (
	"context"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type CapabilityProvider interface {
	Tools() []domainaigateway.ToolCapability
	Resources() []domainaigateway.ResourceCapability
	Prompts() []domainaigateway.PromptCapability
	Skills() []domainaigateway.SkillCapability
}

type ResourceCapabilityRefsProvider interface {
	ResourceCapabilityRefs() []ResourceCapabilityRefs
}

type ToolCapabilityInvoker interface {
	InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error)
}

type ResourceCapabilityRefs struct {
	Resource string
	Tools    []string
	Prompts  []string
	Skills   []string
}

type capabilityRegistry struct {
	providers []CapabilityProvider
}

type staticCapabilityProvider struct{}

func newDefaultCapabilityRegistry() *capabilityRegistry {
	return newCapabilityRegistry(staticCapabilityProvider{})
}

func newCapabilityRegistry(providers ...CapabilityProvider) *capabilityRegistry {
	out := &capabilityRegistry{}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		out.providers = append(out.providers, provider)
	}
	return out
}

func (r *capabilityRegistry) Tools() []domainaigateway.ToolCapability {
	out := make([]domainaigateway.ToolCapability, 0)
	for _, provider := range r.providers {
		out = append(out, provider.Tools()...)
	}
	return out
}

func (r *capabilityRegistry) Resources() []domainaigateway.ResourceCapability {
	out := make([]domainaigateway.ResourceCapability, 0)
	for _, provider := range r.providers {
		out = append(out, provider.Resources()...)
	}
	return out
}

func (r *capabilityRegistry) Prompts() []domainaigateway.PromptCapability {
	out := make([]domainaigateway.PromptCapability, 0)
	for _, provider := range r.providers {
		out = append(out, provider.Prompts()...)
	}
	return out
}

func (r *capabilityRegistry) Skills() []domainaigateway.SkillCapability {
	out := make([]domainaigateway.SkillCapability, 0)
	for _, provider := range r.providers {
		out = append(out, provider.Skills()...)
	}
	return out
}

func (r *capabilityRegistry) ResourceCapabilityRefs() []ResourceCapabilityRefs {
	out := make([]ResourceCapabilityRefs, 0)
	indexes := map[string]int{}
	for _, provider := range r.providers {
		refsProvider, ok := provider.(ResourceCapabilityRefsProvider)
		if !ok {
			continue
		}
		for _, refs := range refsProvider.ResourceCapabilityRefs() {
			resource := normalizeGatewayResourceURI(refs.Resource)
			if resource == "" {
				continue
			}
			index, ok := indexes[resource]
			if !ok {
				index = len(out)
				indexes[resource] = index
				out = append(out, ResourceCapabilityRefs{Resource: resource})
			}
			out[index].Tools = gatewayAppendUniqueStrings(out[index].Tools, refs.Tools...)
			out[index].Prompts = gatewayAppendUniqueStrings(out[index].Prompts, refs.Prompts...)
			out[index].Skills = gatewayAppendUniqueStrings(out[index].Skills, refs.Skills...)
		}
	}
	return out
}

func (r *capabilityRegistry) ResourceRefs(name string) ResourceCapabilityRefs {
	return resourceCapabilityRefsFrom(r.ResourceCapabilityRefs(), name)
}

func (r *capabilityRegistry) ResourceToolRefs(name string) []string {
	return append([]string(nil), r.ResourceRefs(name).Tools...)
}

func (r *capabilityRegistry) ResourcePromptRefs(name string) []string {
	return append([]string(nil), r.ResourceRefs(name).Prompts...)
}

func (r *capabilityRegistry) ResourceSkillRefs(name string) []string {
	return append([]string(nil), r.ResourceRefs(name).Skills...)
}

func (r *capabilityRegistry) ToolByName(name string) (domainaigateway.ToolCapability, bool) {
	name = strings.TrimSpace(name)
	for _, item := range r.Tools() {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.ToolCapability{}, false
}

func (r *capabilityRegistry) ResourceByName(name string) (domainaigateway.ResourceCapability, bool) {
	name = normalizeGatewayResourceURI(name)
	for _, item := range r.Resources() {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.ResourceCapability{}, false
}

func (r *capabilityRegistry) PromptByName(name string) (domainaigateway.PromptCapability, bool) {
	name = strings.TrimSpace(name)
	for _, item := range r.Prompts() {
		if item.Name == name {
			return item, true
		}
	}
	return domainaigateway.PromptCapability{}, false
}

func (r *capabilityRegistry) SkillByID(id string) (domainaigateway.SkillCapability, bool) {
	id = strings.TrimSpace(id)
	for _, item := range r.Skills() {
		if item.ID == id {
			return item, true
		}
	}
	return domainaigateway.SkillCapability{}, false
}

func (r *capabilityRegistry) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, bool, error) {
	for _, provider := range r.providers {
		invoker, ok := provider.(ToolCapabilityInvoker)
		if !ok || !providerHasTool(provider, tool.Name) {
			continue
		}
		output, relatedIDs, err := invoker.InvokeTool(ctx, principal, tool, input)
		return output, relatedIDs, true, err
	}
	return nil, nil, false, nil
}

func (staticCapabilityProvider) Tools() []domainaigateway.ToolCapability {
	return defaultTools()
}

func (staticCapabilityProvider) Resources() []domainaigateway.ResourceCapability {
	return defaultResources()
}

func (staticCapabilityProvider) Prompts() []domainaigateway.PromptCapability {
	return defaultPrompts()
}

func (staticCapabilityProvider) Skills() []domainaigateway.SkillCapability {
	return defaultSkills()
}

func (staticCapabilityProvider) ResourceCapabilityRefs() []ResourceCapabilityRefs {
	return defaultResourceCapabilityRefs()
}

func defaultResourceCapabilityRefs() []ResourceCapabilityRefs {
	return []ResourceCapabilityRefs{
		{
			Resource: "soha://delivery/applications",
			Tools: []string{
				"delivery.applications.list",
				"delivery.applications.detail",
				"delivery.application_environments.list",
				"delivery.application_services.list",
				"delivery.build_sources.list",
				"delivery.release_targets.list",
			},
			Prompts: []string{"soha.delivery.plan_release"},
			Skills:  []string{"delivery-developer"},
		},
		{
			Resource: "soha://delivery/execution-tasks",
			Tools: []string{
				"delivery.release_bundles.list",
				"delivery.execution_tasks.list",
				"delivery.execution_logs.list",
				"delivery.approval_policies.list",
				"delivery.workflow_templates.list",
				"delivery.release_context.diff",
				"delivery.rollback.context",
				"diagnosis.release_failure.analyze",
			},
			Prompts: []string{"soha.delivery.plan_release"},
			Skills:  []string{"delivery-tester", "security-change"},
		},
		{
			Resource: "soha://k8s/runtime",
			Tools: []string{
				"k8s.pods.list",
				"k8s.pods.logs",
				"k8s.pods.describe",
				"k8s.deployments.list",
				"k8s.deployments.rollout_status",
				"k8s.deployments.events",
				"k8s.services.list",
				"k8s.services.backends",
				"k8s.routes.context",
				"k8s.storage.context",
				"k8s.nodes.detail",
				"k8s.events.list",
			},
			Prompts: []string{"soha.k8s.diagnose_workload"},
			Skills:  []string{"k8s-sre", "security-change"},
		},
	}
}

func resourceCapabilityRefsFrom(refs []ResourceCapabilityRefs, name string) ResourceCapabilityRefs {
	name = normalizeGatewayResourceURI(name)
	for _, item := range refs {
		if normalizeGatewayResourceURI(item.Resource) != name {
			continue
		}
		return ResourceCapabilityRefs{
			Resource: name,
			Tools:    gatewayAppendUniqueStrings(nil, item.Tools...),
			Prompts:  gatewayAppendUniqueStrings(nil, item.Prompts...),
			Skills:   gatewayAppendUniqueStrings(nil, item.Skills...),
		}
	}
	return ResourceCapabilityRefs{Resource: name}
}

func providerHasTool(provider CapabilityProvider, name string) bool {
	name = strings.TrimSpace(name)
	for _, item := range provider.Tools() {
		if item.Name == name {
			return true
		}
	}
	return false
}
