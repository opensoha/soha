import { ContextFeedback } from '@/components/context-feedback'
import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { CopyOutlined, ExportOutlined, SettingOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Input, Modal, Segmented, Select, Space, Switch, Typography } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { Link, useInRouterContext } from 'react-router-dom'
import { AudienceSelector } from './audience-selector'
import { appHostingMutations } from './mutations'
import { appHostingQueries } from './queries'
import {
  resolveSafeHostedAppUrl,
  type HostedApp,
  type HostedAppVisibilityInput,
  type WorkspaceHostedAppDeploymentRequest,
  type WorkspaceHostedAppDeploymentResult,
  type WorkspaceHostedAppSource,
} from './types'

const { Paragraph, Text } = Typography

type PublishTargetMode = 'create' | 'update'

export interface WorkspacePublishModalProps {
  open: boolean
  onClose: () => void
  source: WorkspaceHostedAppSource
  sourceSummary: string
  defaultName?: string
  defaultDescription?: string
  defaultSpaFallback?: boolean
  currentUserId?: string
  onPublished?: (result: WorkspaceHostedAppDeploymentResult) => void
}

function MyAppsEntryButton({ onNavigate }: { onNavigate?: () => void }) {
  const inRouter = useInRouterContext()
  if (inRouter) {
    return (
      <Link to="/portal/my-apps" onClick={onNavigate}>
        <Button icon={<SettingOutlined />}>前往我的作品管理</Button>
      </Link>
    )
  }
  return (
    <Button href="/portal/my-apps" icon={<SettingOutlined />} onClick={onNavigate}>
      前往我的作品管理
    </Button>
  )
}

export function WorkspacePublishModal({
  open,
  onClose,
  source,
  sourceSummary,
  defaultName = '',
  defaultDescription = '',
  defaultSpaFallback = true,
  currentUserId = '',
  onPublished,
}: WorkspacePublishModalProps) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()

  const [targetMode, setTargetMode] = useState<PublishTargetMode>('create')
  const [selectedAppId, setSelectedAppId] = useState('')
  const [name, setName] = useState(defaultName)
  const [description, setDescription] = useState(defaultDescription)
  const [spaFallback, setSpaFallback] = useState(defaultSpaFallback)
  const [visibility, setVisibility] = useState<HostedAppVisibilityInput>({ mode: 'private' })
  const [visibilityReason, setVisibilityReason] = useState('')
  const [visibilityChanged, setVisibilityChanged] = useState(false)
  const [idempotencyKey, setIdempotencyKey] = useState(() => crypto.randomUUID())
  const [publishedResult, setPublishedResult] = useState<WorkspaceHostedAppDeploymentResult | null>(
    null,
  )

  const myAppsQuery = useQuery(appHostingQueries.myApps({ limit: 100 }, open))
  const availableApps = useMemo(
    () =>
      (readableQueryData(myAppsQuery)?.items ?? []).filter(
        (item) => item.status !== 'suspended_by_admin' && item.status !== 'offline',
      ),
    [myAppsQuery],
  )

  const selectedApp: HostedApp | undefined = useMemo(
    () => availableApps.find((item) => item.id === selectedAppId),
    [availableApps, selectedAppId],
  )
  const isSelectedAppOwner = !selectedApp || !currentUserId || selectedApp.ownerUserId === currentUserId

  const deployMutation = useOperationMutation(appHostingMutations.deployFromWorkspace(queryClient), {
    label: ['工作区发布托管应用', 'Publish hosted app from workspace'],
    target: (input) => input.name || selectedApp?.name || sourceSummary,
    receipt: (result) => ({
      message: result.created ? '托管应用已创建并上线' : '托管应用版本已更新',
      evidence: `固定访问地址：${result.app.url}`,
    }),
  })
  const resetDeployMutation = deployMutation.reset

  useEffect(() => {
    if (!open) return
    setTargetMode('create')
    setSelectedAppId('')
    setName(defaultName)
    setDescription(defaultDescription)
    setSpaFallback(defaultSpaFallback)
    setVisibility({ mode: 'private' })
    setVisibilityReason('')
    setVisibilityChanged(false)
    setIdempotencyKey(crypto.randomUUID())
    setPublishedResult(null)
    resetDeployMutation()
  }, [defaultDescription, defaultName, defaultSpaFallback, open, resetDeployMutation])

  const refreshIdempotencyKey = () => {
    if (!deployMutation.isPending && !deployMutation.outcomeUnknown) {
      setIdempotencyKey(crypto.randomUUID())
    }
  }

  const handleSelectExistingApp = (appId: string) => {
    setSelectedAppId(appId)
    setVisibilityChanged(false)
    refreshIdempotencyKey()
    const matched = availableApps.find((item) => item.id === appId)
    if (!matched) return
    setName(matched.name)
    setDescription(matched.description ?? '')
    setSpaFallback(matched.spaFallback)
    setVisibility({
      mode: matched.visibility.mode,
      teamIds: matched.visibility.teamIds,
      userIds: matched.visibility.userIds,
    })
    setVisibilityReason('')
  }

  const audienceInvalid =
    (targetMode === 'create' || (isSelectedAppOwner && visibilityChanged)) &&
    visibility.mode === 'selected' &&
    (visibility.teamIds?.length ?? 0) === 0 &&
    (visibility.userIds?.length ?? 0) === 0

  const canSubmit =
    !deployMutation.blocked &&
    !audienceInvalid &&
    (targetMode === 'create' ? Boolean(name.trim()) : Boolean(selectedAppId))

  const handleSubmit = () => {
    if (!canSubmit) return
    const normalizedSource: WorkspaceHostedAppSource =
      source.kind === 'personal_environment'
        ? {
            kind: 'personal_environment',
            sourcePath: source.sourcePath?.trim() || '.',
            ...(source.assistantId ? { assistantId: source.assistantId } : {}),
            ...(typeof source.environmentVersion === 'number' && source.environmentVersion >= 1
              ? { environmentVersion: source.environmentVersion }
              : {}),
          }
        : {
            kind: 'conversation_artifact',
            ...(source.sessionId ? { sessionId: source.sessionId } : {}),
            ...(source.artifactId ? { artifactId: source.artifactId } : {}),
            ...(source.entryFileName ? { entryFileName: source.entryFileName } : {}),
          }

    const trimmedName = name.trim()
    const trimmedDescription = description.trim()
    const trimmedReason = visibilityReason.trim()

    const payload: WorkspaceHostedAppDeploymentRequest =
      targetMode === 'create'
        ? {
            name: trimmedName,
            ...(trimmedDescription ? { description: trimmedDescription } : {}),
            spaFallback,
            visibility,
            ...(visibility.mode === 'organization' && trimmedReason
              ? { visibilityReason: trimmedReason }
              : {}),
            source: normalizedSource,
            idempotencyKey,
          }
        : {
            appId: selectedAppId,
            ...(isSelectedAppOwner && trimmedName ? { name: trimmedName } : {}),
            ...(isSelectedAppOwner ? { description: trimmedDescription } : {}),
            spaFallback,
            ...(isSelectedAppOwner && visibilityChanged ? { visibility } : {}),
            ...(isSelectedAppOwner &&
            visibilityChanged &&
            visibility.mode === 'organization' &&
            trimmedReason
              ? { visibilityReason: trimmedReason }
              : {}),
            source: normalizedSource,
            idempotencyKey,
          }

    deployMutation.mutate(payload, {
      onSuccess: (result) => {
        setPublishedResult(result)
        onPublished?.(result)
      },
    })
  }

  const safeResultUrl = resolveSafeHostedAppUrl(publishedResult?.app.url)

  return (
    <Modal
      styles={operationModalStyles}
      style={{ top: 24 }}
      width={620}
      destroyOnHidden
      mask={{ closable: false }}
      closable={!deployMutation.blocked}
      keyboard={!deployMutation.blocked}
      open={open}
      title={publishedResult ? '托管应用已发布就绪' : '发布为托管应用'}
      onCancel={() => {
        if (deployMutation.blocked) return
        onClose()
      }}
      footer={
        publishedResult ? (
          <Space wrap>
            <MyAppsEntryButton onNavigate={onClose} />
            <Button
              icon={<CopyOutlined />}
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(publishedResult.app.url)
                  void message.success('已复制固定分享链接')
                } catch {
                  void message.error('复制失败，请手动复制地址')
                }
              }}
            >
              复制分享链接
            </Button>
            {safeResultUrl ? (
              <Button
                type="primary"
                icon={<ExportOutlined />}
                href={safeResultUrl}
                target="_blank"
                rel="noopener noreferrer"
              >
                打开应用
              </Button>
            ) : null}
            <Button onClick={onClose}>完成</Button>
          </Space>
        ) : (
          <Space>
            <Button disabled={deployMutation.blocked} onClick={onClose}>
              取消
            </Button>
            <Button
              type="primary"
              disabled={!canSubmit}
              loading={deployMutation.isPending}
              onClick={handleSubmit}
            >
              确认发布
            </Button>
          </Space>
        )
      }
    >
      <Space orientation="vertical" size={14} style={{ width: '100%' }}>
        {deployMutation.feedback}

        {publishedResult ? (
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <ContextFeedback
              showIcon
              type="success"
              title={
                publishedResult.created
                  ? `已创建「${publishedResult.app.name}」并发布版本 v${publishedResult.revision.revisionNumber}`
                  : `「${publishedResult.app.name}」已更新至版本 v${publishedResult.revision.revisionNumber}`
              }
              description="固定子域地址保持稳定，后续更新版本或回滚不会改变分享链接。"
            />
            {publishedResult.app.pendingVisibilityApproval?.status === 'pending' ? (
              <ContextFeedback
                showIcon
                type="warning"
                title="全公司公开待审批，当前范围继续生效"
                description="管理员审批通过后将自动对全公司开放；等待期间沿用当前已生效的可见范围。"
              />
            ) : null}
            <div>
              <Text type="secondary">固定 HTTPS 访问链接</Text>
              <Paragraph copyable style={{ marginTop: 4, marginBottom: 0 }}>
                {publishedResult.app.url}
              </Paragraph>
            </div>
            <Space wrap size={8}>
              <StatusTag value={publishedResult.app.status} label="已上线" />
              <MetadataTag tone="blue" label={`版本 v${publishedResult.revision.revisionNumber}`} />
              <MetadataTag label={`${publishedResult.revision.fileCount} 个文件`} />
            </Space>
          </Space>
        ) : (
          <>
            <ReadFeedback query={myAppsQuery} label="可维护应用列表" canRead={open} />

            {deployMutation.isPending ? (
              <ContextFeedback
                showIcon
                type="info"
                title="正在打包并发布托管应用…"
                description="服务端正在按当前登录身份读取工作区产物、执行敏感信息扫描（DLP）并上传切换版本，请稍候。"
              />
            ) : null}

            <div>
              <Text type="secondary">发布来源</Text>
              <div style={{ marginTop: 4 }}>
                <MetadataTag
                  tone="cyan"
                  label={
                    source.kind === 'personal_environment' ? '个人工作空间目录' : '对话静态成果'
                  }
                />
                <Text code style={{ marginLeft: 8 }}>
                  {sourceSummary}
                </Text>
              </div>
              <Text type="secondary" style={{ display: 'block', marginTop: 4, fontSize: 12 }}>
                由服务端直接受控打包，不在请求中搬运对话正文内容。
              </Text>
            </div>

            <div>
              <Segmented
                aria-label="发布目标方式"
                disabled={deployMutation.blocked}
                options={[
                  { label: '新建托管应用', value: 'create' },
                  { label: '更新已有应用', value: 'update' },
                ]}
                value={targetMode}
                onChange={(next) => {
                  const mode = next as PublishTargetMode
                  setTargetMode(mode)
                  refreshIdempotencyKey()
                  if (mode === 'create') {
                    setSelectedAppId('')
                    setName(defaultName)
                    setDescription(defaultDescription)
                    setSpaFallback(defaultSpaFallback)
                    setVisibility({ mode: 'private' })
                    setVisibilityChanged(false)
                  } else if (availableApps.length > 0 && !selectedAppId) {
                    handleSelectExistingApp(availableApps[0].id)
                  }
                }}
              />
            </div>

            {targetMode === 'update' ? (
              <div>
                <Text>选择要更新的托管应用</Text>
                <Select
                  aria-label="选择已有托管应用"
                  disabled={deployMutation.blocked}
                  loading={myAppsQuery.isLoading}
                  options={availableApps.map((app) => ({
                    value: app.id,
                    label: `${app.name} (${app.host})${
                      currentUserId && app.ownerUserId !== currentUserId ? ' · 协作者' : ' · 作者'
                    }`,
                  }))}
                  placeholder={
                    availableApps.length === 0
                      ? '暂无可更新的托管应用，请切换为新建托管应用'
                      : '搜索并选择本人拥有或协作维护的应用'
                  }
                  showSearch={{ optionFilterProp: 'label' }}
                  style={{ width: '100%', marginTop: 4 }}
                  value={selectedAppId || undefined}
                  onChange={handleSelectExistingApp}
                />
                {selectedApp && !isSelectedAppOwner ? (
                  <ContextFeedback
                    showIcon
                    type="info"
                    title="协作者发版模式"
                    description="您是该应用的协作维护人，本次发布仅上传并激活新版本，应用名称与可见范围保持作者设置不变。"
                    style={{ marginTop: 8 }}
                  />
                ) : null}
              </div>
            ) : null}

            {targetMode === 'create' || isSelectedAppOwner ? (
              <>
                <div>
                  <Text>应用名称</Text>
                  <Input
                    aria-label="托管应用名称"
                    disabled={deployMutation.blocked}
                    maxLength={128}
                    placeholder="例如：季度经营看板、部门排班表"
                    style={{ marginTop: 4 }}
                    value={name}
                    onChange={(event) => {
                      setName(event.target.value)
                      refreshIdempotencyKey()
                    }}
                  />
                </div>

                <div>
                  <Text>一句话简介（可选）</Text>
                  <Input
                    aria-label="托管应用简介"
                    disabled={deployMutation.blocked}
                    maxLength={1024}
                    placeholder="简要说明用途，方便同事在门户「同事作品」中识别"
                    style={{ marginTop: 4 }}
                    value={description}
                    onChange={(event) => {
                      setDescription(event.target.value)
                      refreshIdempotencyKey()
                    }}
                  />
                </div>
              </>
            ) : null}

            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
              }}
            >
              <div>
                <Text>单页应用（SPA）前端路由回退</Text>
                <div>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    开启后，无扩展名的前端深链接路径将回退至 index.html（缺失静态资源仍严格返回 404）。
                  </Text>
                </div>
              </div>
              <Switch
                aria-label="SPA 前端路由回退"
                checked={spaFallback}
                disabled={deployMutation.blocked}
                onChange={(checked) => {
                  setSpaFallback(checked)
                  refreshIdempotencyKey()
                }}
              />
            </div>

            {targetMode === 'create' || isSelectedAppOwner ? (
              <div>
                <Text strong style={{ display: 'block', marginBottom: 6 }}>
                  可见范围
                </Text>
                <AudienceSelector
                  disabled={deployMutation.blocked}
                  previousVisibility={targetMode === 'update' ? selectedApp?.visibility : undefined}
                  reason={visibilityReason}
                  value={visibility}
                  onChange={(next) => {
                    setVisibility(next)
                    setVisibilityChanged(true)
                    refreshIdempotencyKey()
                  }}
                  onReasonChange={(nextReason) => {
                    setVisibilityReason(nextReason)
                    setVisibilityChanged(true)
                    refreshIdempotencyKey()
                  }}
                />
              </div>
            ) : null}
          </>
        )}
      </Space>
    </Modal>
  )
}
