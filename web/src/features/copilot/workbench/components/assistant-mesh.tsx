import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { api, ApiError } from '@/services/api-client'
import { claimApiErrorPresentation } from '@/services/api-error'
import { useAuthStore } from '@/stores/auth-store'
import {
  BookOutlined,
  CloudUploadOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import type {
  AssistantMeshActionRequest,
  AssistantMeshActionResult,
  AssistantMeshAsset,
  AssistantMeshAssetEnvelope,
  AssistantMeshAssetInput,
  AssistantMeshAssetPage,
  AssistantMeshAudience,
  AssistantMeshExtractRequest,
  WorkbenchSession,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Button,
  Card,
  Collapse,
  Drawer,
  Empty,
  Flex,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { workbenchApi } from '../api'
import { workbenchKeys } from '../keys'

const { Paragraph, Text, Title } = Typography

type MeshKind = AssistantMeshAssetInput['kind']
type MeshView = 'mine' | 'shared' | 'review'
type MeshAction =
  | 'publish'
  | 'trial'
  | 'run'
  | 'approve'
  | 'reject'
  | 'withdraw'
  | 'revoke'
  | 'invoke'

const MESH_BASE = '/copilot/assistant-mesh/assets'
const VIEW_OPTIONS: { label: string; value: MeshView }[] = [
  { label: '我的草稿', value: 'mine' },
  { label: '共享可见', value: 'shared' },
  { label: '待审批', value: 'review' },
]
const KIND_OPTIONS: { label: string; value: MeshKind }[] = [
  { label: 'Skill', value: 'skill' },
  { label: '经验卡', value: 'experience_card' },
  { label: 'MCP', value: 'mcp' },
]
const KIND_LABEL: Record<MeshKind, string> = {
  skill: 'Skill',
  experience_card: '经验卡',
  work_pulse: '工作进度',
  mcp: 'MCP',
}

function meshListPath(view: MeshView, kind: MeshKind | undefined, offset: number) {
  const params = new URLSearchParams({ view, offset: String(offset) })
  if (kind) params.set('kind', kind)
  return `${MESH_BASE}?${params}`
}

function meshAssetPath(id: string, kind: MeshKind) {
  return `${MESH_BASE}/${encodeURIComponent(id)}${kind === 'mcp' ? '?kind=mcp' : ''}`
}

function meshActionPath(id: string, action: MeshAction, kind: MeshKind) {
  return `${MESH_BASE}/${encodeURIComponent(id)}/actions/${action}${
    kind === 'mcp' ? '?kind=mcp' : ''
  }`
}

function meshInput(asset: AssistantMeshAsset) {
  return asset.draft ?? asset.published?.input ?? asset.review?.candidate.input
}

function shortDigest(digest?: string) {
  return digest ? `${digest.slice(0, 12)}…` : '无'
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请稍后重试'
}

function parseObject(text: string, label: string): Record<string, unknown> {
  if (!text.trim()) return {}
  const value: unknown = JSON.parse(text)
  if (!value || Array.isArray(value) || typeof value !== 'object')
    throw new Error(`${label}必须是 JSON 对象`)
  return value as Record<string, unknown>
}

function parseStringMap(text: string, label: string) {
  const value = parseObject(text, label)
  if (Object.values(value).some((item) => typeof item !== 'string'))
    throw new Error(`${label}的每个值都必须是字符串`)
  return value as Record<string, string>
}

function parseDependencies(text: string): AssistantMeshAssetInput['dependencies'] {
  if (!text.trim()) return undefined
  const value: unknown = JSON.parse(text)
  if (
    !Array.isArray(value) ||
    value.some(
      (item) =>
        !item ||
        typeof item !== 'object' ||
        typeof (item as { id?: unknown }).id !== 'string' ||
        typeof (item as { version?: unknown }).version !== 'string',
    )
  )
    throw new Error('依赖必须是 {id, version} 数组')
  return value as NonNullable<AssistantMeshAssetInput['dependencies']>
}

function parseParameters(text: string): AssistantMeshAssetInput['parameters'] {
  if (!text.trim()) return undefined
  const value = parseObject(text, '参数定义')
  for (const definition of Object.values(value)) {
    if (
      !definition ||
      typeof definition !== 'object' ||
      !['string', 'integer', 'number', 'boolean'].includes(
        (definition as { type?: string }).type ?? '',
      )
    )
      throw new Error('参数定义必须包含 string、integer、number 或 boolean 类型')
  }
  return value as NonNullable<AssistantMeshAssetInput['parameters']>
}

function parseSampleValues(text: string): NonNullable<AssistantMeshActionRequest['parameters']> {
  const values = parseObject(text, 'Skill 参数')
  for (const value of Object.values(values)) {
    if (typeof value !== 'string' && typeof value !== 'number' && typeof value !== 'boolean') {
      throw new Error('Skill 参数须为字符串、数字或布尔值')
    }
  }
  return values as NonNullable<AssistantMeshActionRequest['parameters']>
}

function browserRequestId() {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export function AssistantMesh({
  owner,
  sessions,
  sessionId,
}: {
  owner?: string
  sessions: Pick<WorkbenchSession, 'id' | 'title'>[]
  sessionId?: string
}) {
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.user)
  const [open, setOpen] = useState(false)
  const [view, setView] = useState<MeshView>('mine')
  const [kindFilter, setKindFilter] = useState<MeshKind>()
  const [selected, setSelected] = useState<{ id: string; kind: MeshKind }>()
  const [draftInput, setDraftInput] = useState<AssistantMeshAssetInput>()
  const [expectedDigest, setExpectedDigest] = useState('')
  const [trialSessionId, setTrialSessionId] = useState('')
  const [trialPrompt, setTrialPrompt] = useState('')
  const [trialParameters, setTrialParameters] = useState('{}')
  const [formError, setFormError] = useState('')
  const [visibility, setVisibility] = useState<AssistantMeshAudience['visibility']>('private')
  const [audienceTeams, setAudienceTeams] = useState<string[]>([])
  const [audienceProjects, setAudienceProjects] = useState<string[]>([])
  const [lifetimeMode, setLifetimeMode] = useState<
    'default' | '30' | '90' | 'custom' | 'permanent'
  >('default')
  const [customLifetime, setCustomLifetime] = useState<number>(90)
  const [reviewReason, setReviewReason] = useState('')
  const [mcpResourceId, setMcpResourceId] = useState('')
  const [lastAction, setLastAction] = useState<AssistantMeshActionResult>()
  const [createOpen, setCreateOpen] = useState(false)
  const [createKind, setCreateKind] = useState<MeshKind>('skill')
  const [createTitle, setCreateTitle] = useState('')
  const [createBody, setCreateBody] = useState('')
  const [createApplicability, setCreateApplicability] = useState('')
  const [createFiles, setCreateFiles] = useState('')
  const [createDependencies, setCreateDependencies] = useState('')
  const [createParameters, setCreateParameters] = useState('')
  const [extractOpen, setExtractOpen] = useState(false)
  const [extractKind, setExtractKind] = useState<'skill' | 'experience_card'>('skill')
  const [extractTitle, setExtractTitle] = useState('')
  const [extractSessionId, setExtractSessionId] = useState('')
  const [extractSource, setExtractSource] = useState<'messages' | 'run'>('messages')
  const [extractMessageIds, setExtractMessageIds] = useState<string[]>([])
  const [extractRunId, setExtractRunId] = useState('')

  const availabilityQuery = useQuery({
    queryKey: workbenchKeys.assistantMesh.availability(owner),
    queryFn: () => api.getEnvelope<AssistantMeshAssetPage>(meshListPath('mine', undefined, 0)),
    enabled: Boolean(owner),
    retry: false,
    staleTime: 60_000,
  })
  const meshUnavailable =
    availabilityQuery.error instanceof ApiError && availabilityQuery.error.status === 403
  useEffect(() => {
    if (!meshUnavailable) return
    return claimApiErrorPresentation(availabilityQuery.error)
  }, [availabilityQuery.error, meshUnavailable])
  const publishingDefaults = availabilityQuery.data?.publishingDefaults
  const hasTeams = Boolean(user?.teams?.length)
  const hasProjects = Boolean(user?.projects?.length)
  useEffect(() => {
    if (!open) return
    const scope = publishingDefaults?.visibility
    setVisibility(
      (scope === 'teams' && hasTeams) || (scope === 'projects' && hasProjects) ? scope : 'private',
    )
    setAudienceTeams([])
    setAudienceProjects([])
    setLifetimeMode('default')
  }, [open, selected?.id, publishingDefaults?.visibility, hasTeams, hasProjects])

  const listQuery = useInfiniteQuery({
    queryKey: workbenchKeys.assistantMesh.list(owner, view, kindFilter),
    queryFn: ({ pageParam }) =>
      api.getEnvelope<AssistantMeshAssetPage>(meshListPath(view, kindFilter, pageParam)),
    initialPageParam: 0,
    getNextPageParam: (lastPage) => lastPage.nextOffset,
    enabled: Boolean(owner) && open && !meshUnavailable,
    retry: false,
  })
  const assets = useMemo(
    () => listQuery.data?.pages.flatMap((page) => page.items) ?? [],
    [listQuery.data?.pages],
  )
  const listDenied = listQuery.error instanceof ApiError && listQuery.error.status === 403

  const detailQuery = useQuery({
    queryKey: workbenchKeys.assistantMesh.asset(owner, selected?.id, selected?.kind),
    queryFn: async () => {
      if (!selected) throw new Error('未选择共享能力')
      const response = await api.getEnvelope<AssistantMeshAssetEnvelope>(
        meshAssetPath(selected.id, selected.kind),
      )
      return response.data
    },
    enabled: Boolean(owner) && open && Boolean(selected),
    refetchInterval: ({ state }) =>
      state.data?.trial && ['queued', 'running'].includes(state.data.trial.status) ? 2_000 : false,
    retry: false,
  })
  const asset = detailQuery.data
  const sourceInput = asset ? meshInput(asset) : undefined

  const extractMessagesQuery = useQuery({
    queryKey: workbenchKeys.assistantMesh.extractMessages(owner, extractSessionId),
    queryFn: async () => (await workbenchApi.sessions.messages(extractSessionId)).data ?? [],
    enabled: open && extractOpen && Boolean(extractSessionId),
    retry: false,
  })
  const extractRunsQuery = useQuery({
    queryKey: workbenchKeys.assistantMesh.extractRuns(owner, extractSessionId),
    queryFn: async () => (await workbenchApi.agentRuns.session(extractSessionId)).data ?? [],
    enabled: open && extractOpen && Boolean(extractSessionId),
    retry: false,
  })

  useEffect(() => {
    setSelected(undefined)
  }, [view, kindFilter])

  useEffect(() => {
    if (!open) return
    setTrialSessionId((current) => current || sessionId || '')
    setExtractSessionId((current) => current || sessionId || '')
  }, [open, sessionId])

  useEffect(() => {
    if (!extractOpen) return
    setExtractSessionId(sessionId || '')
    setExtractMessageIds([])
    setExtractRunId('')
    setExtractTitle('')
  }, [extractOpen, sessionId])

  useEffect(() => {
    if (!asset) {
      setDraftInput(undefined)
      setExpectedDigest('')
      return
    }
    setDraftInput(meshInput(asset))
    setExpectedDigest(asset.draftDigest ?? '')
    setFormError('')
  }, [asset?.id, asset?.draftDigest, asset?.published?.digest, asset?.review?.candidate.digest])

  useEffect(() => {
    setLastAction(undefined)
    setMcpResourceId('')
    setReviewReason('')
  }, [selected?.id])

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: workbenchKeys.assistantMesh.all(owner) })

  const createMutation = useOperationMutation(
    {
      mutationFn: async (input: AssistantMeshAssetInput) => {
        const response = await api.post<AssistantMeshAssetEnvelope>(MESH_BASE, input)
        return response.data
      },
      onSuccess: async (created) => {
        setSelected({ id: created.id, kind: created.kind })
        setCreateOpen(false)
        setCreateTitle('')
        setCreateBody('')
        setCreateApplicability('')
        setCreateFiles('')
        setCreateDependencies('')
        setCreateParameters('')
        await refresh()
      },
    },
    {
      label: ['新建共享能力', 'Create shared capability'],
      target: (input) => input.title,
    },
  )

  const extractMutation = useOperationMutation(
    {
      mutationFn: async (input: AssistantMeshExtractRequest) => {
        const response = await api.post<AssistantMeshAssetEnvelope>(`${MESH_BASE}/extract`, input)
        return response.data
      },
      onSuccess: async (created) => {
        setSelected({ id: created.id, kind: created.kind })
        setExtractOpen(false)
        await refresh()
      },
    },
    {
      label: ['从会话提炼', 'Extract from conversation'],
      target: (input) => input.title,
    },
  )

  const updateMutation = useOperationMutation(
    {
      mutationFn: async (input: {
        id: string
        kind: MeshKind
        value: AssistantMeshAssetInput
        digest: string
      }) => {
        const response = await api.put<AssistantMeshAssetEnvelope>(
          `${MESH_BASE}/${encodeURIComponent(input.id)}`,
          { input: input.value, expectedDigest: input.digest },
        )
        return response.data
      },
      onSuccess: async (updated) => {
        setDraftInput(meshInput(updated))
        setExpectedDigest(updated.draftDigest ?? '')
        await refresh()
      },
    },
    {
      label: ['保存共享草稿', 'Save shared draft'],
      target: (input) => input.value.title,
    },
  )

  const actionMutation = useOperationMutation(
    {
      mutationFn: async (input: {
        id: string
        kind: MeshKind
        action: MeshAction
        body: AssistantMeshActionRequest
      }) => {
        const response = await api.post<{ data: AssistantMeshActionResult }>(
          meshActionPath(input.id, input.action, input.kind),
          input.body,
        )
        return response.data
      },
      onSuccess: async (result, input) => {
        setLastAction(result)
        const session = input.body.sessionId
        await Promise.all([
          refresh(),
          ...(result.runId && session
            ? [
                queryClient.invalidateQueries({
                  queryKey: workbenchKeys.agentRuns.session(session),
                }),
                queryClient.invalidateQueries({
                  queryKey: workbenchKeys.sessions.messages(session, owner),
                }),
              ]
            : []),
        ])
      },
    },
    {
      label: ['共享能力操作', 'Shared capability action'],
      target: (input) => `${KIND_LABEL[input.kind]} ${input.action}`,
    },
  )

  if (!owner || availabilityQuery.isPending || meshUnavailable) return null

  const isOwner = asset?.ownerUserId === owner
  const canEdit = Boolean(asset?.kind !== 'mcp' && asset?.draft && isOwner && draftInput)
  const hasFindings = Boolean(asset?.findings?.length)
  const trialCurrent =
    asset?.trial?.digest &&
    (asset.trial.digest === asset.draftDigest || asset.trial.digest === asset.published?.digest)
  const experienceLifetimeDays =
    lifetimeMode === 'permanent' || lifetimeMode === 'default'
      ? undefined
      : lifetimeMode === 'custom'
        ? customLifetime
        : Number(lifetimeMode)
  const audience: AssistantMeshAudience = {
    visibility,
    teams: visibility === 'teams' ? audienceTeams : undefined,
    projects: visibility === 'projects' ? audienceProjects : undefined,
  }

  const saveDraft = () => {
    if (!asset || !selected || !draftInput || !expectedDigest) return
    if (!draftInput.title.trim() || !draftInput.body.trim()) {
      setFormError('标题和内容不能为空')
      return
    }
    setFormError('')
    updateMutation.mutate({
      id: selected.id,
      kind: selected.kind,
      value: draftInput,
      digest: expectedDigest,
    })
  }

  const publish = () => {
    if (!asset || !selected || !asset.draftDigest) return
    if (hasFindings) {
      setFormError('请先处理敏感内容发现，再发布')
      return
    }
    setFormError('')
    actionMutation.mutate({
      id: selected.id,
      kind: selected.kind,
      action: 'publish',
      body: {
        expectedDigest: asset.draftDigest,
        audience,
        ...(asset.kind === 'experience_card'
          ? lifetimeMode === 'permanent'
            ? { permanent: true }
            : { lifetimeDays: experienceLifetimeDays }
          : {}),
      },
    })
  }

  const submitTrial = (action: 'trial' | 'run') => {
    const digest = action === 'trial' ? asset?.draftDigest : asset?.published?.digest
    if (!selected || !digest) {
      setFormError('请选择草稿或已发布版本')
      return
    }
    if (!trialSessionId || !trialPrompt.trim()) {
      setFormError('请选择会话并填写运行提示词')
      return
    }
    try {
      const parameters = selected.kind === 'skill' ? parseSampleValues(trialParameters) : undefined
      setFormError('')
      actionMutation.mutate({
        id: selected.id,
        kind: selected.kind,
        action,
        body: {
          expectedDigest: digest,
          sessionId: trialSessionId,
          requestId: browserRequestId(),
          prompt: trialPrompt,
          parameters,
        },
      })
    } catch (error) {
      setFormError(errorText(error))
    }
  }

  const submitCreate = () => {
    if (!createTitle.trim() || !createBody.trim()) {
      setFormError('标题和内容不能为空')
      return
    }
    try {
      const files = createFiles.trim() ? parseStringMap(createFiles, '文件') : undefined
      const dependencies = parseDependencies(createDependencies)
      const parameters = parseParameters(createParameters)
      setFormError('')
      createMutation.mutate({
        kind: createKind,
        title: createTitle.trim(),
        body: createBody,
        applicability: createApplicability.trim() || undefined,
        files,
        dependencies,
        parameters,
      })
    } catch (error) {
      setFormError(errorText(error))
    }
  }

  const submitExtract = () => {
    if (!extractTitle.trim() || !extractSessionId) {
      setFormError('请填写标题并选择会话')
      return
    }
    if (extractSource === 'messages' && !extractMessageIds.length) {
      setFormError('请选择至少一条消息')
      return
    }
    if (extractSource === 'run' && !extractRunId) {
      setFormError('请选择任务结果')
      return
    }
    setFormError('')
    extractMutation.mutate({
      kind: extractKind,
      title: extractTitle.trim(),
      sessionId: extractSessionId,
      ...(extractSource === 'messages'
        ? { messageIds: extractMessageIds }
        : { runId: extractRunId }),
    })
  }

  const messageOptions = (extractMessagesQuery.data ?? []).map((item) => ({
    value: item.id,
    label: `${item.role === 'assistant' ? '助手' : '我'} · ${item.content.replace(/\s+/g, ' ').slice(0, 72)}`,
  }))
  const runOptions = (extractRunsQuery.data ?? [])
    .filter((run) => run.status === 'completed')
    .map((run) => ({
      value: run.id,
      label: `${run.capabilityId} · ${run.status} · ${run.id.slice(0, 8)}`,
    }))

  return (
    <>
      <Tooltip title="能力共享" placement="right">
        <Button
          type="text"
          aria-label="能力共享"
          className="soha-ai-rail-action soha-icon-trigger"
          icon={<BookOutlined />}
          onClick={() => setOpen(true)}
        />
      </Tooltip>
      <Drawer
        destroyOnHidden
        size={960}
        open={open}
        onClose={() => setOpen(false)}
        title={
          <Flex align="center" gap={8}>
            <BookOutlined />
            <span>能力共享</span>
            <Text type="secondary" style={{ fontSize: 12, fontWeight: 400 }}>
              草稿 · 可见共享 · 发布审批
            </Text>
          </Flex>
        }
        extra={
          <Button
            icon={<ReloadOutlined />}
            loading={listQuery.isFetching || detailQuery.isFetching}
            onClick={() => void refresh()}
          >
            刷新
          </Button>
        }
      >
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          {availabilityQuery.isError && !meshUnavailable ? (
            <ContextFeedback
              type="error"
              title="能力共享暂不可用"
              description={errorText(availabilityQuery.error)}
            />
          ) : null}
          <Flex gap={12} justify="space-between" wrap="wrap">
            <Segmented
              value={view}
              options={VIEW_OPTIONS}
              onChange={(value) => setView(value as MeshView)}
            />
            <Space wrap>
              <Select
                allowClear
                placeholder="全部类型"
                style={{ width: 128 }}
                value={kindFilter}
                options={KIND_OPTIONS}
                onChange={(value) => setKindFilter(value as MeshKind | undefined)}
              />
              <Button icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
                新建
              </Button>
              <Button icon={<CloudUploadOutlined />} onClick={() => setExtractOpen(true)}>
                从会话提炼
              </Button>
            </Space>
          </Flex>

          <ReadFeedback query={listQuery} label="共享能力列表" canRead={!listDenied} />
          {listDenied ? (
            <ContextFeedback
              type="warning"
              title={view === 'review' ? '当前账号无权审批共享能力' : '无权读取此范围的共享能力'}
            />
          ) : null}

          <Flex gap={16} align="flex-start" wrap="wrap">
            <Space orientation="vertical" size={10} style={{ width: 310, flex: '0 0 310px' }}>
              {listQuery.isPending && !listDenied ? <Spin /> : null}
              {!listQuery.isPending && !listDenied && assets.length === 0 ? (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={view === 'mine' ? '还没有自己的共享草稿' : '没有可见记录'}
                />
              ) : null}
              {assets.map((item) => {
                const input = meshInput(item)
                const active = selected?.id === item.id
                return (
                  <Card
                    key={item.id}
                    size="small"
                    hoverable
                    onClick={() => setSelected({ id: item.id, kind: item.kind })}
                    title={
                      <Flex gap={6} align="center" justify="space-between">
                        <Text ellipsis>{input?.title || '未命名能力'}</Text>
                        <Tag>{KIND_LABEL[item.kind]}</Tag>
                      </Flex>
                    }
                    style={active ? { borderColor: 'var(--ant-color-primary)' } : undefined}
                  >
                    <Paragraph type="secondary" ellipsis={{ rows: 2 }} style={{ marginBottom: 8 }}>
                      {input?.body || '无内容'}
                    </Paragraph>
                    <Space size={4} wrap>
                      {item.review?.status === 'pending' ? <Tag color="gold">审批中</Tag> : null}
                      {item.review?.status === 'rejected' ? <Tag color="red">已驳回</Tag> : null}
                      {item.published ? <Tag color="green">v{item.published.version}</Tag> : null}
                      {item.trial ? <Tag>试跑 {item.trial.status}</Tag> : null}
                      {item.findings?.length ? <Tag color="orange">敏感内容</Tag> : null}
                    </Space>
                  </Card>
                )
              })}
              {listQuery.hasNextPage ? (
                <Button
                  block
                  loading={listQuery.isFetchingNextPage}
                  onClick={() => void listQuery.fetchNextPage()}
                >
                  加载更多
                </Button>
              ) : null}
            </Space>

            <div style={{ flex: '1 1 420px', minWidth: 360 }}>
              {!selected ? (
                <Empty description="选择一项能力查看草稿、共享状态和可执行操作" />
              ) : detailQuery.isPending ? (
                <Spin />
              ) : detailQuery.isError ? (
                <ContextFeedback
                  type="error"
                  title="能力详情读取失败"
                  description={errorText(detailQuery.error)}
                />
              ) : asset ? (
                <Space orientation="vertical" size={14} style={{ width: '100%' }}>
                  <ReadFeedback query={detailQuery} label="共享能力详情" />
                  {createMutation.feedback}
                  {extractMutation.feedback}
                  {updateMutation.feedback}
                  {actionMutation.feedback}
                  {formError ? <ContextFeedback type="error" title={formError} /> : null}

                  <Card
                    size="small"
                    title={
                      <Flex gap={8} wrap="wrap" align="center">
                        <Title level={5} style={{ margin: 0 }}>
                          {sourceInput?.title || '未命名能力'}
                        </Title>
                        <Tag>{KIND_LABEL[asset.kind]}</Tag>
                        {asset.published ? (
                          <Tag color="green">v{asset.published.version}</Tag>
                        ) : null}
                        {asset.review ? (
                          <Tag
                            color={
                              asset.review.status === 'pending'
                                ? 'gold'
                                : asset.review.status === 'rejected'
                                  ? 'red'
                                  : 'blue'
                            }
                          >
                            {asset.review.status === 'pending'
                              ? '审批中'
                              : asset.review.status === 'rejected'
                                ? '已驳回'
                                : asset.review.status === 'withdrawn'
                                  ? '已撤回'
                                  : '已批准'}
                          </Tag>
                        ) : null}
                      </Flex>
                    }
                    extra={
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        草稿 {shortDigest(asset.draftDigest)}
                      </Text>
                    }
                  >
                    {asset.originalAuthorUserId ? (
                      <Text type="secondary" copyable={{ text: asset.originalAuthorUserId }}>
                        原作者 ID {asset.originalAuthorUserId}
                      </Text>
                    ) : null}
                    {hasFindings ? (
                      <ContextFeedback
                        type="warning"
                        style={{ marginBottom: 12 }}
                        title="发现疑似敏感字面量"
                        description={
                          <Space orientation="vertical" size={2}>
                            {asset.findings?.map((finding, index) => (
                              <Text key={`${finding}-${index}`}>• {finding}</Text>
                            ))}
                            <Text type="secondary">处理前不可发布；试跑也仅用于本人核对。</Text>
                          </Space>
                        }
                      />
                    ) : null}
                    {canEdit && draftInput ? (
                      <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                        <Input
                          value={draftInput.title}
                          placeholder="能力名称"
                          onChange={(event) =>
                            setDraftInput((current) =>
                              current ? { ...current, title: event.target.value } : current,
                            )
                          }
                        />
                        <Input.TextArea
                          autoSize={{ minRows: 5, maxRows: 14 }}
                          value={draftInput.body}
                          placeholder="Skill 内容或经验卡的「问题 / 处理结论 / 适用条件」"
                          onChange={(event) =>
                            setDraftInput((current) =>
                              current ? { ...current, body: event.target.value } : current,
                            )
                          }
                        />
                        <Input
                          value={draftInput.applicability ?? ''}
                          placeholder="适用条件（可选）"
                          onChange={(event) =>
                            setDraftInput((current) =>
                              current ? { ...current, applicability: event.target.value } : current,
                            )
                          }
                        />
                        <Flex justify="space-between" align="center" gap={8} wrap="wrap">
                          <Text type="secondary" style={{ fontSize: 12 }}>
                            保存会校验 digest；内容被他人更新时需刷新后重试。
                          </Text>
                          <Button
                            type="primary"
                            loading={updateMutation.isPending}
                            disabled={updateMutation.blocked || !expectedDigest}
                            onClick={saveDraft}
                          >
                            保存草稿
                          </Button>
                        </Flex>
                      </Space>
                    ) : (
                      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                        <Paragraph style={{ whiteSpace: 'pre-wrap', marginBottom: 0 }}>
                          {sourceInput?.body || '无内容'}
                        </Paragraph>
                        {sourceInput?.applicability ? (
                          <Text type="secondary">适用条件：{sourceInput.applicability}</Text>
                        ) : null}
                      </Space>
                    )}
                  </Card>

                  {asset.kind === 'experience_card' && sourceInput?.parameters ? (
                    <Collapse
                      size="small"
                      items={[
                        {
                          key: 'parameters',
                          label: '参数定义',
                          children: (
                            <Paragraph code style={{ whiteSpace: 'pre-wrap', marginBottom: 0 }}>
                              {JSON.stringify(sourceInput.parameters, null, 2)}
                            </Paragraph>
                          ),
                        },
                      ]}
                    />
                  ) : null}

                  {asset.kind === 'skill' ? (
                    <Card size="small" title="Skill 试跑与运行">
                      {asset.trial ? (
                        <ContextFeedback
                          type={trialCurrent ? 'info' : 'warning'}
                          style={{ marginBottom: 12 }}
                          title={
                            trialCurrent
                              ? `试跑 ${asset.trial.status}`
                              : '试跑已失效，内容或依赖已变化，请重跑'
                          }
                          description={
                            trialCurrent && asset.trial.status === 'completed'
                              ? '请核对本次运行结果，再发布此草稿。'
                              : '未完成或已失效的试跑不能作为发布依据。'
                          }
                        />
                      ) : null}
                      <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                        <Select
                          showSearch={{ optionFilterProp: 'label' }}
                          placeholder="选择试跑会话"
                          value={trialSessionId || undefined}
                          options={sessions.map((session) => ({
                            value: session.id,
                            label: session.title,
                          }))}
                          onChange={setTrialSessionId}
                          style={{ width: '100%' }}
                        />
                        <Input.TextArea
                          value={trialPrompt}
                          placeholder="试跑提示词"
                          onChange={(event) => setTrialPrompt(event.target.value)}
                        />
                        <Input.TextArea
                          value={trialParameters}
                          placeholder='试跑参数 JSON，例如 {"namespace":"default"}'
                          onChange={(event) => setTrialParameters(event.target.value)}
                        />
                        <Space wrap>
                          {isOwner && asset.draftDigest ? (
                            <Button
                              loading={actionMutation.isPending}
                              disabled={actionMutation.blocked || hasFindings}
                              onClick={() => submitTrial('trial')}
                            >
                              试跑草稿
                            </Button>
                          ) : null}
                          {asset.published ? (
                            <Button
                              icon={<ThunderboltOutlined />}
                              loading={actionMutation.isPending}
                              disabled={actionMutation.blocked}
                              onClick={() => submitTrial('run')}
                            >
                              运行已发布版本
                            </Button>
                          ) : null}
                        </Space>
                      </Space>
                    </Card>
                  ) : null}

                  {isOwner && asset.draftDigest ? (
                    <Card size="small" title={asset.kind === 'mcp' ? 'MCP 发布提案' : '发布'}>
                      <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                        {visibility === 'organization' ? (
                          <ContextFeedback type="info" title="全组织发布需审批，通过前仅你可见" />
                        ) : null}
                        <Select
                          value={visibility}
                          onChange={setVisibility}
                          options={[
                            { value: 'private', label: '仅自己' },
                            { value: 'teams', label: '指定团队' },
                            { value: 'projects', label: '指定项目' },
                            { value: 'organization', label: '全组织' },
                          ]}
                          style={{ width: '100%' }}
                        />
                        {visibility === 'teams' ? (
                          <Select
                            mode="multiple"
                            options={(user?.teams ?? []).map((id) => ({ value: id, label: id }))}
                            value={audienceTeams}
                            onChange={setAudienceTeams}
                            placeholder="选择你所属的团队"
                            style={{ width: '100%' }}
                          />
                        ) : null}
                        {visibility === 'projects' ? (
                          <Select
                            mode="multiple"
                            options={(user?.projects ?? []).map((id) => ({ value: id, label: id }))}
                            value={audienceProjects}
                            onChange={setAudienceProjects}
                            placeholder="选择你所属的项目"
                            style={{ width: '100%' }}
                          />
                        ) : null}
                        {asset.kind === 'experience_card' ? (
                          <Flex gap={8} align="center" wrap="wrap">
                            <Text>有效期</Text>
                            <Select
                              value={lifetimeMode}
                              onChange={setLifetimeMode}
                              options={[
                                {
                                  value: 'default',
                                  label: `组织默认 · ${publishingDefaults?.lifetimeDays ?? 90} 天`,
                                },
                                { value: '30', label: '30 天' },
                                { value: '90', label: '90 天' },
                                { value: 'custom', label: '自定义' },
                                ...(publishingDefaults?.allowPermanentExperienceCards
                                  ? [{ value: 'permanent' as const, label: '长期' }]
                                  : []),
                              ]}
                              style={{ width: 120 }}
                            />
                            {lifetimeMode === 'custom' ? (
                              <InputNumber
                                min={7}
                                max={365}
                                value={customLifetime}
                                onChange={(value) => setCustomLifetime(value ?? 90)}
                                suffix="天"
                              />
                            ) : null}
                            {lifetimeMode === 'permanent' ? (
                              <Text type="secondary">可随时撤回或更新</Text>
                            ) : null}
                          </Flex>
                        ) : null}
                        <Button
                          type="primary"
                          loading={actionMutation.isPending}
                          disabled={
                            actionMutation.blocked ||
                            hasFindings ||
                            asset.review?.status === 'pending' ||
                            (asset.kind === 'skill' &&
                              (!trialCurrent || asset.trial?.status !== 'completed')) ||
                            (visibility === 'teams' && audienceTeams.length === 0) ||
                            (visibility === 'projects' && audienceProjects.length === 0)
                          }
                          onClick={publish}
                        >
                          {visibility === 'organization' || asset.kind === 'mcp'
                            ? '提交发布审批'
                            : '发布新版本'}
                        </Button>
                      </Space>
                    </Card>
                  ) : null}

                  {asset.kind === 'mcp' ? (
                    <Card
                      size="small"
                      title={
                        <Space>
                          <SafetyCertificateOutlined />
                          <span>调用态</span>
                        </Space>
                      }
                    >
                      <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                        <Flex gap={8} wrap="wrap" align="center">
                          <Text>固定工具</Text>
                          <Tag>native · docker.projects.get</Tag>
                          <Text type="secondary">按调用人 Soha 权限（RBAC）</Text>
                        </Flex>
                        <Text type="secondary">
                          查询前需要管理员授予该 Docker 项目的工具访问权限。
                        </Text>
                        {asset.published ? (
                          <Select
                            showSearch={{ optionFilterProp: 'label' }}
                            placeholder="选择运行会话"
                            value={trialSessionId || undefined}
                            options={sessions.map((session) => ({
                              value: session.id,
                              label: session.title,
                            }))}
                            onChange={setTrialSessionId}
                            style={{ width: '100%' }}
                          />
                        ) : null}
                        {asset.published ? (
                          <Input.TextArea
                            value={trialPrompt}
                            placeholder="输入任务及目标项目 UUID"
                            onChange={(event) => setTrialPrompt(event.target.value)}
                          />
                        ) : null}
                        {asset.published ? (
                          <Button
                            type="primary"
                            loading={actionMutation.isPending}
                            disabled={
                              actionMutation.blocked || !trialSessionId || !trialPrompt.trim()
                            }
                            onClick={() => submitTrial('run')}
                          >
                            在会话中运行
                          </Button>
                        ) : null}
                        <Input
                          value={mcpResourceId}
                          placeholder="直接查询的项目 UUID"
                          onChange={(event) => setMcpResourceId(event.target.value)}
                        />
                        <Button
                          loading={actionMutation.isPending}
                          disabled={
                            actionMutation.blocked || !asset.published || !mcpResourceId.trim()
                          }
                          onClick={() =>
                            asset.published &&
                            actionMutation.mutate({
                              id: asset.id,
                              kind: asset.kind,
                              action: 'invoke',
                              body: {
                                expectedDigest: asset.published.digest,
                                resourceId: mcpResourceId.trim(),
                              },
                            })
                          }
                        >
                          使用已发布摘要调用
                        </Button>
                      </Space>
                    </Card>
                  ) : null}

                  {asset.review?.status === 'pending' ? (
                    <Card size="small" title="审批">
                      <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                        <Text type="secondary">
                          候选 v{asset.review.candidate.version} ·{' '}
                          {shortDigest(asset.review.candidate.digest)}
                        </Text>
                        <Input.TextArea
                          value={reviewReason}
                          placeholder="驳回原因（驳回时必填）"
                          onChange={(event) => setReviewReason(event.target.value)}
                        />
                        <Space wrap>
                          <Button
                            type="primary"
                            loading={actionMutation.isPending}
                            disabled={actionMutation.blocked}
                            onClick={() =>
                              actionMutation.mutate({
                                id: asset.id,
                                kind: asset.kind,
                                action: 'approve',
                                body: { expectedDigest: asset.review?.candidate.digest },
                              })
                            }
                          >
                            批准
                          </Button>
                          <Button
                            danger
                            loading={actionMutation.isPending}
                            disabled={actionMutation.blocked || !reviewReason.trim()}
                            onClick={() =>
                              actionMutation.mutate({
                                id: asset.id,
                                kind: asset.kind,
                                action: 'reject',
                                body: {
                                  expectedDigest: asset.review?.candidate.digest,
                                  reason: reviewReason.trim(),
                                },
                              })
                            }
                          >
                            驳回
                          </Button>
                        </Space>
                      </Space>
                    </Card>
                  ) : asset.review?.status === 'rejected' && asset.review.reason ? (
                    <ContextFeedback
                      type="error"
                      title="审批驳回"
                      description={asset.review.reason}
                    />
                  ) : null}

                  {isOwner ? (
                    <Space wrap>
                      {asset.review?.status === 'pending' ? (
                        <Popconfirm
                          title="撤回待审批版本？"
                          onConfirm={() =>
                            actionMutation.mutate({
                              id: asset.id,
                              kind: asset.kind,
                              action: 'withdraw',
                              body: { expectedDigest: asset.review?.candidate.digest },
                            })
                          }
                        >
                          <Button disabled={actionMutation.blocked}>撤回审批</Button>
                        </Popconfirm>
                      ) : null}
                      {asset.published ? (
                        <Popconfirm
                          title={`撤回已发布 v${asset.published.version}？`}
                          onConfirm={() =>
                            actionMutation.mutate({
                              id: asset.id,
                              kind: asset.kind,
                              action: 'revoke',
                              body: { expectedDigest: asset.published?.digest },
                            })
                          }
                        >
                          <Button danger disabled={actionMutation.blocked}>
                            撤回已发布版本
                          </Button>
                        </Popconfirm>
                      ) : null}
                    </Space>
                  ) : null}

                  {lastAction ? (
                    <ContextFeedback
                      type="success"
                      title="请求已返回"
                      description={
                        <Space orientation="vertical" size={2}>
                          {lastAction.runId ? <Text>运行 ID：{lastAction.runId}</Text> : null}
                          {lastAction.project ? (
                            <Text>
                              项目：{lastAction.project.name}（{lastAction.project.status}）
                            </Text>
                          ) : null}
                          <Text type="secondary">请以实际运行记录和资源状态为准。</Text>
                        </Space>
                      }
                    />
                  ) : null}
                </Space>
              ) : null}
            </div>
          </Flex>
        </Space>
      </Drawer>

      <Modal
        title="新建共享能力"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={submitCreate}
        okText="创建草稿"
        confirmLoading={createMutation.isPending}
        okButtonProps={{ disabled: createMutation.blocked }}
        width={720}
      >
        <Space orientation="vertical" size={10} style={{ width: '100%' }}>
          {formError ? <ContextFeedback type="error" title={formError} /> : null}
          <Select
            value={createKind}
            onChange={setCreateKind}
            options={[
              { value: 'skill', label: 'Skill' },
              { value: 'experience_card', label: '经验卡' },
              { value: 'mcp', label: 'MCP 工具提案' },
            ]}
            style={{ width: '100%' }}
          />
          <Input
            value={createTitle}
            placeholder="名称"
            onChange={(event) => setCreateTitle(event.target.value)}
          />
          <Input.TextArea
            autoSize={{ minRows: 4, maxRows: 10 }}
            value={createBody}
            placeholder="内容、业务说明或 Skill 包正文"
            onChange={(event) => setCreateBody(event.target.value)}
          />
          <Input
            value={createApplicability}
            placeholder="适用条件（可选）"
            onChange={(event) => setCreateApplicability(event.target.value)}
          />
          <Collapse
            size="small"
            items={[
              {
                key: 'advanced',
                label: '可选结构化内容',
                children: (
                  <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                    <Input.TextArea
                      value={createFiles}
                      placeholder='文件 JSON 对象，例如 {"SKILL.md":"..."}'
                      onChange={(event) => setCreateFiles(event.target.value)}
                    />
                    <Input.TextArea
                      value={createDependencies}
                      placeholder='依赖 JSON 数组，例如 [{"id":"...","version":"1.0.0"}]'
                      onChange={(event) => setCreateDependencies(event.target.value)}
                    />
                    <Input.TextArea
                      value={createParameters}
                      placeholder='参数定义 JSON，例如 {"namespace":{"type":"string","required":true}}'
                      onChange={(event) => setCreateParameters(event.target.value)}
                    />
                  </Space>
                ),
              },
            ]}
          />
        </Space>
      </Modal>

      <Modal
        title="从会话提炼"
        open={extractOpen}
        onCancel={() => setExtractOpen(false)}
        onOk={submitExtract}
        okText="提炼为草稿"
        confirmLoading={extractMutation.isPending}
        okButtonProps={{ disabled: extractMutation.blocked }}
        width={720}
      >
        <Space orientation="vertical" size={10} style={{ width: '100%' }}>
          {formError ? <ContextFeedback type="error" title={formError} /> : null}
          <ContextFeedback
            type="info"
            title="提炼后可继续人工编辑；发布前请核对敏感内容与适用范围。"
          />
          <Flex gap={8}>
            <Select
              value={extractKind}
              onChange={setExtractKind}
              options={[
                { value: 'skill', label: 'Skill' },
                { value: 'experience_card', label: '经验卡' },
              ]}
              style={{ width: 160 }}
            />
            <Input
              value={extractTitle}
              placeholder="草稿标题"
              onChange={(event) => setExtractTitle(event.target.value)}
            />
          </Flex>
          <Select
            showSearch={{ optionFilterProp: 'label' }}
            value={extractSessionId || undefined}
            placeholder="选择可见会话"
            options={sessions.map((session) => ({ value: session.id, label: session.title }))}
            onChange={(value) => {
              setExtractSessionId(value)
              setExtractMessageIds([])
              setExtractRunId('')
            }}
            style={{ width: '100%' }}
          />
          <Segmented
            block
            value={extractSource}
            options={[
              { value: 'messages', label: '选取消息' },
              { value: 'run', label: '选取任务结果' },
            ]}
            onChange={(value) => setExtractSource(value as 'messages' | 'run')}
          />
          <ReadFeedback
            query={extractMessagesQuery}
            label="会话消息"
            canRead={extractSource === 'messages'}
          />
          <ReadFeedback
            query={extractRunsQuery}
            label="任务结果"
            canRead={extractSource === 'run'}
          />
          {extractSource === 'messages' ? (
            <Select
              mode="multiple"
              showSearch={{ optionFilterProp: 'label' }}
              value={extractMessageIds}
              placeholder="选择一条或多条消息"
              options={messageOptions}
              loading={extractMessagesQuery.isFetching}
              onChange={setExtractMessageIds}
              style={{ width: '100%' }}
            />
          ) : (
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              value={extractRunId || undefined}
              placeholder="选择任务运行"
              options={runOptions}
              loading={extractRunsQuery.isFetching}
              onChange={setExtractRunId}
              style={{ width: '100%' }}
            />
          )}
        </Space>
      </Modal>
    </>
  )
}
