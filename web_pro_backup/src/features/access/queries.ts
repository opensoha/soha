import { api } from '@/services/api-client'
import type { ApiResponse, ScopeGrant } from '@/types'

export interface AccessListRecord {
  id: string
}

export const accessUsersQueryKey = ['access/users'] as const
export const accessRolesQueryKey = ['access/roles'] as const
export const accessTeamsQueryKey = ['access/teams'] as const
export const accessPoliciesQueryKey = ['access/policies'] as const
export const accessScopeGrantsQueryKey = ['access/scope-grants'] as const

export function accessUsersQueryOptions<T extends AccessListRecord = AccessListRecord>() {
  return {
    queryKey: accessUsersQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/access/users'),
  }
}

export function accessRolesQueryOptions<T extends AccessListRecord = AccessListRecord>() {
  return {
    queryKey: accessRolesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/access/roles'),
  }
}

export function accessTeamsQueryOptions<T extends AccessListRecord = AccessListRecord>() {
  return {
    queryKey: accessTeamsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/access/teams'),
  }
}

export function accessPoliciesQueryOptions<T extends AccessListRecord = AccessListRecord>() {
  return {
    queryKey: accessPoliciesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/access/policies'),
  }
}

export function accessScopeGrantsQueryOptions<T extends ScopeGrant = ScopeGrant>() {
  return {
    queryKey: accessScopeGrantsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/access/scope-grants'),
  }
}
