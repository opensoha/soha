import { api } from '@/services/api-client'
import type {
  CLIDeviceConfirmationEnvelope,
  CLIDeviceDecisionRequest,
  CLIDeviceAuthorizationStatusEnvelope,
  CLIDeviceInspectRequest,
  CLISessionPageEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'

export const cliAuthApi = {
  inspect: async (input: CLIDeviceInspectRequest, signal: AbortSignal) => {
    const response = await api.postWithSignal<CLIDeviceConfirmationEnvelope>(
      '/auth/cli/device-authorizations/inspect',
      input,
      signal,
    )
    return response.data
  },
  decide: async (input: CLIDeviceDecisionRequest, signal: AbortSignal) => {
    const response = await api.postWithSignal<CLIDeviceAuthorizationStatusEnvelope>(
      '/auth/cli/device-authorizations/decision',
      input,
      signal,
    )
    return response.data
  },
  sessions: async (cursor?: string) => {
    const query = new URLSearchParams({ limit: '20' })
    if (cursor) query.set('cursor', cursor)
    const response = await api.get<CLISessionPageEnvelope>(`/auth/cli/sessions?${query}`)
    return response.data
  },
  revokeSession: (id: string) =>
    api.postVoid(`/auth/cli/sessions/${encodeURIComponent(id)}/revoke`),
}
