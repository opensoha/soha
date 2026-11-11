import type {
  EndpointDeviceFilters,
  NetworkAccessGrantFilters,
  NetworkAccessPolicyFilters,
  NetworkGatewayFilters,
  NetworkMihomoProfileFilters,
  NetworkNASBindingFilters,
  NetworkResourceFilters,
  NetworkRuntimeEnrollmentFilters,
  NetworkSiteFilters,
  NetworkSiteProfileBindingFilters,
  NetworkSessionFilters,
  NetworkSpaceFilters,
  NetworkTelemetrySummaryFilters,
} from './api'

const text = (value?: string) => value?.trim() ?? ''

export const networkAccessKeys = {
  all: ['network-access'] as const,
  workloadBindings: ['network-access', 'workload-bindings'] as const,
  devices: {
    all: ['network-access', 'devices'] as const,
    list: (filters: EndpointDeviceFilters) =>
      [
        'network-access',
        'devices',
        'list',
        {
          limit: filters.limit,
          ownerUserId: text(filters.ownerUserId),
          search: text(filters.search),
          siteId: text(filters.siteId),
          status: text(filters.status),
        },
      ] as const,
  },
  sites: {
    all: ['network-access', 'sites'] as const,
    list: (filters: NetworkSiteFilters) =>
      [
        'network-access',
        'sites',
        'list',
        { limit: filters.limit, search: text(filters.search), status: text(filters.status) },
      ] as const,
  },
  spaces: {
    all: ['network-access', 'spaces'] as const,
    list: (filters: NetworkSpaceFilters) =>
      [
        'network-access',
        'spaces',
        'list',
        {
          limit: filters.limit,
          search: text(filters.search),
          siteId: text(filters.siteId),
          status: text(filters.status),
        },
      ] as const,
  },
  resources: {
    all: ['network-access', 'resources'] as const,
    list: (filters: NetworkResourceFilters) =>
      [
        'network-access',
        'resources',
        'list',
        {
          kind: text(filters.kind),
          limit: filters.limit,
          protected: filters.protected,
          search: text(filters.search),
          spaceId: text(filters.spaceId),
        },
      ] as const,
  },
  gateways: {
    all: ['network-access', 'gateways'] as const,
    list: (filters: NetworkGatewayFilters) =>
      [
        'network-access',
        'gateways',
        'list',
        {
          limit: filters.limit,
          search: text(filters.search),
          siteId: text(filters.siteId),
          status: text(filters.status),
        },
      ] as const,
  },
  mihomoProfiles: {
    all: ['network-access', 'mihomo-profiles'] as const,
    list: (filters: NetworkMihomoProfileFilters) =>
      [
        'network-access',
        'mihomo-profiles',
        'list',
        {
          deviceId: text(filters.deviceId),
          limit: filters.limit,
          mode: text(filters.mode),
          search: text(filters.search),
          status: text(filters.status),
        },
      ] as const,
  },
  telemetry: {
    all: ['network-access', 'telemetry'] as const,
    summary: (filters: NetworkTelemetrySummaryFilters) =>
      [
        'network-access',
        'telemetry',
        'summary',
        {
          from: text(filters.from),
          limit: filters.limit,
          producerId: text(filters.producerId),
          to: text(filters.to),
        },
      ] as const,
  },
  nasBindings: {
    all: ['network-access', 'nas-bindings'] as const,
    list: (filters: NetworkNASBindingFilters) =>
      [
        'network-access',
        'nas-bindings',
        'list',
        {
          limit: filters.limit,
          runtimeId: text(filters.runtimeId),
          siteId: text(filters.siteId),
          status: text(filters.status),
        },
      ] as const,
  },
  siteProfileBindings: {
    all: ['network-access', 'site-profile-bindings'] as const,
    list: (filters: NetworkSiteProfileBindingFilters) =>
      [
        'network-access',
        'site-profile-bindings',
        'list',
        {
          accessProfile: text(filters.accessProfile),
          limit: filters.limit,
          siteId: text(filters.siteId),
        },
      ] as const,
  },
  sessions: {
    all: ['network-access', 'sessions'] as const,
    list: (filters: NetworkSessionFilters) =>
      [
        'network-access',
        'sessions',
        'list',
        {
          deviceId: text(filters.deviceId),
          limit: filters.limit,
          runtimeId: text(filters.runtimeId),
          siteId: text(filters.siteId),
          status: text(filters.status),
          subjectId: text(filters.subjectId),
        },
      ] as const,
  },
  enrollments: {
    all: ['network-access', 'enrollments'] as const,
    list: (filters: NetworkRuntimeEnrollmentFilters) =>
      ['network-access', 'enrollments', 'list', { limit: filters.limit }] as const,
  },
  accessGrants: {
    all: ['network-access', 'access-grants'] as const,
    list: (filters: NetworkAccessGrantFilters) =>
      [
        'network-access',
        'access-grants',
        'list',
        {
          deviceId: text(filters.deviceId),
          limit: filters.limit,
          status: text(filters.status),
          subjectId: text(filters.subjectId),
        },
      ] as const,
  },
  policies: {
    all: ['network-access', 'policies'] as const,
    list: (filters: NetworkAccessPolicyFilters) =>
      [
        'network-access',
        'policies',
        'list',
        {
          effect: text(filters.effect),
          enabled: filters.enabled,
          limit: filters.limit,
          search: text(filters.search),
        },
      ] as const,
  },
  policySnapshot: ['network-access', 'policy-snapshot'] as const,
}

export const networkAccessMutationKeys = {
  deviceUpdate: ['network-access', 'mutation', 'device-update'] as const,
  siteCreate: ['network-access', 'mutation', 'site-create'] as const,
  siteUpdate: ['network-access', 'mutation', 'site-update'] as const,
  siteDelete: ['network-access', 'mutation', 'site-delete'] as const,
  spaceCreate: ['network-access', 'mutation', 'space-create'] as const,
  spaceUpdate: ['network-access', 'mutation', 'space-update'] as const,
  spaceDelete: ['network-access', 'mutation', 'space-delete'] as const,
  resourceCreate: ['network-access', 'mutation', 'resource-create'] as const,
  resourceUpdate: ['network-access', 'mutation', 'resource-update'] as const,
  resourceDelete: ['network-access', 'mutation', 'resource-delete'] as const,
  gatewayCreate: ['network-access', 'mutation', 'gateway-create'] as const,
  gatewayUpdate: ['network-access', 'mutation', 'gateway-update'] as const,
  mihomoProfileCreate: ['network-access', 'mutation', 'mihomo-profile-create'] as const,
  mihomoProfileUpdate: ['network-access', 'mutation', 'mihomo-profile-update'] as const,
  mihomoProfileDelete: ['network-access', 'mutation', 'mihomo-profile-delete'] as const,
  policyCreate: ['network-access', 'mutation', 'policy-create'] as const,
  policyUpdate: ['network-access', 'mutation', 'policy-update'] as const,
  policyDelete: ['network-access', 'mutation', 'policy-delete'] as const,
  policyCompile: ['network-access', 'mutation', 'policy-compile'] as const,
  policyPreview: ['network-access', 'mutation', 'policy-preview'] as const,
  admissionPreview: ['network-access', 'mutation', 'admission-preview'] as const,
  policyConflictAnalysis: ['network-access', 'mutation', 'policy-conflict-analysis'] as const,
  enrollmentCreate: ['network-access', 'mutation', 'enrollment-create'] as const,
  enrollmentRevoke: ['network-access', 'mutation', 'enrollment-revoke'] as const,
  accessGrantCreate: ['network-access', 'mutation', 'access-grant-create'] as const,
  accessGrantRevoke: ['network-access', 'mutation', 'access-grant-revoke'] as const,
  nasBindingCreate: ['network-access', 'mutation', 'nas-binding-create'] as const,
  nasBindingUpdate: ['network-access', 'mutation', 'nas-binding-update'] as const,
  nasBindingDelete: ['network-access', 'mutation', 'nas-binding-delete'] as const,
  siteProfileBindingCreate: ['network-access', 'mutation', 'site-profile-binding-create'] as const,
  siteProfileBindingUpdate: ['network-access', 'mutation', 'site-profile-binding-update'] as const,
  siteProfileBindingDelete: ['network-access', 'mutation', 'site-profile-binding-delete'] as const,
  sessionActionPlan: ['network-access', 'mutation', 'session-action-plan'] as const,
  sessionActionExecute: ['network-access', 'mutation', 'session-action-execute'] as const,
}

export const vpnKeys = {
  all: ['network-access', 'vpn'] as const,
  documents: (kind: 'profiles' | 'selection-policies') => ['network-access', 'vpn', kind] as const,
  dashboard: (filters: Record<string, string>) => [...vpnKeys.all, 'dashboard', filters] as const,
  decision: (id: string) => [...vpnKeys.all, 'decision', id] as const,
  revocation: (id: string) => [...vpnKeys.all, 'revocation', id] as const,
  revisions: (kind: 'profiles' | 'selection-policies', id: string | undefined) =>
    [...vpnKeys.documents(kind), 'revisions', id] as const,
}

export const endpointControlKeys = {
  project: (id: string) => ['network-access', 'endpoint-controls', id] as const,
  detail: (id: string, viewer: string, scope: string) =>
    [...endpointControlKeys.project(id), viewer, scope] as const,
}
