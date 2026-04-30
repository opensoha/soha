import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'

export interface OnlineUser {
  id: string
  status: string
  userId: string
  userName: string
  email: string
  providerType: string
  loginTime: string
  lastSeenAt: string
  expiry: string
  source?: string
  sourceIp?: string
  userAgent?: string
}

export interface AnnouncementSummary {
  id: string
  enabled?: boolean
  status?: string
}

export interface MenuSummary {
  id: string
}

export const onlineUsersQueryKey = ['online-users'] as const
export const announcementsQueryKey = ['announcements'] as const
export const menusQueryKey = ['menus'] as const

export function onlineUsersQueryOptions() {
  return {
    queryKey: onlineUsersQueryKey,
    queryFn: () => api.get<ApiResponse<OnlineUser[]>>('/auth/sessions'),
    select: (response: ApiResponse<any[]>) => ({
      data: (response.data ?? []).map((item) => ({
        id: item.id,
        userId: item.userId,
        userName: item.userName,
        email: item.email,
        providerType: item.providerType,
        status: item.status,
        loginTime: item.createdAt,
        lastSeenAt: item.lastSeenAt,
        expiry: item.expiresAt,
        source: item.metadata?.source,
        sourceIp: item.metadata?.sourceIp,
        userAgent: item.metadata?.userAgent,
      })),
    }),
    refetchInterval: 10000,
  }
}

export function announcementsQueryOptions<T extends AnnouncementSummary = AnnouncementSummary>() {
  return {
    queryKey: announcementsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/announcements'),
  }
}

export function menusQueryOptions<T extends MenuSummary = MenuSummary>() {
  return {
    queryKey: menusQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/menus'),
  }
}
