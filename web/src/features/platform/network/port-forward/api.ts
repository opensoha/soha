import { api } from '@/services/api-client'
import type { ApiResponse, ScopeKey } from '@/types'
import { buildPortForwardItemPath, buildPortForwardListPath } from './paths'
import type { PortForwardDraft, PortForwardSession, PortForwardTarget } from './types'

export async function listPortForwards(scope: ScopeKey): Promise<PortForwardSession[]> {
  const response = await api.get<ApiResponse<PortForwardSession[]>>(buildPortForwardListPath(scope))
  return response.data ?? []
}

export async function registerPortForward(draft: PortForwardDraft): Promise<PortForwardSession> {
  const response = await api.post<ApiResponse<PortForwardSession>>(
    buildPortForwardListPath(draft.scope),
    {
      targetKind: draft.targetKind,
      targetName: draft.targetName,
      namespace: draft.namespace,
      localPort: draft.localPort,
      remotePort: draft.remotePort,
    },
  )
  return response.data
}

export async function stopPortForward(target: PortForwardTarget): Promise<void> {
  await api.delete<unknown>(buildPortForwardItemPath(target.scope, target.sessionId))
}
