import { workbenchKeys } from '../workbench/keys'

export const observeKeys = {
  all: ['copilot', 'observe'] as const,
  overview: {
    sessions: () => ['ai-observe-overview-sessions'] as const,
    insights: () => ['ai-observe-overview-insights'] as const,
    analysisRuns: () => ['ai-observe-overview-runs'] as const,
    inspectionRuns: () => ['ai-observe-overview-inspection-runs'] as const,
  },
  operations: {
    tasks: () => ['ai-operations-tasks'] as const,
    runs: () => ['ai-operations-runs'] as const,
    policies: () => ['ai-operations-policies'] as const,
    catalog: () => ['ai-operations-workbench-catalog'] as const,
  },
  tools: {
    catalog: () => ['ai-tools-catalog'] as const,
    session: workbenchKeys.sessions.detail,
    sessions: workbenchKeys.sessions.all,
  },
}

export const observeMutationKeys = {
  operations: (action: string) => ['copilot', 'observe', 'operations', action] as const,
  tools: (action: string) => ['copilot', 'observe', 'tools', action] as const,
}
