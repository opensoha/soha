import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementQueryScope,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { systemQueries } from '@/features/system'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import { InfoCircleOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, DatePicker, Select } from 'antd'
import type { Dayjs } from 'dayjs'
import { useState } from 'react'
import { identityApplicationQueries } from '../applications'
import '../shared/application-access.css'
import {
  APPLICATION_LOGIN_ACTION_PREFIXES,
  APPLICATION_LOGIN_LIMIT,
  applicationLoginRecord,
  type ApplicationLoginRecord,
} from './model'
interface Filters {
  keyword: string
  applicationId?: string
  outcome?: string
  dates: [Dayjs | null, Dayjs | null] | null
}
const EMPTY_FILTERS: Filters = { keyword: '', dates: null }
export function IdentityLoginRecordsPage() {
  const { t } = useI18n()
  const snapshotQuery = usePermissionSnapshot()
  const snapshot = snapshotQuery.data?.data
  const canView =
    hasPermission(snapshot, 'identity.audit.view') || hasPermission(snapshot, 'system.audit.view')
  const canViewApplications = hasPermission(snapshot, 'identity.applications.view')
  const [draft, setDraft] = useState<Filters>(EMPTY_FILTERS)
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const logsQuery = useQuery({
    ...systemQueries.audit('identity', {
      actionPrefixes: APPLICATION_LOGIN_ACTION_PREFIXES,
      resourceKind: 'IdentityProvider',
      limit: APPLICATION_LOGIN_LIMIT,
      from: filters.dates?.[0]?.startOf('day').toISOString(),
      to: filters.dates?.[1]?.endOf('day').toISOString(),
    }),
    enabled: canView,
  })
  const visibleLogsQueryData =
    logsQuery.isEnabled === false ? undefined : readableQueryData(logsQuery)
  const appsQuery = useQuery({
    ...identityApplicationQueries.list({}),
    enabled: canView && canViewApplications,
  })
  const visibleAppsQueryData =
    appsQuery.isEnabled === false ? undefined : readableQueryData(appsQuery)
  const appNames = new Map(
    (canViewApplications ? (visibleAppsQueryData ?? []) : []).map((app) => [app.id, app.name]),
  )
  const records = (visibleLogsQueryData ?? []).flatMap((log) => {
    const record = applicationLoginRecord(log)
    return record ? [record] : []
  })
  const appName = (record: ApplicationLoginRecord) =>
    record.applicationName || appNames.get(record.applicationId) || record.applicationId || '—'
  const applicationOptions = [
    ...new Map(
      records
        .filter((record) => record.applicationId)
        .map((record) => [record.applicationId, appName(record)]),
    ).entries(),
  ].map(([value, label]) => ({ value, label }))
  const keyword = filters.keyword.trim().toLowerCase()
  const filteredRecords = records.filter(
    (record) =>
      (!filters.applicationId || record.applicationId === filters.applicationId) &&
      (!filters.outcome || record.outcome === filters.outcome) &&
      (!keyword ||
        [record.actorName, record.actorId, appName(record), record.sourceIp, record.reason].some(
          (value) => value?.toLowerCase().includes(keyword),
        )),
  )
  const columns: TableColumnsType<ApplicationLoginRecord> = [
    {
      ...tableColumnPresets.datetime,
      title: t('identity.loginRecords.time'),
      dataIndex: 'createdAt',
      width: 170,
      render: formatDateTime,
    },
    {
      title: t('identity.loginRecords.user'),
      dataIndex: 'actorName',
      width: 150,
      render: (_, record) =>
        record.actorName ||
        (record.actorId !== 'system' && record.actorId) ||
        t('identity.loginRecords.unknownUser'),
    },
    {
      title: t('identity.loginRecords.application'),
      dataIndex: 'applicationId',
      width: 190,
      ellipsis: true,
      render: (_, record) => appName(record),
    },
    {
      title: t('identity.loginRecords.stage'),
      dataIndex: 'stage',
      width: 140,
      render: (stage: string) => t(`identity.loginRecords.stage.${stage}`),
    },
    {
      ...tableColumnPresets.status,
      title: t('identity.loginRecords.result'),
      dataIndex: 'outcome',
      width: 90,
      render: (outcome: string) => (
        <StatusTag
          value={outcome}
          label={
            ['success', 'failure'].includes(outcome)
              ? t(`identity.loginRecords.${outcome}`)
              : undefined
          }
        />
      ),
    },
    {
      title: t('identity.loginRecords.sourceIp'),
      dataIndex: 'sourceIp',
      width: 150,
      render: (value: string) => value || '—',
    },
    {
      title: t('identity.loginRecords.reason'),
      dataIndex: 'reason',
      width: 240,
      ellipsis: true,
      render: (value: string) => (value ? t(`identity.loginRecords.reason.${value}`, value) : '—'),
    },
  ]
  if (snapshotQuery.isLoading) return <ManagementState kind="loading" />
  if (!canView)
    return <ManagementState kind="no-permission" title={t('identity.loginRecords.noPermission')} />
  const error = logsQuery.error
  return (
    <ManagementDataPage
      className="soha-identity-access-page soha-identity-login-records-page"
      query={{
        onFinish: () => setFilters({ ...draft }),
        actions: (
          <ManagementQueryActions
            onReset={() => {
              setDraft(EMPTY_FILTERS)
              setFilters(EMPTY_FILTERS)
            }}
          />
        ),
        children: (
          <>
            <ManagementKeywordField
              label={t('identity.loginRecords.keyword')}
              inputProps={{ 'aria-label': t('identity.loginRecords.keyword') }}
              placeholder={t('identity.loginRecords.search')}
              value={draft.keyword}
              onChange={(keyword) => setDraft({ ...draft, keyword })}
            />
            <ManagementQueryField
              label={t('identity.loginRecords.application')}
              width={200}
              minWidth={180}
            >
              <Select
                allowClear
                showSearch={{ optionFilterProp: 'label' }}
                aria-label={t('identity.loginRecords.application')}
                placeholder={t('common.all', '全部')}
                options={applicationOptions}
                value={draft.applicationId}
                onChange={(applicationId) => setDraft({ ...draft, applicationId })}
              />
            </ManagementQueryField>
            <ManagementQueryScope
              label={t('identity.loginRecords.result')}
              value={draft.outcome ?? ''}
              onChange={(outcome) => setDraft({ ...draft, outcome: String(outcome) || undefined })}
              options={[
                { value: '', label: t('common.all', '全部') },
                ...['success', 'failure'].map((value) => ({
                  value,
                  label: t(`identity.loginRecords.${value}`),
                })),
              ]}
            />
            <ManagementQueryField
              label={t('identity.loginRecords.time')}
              minWidth={300}
              width={320}
            >
              <DatePicker.RangePicker
                style={{ width: '100%' }}
                value={draft.dates}
                onChange={(dates) => setDraft({ ...draft, dates })}
              />
            </ManagementQueryField>
          </>
        ),
      }}
      table={{
        error: logsQuery.isEnabled === false ? null : error,
        hasReadData: visibleLogsQueryData !== undefined,
        onRetry: () => {
          if (logsQuery.isEnabled !== false) void logsQuery.refetch()
        },
        columns,
        dataSource: filteredRecords,
        rowKey: 'id',
        loading: logsQuery.isPending,
        empty: <ManagementState kind="empty" title={t('identity.loginRecords.empty')} />,
        pageSize: 15,
        scroll: { x: 1130 },
        tableSize,
        columnSettingPlacement: 'header',
        columnSettingIconOnly: true,
        headerExtra: (
          <ManagementTableToolbar>
            <ManagementIconButton
              aria-label={t('identity.loginRecords.scopeHelp')}
              icon={<InfoCircleOutlined />}
              tooltip={t('identity.loginRecords.scope')}
            />
            <ManagementDensityButton
              aria-label={t('table.density')}
              tooltip={t('table.density')}
              onClick={() => setTableSize((size) => (size === 'small' ? 'middle' : 'small'))}
            />
            <ManagementRefreshButton
              aria-label={t('common.refresh')}
              tooltip={t('common.refresh')}
              loading={logsQuery.isFetching}
              onClick={() => void (logsQuery.isEnabled === false ? undefined : logsQuery.refetch())}
            />
          </ManagementTableToolbar>
        ),
      }}
    >
      <ReadFeedback canRead={appsQuery.isEnabled} query={appsQuery} label="身份数据" />

      {(visibleLogsQueryData?.length ?? 0) >= APPLICATION_LOGIN_LIMIT ? (
        <Alert type="warning" showIcon title={t('identity.loginRecords.limit')} />
      ) : null}
      {appsQuery.isError && !visibleAppsQueryData ? (
        <Alert type="warning" showIcon title={t('identity.loginRecords.applicationError')} />
      ) : null}
    </ManagementDataPage>
  )
}
