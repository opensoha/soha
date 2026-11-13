---
id: onboarding
name: Onboarding
version: 0.1.0
category: delivery
description: New application, environment, cluster, and service onboarding through Soha Gateway.
capabilityRefs:
  - delivery.applications.list
  - delivery.applications.detail
  - delivery.application_environments.list
  - delivery.application_services.list
  - delivery.build_sources.list
  - delivery.release_targets.list
  - k8s.pods.list
  - k8s.services.list
requiredScopes:
  - businessLine
  - application
  - environment
  - cluster
  - namespace
---

# Onboarding

Use this skill when an AI assistant is helping a team verify that an application, environment, service, build source, release target, and runtime namespace are ready to use Soha.

## Operating Contract

- Treat onboarding as discovery and readiness review unless the user explicitly asks for an approved create or change action.
- Use Gateway inventory tools to confirm application, environment, service, build source, release target, cluster, and namespace state.
- Keep business line, application, environment, cluster, namespace, service, and owner explicit.
- Never request credentials, tokens, kubeconfig, or registry passwords from the user.

## Workflow

1. Confirm business line, application, target environment, cluster, and namespace.
2. List and inspect existing applications before deciding that something is missing.
3. Check application environments, services, build sources, and release targets.
4. Check Kubernetes pods and services only inside the requested cluster and namespace.
5. Produce an onboarding checklist with ready, missing, blocked, and needs-owner-review items.
6. If creation is required, stop and hand off to an approved delivery or platform mutation workflow.

## Examples

### Input Example

Check whether `billing-api` is ready for staging deployment in namespace `payments`.

### Expected Tool Calls

- `delivery.applications.list`
- `delivery.applications.detail`
- `delivery.application_environments.list`
- `delivery.application_services.list`
- `delivery.build_sources.list`
- `delivery.release_targets.list`
- `k8s.pods.list`
- `k8s.services.list`

## Permission Boundaries

- Read-only inventory checks are allowed for declared business line, application, environment, cluster, and namespace.
- Create or update steps require a separate approved workflow and owning service.
- Runtime inspection must stay inside Gateway scope.

## Forbidden Actions

- Do not create applications, environments, namespaces, services, or secrets from this skill.
- Do not ask for or expose tokens, registry credentials, kubeconfig, passwords, or private keys.
- Do not infer production readiness without checking release targets and runtime scope.

## Guardrails

- Return a checklist rather than a generic explanation.
- Include missing identifiers and owners when available.
- Mark unavailable Gateway tools as blockers.
- Keep sensitive configuration values out of onboarding notes.
