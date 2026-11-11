import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { DeleteOutlined, EditOutlined, KeyOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Button,
  Form,
  Popconfirm,
  Select,
  Space,
  Tooltip,
  Typography,
  type TableColumnsType,
} from 'antd'
import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { identityProviderQueries } from '../providers'
import { identityRuntimeQueries } from '../runtime'
import '../shared/application-access.css'
import { createIdentityOutpost, rotateIdentityOutpostToken } from './api'
import { OutpostDetailDrawer } from './components/outpost-detail-drawer'
import { IdentityOutpostFormModal } from './components/outpost-form-modal'
import {
  IdentityOutpostTokenModal,
  type IdentityOutpostCreatedToken,
} from './components/outpost-token-modal'
import { providersForOutpost } from './deployment-model'
import { identityOutpostMutations } from './mutations'
import { identityOutpostModeOptions } from './options'
import { identityOutpostQueries } from './queries'
import {
  outpostConfigurationSummary,
  outpostRuntimeLabels,
  outpostRuntimeReason,
} from './runtime-model'
import './styles.css'
import type {
  IdentityOutpost,
  IdentityOutpostFilters,
  IdentityOutpostInput,
  IdentityOutpostMode,
} from './types'
const { Text } = Typography
interface IdentityOutpostPageFilters extends IdentityOutpostFilters {
  query: string
  runtimeStatus?: IdentityOutpost['runtimeStatus'] | ''
}
function modeTag(mode: IdentityOutpostMode) {
  const tone =
    mode === 'embedded'
      ? 'blue'
      : mode === 'kubernetes'
        ? 'purple'
        : mode === 'external'
          ? 'cyan'
          : 'default'
  return <MetadataTag label={mode} tone={tone} />
}
function formatDateTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}
export function IdentityOutpostsPage() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [queryForm] = Form.useForm<IdentityOutpostPageFilters>()
  const [filters, setFilters] = useState<IdentityOutpostPageFilters>({
    mode: '',
    runtimeStatus: '',
    query: '',
  })
  const [params, setParams] = useSearchParams()
  const showDetails = (id: string, view = 'overview') =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.set('outpost', id)
      next.set('outpostTab', view)
      return next
    })
  const closeDetails = () =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('outpost')
      next.delete('outpostTab')
      return next
    })
  const [editing, setEditing] = useState<IdentityOutpost | null>(null)
  const [pagination, setPagination] = useState({ current: 1, pageSize: 15 })
  const [modalOpen, setModalOpen] = useState(false)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [createdToken, setCreatedToken] = useState<IdentityOutpostCreatedToken | null>(null)
  const snapshot = usePermissionSnapshot().data?.data
  const canCreate = hasPermission(snapshot, 'identity.outposts.create')
  const canUpdate = hasPermission(snapshot, 'identity.outposts.update')
  const canDelete = hasPermission(snapshot, 'identity.outposts.delete')
  const canRotate = hasPermission(snapshot, 'identity.outposts.rotate')
  const canViewProviders = hasPermission(snapshot, 'identity.providers.view')
  const providerBindings = useQuery({
    ...identityProviderQueries.list({ type: 'proxy' }),
    enabled: canViewProviders,
  })
  const visibleProviderBindingsData =
    providerBindings.isEnabled === false ? undefined : readableQueryData(providerBindings)
  const outpostsQuery = useQuery({
    ...identityOutpostQueries.list({ mode: filters.mode }),
    refetchInterval: 15000,
  })
  const visibleOutpostsQueryData =
    outpostsQuery.isEnabled === false ? undefined : readableQueryData(outpostsQuery)
  const runtimeQuery = useQuery(identityRuntimeQueries.capabilities())
  const visibleRuntimeQueryData =
    runtimeQuery.isEnabled === false ? undefined : readableQueryData(runtimeQuery)
  const allowedModes = useMemo<IdentityOutpostMode[]>(() => {
    const capability = visibleRuntimeQueryData?.outpost
    if (!capability) return []
    return [
      capability.embeddedRuntime.available && 'embedded',
      capability.agentRuntime.available && 'agent',
      capability.kubernetesArtifact.available && 'kubernetes',
      capability.externalProtocol.available && 'external',
    ].filter((mode): mode is IdentityOutpostMode => Boolean(mode))
  }, [visibleRuntimeQueryData])
  const createMutation = useOperationMutation(
    {
      ...identityOutpostMutations.create(queryClient),
      gcTime: 0,
      mutationFn: async (input: IdentityOutpostInput) => {
        const result = await createIdentityOutpost(input)
        if (result.token) setCreatedToken({ name: result.name, token: result.token })
        return { ...result, token: undefined }
      },
    },
    { label: ['创建 Outpost', 'Create Outpost'], target: (v) => v.name },
  )
  const updateMutation = useOperationMutation(
    {
      ...identityOutpostMutations.update(queryClient),
    },
    { label: ['保存 Outpost', 'Save Outpost'], target: (v) => v.outpostId },
  )
  const deleteMutation = useOperationMutation(
    {
      ...identityOutpostMutations.remove(queryClient),
    },
    { label: ['删除 Outpost', 'Delete Outpost'], target: (v) => v },
  )
  const rotateTokenMutation = useOperationMutation(
    {
      ...identityOutpostMutations.rotateToken(queryClient),
      gcTime: 0,
      mutationFn: async (input: string) => {
        const result = await rotateIdentityOutpostToken(input)
        if (result.token) setCreatedToken({ name: result.name, token: result.token })
        return { ...result, token: undefined }
      },
    },
    { label: ['轮换 Outpost Token', 'Rotate token Outpost'], target: (v) => v },
  )
  const operationBlocked =
    createMutation.blocked ||
    updateMutation.blocked ||
    deleteMutation.blocked ||
    rotateTokenMutation.blocked ||
    (outpostsQuery.isEnabled !== false && queryAccessDenied(outpostsQuery))
  const outposts = visibleOutpostsQueryData ?? []
  const filteredOutposts = useMemo(() => {
    const keyword = filters.query.trim().toLowerCase()
    return outposts.filter(
      (item) =>
        (!filters.runtimeStatus || item.runtimeStatus === filters.runtimeStatus) &&
        (!keyword ||
          [
            item.name,
            item.mode,
            item.runtimeStatus,
            item.forwardAuthUrl,
            item.endpoint,
            item.runtimeVersion,
          ].some((value) =>
            String(value ?? '')
              .toLowerCase()
              .includes(keyword),
          )),
    )
  }, [filters.query, filters.runtimeStatus, outposts])
  const openCreate = () => {
    if (operationBlocked) return
    setEditing(null)
    setModalOpen(true)
  }
  const openEdit = (outpost: IdentityOutpost) => {
    if (operationBlocked) return
    setEditing(outpost)
    setModalOpen(true)
  }
  const submitForm = (input: IdentityOutpostInput) => {
    if (editing) {
      updateMutation.mutate(
        { outpostId: editing.id, input },
        {
          onSuccess: () => {
            setModalOpen(false)
            setEditing(null)
          },
        },
      )
      return
    }
    createMutation.mutate(input, {
      onSuccess: (outpost) => {
        showDetails(outpost.id)
        if (outpost.token) {
          setCreatedToken({ name: outpost.name, token: outpost.token })
        }
        setModalOpen(false)
        setEditing(null)
      },
    })
  }
  const currentPage = Math.min(
    pagination.current,
    Math.max(1, Math.ceil(filteredOutposts.length / pagination.pageSize)),
  )
  const nodeActions = (outpost: IdentityOutpost) => (
    <Space size={4}>
      <ManagementIconButton
        disabled={operationBlocked || !canUpdate}
        icon={<EditOutlined />}
        onClick={() => openEdit(outpost)}
        tooltip="编辑 Outpost"
      />
      <Popconfirm
        title="轮换 Outpost token"
        description="旧 token 将立即失效。确认已准备更新 Outpost 配置。"
        disabled={operationBlocked || !canRotate}
        okButtonProps={{
          ...{ loading: rotateTokenMutation.isPending },
          disabled: operationBlocked,
        }}
        okText="轮换"
        onConfirm={() =>
          rotateTokenMutation.mutate(outpost.id, {
            onSuccess: (result) => {
              if (result.token) setCreatedToken({ name: result.name, token: result.token })
            },
          })
        }
      >
        <ManagementIconButton disabled={!canRotate} icon={<KeyOutlined />} tooltip="轮换 token" />
      </Popconfirm>
      <Popconfirm
        title="删除 Outpost"
        description={`确认删除 ${outpost.name}？`}
        disabled={operationBlocked || !canDelete}
        okButtonProps={{
          ...{ danger: true, loading: deleteMutation.isPending },
          disabled: operationBlocked,
        }}
        okText="删除"
        onConfirm={() =>
          deleteMutation.mutate(outpost.id, {
            onSuccess: () => {
              if (params.get('outpost') === outpost.id) closeDetails()
            },
          })
        }
      >
        <ManagementIconButton
          danger
          disabled={!canDelete}
          icon={<DeleteOutlined />}
          tooltip="删除 Outpost"
        />
      </Popconfirm>
    </Space>
  )
  const columns: TableColumnsType<IdentityOutpost> = [
    {
      title: '接入节点',
      dataIndex: 'name',
      width: 250,
      render: (_, outpost) => (
        <Space orientation="vertical" size={2}>
          <Link to={'?outpost=' + encodeURIComponent(outpost.id)}>{outpost.name}</Link>
          {outpost.mode !== 'embedded' && (
            <Button type="link" size="small" onClick={() => showDetails(outpost.id, 'deploy')}>
              查看部署
            </Button>
          )}
        </Space>
      ),
    },
    { title: '模式', dataIndex: 'mode', width: 100, render: modeTag },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'runtimeStatus',
      render: (_, outpost) => (
        <Tooltip
          title={
            outpost.runtimeStatus === 'available'
              ? undefined
              : outpostRuntimeReason(outpost.runtimeReason)
          }
        >
          <span tabIndex={outpost.runtimeStatus === 'available' ? undefined : 0}>
            <StatusTag
              value={outpost.runtimeStatus}
              label={outpostRuntimeLabels[outpost.runtimeStatus]}
            />
          </span>
        </Tooltip>
      ),
    },
    {
      title: '配置同步',
      key: 'configuration',
      width: 140,
      render: (_, outpost) => outpostConfigurationSummary(outpost),
    },
    {
      title: '关联应用',
      key: 'applications',
      width: 120,
      render: (_, outpost) =>
        !canViewProviders ? (
          '无查看权限'
        ) : providerBindings.isError && !visibleProviderBindingsData ? (
          <Button
            type="link"
            size="small"
            onClick={() =>
              void (providerBindings.isEnabled === false ? undefined : providerBindings.refetch())
            }
          >
            加载失败，重试
          </Button>
        ) : providerBindings.isSuccess ? (
          <Button type="link" size="small" onClick={() => showDetails(outpost.id, 'apps')}>
            {
              new Set(
                providersForOutpost(visibleProviderBindingsData ?? [], outpost.id).map(
                  (provider) => provider.applicationId,
                ),
              ).size
            }{' '}
            个应用
          </Button>
        ) : (
          '加载中'
        ),
    },
    {
      ...tableColumnPresets.datetime,
      title: '最近心跳',
      key: 'heartbeat',
      render: (_, outpost) =>
        outpost.mode === 'embedded' ? (
          <Text type="secondary">—</Text>
        ) : !outpost.lastHeartbeatAt ? (
          '尚未收到'
        ) : (
          formatDateTime(outpost.lastHeartbeatAt)
        ),
    },
    {
      ...tableColumnPresets.action,
      key: 'actions',
      render: (_, outpost) => nodeActions(outpost),
    },
  ]
  return (
    <>
      {!(modalOpen && !(outpostsQuery.isEnabled !== false && queryAccessDenied(outpostsQuery))) && (
        <>
          {' '}
          {createMutation.feedback}
          {updateMutation.feedback}
          {deleteMutation.feedback}
          {rotateTokenMutation.feedback}{' '}
        </>
      )}

      <ReadFeedback
        canRead={providerBindings.isEnabled}
        query={providerBindings}
        label="身份数据"
      />

      <ReadFeedback canRead={runtimeQuery.isEnabled} query={runtimeQuery} label="运行能力" />

      <ManagementDataPage
        className="soha-identity-access-page soha-identity-outposts-page"
        query={{
          actions: (
            <ManagementQueryActions
              disabledReset={!filters.query && !filters.mode && !filters.runtimeStatus}
              loading={outpostsQuery.isFetching}
              onReset={() => {
                queryForm.resetFields()
                setFilters({ mode: '', runtimeStatus: '', query: '' })
                setPagination((current) => ({ ...current, current: 1 }))
              }}
            />
          ),
          children: (
            <>
              <ManagementKeywordField
                label={t('common.keyword', '关键词')}
                name="query"
                placeholder={t('identity.outposts.search', '搜索名称、endpoint、版本')}
              />
              <ManagementQueryField
                label={t('identity.outposts.mode', '模式')}
                name="mode"
                width={180}
                minWidth={180}
              >
                <Select
                  allowClear
                  options={identityOutpostModeOptions}
                  placeholder={t('identity.outposts.mode', '模式')}
                />
              </ManagementQueryField>
              <ManagementQueryField
                label={t('identity.outposts.status', '状态')}
                name="runtimeStatus"
                width={180}
                minWidth={180}
              >
                <Select
                  allowClear
                  options={Object.entries(outpostRuntimeLabels).map(([value, label]) => ({
                    value,
                    label,
                  }))}
                  placeholder={t('identity.outposts.status', '状态')}
                />
              </ManagementQueryField>
            </>
          ),
          form: queryForm,
          initialValues: { mode: '', runtimeStatus: '', query: '' },
          onFinish: (values) => {
            setFilters({
              mode: values.mode ?? '',
              runtimeStatus: values.runtimeStatus ?? '',
              query: String(values.query ?? '').trim(),
            })
            setPagination((current) => ({ ...current, current: 1 }))
          },
        }}
        beforeQuery={
          outpostsQuery.isError && !visibleOutpostsQueryData ? null : runtimeQuery.isError &&
            !visibleRuntimeQueryData ? (
            <ManagementState
              kind="error"
              title="运行能力加载失败"
              description={runtimeQuery.error.message}
              actions={
                <Button
                  onClick={() =>
                    void (runtimeQuery.isEnabled === false ? undefined : runtimeQuery.refetch())
                  }
                >
                  重试
                </Button>
              }
            />
          ) : null
        }
        table={{
          error: outpostsQuery.isEnabled === false ? null : outpostsQuery.error,
          hasReadData:
            readableQueryData(outpostsQuery) !== undefined && outpostsQuery.isEnabled !== false,
          onRetry: () => {
            if (outpostsQuery.isEnabled !== false) void outpostsQuery.refetch()
          },
          rowKey: 'id',
          columns,
          dataSource: outpostsQuery.isError && !visibleOutpostsQueryData ? [] : filteredOutposts,
          loading: outpostsQuery.isLoading || outpostsQuery.isFetching,
          tableSize,
          columnSettingPlacement: 'header',
          columnSettingIconOnly: true,
          pagination: {
            current: currentPage,
            pageSize: pagination.pageSize,
            onChange: (current: number, pageSize: number) => setPagination({ current, pageSize }),
          },
          empty: (
            <ManagementState
              kind={outpostsQuery.isError && !visibleOutpostsQueryData ? 'error' : 'empty'}
              title={
                outpostsQuery.isError && !visibleOutpostsQueryData
                  ? '接入节点加载失败'
                  : '暂无接入节点'
              }
              description={
                filters.query || filters.mode || filters.runtimeStatus
                  ? '没有符合条件的接入节点，请调整筛选条件。'
                  : '创建内嵌或远程节点后，可在认证接入中关联应用。'
              }
            />
          ),
          headerExtra: (
            <ManagementTableToolbar>
              <Button
                disabled={operationBlocked || !canCreate || allowedModes.length === 0}
                icon={<PlusOutlined />}
                onClick={openCreate}
                type="primary"
              >
                {t('identity.outposts.create', '新建 Outpost')}
              </Button>
              <ManagementDensityButton
                aria-label={t('table.density', '切换表格密度')}
                tooltip={t('table.density', '切换表格密度')}
                onClick={() => setTableSize((size) => (size === 'small' ? 'middle' : 'small'))}
              />
              <ManagementRefreshButton
                aria-label={t('common.refresh', '刷新')}
                loading={outpostsQuery.isFetching}
                onClick={() =>
                  void (outpostsQuery.isEnabled === false ? undefined : outpostsQuery.refetch())
                }
                tooltip={t('common.refresh', '刷新')}
              />
            </ManagementTableToolbar>
          ),
        }}
      />

      <IdentityOutpostFormModal
        blocked={operationBlocked}
        feedback={
          <>
            {createMutation.feedback}
            {updateMutation.feedback}
          </>
        }
        allowedModes={allowedModes}
        editing={editing}
        onCancel={() => {
          if (!operationBlocked) (() => setModalOpen(false))()
        }}
        onSubmit={submitForm}
        open={modalOpen && !(outpostsQuery.isEnabled !== false && queryAccessDenied(outpostsQuery))}
        submitting={createMutation.isPending || updateMutation.isPending}
      />

      {params.get('outpost') && (
        <OutpostDetailDrawer
          id={params.get('outpost') ?? ''}
          initialTab={params.get('outpostTab') ?? 'overview'}
          onClose={() => {
            if (!operationBlocked) closeDetails()
          }}
          onEdit={openEdit}
        />
      )}
      <IdentityOutpostTokenModal
        onClose={() => {
          if (!operationBlocked)
            (() => {
              setCreatedToken(null)
              createMutation.reset()
              rotateTokenMutation.reset()
            })()
        }}
        value={createdToken}
      />
    </>
  )
}
