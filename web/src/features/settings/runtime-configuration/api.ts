import type {
  RuntimeConfigApplicationEnvelope,
  RuntimeConfigApplyResultEnvelope,
  RuntimeConfigChangeRequest,
  RuntimeConfigRevisionListEnvelope,
  RuntimeConfigRollbackRequest,
  RuntimeConfigSnapshotEnvelope,
  RuntimeConfigValidationEnvelope,
  RuntimeResourceSnapshotEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

export const runtimeConfigurationApi = {
  get: async () => (await api.get<RuntimeConfigSnapshotEnvelope>('/settings/runtime-config')).data,
  resources: async () =>
    (await api.get<RuntimeResourceSnapshotEnvelope>('/settings/runtime-config/resources')).data,
  validate: async (input: RuntimeConfigChangeRequest) =>
    (await api.post<RuntimeConfigValidationEnvelope>('/settings/runtime-config/validate', input))
      .data,
  apply: async (input: RuntimeConfigChangeRequest) =>
    (await api.post<RuntimeConfigApplyResultEnvelope>('/settings/runtime-config/apply', input))
      .data,
  history: async () =>
    (await api.getEnvelope<RuntimeConfigRevisionListEnvelope>('/settings/runtime-config/history'))
      .items,
  rollback: async (input: RuntimeConfigRollbackRequest) =>
    (await api.post<RuntimeConfigApplyResultEnvelope>('/settings/runtime-config/rollback', input))
      .data,
  application: async (id: string) =>
    (
      await api.get<RuntimeConfigApplicationEnvelope>(
        `/settings/runtime-config/applications/${encodeURIComponent(id)}`,
      )
    ).data,
}
