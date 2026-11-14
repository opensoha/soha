import { api } from '@/services/api-client'
import type {
  DeliveryBatchEnvelope,
  TemplateUpdateBatchInput,
  TemplateUpdateKind,
  TemplateUpdatePolicyEnvelope,
  TemplateUpdatePolicyInput,
  TemplateUpdatePolicyListEnvelope,
  TemplateUpdatePreviewEnvelope,
  TemplateUpdatePreviewInput,
  TemplateUpdateTargetPageEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'

export interface TemplateUpdateScope {
  kind: Exclude<TemplateUpdateKind, 'blueprint'>
  templateId: string
  applicationId?: string
  serviceId?: string
  applicationEnvironmentId?: string
}

function query(scope: TemplateUpdateScope, page?: number, pageSize?: number) {
  const values = new URLSearchParams({ kind: scope.kind, templateId: scope.templateId.trim() })
  if (scope.applicationId) values.set('applicationId', scope.applicationId)
  if (scope.serviceId) values.set('serviceId', scope.serviceId)
  if (scope.applicationEnvironmentId)
    values.set('applicationEnvironmentId', scope.applicationEnvironmentId)
  if (page) values.set('page', String(page))
  if (pageSize) values.set('pageSize', String(pageSize))
  return values.toString()
}

export const templateUpdateApi = {
  targets: async (scope: TemplateUpdateScope, page = 1, pageSize = 20) =>
    (
      await api.get<TemplateUpdateTargetPageEnvelope>(
        `/delivery/template-updates/targets?${query(scope, page, pageSize)}`,
      )
    ).data,
  preview: async (input: TemplateUpdatePreviewInput) =>
    (await api.post<TemplateUpdatePreviewEnvelope>('/delivery/template-updates/previews', input))
      .data,
  getPreview: async (id: string) =>
    (
      await api.get<TemplateUpdatePreviewEnvelope>(
        `/delivery/template-updates/previews/${encodeURIComponent(id)}`,
      )
    ).data,
  batch: async ({ previewId, input }: { previewId: string; input: TemplateUpdateBatchInput }) =>
    (
      await api.post<DeliveryBatchEnvelope>(
        `/delivery/template-updates/previews/${encodeURIComponent(previewId)}/batches`,
        input,
      )
    ).data,
  policies: async (scope: TemplateUpdateScope) =>
    (
      await api.get<TemplateUpdatePolicyListEnvelope>(
        `/delivery/template-updates/policies?${query(scope)}`,
      )
    ).data,
  savePolicy: async (input: TemplateUpdatePolicyInput) =>
    (await api.put<TemplateUpdatePolicyEnvelope>('/delivery/template-updates/policies', input))
      .data,
}
