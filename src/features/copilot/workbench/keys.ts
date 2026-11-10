export const workbenchKeys = {
  runtimeProfiles: (owner?: string) =>
    owner ? (['copilot-runtime-profiles', owner] as const) : (['copilot-runtime-profiles'] as const),
  personalAgentAccess: (owner?: string) => ['personal-agent-access', owner] as const,
  personalAgentAccessRequests: (owner?: string) =>
    owner === undefined
      ? (['personal-agent-access-requests'] as const)
      : (['personal-agent-access-requests', owner] as const),
  messageSearch: (owner: string | undefined, query: string, personal = false) =>
    ['copilot-message-search', owner, personal, query] as const,
  messageWindow: (owner: string | undefined, session: string | undefined, anchor: string) =>
    ['copilot-message-window', owner, session, anchor] as const,
  feed: (owner?: string) => ['copilot-personal-feed', owner] as const,
  feedItem: (owner: string | undefined, id: string) =>
    ['copilot-personal-feed-item', owner, id] as const,
  personalEnvironmentRoot: () => ['copilot-personal-environment'] as const,
  workspaceRuntime: (owner: string, assistantId: string) =>
    ['copilot-workspace-runtime', owner, assistantId] as const,
  guestToolsOperation: (owner: string, assistantId: string, id: string) =>
    ['copilot-guest-tools-operation', owner, assistantId, id] as const,
  personalEnvironmentTemplates: (owner?: string, assistantId?: string) =>
    ['copilot-personal-environment-templates', owner, assistantId] as const,
  personalEnvironment: (owner?: string, assistantId?: string) =>
    ['copilot-personal-environment', owner, assistantId] as const,
  assistant: (owner?: string) => ['copilot-personal-assistant', owner] as const,
  personalTasks: (owner?: string) => ['copilot-personal-tasks', owner] as const,
  personalArtifacts: (owner?: string) => ['copilot-personal-artifacts', owner] as const,
  sessions: {
    main: (owner?: string) => ['copilot-main-chat', owner] as const,
    all: (personal?: boolean, owner?: string) =>
      personal === undefined
        ? (['copilot-workbench-sessions'] as const)
        : owner === undefined
          ? (['copilot-workbench-sessions', personal] as const)
          : (['copilot-workbench-sessions', personal, owner] as const),
    detail: (sessionId?: string, owner?: string) =>
      owner
        ? (['copilot-workbench-session-detail', sessionId, owner] as const)
        : (['copilot-workbench-session-detail', sessionId] as const),
    messages: (sessionId?: string, owner?: string) =>
      owner
        ? (['copilot-workbench-messages', sessionId, owner] as const)
        : (['copilot-workbench-messages', sessionId] as const),
  },
  catalog: () => ['copilot-workbench-catalog'] as const,
  agentRuns: {
    request: (owner: string | undefined, sessionId: string, requestId: string) =>
      ['copilot-run-request', owner, sessionId, requestId] as const,
    all: () => ['copilot-agent-runs'] as const,
    session: (sessionId?: string) => ['copilot-agent-runs', sessionId] as const,
    events: (runId: string, afterSequence: number) =>
      ['copilot-agent-runs', 'events', runId, afterSequence] as const,
  },
}

export const workbenchMutationKeys = {
  feed: (action: string) => ['copilot', 'personal-feed', action] as const,
  sessions: (action: string) => ['copilot', 'workbench', 'sessions', action] as const,
}
