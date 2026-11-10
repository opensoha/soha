import type {
  BuildTemplateInput,
  DeliveryDocumentKind,
  DeliveryWorkflowDefinition,
  ServiceDeploymentTemplateInput,
  WorkflowTemplate,
} from '../types'

const apiVersion = 'delivery.soha.io/v1alpha1'

function metadata(value: { key?: string; name?: string; description?: string }) {
  return {
    name: value.key ?? '',
    displayName: value.name ?? '',
    description: value.description || undefined,
  }
}

export function buildTemplateDocument(value: BuildTemplateInput) {
  return {
    apiVersion,
    kind: 'BuildTemplate' as const,
    metadata: metadata(value),
    spec: {
      builderKind: value.builderKind,
      dockerfileTemplate: value.dockerfileTemplate || undefined,
      buildCommands: value.buildCommands?.length ? value.buildCommands : undefined,
      variableSchema: value.variableSchema,
      defaultVariables: value.defaultVariables,
      enabled: value.enabled,
      migrationPolicy: value.migrationPolicy,
    },
  }
}

export function workflowTemplateDocument(
  value: Pick<
    WorkflowTemplate,
    'key' | 'name' | 'description' | 'category' | 'definition' | 'enabled' | 'migrationPolicy'
  >,
) {
  return {
    apiVersion,
    kind: 'WorkflowTemplate' as const,
    metadata: metadata(value),
    spec: {
      category: value.category || 'release',
      definition: value.definition,
      enabled: value.enabled,
      migrationPolicy: value.migrationPolicy,
    },
  }
}

export function deploymentTemplateDocument(value: ServiceDeploymentTemplateInput) {
  return {
    apiVersion,
    kind: 'DeploymentTemplate' as const,
    metadata: metadata(value),
    spec: {
      source: value.source,
      parameterSchema: value.parameterSchema,
      defaults: value.defaults,
      environmentOverrides: value.environmentOverrides,
      artifacts: value.artifacts,
      health: value.health,
      enabled: value.enabled,
      migrationPolicy: value.migrationPolicy,
    },
  }
}

export function workflowDocument(definition: DeliveryWorkflowDefinition, id = 'workflow') {
  return { apiVersion, kind: 'Workflow' as const, metadata: { name: id }, spec: { definition } }
}

export const documentKindLabels: Record<DeliveryDocumentKind, string> = {
  BuildTemplate: '构建模板',
  DeploymentTemplate: '部署模板',
  WorkflowTemplate: '流程模板',
  Workflow: '工作流',
}
