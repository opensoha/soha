import { mutationOptions, queryOptions, type QueryClient } from '@tanstack/react-query'
import { deliveryApi } from '../api'
import { deliveryKeys } from '../keys'
import { manifestKeys } from '../manifests/keys'
import type { BuildTemplate, ServiceDeploymentTemplate, WorkflowTemplate } from '../types'
import { templateUpdateApi, type TemplateUpdateScope } from './api'

const normalized = (scope: TemplateUpdateScope) => ({
  kind: scope.kind,
  templateId: scope.templateId.trim(),
  applicationId: scope.applicationId?.trim() || undefined,
  serviceId: scope.serviceId?.trim() || undefined,
  applicationEnvironmentId: scope.applicationEnvironmentId?.trim() || undefined,
})
export const templateUpdateKeys = {
  all: ['delivery', 'template-updates'] as const,
  targets: (scope: TemplateUpdateScope, page: number, pageSize: number) =>
    [...templateUpdateKeys.all, 'targets', normalized(scope), page, pageSize] as const,
  policies: (scope: TemplateUpdateScope) =>
    [...templateUpdateKeys.all, 'policies', normalized(scope)] as const,
  preview: (id: string) => [...templateUpdateKeys.all, 'preview', id] as const,
}

export const templateUpdateQueries = {
  targets: (scope: TemplateUpdateScope, page: number, pageSize: number, enabled = true) =>
    queryOptions({
      queryKey: templateUpdateKeys.targets(scope, page, pageSize),
      queryFn: () => templateUpdateApi.targets(scope, page, pageSize),
      enabled: enabled && Boolean(scope.templateId),
      retry: false,
    }),
  policies: (scope: TemplateUpdateScope, enabled = true) =>
    queryOptions({
      queryKey: templateUpdateKeys.policies(scope),
      queryFn: () => templateUpdateApi.policies(scope),
      enabled: enabled && Boolean(scope.templateId),
      retry: false,
    }),
  versions: (scope: TemplateUpdateScope, enabled = true) => {
    const group =
      scope.kind === 'build'
        ? 'buildTemplates'
        : scope.kind === 'workflow'
          ? 'workflowTemplates'
          : 'deploymentTemplates'
    return queryOptions<(BuildTemplate | ServiceDeploymentTemplate | WorkflowTemplate)[]>({
      queryKey: deliveryKeys[group].versions(scope.templateId),
      queryFn: () => deliveryApi[group].versions(scope.templateId),
      enabled: enabled && Boolean(scope.templateId),
    })
  },
  preview: (id: string) =>
    queryOptions({
      queryKey: templateUpdateKeys.preview(id),
      queryFn: () => templateUpdateApi.getPreview(id),
      enabled: Boolean(id),
      retry: false,
    }),
}

export const templateUpdateMutations = {
  preview: (client: QueryClient) =>
    mutationOptions({
      mutationFn: templateUpdateApi.preview,
      onSuccess: (data) => client.setQueryData(templateUpdateKeys.preview(data.id), data),
    }),
  batch: (client: QueryClient) =>
    mutationOptions({
      mutationFn: templateUpdateApi.batch,
      onSuccess: async () => {
        await Promise.all(
          [
            templateUpdateKeys.all,
            deliveryKeys.batches.all,
            deliveryKeys.applications.all,
            deliveryKeys.environments.all,
            manifestKeys.all,
          ].map((queryKey) => client.invalidateQueries({ queryKey })),
        )
      },
    }),
  policy: (client: QueryClient) =>
    mutationOptions({
      mutationFn: templateUpdateApi.savePolicy,
      onSuccess: async () => {
        await client.invalidateQueries({ queryKey: templateUpdateKeys.all })
      },
    }),
}
