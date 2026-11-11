import { queryOptions } from '@tanstack/react-query'
import {
  systemApi,
  type AnnouncementReceiptFilters,
  type AuditLogFilters,
  type OperationLogFilters,
  type SystemEndpointScope,
} from './api'
import { systemKeys } from './keys'

export const systemQueries = {
  sessions: (enabled = true) =>
    queryOptions({
      queryKey: systemKeys.sessions.list(),
      queryFn: systemApi.sessions.list,
      enabled,
      refetchInterval: (query) => (query.state.status === 'error' ? false : 10_000),
    }),
  announcements: () =>
    queryOptions({
      queryKey: systemKeys.announcements.list(),
      queryFn: systemApi.announcements.list,
    }),
  announcementReceipts: (
    announcementId: string,
    filters: AnnouncementReceiptFilters = {},
    enabled = true,
  ) =>
    queryOptions({
      queryKey: systemKeys.announcements.receipts(announcementId, filters),
      queryFn: () => systemApi.announcements.receipts(announcementId, filters),
      enabled,
    }),
  menus: () =>
    queryOptions({
      queryKey: systemKeys.menus.list(),
      queryFn: systemApi.menus.list,
    }),
  menuAccessRoles: (enabled: boolean) =>
    queryOptions({
      queryKey: systemKeys.menus.accessRoles(),
      queryFn: systemApi.menus.accessRoles,
      enabled,
      retry: false,
    }),
  audit: (scope: SystemEndpointScope, filters: AuditLogFilters = {}) =>
    queryOptions({
      queryKey: systemKeys.audit.list(scope, filters),
      queryFn: () => systemApi.audit.list(scope, filters),
    }),
  auditSummary: (enabled = true) =>
    queryOptions({
      queryKey: systemKeys.audit.summary(),
      queryFn: systemApi.audit.summary,
      enabled,
    }),
  operationLogs: (filters: OperationLogFilters = {}) =>
    queryOptions({
      queryKey: systemKeys.operationLogs.list(filters),
      queryFn: () => systemApi.operationLogs.list(filters),
    }),
  operationSummary: (enabled = true) =>
    queryOptions({
      queryKey: systemKeys.operationLogs.summary(),
      queryFn: systemApi.operationLogs.summary,
      enabled,
    }),
}
