import { infiniteQueryOptions, queryOptions } from '@tanstack/react-query'

import { workbenchApi } from './api'
import { workbenchKeys } from './keys'

export const workbenchQueries = {
  runtimeProfiles: (owner?: string, enabled = true) =>
    queryOptions({
      queryKey: workbenchKeys.runtimeProfiles(owner),
      queryFn: workbenchApi.runtimeProfiles.list,
      enabled: Boolean(owner) && enabled,
      retry: false,
    }),
  workspaceRuntime: (owner: string, assistantId: string, enabled: boolean) =>
    queryOptions({
      queryKey: workbenchKeys.workspaceRuntime(owner, assistantId),
      queryFn: () => workbenchApi.assistant.workspaceRuntime(assistantId),
      enabled,
      retry: false,
      refetchInterval: 3000,
    }),
  guestToolsOperation: (owner: string, assistantId: string, id: string) =>
    queryOptions({
      queryKey: workbenchKeys.guestToolsOperation(owner, assistantId, id),
      queryFn: () => workbenchApi.assistant.guestToolsOperation(id),
      enabled: !!id,
      retry: false,
      refetchInterval: (query) =>
        ['queued', 'running'].includes(query.state.data?.data?.status ?? '') ? 3000 : false,
    }),
  messageSearch: (owner: string | undefined, query: string, enabled: boolean, personal = false) =>
    infiniteQueryOptions({
      queryKey: workbenchKeys.messageSearch(owner, query, personal),
      initialPageParam: '',
      queryFn: ({ pageParam }) => workbenchApi.searchMessages(query, pageParam, personal),
      getNextPageParam: (page) => page.data?.nextCursor || undefined,
      enabled: !!owner && !!query.trim() && enabled,
      retry: false,
    }),
  messageWindow: (owner: string | undefined, session: string | undefined, anchor: string) =>
    queryOptions({
      queryKey: workbenchKeys.messageWindow(owner, session, anchor),
      queryFn: () => workbenchApi.messageWindow(session!, { around: anchor }),
      enabled: !!owner && !!session && !!anchor,
      retry: false,
    }),
  feed: (owner: string | undefined, enabled: boolean) =>
    infiniteQueryOptions({
      queryKey: workbenchKeys.feed(owner),
      initialPageParam: '',
      queryFn: ({ pageParam }) => workbenchApi.feed.list(pageParam),
      getNextPageParam: (page) => page.data?.nextCursor || undefined,
      enabled: !!owner && enabled,
      retry: false,
      refetchInterval: (query) =>
        !query.state.error &&
        ['queued', 'running'].includes(query.state.data?.pages[0]?.data?.state ?? '')
          ? 3000
          : false,
    }),
  feedItem: (owner: string | undefined, id: string) =>
    queryOptions({
      queryKey: workbenchKeys.feedItem(owner, id),
      queryFn: () => workbenchApi.feed.item(id),
      enabled: !!owner && !!id,
      retry: false,
    }),
  personalEnvironmentTemplates: (owner?: string, assistantId?: string, enabled = true) =>
    queryOptions({
      queryKey: workbenchKeys.personalEnvironmentTemplates(owner, assistantId),
      queryFn: workbenchApi.assistant.environmentTemplates,
      enabled: Boolean(owner && assistantId) && enabled,
      retry: false,
    }),
  personalEnvironment: (owner?: string, assistantId?: string, enabled = true) =>
    queryOptions({
      queryKey: workbenchKeys.personalEnvironment(owner, assistantId),
      queryFn: () => workbenchApi.assistant.environment(assistantId!),
      enabled: Boolean(owner && assistantId) && enabled,
      retry: false,
      refetchInterval: 5000,
    }),
  personalArtifacts: (owner?: string, enabled = true) =>
    queryOptions({
      queryKey: workbenchKeys.personalArtifacts(owner),
      queryFn: workbenchApi.assistant.artifacts,
      enabled: Boolean(owner) && enabled,
      refetchInterval: 5000,
    }),
  assistant: (owner?: string, enabled = true) =>
    queryOptions({
      queryKey: workbenchKeys.assistant(owner),
      queryFn: workbenchApi.assistant.ensure,
      enabled: Boolean(owner) && enabled,
      staleTime: 15000,
    }),
  personalTasks: (owner?: string, enabled = true) =>
    queryOptions({
      queryKey: workbenchKeys.personalTasks(owner),
      queryFn: workbenchApi.assistant.tasks,
      enabled: Boolean(owner) && enabled,
      refetchInterval: 5000,
    }),
  sessions: {
    main: (provider: string, owner?: string, enabled = true) =>
      queryOptions({
        queryKey: workbenchKeys.sessions.main(owner),
        queryFn: () => workbenchApi.sessions.main(provider),
        enabled,
        staleTime: Infinity,
      }),
    all: (enabled = true, personal = false, owner?: string) =>
      queryOptions({
        queryKey: workbenchKeys.sessions.all(personal, owner),
        queryFn: personal ? workbenchApi.personalAgent.sessions : workbenchApi.sessions.all,
        enabled,
      }),
    detail: (sessionId?: string, owner?: string) =>
      queryOptions({
        queryKey: workbenchKeys.sessions.detail(sessionId, owner),
        queryFn: () => workbenchApi.sessions.detail(sessionId!),
        enabled: Boolean(sessionId),
      }),
    messages: (sessionId?: string, owner?: string) =>
      queryOptions({
        queryKey: workbenchKeys.sessions.messages(sessionId, owner),
        queryFn: () => workbenchApi.sessions.messages(sessionId!),
        enabled: Boolean(sessionId),
      }),
  },
  catalog: () =>
    queryOptions({
      queryKey: workbenchKeys.catalog(),
      queryFn: workbenchApi.catalog,
    }),
  agentRuns: {
    request: (owner: string | undefined, sessionId: string, requestId: string) =>
      queryOptions({
        queryKey: workbenchKeys.agentRuns.request(owner, sessionId, requestId),
        queryFn: () => workbenchApi.agentRuns.findRequest(sessionId, requestId),
        retry: false,
        refetchInterval: 5000,
      }),
    events: (runId: string, afterSequence: number) =>
      queryOptions({
        queryKey: workbenchKeys.agentRuns.events(runId, afterSequence),
        queryFn: async () => ({
          runId,
          afterSequence,
          page: (await workbenchApi.agentRuns.events(runId, afterSequence)).data,
        }),
        refetchInterval: (query) => (query.state.error ? false : 2500),
        gcTime: 30000,
      }),
    all: () =>
      queryOptions({
        queryKey: workbenchKeys.agentRuns.all(),
        queryFn: workbenchApi.agentRuns.all,
      }),
    session: (sessionId?: string) =>
      queryOptions({
        queryKey: workbenchKeys.agentRuns.session(sessionId),
        queryFn: () => workbenchApi.agentRuns.session(sessionId!),
        enabled: Boolean(sessionId),
      }),
  },
}
