import { api } from '@/services/api-client'
import { ApiError } from '@/services/api-error'
import type { ApiResponse } from '@/types'
import type { WorkbenchRuntimeProfile } from './runtime-profile'
import type {
  PersonalAgentAccess,
  WorkbenchMessageSearchPage,
  WorkbenchMessageWindow,
  PersonalFeedPage,
  PersonalFeedItem,
  PersonalFeedSettings,
  PersonalFeedFeedback,
  AgentRunEventPage,
  AgentRunResumeRequest,
  PersonalEnvironment,
  PersonalEnvironmentActionRequest,
  PersonalEnvironmentDataDeletionRequest,
  PersonalEnvironmentTemplate,
  PersonalGuestRuntimeInput,
  PersonalGuestRuntime,
  PersonalWorkspaceRuntime,
  PersonalGuestToolsInput,
  VirtualizationOperation,
} from '@opensoha/contracts/gen/ts/sohaapi'

import type {
  PersonalAssistant,
  PersonalAssistantAvatarInput,
  PersonalArtifact,
  WorkbenchAgentRun,
  WorkbenchCatalog,
  WorkbenchMessage,
  WorkbenchSession,
  WorkbenchSessionScope,
} from './types'

// Published SDKs before the additive export request share these existing fields.
export type PersonalEnvironmentDataExportRequest = Pick<
  PersonalEnvironmentDataDeletionRequest,
  'assistantId' | 'environmentVersion'
>

export interface CreateWorkbenchSessionInput {
  pinnedContext?: Record<string, unknown>
  title: string
  mode: NonNullable<NonNullable<WorkbenchSession['metadata']>['mode']>
  agentProviderId: string
  scope: WorkbenchSessionScope
  tags: string[]
}

export interface PatchWorkbenchSessionInput {
  sessionId: string
  body: Record<string, unknown>
}

export interface CreateInspectionTaskInput {
  sessionId: string
  body: {
    title: string
    scopeType: string
    clusterId?: string
    namespace?: string
    checks: string[]
    enabled: boolean
    intervalMinutes: number
    metadata: Record<string, unknown>
  }
}

export const workbenchApi = {
  runtimeProfiles: {
    list: () => api.get<ApiResponse<WorkbenchRuntimeProfile[]>>('/copilot/workbench/runtime-profiles'),
    select: async ({ sessionId, profileId }: { sessionId: string; profileId: string }) => {
      const path = `/copilot/sessions/${encodeURIComponent(sessionId)}/runtime-profile`
      const response = await api.put<ApiResponse<WorkbenchSession>>(path, { profileId })
      if (
        response.data?.id !== sessionId ||
        (response.data.metadata?.runtimeProfileId ?? '') !== profileId
      ) {
        throw new ApiError(502, '运行配置回执与会话不匹配', { method: 'PUT', path })
      }
      return response
    },
  },
  searchMessages: (query: string, cursor = '', personal = false) =>
    api.get<ApiResponse<WorkbenchMessageSearchPage>>(
      `/copilot/messages/search?${new URLSearchParams({ q: query, workspace: personal ? 'heya' : 'chat', ...(cursor ? { cursor } : {}) })}`,
    ),
  messageWindow: (
    sessionId: string,
    selector: { around?: string; before?: string; after?: string } = {},
  ) =>
    api.get<ApiResponse<WorkbenchMessageWindow>>(
      `/copilot/sessions/${encodeURIComponent(sessionId)}/messages/window?${new URLSearchParams(Object.entries(selector).filter((entry): entry is [string, string] => !!entry[1]))}`,
    ),
  feed: {
    list: (before = '') =>
      api.get<ApiResponse<PersonalFeedPage>>(
        `/copilot/assistant/feed${before ? `?before=${encodeURIComponent(before)}` : ''}`,
      ),
    item: (id: string) =>
      api.get<ApiResponse<PersonalFeedItem>>(
        `/copilot/assistant/feed/items/${encodeURIComponent(id)}`,
      ),
    settings: (input: PersonalFeedSettings) =>
      api.put<ApiResponse<PersonalFeedPage>>('/copilot/assistant/feed/settings', input),
    refresh: () => api.post<ApiResponse<PersonalFeedPage>>('/copilot/assistant/feed/refresh', {}),
    feedback: (id: string, input: PersonalFeedFeedback) =>
      api.patch<ApiResponse<PersonalFeedItem>>(
        `/copilot/assistant/feed/items/${encodeURIComponent(id)}`,
        input,
      ),
  },
  serviceScope: () => api.serviceScope(),
  personalAgent: {
    access: () => api.get<ApiResponse<PersonalAgentAccess>>('/copilot/assistant/access'),
    request: () => api.post<ApiResponse<PersonalAgentAccess>>('/copilot/assistant/access', {}),
    requests: () =>
      api.get<ApiResponse<PersonalAgentAccess[]>>('/copilot/assistant/access/requests'),
    decide: (input: {
      userId: string
      status: 'approved' | 'rejected' | 'revoked'
      version: number
    }) =>
      api.patch<ApiResponse<PersonalAgentAccess>>(
        `/copilot/assistant/access/requests/${encodeURIComponent(input.userId)}`,
        { status: input.status, version: input.version },
      ),
    sessions: () => api.get<ApiResponse<WorkbenchSession[]>>('/copilot/assistant/sessions'),
    create: (input: CreateWorkbenchSessionInput) =>
      api.post<ApiResponse<WorkbenchSession>>('/copilot/assistant/sessions', input),
  },
  assistant: {
    runtime: async (assistantId: string, input: PersonalGuestRuntimeInput) => {
      const result = await api.post<ApiResponse<PersonalGuestRuntime>>('/copilot/assistant/environment/runtime', input)
      if (result.data?.assistantId !== assistantId) throw new Error('运行回执与助手不匹配')
      return result
    },
    workspaceRuntime: async (assistantId: string) => {
      const result = await api.get<ApiResponse<PersonalWorkspaceRuntime>>('/copilot/assistant/environment/runtime')
      if (result.data?.assistantId !== assistantId) throw new Error('运行状态与助手不匹配')
      return result
    },
    runtimeReceipt: async (assistantId: string, id: string) => {
      const result = await api.get<ApiResponse<PersonalGuestRuntime>>(`/copilot/assistant/environment/runtime/${encodeURIComponent(id)}`)
      if (result.data?.assistantId !== assistantId || result.data.id !== id) throw new Error('原运行回执不匹配')
      return result
    },
    guestTools: (input: PersonalGuestToolsInput) => api.post<ApiResponse<VirtualizationOperation>>('/copilot/assistant/environment/guest-tools', input),
    guestToolsOperation: (id: string) => api.get<ApiResponse<VirtualizationOperation>>(`/virtualization/operations/${encodeURIComponent(id)}`),
    exportEnvironmentData: async (
      input: PersonalEnvironmentDataExportRequest,
      signal: AbortSignal,
    ) => {
      const response = await api.postResponse(
        '/copilot/assistant/environment/export',
        input,
        signal,
      )
      const size = Number(response.headers.get('Content-Length'))
      const digest = response.headers.get('X-Content-SHA256') || ''
      if (
        response.headers.get('X-Assistant-ID') !== input.assistantId ||
        !Number.isSafeInteger(size) ||
        size < 1 ||
        size > 256 * 1048576 ||
        !/^[a-f0-9]{64}$/.test(digest) ||
        !response.body
      ) {
        throw new Error('个人数据导出回执不完整或与助手不匹配')
      }
      const reader = response.body.getReader()
      const parts: Uint8Array<ArrayBuffer>[] = []
      let received = 0
      try {
        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          received += value.byteLength
          if (received > size || signal.aborted) throw new Error('个人数据导出已中止或超过边界')
          parts.push(new Uint8Array(value))
        }
      } finally {
        await reader.cancel()
      }
      const blob = new Blob(parts, { type: 'application/x-tar' })
      const actual = Array.from(
        new Uint8Array(await crypto.subtle.digest('SHA-256', await blob.arrayBuffer())),
        (byte) => byte.toString(16).padStart(2, '0'),
      ).join('')
      if (received !== size || actual !== digest || signal.aborted)
        throw new Error('个人数据导出被截断或摘要不匹配')
      return blob
    },
    deleteEnvironmentData: async (input: PersonalEnvironmentDataDeletionRequest) => {
      const result = await api.delete<ApiResponse<PersonalEnvironment>>(
        '/copilot/assistant/environment',
        input,
      )
      if (result.data?.assistantId !== input.assistantId)
        throw new Error('个人环境回执与助手不匹配')
      return result
    },
    environmentTemplates: () =>
      api.get<ApiResponse<PersonalEnvironmentTemplate[]>>(
        '/copilot/assistant/environment/templates',
      ),
    changeEnvironment: async (assistantId: string, input: PersonalEnvironmentActionRequest) => {
      const result = await api.post<ApiResponse<PersonalEnvironment>>(
        '/copilot/assistant/environment',
        input,
      )
      if (result.data?.assistantId !== assistantId) throw new Error('个人环境回执与助手不匹配')
      return result
    },
    environment: async (assistantId: string) => {
      const result = await api.get<ApiResponse<PersonalEnvironment>>(
        '/copilot/assistant/environment',
      )
      if (result.data?.assistantId !== assistantId) throw new Error('个人环境回执与助手不匹配')
      return result
    },
    ensure: () => api.post<ApiResponse<PersonalAssistant>>('/copilot/assistant', {}),
    rename: (name: string) =>
      api.patch<ApiResponse<PersonalAssistant>>('/copilot/assistant', { name }),
    avatar: (input: PersonalAssistantAvatarInput) =>
      api.patch<ApiResponse<PersonalAssistant>>('/copilot/assistant/avatar', input),
    generateAvatar: (provider: string, model: string, prompt: string, signal: AbortSignal) =>
      api.postWithSignal<{ data?: { b64_json?: string; url?: string }[] }>(
        `/ai-gateway/llm/${provider}/v1/images/generations`,
        { model, prompt, n: 1, size: '1024x1024' },
        signal,
      ),
    tasks: () => api.get<ApiResponse<WorkbenchAgentRun[]>>('/copilot/assistant/tasks'),
    artifacts: () => api.get<ApiResponse<PersonalArtifact[]>>('/copilot/assistant/artifacts'),
  },
  sessions: {
    main: (agentProviderId: string) =>
      api.post<ApiResponse<WorkbenchSession>>('/copilot/sessions/main', { agentProviderId }),
    all: () => api.get<ApiResponse<WorkbenchSession[]>>('/copilot/sessions'),
    detail: (sessionId: string) =>
      api.get<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${sessionId}`),
    messages: async (sessionId: string) => {
      const result = await api.get<ApiResponse<WorkbenchMessage[]>>(
        `/copilot/sessions/${sessionId}/messages`,
      )
      if ((result.data?.length ?? 0) < 100) return result
      const latest = await workbenchApi.messageWindow(sessionId)
      return { ...latest, data: latest.data?.messages as WorkbenchMessage[] | undefined }
    },
    create: (input: CreateWorkbenchSessionInput) =>
      api.post<ApiResponse<WorkbenchSession>>('/copilot/sessions', input),
    patch: (input: PatchWorkbenchSessionInput) =>
      api.patch<ApiResponse<WorkbenchSession>>(`/copilot/sessions/${input.sessionId}`, input.body),
    archive: (sessionId: string) => api.delete<void>(`/copilot/sessions/${sessionId}`),
    createInspectionTask: (input: CreateInspectionTaskInput) =>
      api.post<void>(`/copilot/sessions/${input.sessionId}/inspection-task`, input.body),
  },
  catalog: () => api.get<ApiResponse<WorkbenchCatalog>>('/copilot/workbench/catalog'),
  agentRuns: {
    findRequest: async (sessionId: string, requestId: string) => {
      const result = await api.get<ApiResponse<WorkbenchAgentRun>>(
        `/copilot/sessions/${encodeURIComponent(sessionId)}/native-request?requestId=${encodeURIComponent(requestId)}`,
      )
      if (
        result.data?.sessionId !== sessionId ||
        !['native', 'dsh'].includes(result.data?.providerKind ?? '')
      )
        throw new Error('恢复回执与原会话或任务不匹配')
      return result
    },
    events: (runId: string, afterSequence: number) =>
      api.get<ApiResponse<AgentRunEventPage>>(
        `/copilot/agent-runs/${encodeURIComponent(runId)}/events?afterSequence=${afterSequence}`,
      ),
    resume: (runId: string, input: AgentRunResumeRequest) =>
      api.post<ApiResponse<WorkbenchAgentRun>>(
        `/copilot/agent-runs/${encodeURIComponent(runId)}/resume`,
        input,
      ),
    cancel: (runId: string) =>
      api.post<ApiResponse<WorkbenchAgentRun>>(
        `/copilot/agent-runs/${encodeURIComponent(runId)}/cancel`,
        {},
      ),
    all: () => api.get<ApiResponse<WorkbenchAgentRun[]>>('/copilot/agent-runs'),
    session: (sessionId: string) =>
      api.get<ApiResponse<WorkbenchAgentRun[]>>(
        `/copilot/agent-runs?sessionId=${encodeURIComponent(sessionId)}`,
      ),
  },
}
