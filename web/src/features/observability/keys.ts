const observabilityRoot = ['observability'] as const
const monitoringRoot = [...observabilityRoot, 'monitoring'] as const
const eventRoot = [...observabilityRoot, 'events'] as const
const integrationRoot = [...observabilityRoot, 'integrations'] as const
const notificationRoot = [...observabilityRoot, 'notifications'] as const
const ruleRoot = [...observabilityRoot, 'rules'] as const
const alertRoot = [...observabilityRoot, 'alerts'] as const
const oncallRoot = [...observabilityRoot, 'oncall'] as const
const healingRoot = [...observabilityRoot, 'healing'] as const
const logRoot = [...observabilityRoot, 'logs'] as const
const signalRoot = [...observabilityRoot, 'signals'] as const

function normalizedId(value: string) {
  return value.trim()
}

export const observabilityKeys = {
  all: observabilityRoot,
  monitoring: {
    all: monitoringRoot,
    summary: () => [...monitoringRoot, 'summary'] as const,
  },
  events: {
    all: eventRoot,
    lists: () => [...eventRoot, 'list'] as const,
    list: () => [...eventRoot, 'list'] as const,
  },
  integrations: {
    all: integrationRoot,
    lists: () => [...integrationRoot, 'list'] as const,
    list: () => [...integrationRoot, 'list'] as const,
    detail: (integrationId: string) =>
      [...integrationRoot, 'detail', normalizedId(integrationId)] as const,
  },
  notifications: {
    all: notificationRoot,
    policies: () => [...notificationRoot, 'policies'] as const,
    templates: () => [...notificationRoot, 'templates'] as const,
    channels: () => [...notificationRoot, 'channels'] as const,
    routes: () => [...notificationRoot, 'routes'] as const,
    silences: () => [...notificationRoot, 'silences'] as const,
    previewEvents: () => [...notificationRoot, 'preview-events'] as const,
    oncallSchedules: () => [...notificationRoot, 'oncall-schedules'] as const,
    oncallPolicies: () => [...notificationRoot, 'oncall-policies'] as const,
    preview: (policyId: string, eventId: string) =>
      [...notificationRoot, 'preview', normalizedId(policyId), normalizedId(eventId)] as const,
  },
  rules: {
    all: ruleRoot,
    lists: () => [...ruleRoot, 'list'] as const,
    list: () => [...ruleRoot, 'list'] as const,
    detail: (ruleId: string) => [...ruleRoot, 'detail', normalizedId(ruleId)] as const,
    runs: (ruleId: string) => [...ruleRoot, 'runs', normalizedId(ruleId)] as const,
  },
  alerts: {
    all: alertRoot,
    lists: () => [...alertRoot, 'list'] as const,
    list: () => [...alertRoot, 'list'] as const,
    recent: (limit: number, clusterId?: string) =>
      [...alertRoot, 'list', 'recent', limit, normalizedId(clusterId ?? '')] as const,
    detail: (eventId: string) => [...alertRoot, 'detail', normalizedId(eventId)] as const,
    healingRuns: (eventId: string) =>
      [...alertRoot, 'detail', normalizedId(eventId), 'healing-runs'] as const,
    preview: (eventId: string, policyId: string) =>
      [...alertRoot, 'detail', normalizedId(eventId), 'preview', normalizedId(policyId)] as const,
    deliveryLogs: (eventId: string) =>
      [...alertRoot, 'detail', normalizedId(eventId), 'delivery-logs'] as const,
    healingPolicies: () => [...alertRoot, 'healing-policies'] as const,
  },
  oncall: {
    all: oncallRoot,
    schedules: () => [...oncallRoot, 'schedules'] as const,
    rotations: () => [...oncallRoot, 'rotations'] as const,
    escalationPolicies: () => [...oncallRoot, 'escalation-policies'] as const,
    routes: () => [...oncallRoot, 'routes'] as const,
    tasks: () => [...oncallRoot, 'tasks', 'active'] as const,
    users: () => [...oncallRoot, 'users'] as const,
  },
  healing: {
    all: healingRoot,
    policies: () => [...healingRoot, 'policies'] as const,
    runs: () => [...healingRoot, 'runs'] as const,
    recentRuns: (limit: number) => [...healingRoot, 'runs', 'recent', limit] as const,
  },
  logs: {
    all: logRoot,
    providers: () => [...logRoot, 'providers'] as const,
    dataSources: () => [...logRoot, 'data-sources'] as const,
    snapshot: (clusterId: string, query: unknown) =>
      [...logRoot, 'snapshot', normalizedId(clusterId), query] as const,
  },
  signals: {
    all: signalRoot,
    metricCatalog: () => [...signalRoot, 'metric-catalog'] as const,
    metrics: (input: unknown) => [...signalRoot, 'metrics', input] as const,
    traces: (input: unknown) => [...signalRoot, 'traces', input] as const,
    services: (query: unknown) => [...signalRoot, 'services', query] as const,
    service: (serviceId: string, query: unknown) =>
      [...signalRoot, 'service', normalizedId(serviceId), query] as const,
    topology: (serviceId: string, query: unknown) =>
      [...signalRoot, 'topology', normalizedId(serviceId), query] as const,
  },
  legacy: {
    monitoringOverviewIntegrations: ['monitoring-overview-integrations'] as const,
  },
}

export const observabilityMutationKeys = {
  logs: (action: 'create-data-source' | 'update-data-source' | 'validate-data-source') =>
    [...logRoot, 'mutation', action] as const,
  integrations: {
    create: [...integrationRoot, 'mutation', 'create'] as const,
    update: [...integrationRoot, 'mutation', 'update'] as const,
    test: [...integrationRoot, 'mutation', 'test'] as const,
  },
  notifications: (resource: string, action: 'create' | 'update' | 'preview') =>
    [...notificationRoot, 'mutation', resource, action] as const,
  rules: (action: 'create' | 'update' | 'test') => [...ruleRoot, 'mutation', action] as const,
  alerts: (action: 'acknowledge' | 'resolve' | 'heal') =>
    [...alertRoot, 'mutation', action] as const,
  oncall: (resource: string, action: 'create' | 'update') =>
    [...oncallRoot, 'mutation', resource, action] as const,
  healing: (action: 'create' | 'update' | 'approve' | 'reject' | 'retry') =>
    [...healingRoot, 'mutation', action] as const,
}
