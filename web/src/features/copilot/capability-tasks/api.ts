import { api } from '@/services/api-client'
import type {
  CapabilityTaskEnvelope,
  CapabilityTaskInput,
  CapabilityTaskListEnvelope,
  CapabilityTaskRevisionInput,
  CapabilityPlanValidationEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type {
  CapabilityTask,
  CapabilityTaskInput,
  CapabilityTaskRevisionInput,
} from '@opensoha/contracts/gen/ts/sohaapi'

const taskPath = (id: string) => `/ai-gateway/tasks/${encodeURIComponent(id)}`
export const capabilityTasksApi = {
  list: () => api.getEnvelope<CapabilityTaskListEnvelope>('/ai-gateway/tasks?limit=100'),
  get: (id: string, version = 0) =>
    api.get<CapabilityTaskEnvelope>(`${taskPath(id)}${version ? `?planVersion=${version}` : ''}`),
  validate: (input: CapabilityTaskInput) =>
    api.post<CapabilityPlanValidationEnvelope>('/ai-gateway/plans/validate', input),
  create: (input: CapabilityTaskInput) =>
    api.post<CapabilityTaskEnvelope>('/ai-gateway/tasks', input),
  resume: ({ id, input }: { id: string; input: CapabilityTaskRevisionInput }) =>
    api.post<CapabilityTaskEnvelope>(`${taskPath(id)}/resume`, input),
  cancel: (id: string) => api.post<CapabilityTaskEnvelope>(`${taskPath(id)}/cancel`),
}
