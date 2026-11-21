import { AdminTable } from '@/components/admin-table'
import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import {
  AudienceSelector,
  appHostingMutations,
  appHostingQueries,
  formatApprovalInvalidationReason,
  formatHostedAppBytes,
  hostedAppKindLabel,
  hostedAppRevisionStatusLabel,
  hostedAppStatusLabel,
  hostedAppStatusTagValue,
  hostedAppVisibilitySummary,
  resolveSafeHostedAppUrl,
  type AppHostingAudienceUser,
  type HostedApp,
  type HostedAppRevision,
  type HostedAppVisibilityInput,
} from '@/features/app-hosting'
import {
  HostedAppRevisionLogs,
  HostedAppRuntimeEnvironment,
  HostedAppRuntimeUsage,
  HostedAppSensitivePublisherConsent,
  HostedAppVisitorAttributes,
} from '@/features/app-hosting/runtime-controls'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { CopyOutlined, ExportOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  App,
  Button,
  Card,
  Descriptions,
  Drawer,
  Input,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tabs,
  Typography,
} from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { formatPortalDateTime } from '../shared/formatters'

const { Paragraph, Text } = Typography

export interface HostedAppDetailDrawerProps {
  appId: string | null
  initialApp?: HostedApp
  currentUserId: string
  canPublish: boolean
  onClose: () => void
  onDeleted?: () => void
}

function formatCandidateLabel(user: AppHostingAudienceUser) {
  const base = user.displayName || user.username || user.id
  return user.department ? `${base} · ${user.department}` : base
}

export function HostedAppDetailDrawer({
  appId,
  initialApp,
  currentUserId,
  canPublish,
  onClose,
  onDeleted,
}: HostedAppDetailDrawerProps) {
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const queryClient = useQueryClient()
  const open = Boolean(appId)
  const resolvedAppId = appId ?? ''

  const detailQuery = useQuery(appHostingQueries.appDetail(resolvedAppId, open))
  const revisionsQuery = useQuery(appHostingQueries.revisions(resolvedAppId, { limit: 30 }, open))

  const app = readableQueryData(detailQuery) ?? initialApp
  const isOwner = Boolean(app && currentUserId && app.ownerUserId === currentUserId)

  const approvalsQuery = useQuery(
    appHostingQueries.myApprovals({ appId: resolvedAppId, limit: 5 }, open && isOwner),
  )

  const [activeTab, setActiveTab] = useState('overview')
  const [logRevision, setLogRevision] = useState<HostedAppRevision | null>(null)
  const canUseControls = open && !queryAccessDenied(detailQuery)
  const canEditControls = isOwner && canPublish && app?.status !== 'suspended_by_admin'
  const [nameDraft, setNameDraft] = useState('')
  const [descDraft, setDescDraft] = useState('')
  const [spaFallbackDraft, setSpaFallbackDraft] = useState(true)
  const [visibilityDraft, setVisibilityDraft] = useState<HostedAppVisibilityInput>({
    mode: 'private',
  })
  const [visibilityReason, setVisibilityReason] = useState('')
  const [maintainerIdsDraft, setMaintainerIdsDraft] = useState<string[]>([])
  const [maintainerSearch, setMaintainerSearch] = useState('')
  const [deleteConfirmName, setDeleteConfirmName] = useState('')
  const maintainerLabelCache = useRef(new Map<string, string>())

  useEffect(() => {
    if (!open) {
      setActiveTab('overview')
      setLogRevision(null)
      setDeleteConfirmName('')
      setMaintainerSearch('')
    }
  }, [open, resolvedAppId])

  useEffect(() => {
    if (!app) return
    setNameDraft(app.name)
    setDescDraft(app.description ?? '')
    setSpaFallbackDraft(app.spaFallback)
    setVisibilityDraft({
      mode: app.visibility.mode,
      teamIds: app.visibility.teamIds,
      userIds: app.visibility.userIds,
    })
    setVisibilityReason('')
    setMaintainerIdsDraft(app.maintainerUserIds ?? [])
    for (const maintainer of app.maintainers ?? []) {
      const teams = maintainer.teamNames.length ? ` · ${maintainer.teamNames.join(', ')}` : ''
      maintainerLabelCache.current.set(maintainer.userId, `${maintainer.displayName}${teams}`)
    }
  }, [app])

  const maintainerCandidatesQuery = useQuery(
    appHostingQueries.audienceCandidates(
      { query: maintainerSearch, limit: 25 },
      open && isOwner && activeTab === 'maintainers',
    ),
  )
  const maintainerCandidates = readableQueryData(maintainerCandidatesQuery)?.users ?? []

  useEffect(() => {
    for (const user of maintainerCandidates) {
      maintainerLabelCache.current.set(user.id, formatCandidateLabel(user))
    }
  }, [maintainerCandidates])

  const maintainerOptions = useMemo(() => {
    const merged = new Map<string, string>()
    for (const user of maintainerCandidates) {
      if (app && user.id === app.ownerUserId) continue
      merged.set(user.id, formatCandidateLabel(user))
    }
    for (const userId of maintainerIdsDraft) {
      if (app && userId === app.ownerUserId) continue
      if (!merged.has(userId)) {
        merged.set(userId, maintainerLabelCache.current.get(userId) ?? userId)
      }
    }
    return Array.from(merged, ([value, label]) => ({ value, label }))
  }, [app, maintainerCandidates, maintainerIdsDraft])

  const updateAppMutation = useOperationMutation(appHostingMutations.updateApp(queryClient), {
    label: ['更新应用信息', 'Update hosted app'],
    target: () => app?.name ?? resolvedAppId,
    receipt: (updated) => ({ message: `「${updated.name}」基本信息已保存` }),
  })

  const updateVisibilityMutation = useOperationMutation(
    appHostingMutations.updateVisibility(queryClient),
    {
      label: ['更新可见范围', 'Update visibility'],
      target: () => app?.name ?? resolvedAppId,
      receipt: (updated) => ({
        message:
          updated.pendingVisibilityApproval?.status === 'pending'
            ? '全公司公开申请已提交待审，当前范围继续生效'
            : '可见范围已立即更新生效',
      }),
    },
  )

  const withdrawApprovalMutation = useOperationMutation(
    appHostingMutations.withdrawVisibilityApproval(queryClient),
    {
      label: ['撤回全公司公开申请', 'Withdraw visibility approval'],
      target: () => app?.name ?? resolvedAppId,
      receipt: () => ({ message: '已撤回全公司公开申请，当前可见范围保持不变' }),
    },
  )

  const updateMaintainersMutation = useOperationMutation(
    appHostingMutations.updateMaintainers(queryClient),
    {
      label: ['更新协作维护人', 'Update collaborators'],
      target: () => app?.name ?? resolvedAppId,
      receipt: () => ({ message: '协作维护人已保存' }),
    },
  )

  const rollbackMutation = useOperationMutation(appHostingMutations.rollbackApp(queryClient), {
    label: ['回滚托管应用版本', 'Rollback hosted app revision'],
    target: () => app?.name ?? resolvedAppId,
    receipt: (receipt) => ({
      message: `已切换回版本 v${receipt.revision.revisionNumber}，固定链接保持不变`,
    }),
  })

  const offlineMutation = useOperationMutation(appHostingMutations.takeOffline(queryClient), {
    label: ['下线暂停托管应用', 'Take hosted app offline'],
    target: () => app?.name ?? resolvedAppId,
    receipt: () => ({ message: '应用已下线暂停，外部访问已立即阻断' }),
  })

  const onlineMutation = useOperationMutation(appHostingMutations.bringOnline(queryClient), {
    label: ['恢复上线托管应用', 'Bring hosted app online'],
    target: () => app?.name ?? resolvedAppId,
    receipt: () => ({ message: '应用已恢复上线' }),
  })

  const deleteMutation = useOperationMutation(appHostingMutations.deleteApp(queryClient), {
    label: ['彻底删除托管应用', 'Delete hosted app'],
    target: () => app?.name ?? resolvedAppId,
    receipt: () => ({ message: '托管应用及历史版本存储已彻底删除' }),
  })

  const anyBlocked =
    updateAppMutation.blocked ||
    updateVisibilityMutation.blocked ||
    withdrawApprovalMutation.blocked ||
    updateMaintainersMutation.blocked ||
    rollbackMutation.blocked ||
    offlineMutation.blocked ||
    onlineMutation.blocked ||
    deleteMutation.blocked

  const revisions = readableQueryData(revisionsQuery)?.items ?? []
  const approvals = readableQueryData(approvalsQuery)?.items ?? []

  const pendingApproval =
    app?.pendingVisibilityApproval?.status === 'pending'
      ? app.pendingVisibilityApproval
      : approvals.find((item) => item.status === 'pending')

  const latestHistoricalApproval = useMemo(() => {
    if (!app || app.visibility.mode === 'organization' || pendingApproval) return undefined
    const candidate =
      app.pendingVisibilityApproval && app.pendingVisibilityApproval.status !== 'pending'
        ? app.pendingVisibilityApproval
        : approvals[0]
    if (!candidate) return undefined
    if (candidate.status === 'rejected' || candidate.status === 'invalidated') {
      return candidate
    }
    return undefined
  }, [app, approvals, pendingApproval])

  const revisionColumns = useMemo<TableColumnsType<HostedAppRevision>>(
    () => [
      {
        key: 'revisionNumber',
        title: '版本',
        width: 120,
        render: (_, rev) => (
          <Space size={6}>
            <Text strong>v{rev.revisionNumber}</Text>
            {app?.activeRevisionId === rev.id ? (
              <StatusTag value="active" label="当前线上" />
            ) : null}
          </Space>
        ),
      },
      {
        key: 'bundleSha256',
        title: '包摘要',
        width: 140,
        render: (_, rev) => (
          <Text code>{rev.bundleSha256 ? rev.bundleSha256.slice(0, 12) : '-'}</Text>
        ),
      },
      {
        key: 'stats',
        title: '产物规模',
        width: 150,
        render: (_, rev) => (
          <Text>
            {rev.fileCount} 个文件 · {formatHostedAppBytes(rev.totalBytes)}
          </Text>
        ),
      },
      {
        key: 'status',
        title: '状态与诊断',
        width: 220,
        render: (_, rev) => (
          <Space orientation="vertical" size={2}>
            <StatusTag value={rev.status} label={hostedAppRevisionStatusLabel(rev.status)} />
            {rev.failureStage || rev.failureReason ? (
              <Text type="danger" style={{ fontSize: 12 }}>
                {[rev.failureStage, rev.failureReason].filter(Boolean).join('：')}
              </Text>
            ) : null}
          </Space>
        ),
      },
      {
        key: 'createdAt',
        title: '发布时间',
        width: 140,
        render: (_, rev) => formatPortalDateTime(rev.createdAt, localeCode),
      },
      {
        key: 'actions',
        title: '操作',
        width: 180,
        render: (_, rev) => {
          const canRollback =
            canPublish &&
            app?.status !== 'suspended_by_admin' &&
            rev.status === 'ready' &&
            rev.id !== app?.activeRevisionId
          const canReadLogs = canUseControls && rev.kind === 'dynamic'
          if (!canRollback && !canReadLogs) return <Text type="secondary">-</Text>
          return (
            <Space size={0} wrap>
              {canReadLogs ? (
                <Button size="small" type="link" onClick={() => setLogRevision(rev)}>
                  查看脱敏日志
                </Button>
              ) : null}
              {canRollback ? (
                <Popconfirm
                  title={`确认回滚到版本 v${rev.revisionNumber}？`}
                  description={
                    rev.kind === 'dynamic'
                      ? '回滚程序版本并继续使用当前数据库。兼容检查失败时，当前版本继续提供服务。'
                      : '回滚后固定访问地址保持不变，立即切换至该历史版本。'
                  }
                  okText="确认回滚"
                  cancelText="取消"
                  disabled={anyBlocked}
                  onConfirm={() => {
                    if (!app) return
                    rollbackMutation.mutate({
                      appId: app.id,
                      input: {
                        targetRevisionId: rev.id,
                        idempotencyKey: crypto.randomUUID(),
                      },
                    })
                  }}
                >
                  <Button size="small" type="link" disabled={anyBlocked}>
                    回滚到此版本
                  </Button>
                </Popconfirm>
              ) : null}
            </Space>
          )
        },
      },
    ],
    [anyBlocked, app, canPublish, canUseControls, localeCode, rollbackMutation],
  )

  const safeAppUrl = resolveSafeHostedAppUrl(app?.url)
  const audienceInvalid =
    visibilityDraft.mode === 'selected' &&
    (visibilityDraft.teamIds?.length ?? 0) === 0 &&
    (visibilityDraft.userIds?.length ?? 0) === 0

  return (
    <Drawer
      title={app ? `管理作品：${app.name}` : '作品详情'}
      open={open}
      size="large"
      destroyOnHidden
      closable={!anyBlocked}
      keyboard={!anyBlocked}
      onClose={() => {
        if (anyBlocked) return
        onClose()
      }}
    >
      <Space orientation="vertical" size={14} style={{ width: '100%' }}>
        <ReadFeedback query={detailQuery} label="托管应用详情" canRead={open} />
        <ReadFeedback query={revisionsQuery} label="历史版本列表" canRead={open} />
        {isOwner ? (
          <ReadFeedback query={approvalsQuery} label="公开审批记录" canRead={open && isOwner} />
        ) : null}

        {updateAppMutation.feedback}
        {updateVisibilityMutation.feedback}
        {withdrawApprovalMutation.feedback}
        {updateMaintainersMutation.feedback}
        {rollbackMutation.feedback}
        {offlineMutation.feedback}
        {onlineMutation.feedback}
        {deleteMutation.feedback}

        {app ? (
          <>
            {app.status === 'suspended_by_admin' ? (
              <ContextFeedback
                showIcon
                type="error"
                title="该应用已被平台管理员强制封停"
                description={`封停原因：${app.suspendedReason || '未注明原因'}。封停期间所有访问已被立即阻断，作者不能自行恢复上线，请联系平台管理员核实并解除处置。`}
              />
            ) : null}

            <Card size="small" variant="outlined">
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    flexWrap: 'wrap',
                    gap: 8,
                  }}
                >
                  <Space size={6} wrap>
                    <StatusTag
                      value={hostedAppStatusTagValue(app.status)}
                      label={hostedAppStatusLabel(app.status)}
                    />
                    <MetadataTag
                      tone={app.kind === 'dynamic' ? 'purple' : 'blue'}
                      label={hostedAppKindLabel(app.kind)}
                    />
                    <MetadataTag
                      tone={isOwner ? 'cyan' : 'default'}
                      label={isOwner ? '作者' : '协作者'}
                    />
                    {app.activeRevisionNumber ? (
                      <MetadataTag label={`当前版本 v${app.activeRevisionNumber}`} />
                    ) : null}
                  </Space>
                  <Space size={8} wrap>
                    <Button
                      size="small"
                      icon={<CopyOutlined />}
                      onClick={async () => {
                        try {
                          await navigator.clipboard.writeText(app.url)
                          void message.success('已复制固定分享链接')
                        } catch {
                          void message.error('复制失败，请手动复制地址')
                        }
                      }}
                    >
                      复制链接
                    </Button>
                    {safeAppUrl && app.status === 'online' ? (
                      <Button
                        size="small"
                        type="primary"
                        icon={<ExportOutlined />}
                        href={safeAppUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                      >
                        打开应用
                      </Button>
                    ) : (
                      <Button size="small" disabled icon={<ExportOutlined />}>
                        打开应用
                      </Button>
                    )}
                  </Space>
                </div>
                <div>
                  <Text type="secondary">固定 HTTPS 地址：</Text>
                  <Text code>{app.url}</Text>
                </div>
              </Space>
            </Card>

            <Tabs
              activeKey={activeTab}
              onChange={setActiveTab}
              items={[
                {
                  key: 'overview',
                  label: '概览与分享',
                  children: (
                    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                      {pendingApproval ? (
                        <ContextFeedback
                          showIcon
                          type="warning"
                          title="全公司公开待审批，当前范围继续生效"
                          description={
                            <Space orientation="vertical" size={6}>
                              <Text>
                                当前生效范围仍为「{hostedAppVisibilitySummary(app.visibility)}
                                」，待管理员批准后才会切换为全公司可见。
                                {pendingApproval.reason
                                  ? ` 申请说明：${pendingApproval.reason}`
                                  : ''}
                              </Text>
                              {isOwner ? (
                                <Button
                                  size="small"
                                  disabled={anyBlocked}
                                  loading={withdrawApprovalMutation.isPending}
                                  onClick={() => withdrawApprovalMutation.mutate({ appId: app.id })}
                                >
                                  撤回申请
                                </Button>
                              ) : null}
                            </Space>
                          }
                        />
                      ) : latestHistoricalApproval?.status === 'rejected' ? (
                        <ContextFeedback
                          showIcon
                          type="warning"
                          title="最近一次全公司公开申请已被拒绝"
                          description={`审批意见：${latestHistoricalApproval.reviewComment || '未填写意见'}。当前仍保持原可见范围，调整后可重新提交申请。`}
                        />
                      ) : latestHistoricalApproval?.status === 'invalidated' ? (
                        <ContextFeedback
                          showIcon
                          type="info"
                          title="最近一次全公司公开申请已失效"
                          description={`失效原因：${formatApprovalInvalidationReason(latestHistoricalApproval.invalidationReason)}。如需对全公司公开，可重新提交申请。`}
                        />
                      ) : null}

                      {isOwner ? (
                        <>
                          <Card size="small" title="基本信息与路由策略" variant="outlined">
                            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                              <div>
                                <Text>应用名称</Text>
                                <Input
                                  aria-label="编辑应用名称"
                                  disabled={anyBlocked}
                                  maxLength={128}
                                  style={{ marginTop: 4 }}
                                  value={nameDraft}
                                  onChange={(event) => setNameDraft(event.target.value)}
                                />
                              </div>
                              <div>
                                <Text>一句话简介</Text>
                                <Input.TextArea
                                  aria-label="编辑应用简介"
                                  disabled={anyBlocked}
                                  maxLength={1024}
                                  rows={2}
                                  style={{ marginTop: 4 }}
                                  value={descDraft}
                                  onChange={(event) => setDescDraft(event.target.value)}
                                />
                              </div>
                              <div
                                style={{
                                  display: 'flex',
                                  alignItems: 'center',
                                  justifyContent: 'space-between',
                                  gap: 12,
                                }}
                              >
                                <div>
                                  <Text>SPA 前端深链接回退（index.html）</Text>
                                  <div>
                                    <Text type="secondary" style={{ fontSize: 12 }}>
                                      子域标识 {app.dnsLabel} 保持不变
                                    </Text>
                                  </div>
                                </div>
                                <Switch
                                  aria-label="编辑 SPA 回退开关"
                                  checked={spaFallbackDraft}
                                  disabled={anyBlocked}
                                  onChange={setSpaFallbackDraft}
                                />
                              </div>
                              <div>
                                <Button
                                  type="primary"
                                  disabled={anyBlocked || !nameDraft.trim()}
                                  loading={updateAppMutation.isPending}
                                  onClick={() =>
                                    updateAppMutation.mutate({
                                      appId: app.id,
                                      input: {
                                        name: nameDraft.trim(),
                                        description: descDraft.trim(),
                                        spaFallback: spaFallbackDraft,
                                      },
                                    })
                                  }
                                >
                                  保存基本信息
                                </Button>
                              </div>
                            </Space>
                          </Card>

                          <Card size="small" title="可见范围与分享控制" variant="outlined">
                            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                              <AudienceSelector
                                disabled={anyBlocked}
                                previousVisibility={app.visibility}
                                reason={visibilityReason}
                                value={visibilityDraft}
                                onChange={setVisibilityDraft}
                                onReasonChange={setVisibilityReason}
                              />
                              <div>
                                <Button
                                  type="primary"
                                  disabled={anyBlocked || audienceInvalid}
                                  loading={updateVisibilityMutation.isPending}
                                  onClick={() =>
                                    updateVisibilityMutation.mutate({
                                      appId: app.id,
                                      input: {
                                        visibility: visibilityDraft,
                                        ...(visibilityDraft.mode === 'organization' &&
                                        visibilityReason.trim()
                                          ? { reason: visibilityReason.trim() }
                                          : {}),
                                      },
                                    })
                                  }
                                >
                                  保存可见范围
                                </Button>
                              </div>
                            </Space>
                          </Card>
                        </>
                      ) : (
                        <>
                          <ContextFeedback
                            showIcon
                            type="info"
                            title="协作维护人只读概览"
                            description="您是该应用的协作维护人，可在「版本与回滚」中回滚历史版本或通过 CLI / AI 助手发布新版本；修改可见范围、管理协作者、上下线与彻底删除仅限应用作者操作。"
                          />
                          <Descriptions
                            bordered
                            column={1}
                            size="small"
                            items={[
                              { key: 'name', label: '应用名称', children: app.name },
                              {
                                key: 'description',
                                label: '应用简介',
                                children: app.description || '-',
                              },
                              {
                                key: 'owner',
                                label: '应用作者',
                                children: app.ownerProfile?.displayName || app.ownerUserId,
                              },
                              {
                                key: 'visibility',
                                label: '当前可见范围',
                                children: hostedAppVisibilitySummary(app.visibility),
                              },
                              {
                                key: 'spaFallback',
                                label: 'SPA 路由回退',
                                children: app.spaFallback ? '已开启' : '未开启',
                              },
                            ]}
                          />
                        </>
                      )}
                    </Space>
                  ),
                },
                {
                  key: 'maintainers',
                  label: '协作者管理',
                  children: (
                    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                      <ContextFeedback
                        showIcon
                        type="info"
                        title="协作权限边界说明"
                        description={
                          app.kind === 'dynamic'
                            ? '动态应用协作发布需要作者单独授权。访问线上页面仍由可见范围独立控制。'
                            : '协作维护人可上传发布新版本与执行历史版本回滚，但不能修改可见范围、上下线或删除应用；访问线上页面仍由可见范围独立控制。'
                        }
                      />
                      {isOwner &&
                      app.kind === 'dynamic' &&
                      canUseControls &&
                      activeTab === 'maintainers' ? (
                        <HostedAppSensitivePublisherConsent
                          key={app.id}
                          app={app}
                          canEdit={canEditControls && !anyBlocked}
                        />
                      ) : null}
                      {isOwner ? (
                        <Card size="small" title="编辑协作维护人" variant="outlined">
                          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                            <ReadFeedback
                              query={maintainerCandidatesQuery}
                              label="协作者候选检索"
                              canRead={open && isOwner && activeTab === 'maintainers'}
                            />
                            <Select
                              allowClear
                              aria-label="搜索并选择协作者"
                              disabled={anyBlocked}
                              loading={maintainerCandidatesQuery.isFetching}
                              mode="multiple"
                              options={maintainerOptions}
                              placeholder="输入同事姓名或账号搜索添加协作维护人"
                              showSearch={{
                                filterOption: false,
                                onSearch: setMaintainerSearch,
                              }}
                              style={{ width: '100%' }}
                              value={maintainerIdsDraft}
                              onChange={(nextIds: string[]) => setMaintainerIdsDraft(nextIds)}
                            />
                            <div>
                              <Button
                                type="primary"
                                disabled={anyBlocked}
                                loading={updateMaintainersMutation.isPending}
                                onClick={() =>
                                  updateMaintainersMutation.mutate({
                                    appId: app.id,
                                    input: { maintainerUserIds: maintainerIdsDraft },
                                  })
                                }
                              >
                                保存协作者
                              </Button>
                            </div>
                          </Space>
                        </Card>
                      ) : (
                        <Card size="small" title="当前协作维护人" variant="outlined">
                          {(app.maintainers ?? []).length > 0 ? (
                            <Space wrap size={[8, 8]}>
                              {(app.maintainers ?? []).map((member) => (
                                <MetadataTag
                                  key={member.userId}
                                  label={
                                    member.teamNames.length > 0
                                      ? `${member.displayName}（${member.teamNames.join(', ')}）`
                                      : member.displayName
                                  }
                                />
                              ))}
                            </Space>
                          ) : (
                            <Text type="secondary">暂无其他协作者</Text>
                          )}
                        </Card>
                      )}
                    </Space>
                  ),
                },
                {
                  key: 'revisions',
                  label: '版本与回滚',
                  children: (
                    <AdminTable
                      enableColumnSelection={false}
                      rowKey="id"
                      columns={revisionColumns}
                      dataSource={revisions}
                      error={revisionsQuery.error}
                      hasReadData={Boolean(readableQueryData(revisionsQuery))}
                      loading={revisionsQuery.isLoading}
                      refreshing={revisionsQuery.isFetching}
                      onRefresh={() => revisionsQuery.refetch()}
                      onRetry={() => revisionsQuery.refetch()}
                      pagination={{ pageSize: 10 }}
                    />
                  ),
                },
                ...(app.kind === 'dynamic' && canUseControls
                  ? [
                      {
                        key: 'runtime',
                        label: '运行配置',
                        children:
                          activeTab === 'runtime' ? (
                            <Space
                              key={app.id}
                              orientation="vertical"
                              size={12}
                              style={{ width: '100%' }}
                            >
                              <HostedAppRuntimeUsage
                                appId={app.id}
                                canSetWarning={canEditControls}
                              />
                              {isOwner ? (
                                <HostedAppRuntimeEnvironment
                                  appId={app.id}
                                  ownerId={currentUserId}
                                  canEdit={canEditControls}
                                />
                              ) : null}
                            </Space>
                          ) : null,
                      },
                    ]
                  : []),
                ...(isOwner && canUseControls
                  ? [
                      {
                        key: 'visitor-attributes',
                        label: '访客属性',
                        children:
                          activeTab === 'visitor-attributes' ? (
                            <HostedAppVisitorAttributes
                              key={app.id}
                              appId={app.id}
                              access={{ role: 'owner', canRequest: canEditControls }}
                            />
                          ) : null,
                      },
                    ]
                  : []),
                ...(isOwner
                  ? [
                      {
                        key: 'lifecycle',
                        label: '上下线与删除',
                        children: (
                          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                            <Card
                              size="small"
                              title="服务上下线（保留版本数据）"
                              variant="outlined"
                            >
                              <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                                <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                                  下线暂停后，包括作者本人在内的所有外部访问立即阻断，但历史版本与固定子域绑定完整保留，后续可随时一键恢复上线。
                                </Paragraph>
                                {app.status === 'online' ? (
                                  <div>
                                    <Popconfirm
                                      title="确认下线暂停此应用？"
                                      description="下线后所有访问请求立即拒绝，历史版本继续保留。"
                                      okText="确认下线"
                                      cancelText="取消"
                                      disabled={anyBlocked}
                                      onConfirm={() => offlineMutation.mutate({ appId: app.id })}
                                    >
                                      <Button
                                        danger
                                        disabled={anyBlocked}
                                        loading={offlineMutation.isPending}
                                      >
                                        下线暂停
                                      </Button>
                                    </Popconfirm>
                                  </div>
                                ) : app.status === 'suspended_by_admin' ? (
                                  <div>
                                    <Button disabled>已被管理员封停（不可自行恢复上线）</Button>
                                  </div>
                                ) : (
                                  <div>
                                    <Button
                                      type="primary"
                                      disabled={anyBlocked || !app.activeRevisionId}
                                      loading={onlineMutation.isPending}
                                      onClick={() => onlineMutation.mutate({ appId: app.id })}
                                    >
                                      恢复上线
                                    </Button>
                                    {!app.activeRevisionId ? (
                                      <Text
                                        type="secondary"
                                        style={{ display: 'block', marginTop: 6, fontSize: 12 }}
                                      >
                                        需先上传至少一个就绪版本后方可上线。
                                      </Text>
                                    ) : null}
                                  </div>
                                )}
                              </Space>
                            </Card>

                            <Card size="small" title="彻底删除应用（不可逆）" variant="outlined">
                              <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                                <ContextFeedback
                                  showIcon
                                  type="error"
                                  title="不可恢复操作"
                                  description="彻底删除将永久销毁该应用记录、释放授权关系并清理对象存储中的全部历史版本文件，删除后无法恢复。"
                                />
                                <div>
                                  <Text>
                                    请输入应用名称 <Text code>{app.name}</Text> 以确认删除：
                                  </Text>
                                  <Input
                                    aria-label="确认删除应用名称"
                                    disabled={anyBlocked}
                                    placeholder={app.name}
                                    style={{ marginTop: 6 }}
                                    value={deleteConfirmName}
                                    onChange={(event) => setDeleteConfirmName(event.target.value)}
                                  />
                                </div>
                                <div>
                                  <Button
                                    danger
                                    type="primary"
                                    disabled={anyBlocked || deleteConfirmName !== app.name}
                                    loading={deleteMutation.isPending}
                                    onClick={() =>
                                      deleteMutation.mutate(
                                        {
                                          appId: app.id,
                                          input: { confirmationName: deleteConfirmName },
                                        },
                                        {
                                          onSuccess: () => {
                                            onDeleted?.()
                                            onClose()
                                          },
                                        },
                                      )
                                    }
                                  >
                                    确认彻底删除
                                  </Button>
                                </div>
                              </Space>
                            </Card>
                          </Space>
                        ),
                      },
                    ]
                  : []),
              ]}
            />
            <HostedAppRevisionLogs
              appId={app.id}
              revision={
                canUseControls && activeTab === 'revisions' && logRevision?.appId === app.id
                  ? logRevision
                  : null
              }
              onClose={() => setLogRevision(null)}
            />
          </>
        ) : null}
      </Space>
    </Drawer>
  )
}
