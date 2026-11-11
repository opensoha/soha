import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type { MFAAdminResetResult, PermissionCatalog } from '@opensoha/contracts/gen/ts/sohaapi'
import { settingsApi } from '@/features/settings'
import type {
  AccessApplicationOption,
  AccessApplicationEnvironment,
  AccessClusterOption,
  AccessMutationValues,
  AccessPolicy,
  AccessRole,
  ResetAccessUserMFAVariables,
  AccessScopeGrant,
  AccessScopeGrantSubject,
  AccessTeam,
  AccessUpdateVariables,
  AccessUser,
} from './types'

async function unwrap<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  const response = await request
  return response.data
}

async function discard(request: Promise<unknown>): Promise<void> {
  await request
}

function resourcePath(resource: string, id: string) {
  return `/${resource}/${encodeURIComponent(id)}`
}

function scopeGrantPath(subject: AccessScopeGrantSubject, id?: string) {
  const resource = subject.subjectType === 'user' ? 'users' : 'teams'
  const base = `/access/${resource}/${encodeURIComponent(subject.subjectId)}/scope-grants`
  return id ? `${base}/${encodeURIComponent(id)}` : base
}

export const accessApi = {
  permissions: {
    catalog: () => unwrap(api.get<ApiResponse<PermissionCatalog>>('/access/permissions')),
  },
  users: {
    list: () => unwrap(api.get<ApiResponse<AccessUser[]>>('/access/users')),
    create: (values: AccessMutationValues) => discard(api.post('/access/users', values)),
    update: ({ id, values }: AccessUpdateVariables) =>
      discard(api.put(resourcePath('access/users', id), values)),
    delete: (id: string) => discard(api.delete(resourcePath('access/users', id))),
    resetMFA: ({ id, input }: ResetAccessUserMFAVariables) =>
      unwrap(
        api.post<ApiResponse<MFAAdminResetResult>>(
          `/identity/users/${encodeURIComponent(id.trim())}/mfa/reset`,
          input,
        ),
      ),
  },
  roles: {
    list: () => unwrap(api.get<ApiResponse<AccessRole[]>>('/access/roles')),
    create: (values: AccessMutationValues) => discard(api.post('/access/roles', values)),
    update: ({ id, values }: AccessUpdateVariables) =>
      discard(api.put(resourcePath('access/roles', id), values)),
    delete: (id: string) => discard(api.delete(resourcePath('access/roles', id))),
  },
  teams: {
    list: () => unwrap(api.get<ApiResponse<AccessTeam[]>>('/access/teams')),
    create: (values: AccessMutationValues) => discard(api.post('/access/teams', values)),
    update: ({ id, values }: AccessUpdateVariables) =>
      discard(api.put(resourcePath('access/teams', id), values)),
    delete: (id: string) => discard(api.delete(resourcePath('access/teams', id))),
  },
  policies: {
    list: () => unwrap(api.get<ApiResponse<AccessPolicy[]>>('/access/policies')),
    create: (values: AccessMutationValues) => discard(api.post('/access/policies', values)),
    update: ({ id, values }: AccessUpdateVariables) =>
      discard(api.put(resourcePath('access/policies', id), values)),
    delete: (id: string) => discard(api.delete(resourcePath('access/policies', id))),
  },
  scopeGrants: {
    listAll: () => unwrap(api.get<ApiResponse<AccessScopeGrant[]>>('/access/scope-grants')),
    list: (subject: AccessScopeGrantSubject) =>
      unwrap(api.get<ApiResponse<AccessScopeGrant[]>>(scopeGrantPath(subject))),
    create: ({ values, ...subject }: AccessScopeGrantSubject & { values: AccessMutationValues }) =>
      discard(api.post(scopeGrantPath(subject), values)),
    update: ({ id, values, ...subject }: AccessScopeGrantSubject & AccessUpdateVariables) =>
      discard(api.put(scopeGrantPath(subject, id), values)),
    delete: ({ id, ...subject }: AccessScopeGrantSubject & { id: string }) =>
      discard(api.delete(scopeGrantPath(subject, id))),
  },
  dependencies: {
    applications: () => unwrap(api.get<ApiResponse<AccessApplicationOption[]>>('/applications')),
    applicationEnvironments: () =>
      unwrap(api.get<ApiResponse<AccessApplicationEnvironment[]>>('/application-environments')),
    clusters: () => unwrap(api.get<ApiResponse<AccessClusterOption[]>>('/clusters')),
    loginProviders: async () => {
      const settings = await settingsApi.identity.get()
      return Array.isArray(settings?.providers) ? settings.providers : []
    },
  },
}
