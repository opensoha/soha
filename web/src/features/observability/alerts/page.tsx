import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { TableRowActionMenu } from '@/components/table-row-action-menu'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  encodeAIContextForElement,
  getAIWorkbenchPathForMode,
  useAIPageContext,
} from '@/features/copilot'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import {
  AlertOutlined,
  BellOutlined,
  EyeOutlined,
  FireOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableProps } from 'antd'
import { Button, Modal, Select, Space } from 'antd'
import { lazy, Suspense, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import '../observability-pages.css'
import { alertDisplayStatus } from './model'
import { observabilityAlertMutations } from './mutations'
import { observabilityAlertQueries } from './queries'
import type { AlertEvent } from './types'

const AlertEventDetailDrawer = lazy(async () => {
  const module = await import('./detail-page')
  return { default: module.AlertEventDetailDrawer }
})

export function AlertsPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canAcknowledge = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alerts.ack')
  const canResolve = hasPermission(permissionSnapshotQuery.data?.data, 'observe.alerts.update')
  const canHeal = hasPermission(permissionSnapshotQuery.data?.data, 'observe.healing.heal')
  const [healOpen, setHealOpen] = useState(false)
  const [healingPolicyId, setHealingPolicyId] = useState('')
  const [selectedAlertId, setSelectedAlertId] = useState('')
  const [detailOpen, setDetailOpen] = useState(false)
  const [detailEventId, setDetailEventId] = useState('')

  const alertsQuery = useQuery(observabilityAlertQueries.list())
  const healingPoliciesQuery = useQuery({
    ...observabilityAlertQueries.healingPolicies(),
    enabled: canHeal,
  })
  const alerts = readableQueryData(alertsQuery) ?? []
  useAIPageContext({
    sourceWorkbench: 'monitoring',
    sourceTitle: '活跃告警',
    entityKind: 'monitoring.alert.list',
    entityName: 'Alerts',
    timeRangeMinutes: 60,
    pinnedData: {
      total: readableQueryData(alertsQuery) ? alerts.length : undefined,
      firing: readableQueryData(alertsQuery)
        ? alerts.filter((item) => alertDisplayStatus(item) !== 'resolved').length
        : undefined,
    },
    promptHint: '分析当前活跃告警的严重程度、范围、来源系统和最近命中趋势。',
  })
  const acknowledgeMutation = useOperationMutation(
    observabilityAlertMutations.acknowledge(queryClient),
    {
      label: ['确认告警', 'Acknowledge alert'],
      target: (id) => id,
      receipt: () => ({ message: '告警确认记录已返回' }),
    },
  )
  const resolveMutation = useOperationMutation(observabilityAlertMutations.resolve(queryClient), {
    label: ['恢复告警', 'Resolve alert'],
    target: (id) => id,
    receipt: () => ({ message: '告警恢复记录已返回' }),
  })
  const healMutation = useOperationMutation(observabilityAlertMutations.heal(queryClient), {
    label: ['发起自愈', 'Start healing'],
    target: ({ eventId, policyId }) => `${eventId} · ${policyId}`,
    receipt: (result) => ({
      message: '自愈请求已返回',
      evidence: (
        <Space>
          <span>运行 {result.id}</span>
          <a href="/monitoring-workbench/healing">查看执行记录</a>
        </Space>
      ),
    }),
  })
  const denied = queryAccessDenied(alertsQuery)
  const blocked =
    denied || acknowledgeMutation.blocked || resolveMutation.blocked || healMutation.blocked
  useEffect(() => {
    if (denied) {
      setHealOpen(false)
      setHealingPolicyId('')
    }
  }, [denied])

  const columns: TableProps<AlertEvent>['columns'] = [
    {
      title: '名称',
      dataIndex: 'title',
      render: (value: string, record) => (
        <Button
          type="link"
          style={{ padding: 0, height: 'auto', fontWeight: 600 }}
          onClick={() => {
            setDetailEventId(record.id)
            setDetailOpen(true)
          }}
        >
          {value || record.id}
        </Button>
      ),
    },
    {
      ...tableColumnPresets.status,
      title: '严重程度',
      dataIndex: 'severity',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (_: string, record) => <StatusTag value={alertDisplayStatus(record)} />,
    },
    {
      title: '来源',
      dataIndex: 'sourceSystem',
      render: (value: string, record) => value || record.sourceType || '-',
    },
    {
      title: '范围',
      dataIndex: 'namespace',
      render: (value: string, record) =>
        [record.clusterId, value].filter(Boolean).join(' / ') || '-',
    },
    { title: '消息', dataIndex: 'summary', ellipsis: true },
    {
      ...tableColumnPresets.datetime,
      title: '触发时间',
      dataIndex: 'startsAt',
      render: formatDateTime,
    },
    {
      ...tableColumnPresets.datetime,
      title: '最近命中',
      dataIndex: 'lastSeenAt',
      render: formatDateTime,
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record) => (
        <TableRowActionMenu
          targetLabel={record.title || record.id}
          items={[
            {
              key: 'detail',
              label: '查看告警详情',
              icon: <EyeOutlined />,
              onClick: () => {
                setDetailEventId(record.id)
                setDetailOpen(true)
              },
            },
            {
              key: 'ai',
              label: '发起 AI 调查',
              icon: <AlertOutlined />,
              onClick: () => {
                const search = new URLSearchParams()
                search.set('alertId', record.id)
                search.set('timeRangeMinutes', '60')
                if (record.clusterId) search.set('clusterId', record.clusterId)
                if (record.namespace) search.set('namespace', record.namespace)
                navigate(getAIWorkbenchPathForMode('root_cause', search))
              },
            },
            ...(canHeal
              ? [
                  {
                    key: 'heal',
                    label: '触发自愈',
                    icon: <FireOutlined />,
                    disabled: blocked,
                    onClick: () => {
                      if (blocked) return
                      healMutation.reset()
                      setSelectedAlertId(record.id)
                      setHealingPolicyId('')
                      setHealOpen(true)
                    },
                  },
                ]
              : []),
            ...(canAcknowledge && alertDisplayStatus(record) !== 'acknowledged'
              ? [
                  {
                    key: 'ack',
                    label: '确认告警',
                    icon: <BellOutlined />,
                    disabled: blocked,
                    onClick: () => acknowledgeMutation.mutate(record.id),
                  },
                ]
              : []),
            ...(canResolve && alertDisplayStatus(record) !== 'resolved'
              ? [
                  {
                    key: 'resolve',
                    label: '恢复告警',
                    icon: <ReloadOutlined />,
                    disabled: blocked,
                    onClick: () => resolveMutation.mutate(record.id),
                  },
                ]
              : []),
          ]}
        />
      ),
    },
  ]

  return (
    <div className="soha-page">
      {acknowledgeMutation.feedback}
      {resolveMutation.feedback}
      {!healOpen ? healMutation.feedback : null}
      <h1 className="soha-observability-page-heading">活跃告警</h1>
      <AdminTable
        enableDensity
        error={alertsQuery.error}
        hasReadData={alertsQuery.data !== undefined}
        refreshing={alertsQuery.isFetching}
        onRefresh={() => alertsQuery.refetch()}
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={alerts}
        rowKey="id"
        onRow={(record: AlertEvent) => ({
          'data-ai-context': encodeAIContextForElement({
            sourceWorkbench: 'monitoring',
            sourceRoute: `/monitoring-workbench/alerts/${record.id}`,
            sourceTitle: record.title,
            entityKind: 'monitoring.alert',
            entityName: record.title,
            alertId: record.id,
            clusterId: record.clusterId,
            namespace: record.namespace,
            timeRangeMinutes: 60,
          }),
        })}
        loading={alertsQuery.isLoading}
        pageSize={20}
        scroll={{ x: 'max-content' }}
      />
      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title="发起自愈"
        open={healOpen && !denied}
        closable={!healMutation.blocked}
        keyboard={!healMutation.blocked}
        cancelButtonProps={{ disabled: healMutation.blocked }}
        mask={{ closable: false }}
        onCancel={() => {
          if (!healMutation.blocked) {
            setHealOpen(false)
            setHealingPolicyId('')
          }
        }}
        onOk={() => {
          if (blocked || !canHeal || !healingPolicyId || !readableQueryData(healingPoliciesQuery))
            return
          healMutation.mutate(
            { eventId: selectedAlertId, policyId: healingPolicyId },
            {
              onSuccess: () => {
                setHealOpen(false)
              },
            },
          )
        }}
        okButtonProps={{
          disabled: blocked || !healingPolicyId || !readableQueryData(healingPoliciesQuery),
          loading: healMutation.isPending,
        }}
        destroyOnHidden
      >
        {healMutation.feedback}
        <ReadFeedback label="自愈策略选项" query={healingPoliciesQuery} />
        <p>告警：{selectedAlertId}</p>
        <Select
          style={{ width: '100%' }}
          disabled={blocked || !readableQueryData(healingPoliciesQuery)}
          placeholder="选择自愈策略"
          value={healingPolicyId || undefined}
          onChange={(value) => setHealingPolicyId(String(value))}
          options={(readableQueryData(healingPoliciesQuery) ?? [])
            .filter((item) => item.enabled)
            .map((item) => ({ value: item.id, label: item.name }))}
        />
      </Modal>
      {detailOpen ? (
        <Suspense fallback={null}>
          <AlertEventDetailDrawer
            key={detailEventId}
            eventId={detailEventId}
            open={detailOpen}
            onClose={() => setDetailOpen(false)}
            onOpenStandalone={(eventId) => {
              setDetailOpen(false)
              navigate(`/monitoring-workbench/alerts/${eventId}`)
            }}
          />
        </Suspense>
      ) : null}
    </div>
  )
}
