import { AdminTable } from '@/components/admin-table'
import { ContextFeedback } from '@/components/context-feedback'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import {
  AuditOutlined,
  CheckCircleOutlined,
  DeleteOutlined,
  EyeOutlined,
  LinkOutlined,
  StopOutlined,
  SwapOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Tooltip,
  Typography,
} from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { Link, useInRouterContext } from 'react-router-dom'
import {
  formatBytes,
  formatHostedAppKind,
  formatHostedAppStatus,
  formatRevisionStatus,
  formatVisibilityScope,
  resolveSafeExternalUrl,
} from '../model'
import { appHostingAdminMutations } from '../mutations'
import { appHostingAdminQueries } from '../queries'
import type { HostedApp, HostedAppRevision, HostedAppStatus } from '../types'

const { Text } = Typography

interface LedgerFilterDraft {
  query: string
  status?: HostedAppStatus
  ownerUserId?: string
  teamId?: string
}

const EMPTY_FILTERS: LedgerFilterDraft = {
  query: '',
  status: undefined,
  ownerUserId: undefined,
  teamId: undefined,
}

const APP_STATUS_OPTIONS: Array<{ label: string; value: HostedAppStatus }> = [
  { label: '草稿（draft）', value: 'draft' },
  { label: '在线（online）', value: 'online' },
  { label: '已下线（offline）', value: 'offline' },
  { label: '已强制封停（suspended_by_admin）', value: 'suspended_by_admin' },
]

function AuditLogLink({ appId, canViewAudit }: { appId: string; canViewAudit: boolean }) {
  const inRouter = useInRouterContext()
  const auditPath = `/system/audit?metadataKey=appId&metadataValue=${encodeURIComponent(appId)}`

  if (!canViewAudit) {
    return (
      <Tooltip title="当前账号缺少审计日志查看权限（system.audit.view）">
        <Button disabled icon={<AuditOutlined />} size="small">
          查看审计日志
        </Button>
      </Tooltip>
    )
  }

  if (inRouter) {
    return (
      <Link to={auditPath}>
        <Button icon={<AuditOutlined />} size="small">
          查看审计日志
        </Button>
      </Link>
    )
  }

  return (
    <Button href={auditPath} icon={<AuditOutlined />} size="small">
      查看审计日志
    </Button>
  )
}

function HostedAppDetailDrawer({
  app,
  canView,
  canManage,
  canViewAudit,
  operationBlocked,
  onClose,
  onSuspend,
  onUnsuspend,
  onTransfer,
  onDelete,
}: {
  app: HostedApp | null
  canView: boolean
  canManage: boolean
  canViewAudit: boolean
  operationBlocked: boolean
  onClose: () => void
  onSuspend: (target: HostedApp) => void
  onUnsuspend: (target: HostedApp) => void
  onTransfer: (target: HostedApp) => void
  onDelete: (target: HostedApp) => void
}) {
  const appId = app?.id ?? ''
  const detailQuery = useQuery(appHostingAdminQueries.appDetail(appId, canView && Boolean(appId)))
  const revisionsQuery = useQuery(
    appHostingAdminQueries.appRevisions(appId, { limit: 20, offset: 0 }, canView && Boolean(appId)),
  )

  const activeApp =
    (detailQuery.isEnabled === false ? undefined : readableQueryData(detailQuery)) ?? app
  const visibleRevisions =
    revisionsQuery.isEnabled === false ? undefined : readableQueryData(revisionsQuery)

  const revisionColumns = useMemo<TableColumnsType<HostedAppRevision>>(
    () => [
      {
        title: '版本',
        key: 'revisionNumber',
        width: 120,
        render: (_, rev) => (
          <Space size={4} wrap>
            <Text strong>{`v${rev.revisionNumber}`}</Text>
            {activeApp?.activeRevisionId === rev.id ? (
              <MetadataTag label="当前线上" tone="cyan" />
            ) : null}
          </Space>
        ),
      },
      {
        ...tableColumnPresets.status,
        title: '状态',
        dataIndex: 'status',
        width: 100,
        render: (status: HostedAppRevision['status'], rev) => {
          const meta = formatRevisionStatus(status)
          return (
            <Space orientation="vertical" size={2}>
              <StatusTag label={meta.label} value={meta.value} />
              {rev.failureReason ? (
                <Text type="danger" className="soha-identity-app-hosting-subtext">
                  {rev.failureReason}
                </Text>
              ) : null}
            </Space>
          )
        },
      },
      {
        title: '文件数 / 大小',
        key: 'bundleSize',
        width: 140,
        render: (_, rev) => `${rev.fileCount} 个 · ${formatBytes(rev.totalBytes)}`,
      },
      {
        title: '发布人',
        dataIndex: 'createdBy',
        width: 130,
        render: (value: string) => value || '-',
      },
      {
        ...tableColumnPresets.datetime,
        title: '创建时间',
        dataIndex: 'createdAt',
        width: 165,
        render: (value: string) => formatDateTime(value),
      },
    ],
    [activeApp?.activeRevisionId],
  )

  const statusMeta = activeApp ? formatHostedAppStatus(activeApp.status) : undefined
  const kindMeta = activeApp ? formatHostedAppKind(activeApp.kind) : undefined
  const safeUrl = resolveSafeExternalUrl(activeApp?.url)
  const maintainers = activeApp?.maintainers ?? []

  return (
    <Drawer
      destroyOnHidden
      open={Boolean(app)}
      size={760}
      title={activeApp ? `应用详情 · ${activeApp.name}` : '应用详情'}
      extra={
        activeApp ? (
          <Space size={8} wrap>
            <AuditLogLink appId={activeApp.id} canViewAudit={canViewAudit} />
            {activeApp.status === 'suspended_by_admin' ? (
              <Button
                disabled={!canManage || operationBlocked}
                icon={<CheckCircleOutlined />}
                size="small"
                onClick={() => onUnsuspend(activeApp)}
              >
                解除封停
              </Button>
            ) : (
              <Button
                danger
                disabled={!canManage || operationBlocked}
                icon={<StopOutlined />}
                size="small"
                onClick={() => onSuspend(activeApp)}
              >
                强制封停
              </Button>
            )}
            <Button
              disabled={!canManage || operationBlocked}
              icon={<SwapOutlined />}
              size="small"
              onClick={() => onTransfer(activeApp)}
            >
              归属转交
            </Button>
            <Button
              danger
              disabled={!canManage || operationBlocked}
              icon={<DeleteOutlined />}
              size="small"
              onClick={() => onDelete(activeApp)}
            >
              彻底删除
            </Button>
          </Space>
        ) : null
      }
      onClose={onClose}
    >
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="托管应用详情" />
      <ReadFeedback
        canRead={revisionsQuery.isEnabled}
        query={revisionsQuery}
        label="托管应用历史版本"
      />

      {activeApp ? (
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          {activeApp.status === 'suspended_by_admin' ? (
            <ContextFeedback
              type="error"
              title="当前应用处于管理员强制封停状态"
              description={
                activeApp.suspendedReason
                  ? `封停原因：${activeApp.suspendedReason}`
                  : '所有访问已阻断，作者无法自行恢复上线。'
              }
            />
          ) : null}

          {activeApp.pendingVisibilityApproval?.status === 'pending' ? (
            <ContextFeedback
              type="warning"
              title="存在待审批的全公司公开申请"
              description={`当前生效范围仍为「${formatVisibilityScope(activeApp.visibility)}」，申请说明：${activeApp.pendingVisibilityApproval.reason || '未填写'}`}
            />
          ) : null}

          <Descriptions
            bordered
            column={{ xs: 1, sm: 2 }}
            size="small"
            items={[
              { key: 'name', label: '应用名称', children: activeApp.name },
              {
                key: 'status',
                label: '当前状态',
                children: statusMeta ? (
                  <StatusTag label={statusMeta.label} value={statusMeta.value} />
                ) : (
                  '-'
                ),
              },
              { key: 'id', label: '应用 ID', children: <Text code>{activeApp.id}</Text> },
              {
                key: 'kind',
                label: '应用类型',
                children: kindMeta ? (
                  <Space size={4}>
                    <MetadataTag label={kindMeta.label} tone={kindMeta.tone} />
                    {activeApp.spaFallback ? <MetadataTag label="SPA 回退" /> : null}
                  </Space>
                ) : (
                  '-'
                ),
              },
              {
                key: 'url',
                label: '固定访问地址',
                span: 'filled',
                children: safeUrl ? (
                  <Space size={8} wrap>
                    <a href={safeUrl} rel="noopener noreferrer" target="_blank">
                      {safeUrl}
                    </a>
                    <Text copyable={{ text: safeUrl }} />
                  </Space>
                ) : (
                  activeApp.host || '-'
                ),
              },
              {
                key: 'owner',
                label: '归属作者',
                children: (
                  <Space orientation="vertical" size={0}>
                    <Text>{activeApp.ownerProfile?.displayName || activeApp.ownerUserId}</Text>
                    <Text type="secondary" className="soha-identity-app-hosting-subtext">
                      {activeApp.ownerProfile?.teamNames?.length
                        ? activeApp.ownerProfile.teamNames.join('、')
                        : activeApp.ownerUserId}
                    </Text>
                  </Space>
                ),
              },
              {
                key: 'maintainers',
                label: '协作维护人',
                children:
                  maintainers.length > 0
                    ? maintainers.map((item) => item.displayName || item.userId).join('、')
                    : activeApp.maintainerUserIds.length > 0
                      ? activeApp.maintainerUserIds.join('、')
                      : '无协作者',
              },
              {
                key: 'visibility',
                label: '生效可见范围',
                span: 'filled',
                children: (
                  <Space orientation="vertical" size={2}>
                    <Text>{formatVisibilityScope(activeApp.visibility)}</Text>
                    {activeApp.visibility.mode === 'selected' ? (
                      <Text type="secondary" className="soha-identity-app-hosting-subtext">
                        {`指定部门 ID：${activeApp.visibility.teamIds?.join(', ') || '无'} · 指定员工 ID：${activeApp.visibility.userIds?.join(', ') || '无'}`}
                      </Text>
                    ) : null}
                  </Space>
                ),
              },
              {
                key: 'activeRevision',
                label: '活动版本 / 总占用',
                children: `${activeApp.activeRevisionNumber != null ? `v${activeApp.activeRevisionNumber}` : '未发版'} · ${formatBytes(activeApp.totalBytes)}`,
              },
              {
                key: 'routeGeneration',
                label: '路由世代',
                children: `Gen #${activeApp.routeGeneration}`,
              },
              {
                key: 'description',
                label: '应用简介',
                span: 'filled',
                children: activeApp.description || '暂无简介',
              },
              {
                key: 'createdAt',
                label: '创建时间',
                children: formatDateTime(activeApp.createdAt),
              },
              {
                key: 'updatedAt',
                label: '最近更新',
                children: formatDateTime(activeApp.updatedAt),
              },
            ]}
          />

          <div className="soha-identity-app-hosting-drawer-section">
            <Text strong>历史版本记录</Text>
            <AdminTable
              columns={revisionColumns}
              dataSource={visibleRevisions?.items ?? []}
              empty={
                <ManagementState
                  bordered={false}
                  compact
                  kind="empty"
                  title="暂无版本记录"
                  description="该托管应用尚未上传产物版本。"
                />
              }
              enableColumnSelection={false}
              error={revisionsQuery.isEnabled === false ? null : revisionsQuery.error}
              hasReadData={visibleRevisions !== undefined}
              loading={revisionsQuery.isLoading}
              pagination={false}
              rowKey="id"
              scroll={{ x: 640 }}
              tableSize="small"
              onRetry={() => {
                if (revisionsQuery.isEnabled !== false) void revisionsQuery.refetch()
              }}
            />
          </div>
        </Space>
      ) : null}
    </Drawer>
  )
}

export function GlobalAppLedgerPanel({
  canView,
  canManage,
  canViewAudit,
}: {
  canView: boolean
  canManage: boolean
  canViewAudit: boolean
}) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<LedgerFilterDraft>(EMPTY_FILTERS)
  const [filters, setFilters] = useState<LedgerFilterDraft>(EMPTY_FILTERS)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')

  const [ownerSearch, setOwnerSearch] = useState('')
  const [teamSearch, setTeamSearch] = useState('')
  const [transferSearch, setTransferSearch] = useState('')

  const [detailApp, setDetailApp] = useState<HostedApp | null>(null)
  const [suspendTarget, setSuspendTarget] = useState<HostedApp | null>(null)
  const [suspendReason, setSuspendReason] = useState('')
  const [suspendError, setSuspendError] = useState<string>()

  const [unsuspendTarget, setUnsuspendTarget] = useState<HostedApp | null>(null)
  const [unsuspendReason, setUnsuspendReason] = useState('')

  const [transferTarget, setTransferTarget] = useState<HostedApp | null>(null)
  const [newOwnerUserId, setNewOwnerUserId] = useState<string>()
  const [transferReason, setTransferReason] = useState('')
  const [transferError, setTransferError] = useState<string>()

  const [deleteTarget, setDeleteTarget] = useState<HostedApp | null>(null)
  const [confirmationName, setConfirmationName] = useState('')
  const [deleteReason, setDeleteReason] = useState('')

  const appsQuery = useQuery(
    appHostingAdminQueries.apps(
      {
        query: filters.query || undefined,
        status: filters.status,
        ownerUserId: filters.ownerUserId,
        teamId: filters.teamId,
        limit: pageSize,
        offset: (page - 1) * pageSize,
      },
      canView,
    ),
  )
  const visibleAppsData = appsQuery.isEnabled === false ? undefined : readableQueryData(appsQuery)

  const ownerCandidatesQuery = useQuery(
    appHostingAdminQueries.audienceCandidates(ownerSearch, 20, canView),
  )
  const teamCandidatesQuery = useQuery(
    appHostingAdminQueries.audienceCandidates(teamSearch, 20, canView),
  )
  const transferCandidatesQuery = useQuery(
    appHostingAdminQueries.audienceCandidates(
      transferSearch,
      20,
      canView && Boolean(transferTarget),
    ),
  )

  const visibleOwnerCandidates =
    ownerCandidatesQuery.isEnabled === false ? undefined : readableQueryData(ownerCandidatesQuery)
  const visibleTeamCandidates =
    teamCandidatesQuery.isEnabled === false ? undefined : readableQueryData(teamCandidatesQuery)
  const visibleTransferCandidates =
    transferCandidatesQuery.isEnabled === false
      ? undefined
      : readableQueryData(transferCandidatesQuery)

  const suspendMutation = useOperationMutation(appHostingAdminMutations.suspendApp(queryClient), {
    label: ['强制封停托管应用', 'Suspend hosted app'],
    target: (input) => input.appName || input.appId,
  })
  const unsuspendMutation = useOperationMutation(
    appHostingAdminMutations.unsuspendApp(queryClient),
    {
      label: ['解除托管应用封停', 'Unsuspend hosted app'],
      target: (input) => input.appName || input.appId,
    },
  )
  const transferMutation = useOperationMutation(appHostingAdminMutations.transferApp(queryClient), {
    label: ['转交托管应用归属', 'Transfer hosted app ownership'],
    target: (input) => input.appName || input.appId,
  })
  const deleteMutation = useOperationMutation(appHostingAdminMutations.deleteApp(queryClient), {
    label: ['彻底删除托管应用', 'Delete hosted app'],
    target: (input) => input.appName || input.appId,
  })

  const operationBlocked =
    suspendMutation.blocked ||
    unsuspendMutation.blocked ||
    transferMutation.blocked ||
    deleteMutation.blocked ||
    (appsQuery.isEnabled !== false && queryAccessDenied(appsQuery))

  useEffect(() => {
    if (suspendTarget) {
      setSuspendReason('')
      setSuspendError(undefined)
    }
  }, [suspendTarget])

  useEffect(() => {
    if (unsuspendTarget) {
      setUnsuspendReason('')
    }
  }, [unsuspendTarget])

  useEffect(() => {
    if (transferTarget) {
      setNewOwnerUserId(undefined)
      setTransferReason('')
      setTransferSearch('')
      setTransferError(undefined)
    }
  }, [transferTarget])

  useEffect(() => {
    if (deleteTarget) {
      setConfirmationName('')
      setDeleteReason('')
    }
  }, [deleteTarget])

  const ownerOptions = useMemo(
    () =>
      (visibleOwnerCandidates?.users ?? []).map((user) => ({
        label: user.department
          ? `${user.displayName} (${user.username}) · ${user.department}`
          : `${user.displayName} (${user.username})`,
        value: user.id,
      })),
    [visibleOwnerCandidates?.users],
  )

  const teamOptions = useMemo(
    () =>
      (visibleTeamCandidates?.teams ?? []).map((team) => ({
        label: team.slug ? `${team.name} (${team.slug})` : team.name,
        value: team.id,
      })),
    [visibleTeamCandidates?.teams],
  )

  const transferUserOptions = useMemo(
    () =>
      (visibleTransferCandidates?.users ?? [])
        .filter((user) => user.id !== transferTarget?.ownerUserId)
        .map((user) => ({
          label: user.department
            ? `${user.displayName} (${user.username}) · ${user.department}`
            : `${user.displayName} (${user.username})`,
          value: user.id,
        })),
    [transferTarget?.ownerUserId, visibleTransferCandidates?.users],
  )

  const columns = useMemo<TableColumnsType<HostedApp>>(
    () => [
      {
        title: '应用名称与简介',
        dataIndex: 'name',
        key: 'name',
        width: 240,
        render: (_, record) => (
          <Space orientation="vertical" size={2}>
            <Button
              className="soha-identity-app-hosting-title-link"
              type="link"
              onClick={() => setDetailApp(record)}
            >
              {record.name}
            </Button>
            <Text
              type="secondary"
              ellipsis
              title={record.description}
              className="soha-identity-app-hosting-subtext"
            >
              {record.description || '暂无简介'}
            </Text>
          </Space>
        ),
      },
      {
        title: '固定域名链接',
        dataIndex: 'url',
        key: 'url',
        width: 250,
        render: (_, record) => {
          const safeUrl = resolveSafeExternalUrl(record.url)
          return (
            <Space size={6} wrap>
              {safeUrl ? (
                <a
                  href={safeUrl}
                  rel="noopener noreferrer"
                  target="_blank"
                  className="soha-identity-app-hosting-domain-link"
                >
                  <LinkOutlined /> {record.host || safeUrl}
                </a>
              ) : (
                <Text>{record.host || record.dnsLabel}</Text>
              )}
              {safeUrl ? <Text copyable={{ text: safeUrl }} /> : null}
            </Space>
          )
        },
      },
      {
        title: '类型',
        dataIndex: 'kind',
        key: 'kind',
        width: 110,
        render: (kind: HostedApp['kind']) => {
          const meta = formatHostedAppKind(kind)
          return <MetadataTag label={meta.label} tone={meta.tone} />
        },
      },
      {
        title: '作者与部门',
        key: 'owner',
        width: 180,
        render: (_, record) => {
          const displayName = record.ownerProfile?.displayName || record.ownerUserId
          const departments = record.ownerProfile?.teamNames ?? []
          return (
            <Space orientation="vertical" size={2}>
              <Text>{displayName}</Text>
              <Text type="secondary" className="soha-identity-app-hosting-subtext">
                {departments.length > 0 ? departments.join('、') : '未标注部门'}
              </Text>
            </Space>
          )
        },
      },
      {
        title: '协作者',
        key: 'maintainers',
        width: 96,
        render: (_, record) => {
          const count = record.maintainers?.length ?? record.maintainerUserIds.length
          if (count <= 0) return <Text type="secondary">0 人</Text>
          const names = record.maintainers?.map((m) => m.displayName).join('、')
          return <Text title={names}>{`${count} 人`}</Text>
        },
      },
      {
        title: '可见范围',
        key: 'visibility',
        width: 210,
        render: (_, record) => (
          <Space size={4} wrap>
            <Text>{formatVisibilityScope(record.visibility)}</Text>
            {record.pendingVisibilityApproval?.status === 'pending' ? (
              <StatusTag label="全公司待审" value="pending" />
            ) : null}
          </Space>
        ),
      },
      {
        title: '版本 / 大小',
        key: 'revisionSize',
        width: 140,
        render: (_, record) => (
          <Space orientation="vertical" size={2}>
            <Text>
              {record.activeRevisionNumber != null ? `v${record.activeRevisionNumber}` : '未发版'}
            </Text>
            <Text type="secondary" className="soha-identity-app-hosting-subtext">
              {formatBytes(record.totalBytes)}
            </Text>
          </Space>
        ),
      },
      {
        ...tableColumnPresets.status,
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        width: 160,
        render: (status: HostedApp['status'], record) => {
          const meta = formatHostedAppStatus(status)
          return (
            <Space orientation="vertical" size={2}>
              <StatusTag label={meta.label} value={meta.value} />
              {status === 'suspended_by_admin' && record.suspendedReason ? (
                <Text
                  type="danger"
                  ellipsis
                  title={record.suspendedReason}
                  className="soha-identity-app-hosting-subtext"
                >
                  {record.suspendedReason}
                </Text>
              ) : null}
            </Space>
          )
        },
      },
      {
        ...tableColumnPresets.datetime,
        title: '更新时间',
        dataIndex: 'updatedAt',
        key: 'updatedAt',
        width: 165,
        render: (value: string) => formatDateTime(value),
      },
      {
        ...tableColumnPresets.action,
        title: '操作',
        key: 'actions',
        width: 240,
        render: (_, record) => (
          <Space size={4} wrap>
            <Button
              icon={<EyeOutlined />}
              size="small"
              type="link"
              onClick={() => setDetailApp(record)}
            >
              详情
            </Button>
            {record.status === 'suspended_by_admin' ? (
              <Button
                disabled={!canManage || operationBlocked}
                size="small"
                type="link"
                onClick={() => setUnsuspendTarget(record)}
              >
                解除封停
              </Button>
            ) : (
              <Button
                danger
                disabled={!canManage || operationBlocked}
                size="small"
                type="link"
                onClick={() => setSuspendTarget(record)}
              >
                强制封停
              </Button>
            )}
            <Button
              disabled={!canManage || operationBlocked}
              size="small"
              type="link"
              onClick={() => setTransferTarget(record)}
            >
              转交
            </Button>
            <Button
              danger
              disabled={!canManage || operationBlocked}
              size="small"
              type="link"
              onClick={() => setDeleteTarget(record)}
            >
              删除
            </Button>
          </Space>
        ),
      },
    ],
    [canManage, operationBlocked],
  )

  const hasFilters = Boolean(
    filters.query ||
    filters.status ||
    filters.ownerUserId ||
    filters.teamId ||
    draft.query ||
    draft.status ||
    draft.ownerUserId ||
    draft.teamId,
  )

  const handleSubmitSuspend = () => {
    if (!suspendTarget || !canManage || operationBlocked) return
    const trimmed = suspendReason.trim()
    if (!trimmed) {
      setSuspendError('请填写强制封停原因')
      return
    }
    setSuspendError(undefined)
    suspendMutation.mutate(
      {
        appId: suspendTarget.id,
        appName: suspendTarget.name,
        reason: trimmed,
      },
      {
        onSuccess: () => setSuspendTarget(null),
      },
    )
  }

  const handleSubmitUnsuspend = () => {
    if (!unsuspendTarget || !canManage || operationBlocked) return
    unsuspendMutation.mutate(
      {
        appId: unsuspendTarget.id,
        appName: unsuspendTarget.name,
        reason: unsuspendReason.trim() || undefined,
      },
      {
        onSuccess: () => setUnsuspendTarget(null),
      },
    )
  }

  const handleSubmitTransfer = () => {
    if (!transferTarget || !canManage || operationBlocked) return
    if (!newOwnerUserId?.trim()) {
      setTransferError('请选择新归属员工')
      return
    }
    setTransferError(undefined)
    transferMutation.mutate(
      {
        appId: transferTarget.id,
        appName: transferTarget.name,
        newOwnerUserId: newOwnerUserId.trim(),
        reason: transferReason.trim() || undefined,
      },
      {
        onSuccess: () => setTransferTarget(null),
      },
    )
  }

  const handleSubmitDelete = () => {
    if (!deleteTarget || !canManage || operationBlocked) return
    if (confirmationName !== deleteTarget.name) return
    const deletedId = deleteTarget.id
    deleteMutation.mutate(
      {
        appId: deletedId,
        appName: deleteTarget.name,
        confirmationName,
        reason: deleteReason.trim() || undefined,
      },
      {
        onSuccess: () => {
          setDeleteTarget(null)
          if (detailApp?.id === deletedId) {
            setDetailApp(null)
          }
        },
      },
    )
  }

  return (
    <div className="soha-identity-app-hosting-section">
      {!suspendTarget && suspendMutation.feedback}
      {!unsuspendTarget && unsuspendMutation.feedback}
      {!transferTarget && transferMutation.feedback}
      {!deleteTarget && deleteMutation.feedback}

      <ReadFeedback
        canRead={ownerCandidatesQuery.isEnabled}
        query={ownerCandidatesQuery}
        label="员工与部门检索"
      />

      <ManagementDataPage
        className="soha-identity-app-hosting-ledger"
        query={{
          onFinish: () => {
            setFilters({ ...draft })
            setPage(1)
          },
          actions: (
            <ManagementQueryActions
              disabledReset={!hasFilters}
              loading={appsQuery.isFetching}
              onReset={() => {
                setDraft(EMPTY_FILTERS)
                setFilters(EMPTY_FILTERS)
                setOwnerSearch('')
                setTeamSearch('')
                setPage(1)
              }}
            />
          ),
          children: (
            <>
              <ManagementKeywordField
                label="关键词"
                inputProps={{ 'aria-label': '台账关键词' }}
                placeholder="搜索应用名称、简介或子域"
                value={draft.query}
                onChange={(query) => setDraft((prev) => ({ ...prev, query }))}
              />
              <ManagementQueryField label="状态" width={200} minWidth={180}>
                <Select
                  allowClear
                  aria-label="应用状态筛选"
                  options={APP_STATUS_OPTIONS}
                  placeholder="全部状态"
                  value={draft.status}
                  onChange={(status) => setDraft((prev) => ({ ...prev, status }))}
                />
              </ManagementQueryField>
              <ManagementQueryField label="归属作者" width={220} minWidth={190}>
                <Select
                  allowClear
                  aria-label="归属作者筛选"
                  loading={ownerCandidatesQuery.isFetching}
                  options={ownerOptions}
                  placeholder="搜索员工姓名或账号"
                  showSearch={{ filterOption: false, onSearch: setOwnerSearch }}
                  value={draft.ownerUserId}
                  onChange={(ownerUserId) => setDraft((prev) => ({ ...prev, ownerUserId }))}
                />
              </ManagementQueryField>
              <ManagementQueryField label="所属部门" width={220} minWidth={190}>
                <Select
                  allowClear
                  aria-label="所属部门筛选"
                  loading={teamCandidatesQuery.isFetching}
                  options={teamOptions}
                  placeholder="搜索部门名称"
                  showSearch={{ filterOption: false, onSearch: setTeamSearch }}
                  value={draft.teamId}
                  onChange={(teamId) => setDraft((prev) => ({ ...prev, teamId }))}
                />
              </ManagementQueryField>
            </>
          ),
        }}
        table={{
          columns,
          columnSettingIconOnly: true,
          columnSettingPlacement: 'header',
          dataSource: visibleAppsData?.items ?? [],
          empty:
            appsQuery.isError && !visibleAppsData ? null : (
              <ManagementState
                kind="empty"
                title="暂无托管应用记录"
                description="员工通过 soha host deploy 或内置助手发布工作区应用后，将在此集中巡检与治理。"
              />
            ),
          error: appsQuery.isEnabled === false ? null : appsQuery.error,
          hasReadData: visibleAppsData !== undefined,
          headerExtra: (
            <ManagementTableToolbar>
              <ManagementDensityButton
                aria-label="切换表格密度"
                tooltip="切换表格密度"
                onClick={() =>
                  setTableSize((current) => (current === 'small' ? 'middle' : 'small'))
                }
              />
              <ManagementRefreshButton
                aria-label="刷新应用台账"
                loading={appsQuery.isFetching}
                tooltip="刷新应用台账"
                onClick={() => {
                  if (appsQuery.isEnabled !== false) void appsQuery.refetch()
                }}
              />
            </ManagementTableToolbar>
          ),
          loading: appsQuery.isLoading || appsQuery.isFetching,
          onRetry: () => {
            if (appsQuery.isEnabled !== false) void appsQuery.refetch()
          },
          pagination: {
            current: page,
            currentPage: page,
            pageSize,
            total: visibleAppsData?.total ?? 0,
            onChange: (nextPage: number, nextPageSize: number) => {
              if (nextPageSize !== pageSize) {
                setPageSize(nextPageSize)
                setPage(1)
              } else {
                setPage(nextPage)
              }
            },
          },
          rowKey: 'id',
          scroll: { x: 1580 },
          tableSize,
        }}
      />

      <HostedAppDetailDrawer
        app={detailApp}
        canManage={canManage}
        canView={canView}
        canViewAudit={canViewAudit}
        operationBlocked={operationBlocked}
        onClose={() => {
          if (!operationBlocked) setDetailApp(null)
        }}
        onDelete={(target) => setDeleteTarget(target)}
        onSuspend={(target) => setSuspendTarget(target)}
        onTransfer={(target) => setTransferTarget(target)}
        onUnsuspend={(target) => setUnsuspendTarget(target)}
      />

      <Modal
        closable={!operationBlocked}
        destroyOnHidden
        mask={{ closable: false }}
        open={Boolean(suspendTarget)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        title={suspendTarget ? `强制封停 · ${suspendTarget.name}` : '强制封停'}
        width={560}
        footer={
          <Space>
            <Button
              disabled={operationBlocked}
              onClick={() => {
                if (!operationBlocked) setSuspendTarget(null)
              }}
            >
              取消
            </Button>
            <Button
              danger
              disabled={!canManage || operationBlocked}
              loading={suspendMutation.isPending}
              type="primary"
              onClick={handleSubmitSuspend}
            >
              确认强制封停
            </Button>
          </Space>
        }
        onCancel={() => {
          if (!operationBlocked) setSuspendTarget(null)
        }}
      >
        {suspendMutation.feedback}
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <ContextFeedback
            type="warning"
            title="封停后立即切断所有网关访问，且作者无法自行恢复上线"
            description="提交后将同步推进路由世代并写入治理审计；作者在「我的作品」将看到封停原因，仅在管理员解除封停后才可恢复。"
          />
          <Form layout="vertical">
            <Form.Item
              help={suspendError}
              label={<label htmlFor="soha-app-hosting-suspend-reason">封停原因</label>}
              required
              validateStatus={suspendError ? 'error' : undefined}
            >
              <Input.TextArea
                id="soha-app-hosting-suspend-reason"
                aria-label="封停原因"
                disabled={!canManage || operationBlocked}
                maxLength={500}
                placeholder="请说明违规内容、异常流量或安全风险原因（必填）"
                rows={3}
                value={suspendReason}
                onChange={(event) => {
                  setSuspendReason(event.target.value)
                  if (suspendError && event.target.value.trim()) {
                    setSuspendError(undefined)
                  }
                }}
              />
            </Form.Item>
          </Form>
        </Space>
      </Modal>

      <Modal
        closable={!operationBlocked}
        destroyOnHidden
        mask={{ closable: false }}
        open={Boolean(unsuspendTarget)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        title={unsuspendTarget ? `解除封停 · ${unsuspendTarget.name}` : '解除封停'}
        width={520}
        footer={
          <Space>
            <Button
              disabled={operationBlocked}
              onClick={() => {
                if (!operationBlocked) setUnsuspendTarget(null)
              }}
            >
              取消
            </Button>
            <Button
              disabled={!canManage || operationBlocked}
              loading={unsuspendMutation.isPending}
              type="primary"
              onClick={handleSubmitUnsuspend}
            >
              确认解除封停
            </Button>
          </Space>
        }
        onCancel={() => {
          if (!operationBlocked) setUnsuspendTarget(null)
        }}
      >
        {unsuspendMutation.feedback}
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <ContextFeedback
            type="info"
            title="解除强制封停"
            description={
              unsuspendTarget?.suspendedReason
                ? `原封停原因：${unsuspendTarget.suspendedReason}。解除后作者可按当前可见范围恢复管理或上线。`
                : '解除后将移除管理员强制封停状态，并记录治理审计。'
            }
          />
          <Form layout="vertical">
            <Form.Item
              label={<label htmlFor="soha-app-hosting-unsuspend-reason">解除备注（可选）</label>}
            >
              <Input.TextArea
                id="soha-app-hosting-unsuspend-reason"
                aria-label="解除备注"
                disabled={!canManage || operationBlocked}
                maxLength={500}
                placeholder="可选填写核查结果或恢复依据"
                rows={3}
                value={unsuspendReason}
                onChange={(event) => setUnsuspendReason(event.target.value)}
              />
            </Form.Item>
          </Form>
        </Space>
      </Modal>

      <Modal
        closable={!operationBlocked}
        destroyOnHidden
        mask={{ closable: false }}
        open={Boolean(transferTarget)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        title={transferTarget ? `归属转交 · ${transferTarget.name}` : '归属转交'}
        width={560}
        footer={
          <Space>
            <Button
              disabled={operationBlocked}
              onClick={() => {
                if (!operationBlocked) setTransferTarget(null)
              }}
            >
              取消
            </Button>
            <Button
              disabled={!canManage || operationBlocked}
              loading={transferMutation.isPending}
              type="primary"
              onClick={handleSubmitTransfer}
            >
              确认转交归属
            </Button>
          </Space>
        }
        onCancel={() => {
          if (!operationBlocked) setTransferTarget(null)
        }}
      >
        {transferMutation.feedback}
        <ReadFeedback
          canRead={transferCandidatesQuery.isEnabled}
          query={transferCandidatesQuery}
          label="转交候选员工"
        />
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <ContextFeedback
            type="warning"
            title="固定链接保持不变，原作者私有访问权与待审申请将立即重算/失效"
            description="归属转交后子域地址保持不变；系统将自动递增路由世代，撤回原作者仅本人模式的私有访问权，并使当前待审的全公司公开申请失效。"
          />
          <Form layout="vertical">
            <Form.Item
              help={transferError}
              label="新归属员工"
              required
              validateStatus={transferError ? 'error' : undefined}
            >
              <Select
                aria-label="新归属员工"
                disabled={!canManage || operationBlocked}
                loading={transferCandidatesQuery.isFetching}
                options={transferUserOptions}
                placeholder="检索并选择接手应用的员工"
                showSearch={{ filterOption: false, onSearch: setTransferSearch }}
                value={newOwnerUserId}
                onChange={(value) => {
                  setNewOwnerUserId(value)
                  if (transferError && value) setTransferError(undefined)
                }}
              />
            </Form.Item>
            <Form.Item label={<label htmlFor="soha-app-hosting-transfer-reason">转交原因</label>}>
              <Input.TextArea
                id="soha-app-hosting-transfer-reason"
                aria-label="转交原因"
                disabled={!canManage || operationBlocked}
                maxLength={500}
                placeholder="例如：员工调岗交接 / 离职资产归属转移"
                rows={3}
                value={transferReason}
                onChange={(event) => setTransferReason(event.target.value)}
              />
            </Form.Item>
          </Form>
        </Space>
      </Modal>

      <Modal
        closable={!operationBlocked}
        destroyOnHidden
        mask={{ closable: false }}
        open={Boolean(deleteTarget)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        title={deleteTarget ? `彻底删除 · ${deleteTarget.name}` : '彻底删除'}
        width={560}
        footer={
          <Space>
            <Button
              disabled={operationBlocked}
              onClick={() => {
                if (!operationBlocked) setDeleteTarget(null)
              }}
            >
              取消
            </Button>
            <Button
              danger
              disabled={
                !canManage ||
                operationBlocked ||
                !deleteTarget ||
                confirmationName !== deleteTarget.name
              }
              loading={deleteMutation.isPending}
              type="primary"
              onClick={handleSubmitDelete}
            >
              确认彻底删除
            </Button>
          </Space>
        }
        onCancel={() => {
          if (!operationBlocked) setDeleteTarget(null)
        }}
      >
        {deleteMutation.feedback}
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <ContextFeedback
            type="error"
            title="彻底删除不可恢复"
            description={`删除后将立即阻断访问，并清理「${deleteTarget?.name ?? ''}」的全部版本记录、审批历史及对象存储文件。`}
          />
          <Form layout="vertical">
            <Form.Item
              label={
                <label htmlFor="soha-app-hosting-delete-confirm">
                  {`请输入精确应用名称「${deleteTarget?.name ?? ''}」以确认删除`}
                </label>
              }
              required
            >
              <Input
                id="soha-app-hosting-delete-confirm"
                aria-label="确认应用名称"
                autoComplete="off"
                disabled={!canManage || operationBlocked}
                placeholder={deleteTarget?.name ?? ''}
                value={confirmationName}
                onChange={(event) => setConfirmationName(event.target.value)}
              />
            </Form.Item>
            <Form.Item
              label={<label htmlFor="soha-app-hosting-delete-reason">删除原因（可选）</label>}
            >
              <Input.TextArea
                id="soha-app-hosting-delete-reason"
                aria-label="删除原因"
                disabled={!canManage || operationBlocked}
                maxLength={500}
                placeholder="可选记录治理删除原因供审计留存"
                rows={2}
                value={deleteReason}
                onChange={(event) => setDeleteReason(event.target.value)}
              />
            </Form.Item>
          </Form>
        </Space>
      </Modal>
    </div>
  )
}
