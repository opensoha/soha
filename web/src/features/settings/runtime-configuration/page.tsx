import {
  ManagementIconButton,
  ManagementQueryField,
  ManagementQueryPanel,
} from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { isApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import {
  CheckCircleOutlined,
  EyeOutlined,
  ReloadOutlined,
  SaveOutlined,
  UndoOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  App as AntdApp,
  Button,
  Checkbox,
  Collapse,
  Empty,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Tabs,
  Typography,
} from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { SettingsAdminTable } from '../shared/components'
import {
  AI_WORKBENCH_CONFIG_KEY,
  aiWorkbenchDraft,
  aiWorkbenchEnabled,
  isAIWorkbenchChildConfig,
  isAIWorkbenchConfig,
} from './ai-module-model'
import { RuntimeConfigurationHistoryDrawer } from './components/history-detail-drawer'
import { RuntimeResourceOverview } from './components/resource-overview'
import {
  computeModuleDraft,
  computeModuleState,
  isComputeChildConfig,
} from './compute-module-model'
import { runtimeConfigurationMutations } from './mutations'
import { runtimeConfigurationQueries } from './queries'
import {
  buildRuntimeConfigChanges,
  summarizeRuntimeConfigChanges,
  visibleRuntimeConfigItems,
} from './runtime-config-model'
import './styles.css'
import type {
  RuntimeConfigApplyMode,
  RuntimeConfigApplyResult,
  RuntimeConfigChange,
  RuntimeConfigChangeRequest,
  RuntimeConfigItem,
  RuntimeConfigRevision,
  RuntimeConfigSnapshot,
  RuntimeConfigSource,
  RuntimeConfigValidationResult,
  RuntimeConfigValue,
} from './types'

const { Text } = Typography

type RuntimeConfigurationTab = 'configuration' | 'resources' | 'history'

const APPLY_MODE_LABELS: Record<RuntimeConfigApplyMode, string> = {
  hot: '即时生效',
  reconfigure: '重建连接',
  lifecycle: '启停模块',
  restart: '需要重启',
}

const SOURCE_LABELS: Record<RuntimeConfigSource, string> = {
  default: '默认值',
  config_file: '配置文件',
  runtime_override: '运行时覆盖',
  environment: '环境变量',
  secret: 'Secret',
}

const COMPUTE_STATE_LABELS = {
  disabled: '已关闭',
  partial: '部分启用',
  enabled: '全部启用',
} as const

function displayValue(item: RuntimeConfigItem) {
  if (item.sensitive) return '********'
  if (item.effectiveValue === undefined) return '-'
  if (item.valueType === 'boolean' && typeof item.effectiveValue === 'boolean') {
    return item.effectiveValue ? '已启用' : '已停用'
  }
  if (Array.isArray(item.effectiveValue)) return item.effectiveValue.join(', ')
  return String(item.effectiveValue)
}

function isLocked(item: RuntimeConfigItem) {
  return !item.editable || item.source === 'environment' || item.source === 'secret'
}

function ConfigValueEditor({
  disabled,
  item,
  onChange,
  value,
}: {
  disabled: boolean
  item: RuntimeConfigItem
  onChange: (value: RuntimeConfigValue) => void
  value: RuntimeConfigValue | undefined
}) {
  if (item.valueType === 'boolean') {
    return (
      <Switch
        aria-label={item.label || item.key}
        checked={Boolean(value)}
        disabled={disabled}
        onChange={onChange}
      />
    )
  }
  if (item.valueType === 'integer' || item.valueType === 'number') {
    return (
      <InputNumber
        aria-label={item.label || item.key}
        disabled={disabled}
        precision={item.valueType === 'integer' ? 0 : undefined}
        value={typeof value === 'number' ? value : undefined}
        onChange={(next) => next !== null && onChange(next)}
      />
    )
  }
  if (item.valueType === 'string_list') {
    return (
      <Select
        aria-label={item.label || item.key}
        disabled={disabled}
        mode="tags"
        value={Array.isArray(value) ? value : []}
        onChange={onChange}
      />
    )
  }
  return (
    <Input
      aria-label={item.label || item.key}
      disabled={disabled}
      placeholder={item.sensitive ? '输入新值' : undefined}
      type={item.sensitive ? 'password' : item.valueType === 'url' ? 'url' : 'text'}
      value={typeof value === 'string' ? value : ''}
      onChange={(event) => onChange(event.target.value)}
    />
  )
}

export function RuntimeConfigurationPage() {
  const { modal } = AntdApp.useApp()
  const queryClient = useQueryClient()
  const permissionQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionQuery)?.data
  const canView = hasPermission(snapshot, 'settings.runtime-config.view')
  const canApply = hasPermission(snapshot, 'settings.runtime-config.apply')
  const canValidate = hasPermission(snapshot, 'settings.runtime-config.validate')
  const canRollback = hasPermission(snapshot, 'settings.runtime-config.rollback')
  const canManage = canApply || canValidate
  const [activeTab, setActiveTab] = useState<RuntimeConfigurationTab>('configuration')
  const snapshotQuery = useQuery(runtimeConfigurationQueries.snapshot(canView))
  const resourceQuery = useQuery(
    runtimeConfigurationQueries.resources(canView && activeTab === 'resources'),
  )
  const historyQuery = useQuery(runtimeConfigurationQueries.history(canView))
  const reportConflict = (error: Error) => {
    if (isApiError(error) && error.status === 409) void snapshotQuery.refetch()
  }
  const resultError = (result: RuntimeConfigApplyResult) =>
    ['failed', 'partially_applied'].includes(result.application.status)
      ? new Error(`配置应用状态：${result.application.status}，请核对变更历史。`)
      : undefined
  const receipt = (result: RuntimeConfigApplyResult) => ({
    message: `已返回配置版本 v${result.revision.version}`,
    evidence: `应用记录 ${result.application.id} · ${result.application.status}；运行状态以变更历史和服务观察结果为准。`,
  })
  const validateMutation = useOperationMutation(
    { ...runtimeConfigurationMutations.validate(), onError: reportConflict },
    {
      label: ['校验运行时配置', 'Validate runtime configuration'],
      target: (v) => `版本 v${v.expectedVersion} · ${v.changes.length} 项`,
      readOnly: true,
      resultError: (r) => (r.valid ? undefined : new Error('配置校验未通过，请处理校验问题。')),
    },
  )
  const applyMutation = useOperationMutation(
    { ...runtimeConfigurationMutations.apply(queryClient), onError: reportConflict },
    {
      label: ['应用运行时配置', 'Apply runtime configuration'],
      target: (v) => `版本 v${v.expectedVersion} · ${v.changes.length} 项`,
      receipt,
      resultError,
    },
  )
  const rollbackMutation = useOperationMutation(
    { ...runtimeConfigurationMutations.rollback(queryClient), onError: reportConflict },
    {
      label: ['回滚运行时配置', 'Rollback runtime configuration'],
      target: (v) => `版本 v${v.targetVersion}`,
      receipt,
      resultError,
    },
  )
  const [applyReview, setApplyReview] = useState<RuntimeConfigChangeRequest | null>(null)
  const [rollbackReview, setRollbackReview] = useState<{
    revision: RuntimeConfigRevision
    expectedVersion: number
  } | null>(null)
  const [draft, setDraft] = useState<Record<string, RuntimeConfigValue>>({})
  const [resetKeys, setResetKeys] = useState<Set<string>>(() => new Set())
  const [validation, setValidation] = useState<RuntimeConfigValidationResult | null>(null)
  const [keyword, setKeyword] = useState('')
  const [applyMode, setApplyMode] = useState('')
  const [source, setSource] = useState('')
  const [changedOnly, setChangedOnly] = useState(false)
  const [reason, setReason] = useState('')
  const [activeRevision, setActiveRevision] = useState<RuntimeConfigRevision | null>(null)

  const observedSnapshot = canView ? readableQueryData(snapshotQuery) : undefined
  const [editingSnapshot, setEditingSnapshot] = useState<RuntimeConfigSnapshot>()
  const denied = !canView || queryAccessDenied(snapshotQuery)
  const runtimeSnapshot = denied ? undefined : (editingSnapshot ?? observedSnapshot)
  const blocked =
    denied || validateMutation.blocked || applyMutation.blocked || rollbackMutation.blocked
  const editingBlocked = blocked || Boolean(applyReview || rollbackReview)
  useEffect(() => {
    if (denied) {
      setDraft((current) => (Object.keys(current).length ? {} : current))
      setResetKeys((current) => (current.size ? new Set<string>() : current))
      setValidation(null)
      setReason('')
      setEditingSnapshot(undefined)
      setApplyReview(null)
      setRollbackReview(null)
      setActiveRevision(null)
    } else if (!Object.keys(draft).length && !resetKeys.size && !applyReview && !rollbackReview) {
      setEditingSnapshot(observedSnapshot)
    }
  }, [denied, observedSnapshot, draft, resetKeys, applyReview, rollbackReview])

  const changes = useMemo<RuntimeConfigChange[]>(() => {
    return buildRuntimeConfigChanges(draft, resetKeys, runtimeSnapshot?.items ?? [])
  }, [draft, resetKeys, runtimeSnapshot?.items])
  const changedKeys = useMemo(() => new Set(changes.map((change) => change.key)), [changes])
  const impactSummary = useMemo(
    () => summarizeRuntimeConfigChanges(changes, runtimeSnapshot?.items ?? []),
    [changes, runtimeSnapshot?.items],
  )

  useEffect(() => {
    if (changes.length > 0) return undefined
    setChangedOnly(false)
    return undefined
  }, [changes.length])

  useEffect(() => {
    if (changes.length === 0) return undefined
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [changes.length])

  const updateDraft = (key: string, value: RuntimeConfigValue) => {
    setDraft((current) => ({ ...current, [key]: value }))
    setResetKeys((current) => {
      if (!current.has(key)) return current
      const next = new Set(current)
      next.delete(key)
      return next
    })
    setValidation(null)
  }

  const updateDraftValues = (values: Record<string, RuntimeConfigValue>) => {
    const keys = Object.keys(values)
    setDraft((current) => ({ ...current, ...values }))
    setResetKeys((current) => {
      if (!keys.some((key) => current.has(key))) return current
      const next = new Set(current)
      keys.forEach((key) => next.delete(key))
      return next
    })
    setValidation(null)
  }

  const resetOverride = (key: string) => {
    setDraft((current) => {
      if (!(key in current)) return current
      const next = { ...current }
      delete next[key]
      return next
    })
    setResetKeys((current) => new Set(current).add(key))
    setValidation(null)
  }

  const filteredItems = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLowerCase()
    return visibleRuntimeConfigItems(runtimeSnapshot?.items ?? []).filter((item) => {
      const matchesKeyword =
        !normalizedKeyword ||
        [
          item.key,
          item.label,
          item.description,
          item.category,
          isComputeChildConfig(item.key) ? '计算资源工作台' : '',
          isAIWorkbenchConfig(item.key) ? 'AI 工作台' : '',
        ]
          .filter(Boolean)
          .some((value) => String(value).toLowerCase().includes(normalizedKeyword))
      return (
        matchesKeyword &&
        (!applyMode || item.applyMode === applyMode) &&
        (!source || item.source === source) &&
        (!changedOnly || changedKeys.has(item.key))
      )
    })
  }, [applyMode, changedKeys, changedOnly, keyword, runtimeSnapshot?.items, source])

  const groupedItems = useMemo(() => {
    const groups = new Map<string, RuntimeConfigItem[]>()
    filteredItems.forEach((item) => {
      const category =
        isComputeChildConfig(item.key) || isAIWorkbenchConfig(item.key)
          ? '模块'
          : item.category || '其他配置'
      const group = groups.get(category) ?? []
      group.push(item)
      groups.set(category, group)
    })
    return Array.from(groups, ([category, items]) => ({ category, items }))
  }, [filteredItems])

  const computeItems = useMemo(
    () => (runtimeSnapshot?.items ?? []).filter((item) => isComputeChildConfig(item.key)),
    [runtimeSnapshot?.items],
  )
  const computeCurrentState = computeModuleState(computeItems, {})
  const computeDraftState = computeModuleState(computeItems, draft)
  const computeParentLocked =
    computeItems.length !== 2 || computeItems.some((item) => isLocked(item))
  const aiItems = useMemo(
    () => (runtimeSnapshot?.items ?? []).filter((item) => isAIWorkbenchConfig(item.key)),
    [runtimeSnapshot?.items],
  )
  const aiWorkbenchItem = aiItems.find((item) => item.key === AI_WORKBENCH_CONFIG_KEY)
  const aiWorkbenchDraftEnabled = aiWorkbenchEnabled(aiItems, draft)
  const aiWorkbenchLocked = !aiWorkbenchItem || isLocked(aiWorkbenchItem)

  const request = () => ({
    expectedVersion: runtimeSnapshot?.version ?? 0,
    changes,
    reason: reason.trim() || undefined,
  })

  const clearDraft = () => {
    setDraft({})
    setResetKeys(new Set())
    setValidation(null)
    setEditingSnapshot(observedSnapshot)
  }
  const handleValidate = async () => {
    if (!canValidate || blocked || changes.length === 0) return
    try {
      setValidation(await validateMutation.mutateAsync(request()))
    } catch {
      /* Feedback owns request errors. */
    }
  }
  const handleApply = () => {
    if (!canApply || blocked || changes.length === 0) return
    applyMutation.reset()
    validateMutation.reset()
    setApplyReview(request())
  }
  const handleRollback = (revision: RuntimeConfigRevision) => {
    if (!canRollback || blocked) return
    rollbackMutation.reset()
    setRollbackReview({ revision, expectedVersion: observedSnapshot?.version ?? 0 })
  }
  const submitApply = async () => {
    if (!applyReview || blocked || !canApply) return
    try {
      const checked = await validateMutation.mutateAsync(applyReview)
      setValidation(checked)
      if (!checked.valid) return
      const result = await applyMutation.mutateAsync(applyReview)
      if (resultError(result)) return
      clearDraft()
      setReason('')
      setApplyReview(null)
      setActiveRevision(result.revision)
    } catch {
      /* Keep the frozen review and draft for reconciliation. */
    }
  }
  const submitRollback = async () => {
    if (!rollbackReview || blocked || !canRollback) return
    try {
      const result = await rollbackMutation.mutateAsync({
        expectedVersion: rollbackReview.expectedVersion,
        targetVersion: rollbackReview.revision.version,
        reason: `回滚到版本 v${rollbackReview.revision.version}`,
      })
      if (resultError(result)) return
      setRollbackReview(null)
      setActiveRevision(result.revision)
    } catch {
      /* Keep the original target visible. */
    }
  }

  if (!permissionQuery.isLoading && !canView) {
    return (
      <div className="soha-page">
        <Alert showIcon title="无权查看运行时配置" type="warning" />
      </div>
    )
  }

  const historyColumns: TableColumnsType<RuntimeConfigRevision> = [
    { title: '版本', dataIndex: 'version', width: 90, render: (value: number) => `v${value}` },
    {
      title: '状态',
      dataIndex: 'status',
      width: 150,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '变更数',
      dataIndex: 'changes',
      width: 100,
      render: (value: unknown[]) => value.length,
    },
    { title: '操作者', dataIndex: 'actor', width: 160 },
    { title: '原因', dataIndex: 'reason', ellipsis: true },
    {
      ...tableColumnPresets.datetime,
      title: '创建时间',
      dataIndex: 'createdAt',
      render: formatDateTime,
    },
    {
      ...tableColumnPresets.action,
      key: 'actions',
      render: (_, revision) => (
        <ManagementIconButton
          aria-label={`查看版本 v${revision.version}`}
          icon={<EyeOutlined />}
          tooltip="查看变更"
          onClick={() => setActiveRevision(revision)}
        />
      ),
    },
  ]

  const configurationView = (
    <>
      <ReadFeedback query={snapshotQuery} label="运行时配置" canRead={canView} />
      {!applyReview && !rollbackReview && !denied ? (
        <>
          {validateMutation.feedback}
          {applyMutation.feedback}
          {rollbackMutation.feedback}
        </>
      ) : null}
      {runtimeSnapshot &&
      observedSnapshot &&
      runtimeSnapshot.version !== observedSnapshot.version ? (
        <Alert
          type="warning"
          showIcon
          title={`服务端已更新为 v${observedSnapshot.version}，保留草稿及原始版本 v${runtimeSnapshot.version}。保存将检查版本冲突；放弃草稿后读取新版。`}
        />
      ) : null}
      {runtimeSnapshot?.pendingRestart ? (
        <Alert
          className="soha-runtime-config-alert"
          showIcon
          title="存在等待重启生效的配置"
          description="运行中的进程仍使用旧值，请按部署方式完成滚动重启。"
          type="warning"
        />
      ) : null}
      {validation ? (
        <Alert
          className="soha-runtime-config-alert"
          showIcon
          title={validation.valid ? '校验通过' : '校验未通过'}
          description={
            validation.issues.length
              ? validation.issues
                  .map((issue) => `${issue.key ? `${issue.key}: ` : ''}${issue.message}`)
                  .join('；')
              : validation.requiresRestart
                ? '部分变更需要重启后生效。'
                : '所有变更均可提交。'
          }
          type={validation.valid ? (validation.requiresRestart ? 'warning' : 'success') : 'error'}
        />
      ) : null}
      <ManagementQueryPanel
        actions={
          <Space size={12}>
            <Checkbox
              checked={changedOnly}
              disabled={editingBlocked || changes.length === 0}
              onChange={(event) => setChangedOnly(event.target.checked)}
            >
              仅看已修改
            </Checkbox>
            <Button
              disabled={!keyword && !applyMode && !source && !changedOnly}
              icon={<UndoOutlined />}
              onClick={() => {
                setKeyword('')
                setApplyMode('')
                setSource('')
                setChangedOnly(false)
              }}
            >
              重置筛选
            </Button>
          </Space>
        }
        onFinish={() => undefined}
      >
        <ManagementQueryField grow label="关键词" minWidth={260} width={320}>
          <Input
            allowClear
            placeholder="名称、配置键或说明"
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
          />
        </ManagementQueryField>
        <ManagementQueryField label="生效方式" width={180}>
          <Select
            allowClear
            options={Object.entries(APPLY_MODE_LABELS).map(([value, label]) => ({ value, label }))}
            value={applyMode || undefined}
            onChange={(value) => setApplyMode(value ?? '')}
          />
        </ManagementQueryField>
        <ManagementQueryField label="配置来源" width={180}>
          <Select
            allowClear
            options={Object.entries(SOURCE_LABELS).map(([value, label]) => ({ value, label }))}
            value={source || undefined}
            onChange={(value) => setSource(value ?? '')}
          />
        </ManagementQueryField>
      </ManagementQueryPanel>
      <div className="soha-runtime-config-actionbar">
        <div className="soha-runtime-config-actionbar__context">
          <div className="soha-runtime-config-actionbar__summary">
            <Text strong>{changes.length ? `已修改 ${changes.length} 项` : '暂无未应用变更'}</Text>
            <Text type="secondary">配置版本 v{runtimeSnapshot?.version ?? '-'}</Text>
            {Object.entries(impactSummary)
              .filter(([, count]) => count > 0)
              .map(([mode, count]) => (
                <MetadataTag
                  key={mode}
                  label={`${APPLY_MODE_LABELS[mode as RuntimeConfigApplyMode]} ${count}`}
                  tone={mode === 'restart' ? 'orange' : 'blue'}
                />
              ))}
          </div>
          <Input
            disabled={editingBlocked || !canManage}
            className="soha-runtime-config-reason"
            maxLength={1000}
            placeholder="变更原因（可选）"
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </div>
        <Space className="soha-runtime-config-actionbar__actions" size={8} wrap={false}>
          {canManage ? (
            <>
              <Button
                disabled={editingBlocked || changes.length === 0}
                icon={<UndoOutlined />}
                onClick={() => {
                  if (changes.length === 1) {
                    clearDraft()
                    return
                  }
                  modal.confirm({
                    title: `放弃 ${changes.length} 项未应用变更？`,
                    okText: '放弃草稿',
                    okButtonProps: { danger: true },
                    cancelText: '取消',
                    onOk: clearDraft,
                  })
                }}
              >
                放弃草稿
              </Button>
              {canValidate ? (
                <Button
                  disabled={editingBlocked || changes.length === 0}
                  icon={<CheckCircleOutlined />}
                  loading={validateMutation.isPending}
                  onClick={() => void handleValidate()}
                >
                  校验
                </Button>
              ) : null}
              {canApply ? (
                <Button
                  disabled={editingBlocked || changes.length === 0}
                  icon={<SaveOutlined />}
                  loading={applyMutation.isPending}
                  type="primary"
                  onClick={handleApply}
                >
                  应用 {changes.length} 项
                </Button>
              ) : null}
            </>
          ) : null}
          <Button
            icon={<ReloadOutlined />}
            loading={snapshotQuery.isFetching}
            onClick={() => void snapshotQuery.refetch()}
          >
            刷新
          </Button>
        </Space>
      </div>
      <div className="soha-runtime-config-list">
        {snapshotQuery.isLoading ? (
          <div className="soha-runtime-config-list__state">正在读取最新配置...</div>
        ) : !runtimeSnapshot ? null : groupedItems.length === 0 ? (
          <Empty description="当前筛选条件下没有匹配的配置项" />
        ) : (
          groupedItems.map((group) => {
            const regularItems = group.items.filter(
              (item) => !isComputeChildConfig(item.key) && !isAIWorkbenchConfig(item.key),
            )
            const visibleComputeItems = group.items.filter((item) => isComputeChildConfig(item.key))
            const visibleAIItems = group.items.filter((item) => isAIWorkbenchConfig(item.key))
            const visibleAIParent = visibleAIItems.some(
              (item) => item.key === AI_WORKBENCH_CONFIG_KEY,
            )
            const visibleAIChildren = (
              visibleAIParent && !changedOnly ? aiItems : visibleAIItems
            ).filter((item) => isAIWorkbenchChildConfig(item.key))
            return (
              <section className="soha-runtime-config-group" key={group.category}>
                <div className="soha-runtime-config-group__header">
                  <Text strong>{group.category}</Text>
                  <Text type="secondary">{group.items.length} 项配置</Text>
                </div>
                <div className="soha-runtime-config-group__items">
                  {aiWorkbenchItem && visibleAIItems.length > 0 ? (
                    <Collapse
                      bordered={false}
                      className={`soha-runtime-config-branch${changedKeys.has(aiWorkbenchItem.key) ? ' is-changed' : ''}`}
                      defaultActiveKey={['ai-workbench']}
                      items={[
                        {
                          key: 'ai-workbench',
                          label: (
                            <div className="soha-runtime-config-branch__header">
                              <div className="soha-runtime-config-item__info">
                                <Text strong>{aiWorkbenchItem.label || 'AI 工作台'}</Text>
                                <Text type="secondary">1 个功能</Text>
                              </div>
                              <div className="soha-runtime-config-item__effective">
                                <Text type="secondary">当前值</Text>
                                <Text code>{displayValue(aiWorkbenchItem)}</Text>
                              </div>
                              <div
                                className="soha-runtime-config-item__editor"
                                onClick={(event) => event.stopPropagation()}
                              >
                                <Text type="secondary">新值</Text>
                                <Switch
                                  aria-label="AI 工作台"
                                  checked={aiWorkbenchDraftEnabled}
                                  disabled={!canManage || aiWorkbenchLocked || editingBlocked}
                                  onChange={(enabled) => {
                                    updateDraftValues(aiWorkbenchDraft(enabled))
                                  }}
                                />
                              </div>
                              <div className="soha-runtime-config-item__meta">
                                {changedKeys.has(aiWorkbenchItem.key) ? (
                                  <MetadataTag label="已修改" tone="gold" />
                                ) : null}
                                {aiWorkbenchItem.source === 'runtime_override' ? (
                                  <Button
                                    disabled={!canManage || editingBlocked}
                                    icon={<UndoOutlined />}
                                    size="small"
                                    onClick={(event) => {
                                      event.stopPropagation()
                                      resetOverride(aiWorkbenchItem.key)
                                    }}
                                  >
                                    恢复来源值
                                  </Button>
                                ) : null}
                                <MetadataTag label={SOURCE_LABELS[aiWorkbenchItem.source]} />
                                <MetadataTag
                                  label={APPLY_MODE_LABELS[aiWorkbenchItem.applyMode]}
                                  tone="blue"
                                />
                                <StatusTag
                                  value={isLocked(aiWorkbenchItem) ? 'locked' : 'active'}
                                />
                              </div>
                            </div>
                          ),
                          children: (
                            <div className="soha-runtime-config-branch__children">
                              {visibleAIChildren.map((item) => (
                                <article
                                  className={`soha-runtime-config-item${changedKeys.has(item.key) ? ' is-changed' : ''}`}
                                  key={item.key}
                                >
                                  <div className="soha-runtime-config-item__info">
                                    <Text strong>{item.label || item.key}</Text>
                                    <Text code>{item.key}</Text>
                                    {item.description ? (
                                      <Text type="secondary">{item.description}</Text>
                                    ) : null}
                                  </div>
                                  <div className="soha-runtime-config-item__effective">
                                    <Text type="secondary">当前值</Text>
                                    <Text code>{displayValue(item)}</Text>
                                  </div>
                                  <div className="soha-runtime-config-item__editor">
                                    <Text type="secondary">新值</Text>
                                    <ConfigValueEditor
                                      disabled={
                                        !canManage ||
                                        !aiWorkbenchDraftEnabled ||
                                        isLocked(item) ||
                                        editingBlocked
                                      }
                                      item={item}
                                      value={draft[item.key] ?? item.effectiveValue}
                                      onChange={(value) => {
                                        updateDraft(item.key, value)
                                      }}
                                    />
                                  </div>
                                  <div className="soha-runtime-config-item__meta">
                                    {changedKeys.has(item.key) ? (
                                      <MetadataTag label="已修改" tone="gold" />
                                    ) : null}
                                    {item.source === 'runtime_override' ? (
                                      <Button
                                        disabled={!canManage || editingBlocked}
                                        icon={<UndoOutlined />}
                                        size="small"
                                        onClick={() => resetOverride(item.key)}
                                      >
                                        恢复来源值
                                      </Button>
                                    ) : null}
                                    <MetadataTag label="依赖 AI 工作台" tone="cyan" />
                                    <MetadataTag label={SOURCE_LABELS[item.source]} />
                                    <MetadataTag
                                      label={APPLY_MODE_LABELS[item.applyMode]}
                                      tone="blue"
                                    />
                                    <StatusTag value={isLocked(item) ? 'locked' : 'active'} />
                                  </div>
                                </article>
                              ))}
                            </div>
                          ),
                        },
                      ]}
                    />
                  ) : null}
                  {regularItems.map((item) => (
                    <article
                      className={`soha-runtime-config-item${changedKeys.has(item.key) ? ' is-changed' : ''}`}
                      key={item.key}
                    >
                      <div className="soha-runtime-config-item__info">
                        <Text strong>{item.label || item.key}</Text>
                        <Text code>{item.key}</Text>
                        {item.description ? <Text type="secondary">{item.description}</Text> : null}
                      </div>
                      <div className="soha-runtime-config-item__effective">
                        <Text type="secondary">当前值</Text>
                        <Text code>{displayValue(item)}</Text>
                      </div>
                      <div className="soha-runtime-config-item__editor">
                        <Text type="secondary">新值</Text>
                        <ConfigValueEditor
                          disabled={!canManage || isLocked(item) || editingBlocked}
                          item={item}
                          value={
                            draft[item.key] ?? (item.sensitive ? undefined : item.effectiveValue)
                          }
                          onChange={(value) => {
                            updateDraft(item.key, value)
                          }}
                        />
                      </div>
                      <div className="soha-runtime-config-item__meta">
                        {changedKeys.has(item.key) ? (
                          <MetadataTag label="已修改" tone="gold" />
                        ) : null}
                        {item.source === 'runtime_override' ? (
                          <Button
                            disabled={!canManage || editingBlocked}
                            icon={<UndoOutlined />}
                            size="small"
                            onClick={() => resetOverride(item.key)}
                          >
                            恢复来源值
                          </Button>
                        ) : null}
                        <MetadataTag label={SOURCE_LABELS[item.source]} />
                        <MetadataTag
                          label={APPLY_MODE_LABELS[item.applyMode]}
                          tone={item.applyMode === 'restart' ? 'orange' : 'blue'}
                        />
                        <StatusTag
                          value={
                            item.pendingRestart
                              ? 'restart_required'
                              : isLocked(item)
                                ? 'locked'
                                : 'active'
                          }
                        />
                      </div>
                    </article>
                  ))}
                  {visibleComputeItems.length > 0 ? (
                    <Collapse
                      bordered={false}
                      className={`soha-runtime-config-branch${visibleComputeItems.some((item) => changedKeys.has(item.key)) ? ' is-changed' : ''}`}
                      defaultActiveKey={['compute']}
                      items={[
                        {
                          key: 'compute',
                          label: (
                            <div className="soha-runtime-config-branch__header">
                              <div className="soha-runtime-config-item__info">
                                <Text strong>计算资源工作台</Text>
                                <Text type="secondary">2 个子模块</Text>
                              </div>
                              <div className="soha-runtime-config-item__effective">
                                <Text type="secondary">当前值</Text>
                                <Text>{COMPUTE_STATE_LABELS[computeCurrentState]}</Text>
                              </div>
                              <div
                                className="soha-runtime-config-item__editor"
                                onClick={(event) => event.stopPropagation()}
                              >
                                <Text type="secondary">批量设置</Text>
                                <Checkbox
                                  aria-label="计算资源工作台"
                                  checked={computeDraftState === 'enabled'}
                                  disabled={!canManage || computeParentLocked || editingBlocked}
                                  indeterminate={computeDraftState === 'partial'}
                                  onChange={(event) => {
                                    updateDraftValues(computeModuleDraft(event.target.checked))
                                  }}
                                >
                                  {COMPUTE_STATE_LABELS[computeDraftState]}
                                </Checkbox>
                              </div>
                              <div className="soha-runtime-config-item__meta">
                                {visibleComputeItems.some((item) => changedKeys.has(item.key)) ? (
                                  <MetadataTag
                                    label={`已修改 ${visibleComputeItems.filter((item) => changedKeys.has(item.key)).length} 项`}
                                    tone="gold"
                                  />
                                ) : null}
                                <MetadataTag label="派生状态" />
                                <MetadataTag label="批量联动" tone="cyan" />
                              </div>
                            </div>
                          ),
                          children: (
                            <div className="soha-runtime-config-branch__children">
                              {visibleComputeItems.map((item) => (
                                <article
                                  className={`soha-runtime-config-item${changedKeys.has(item.key) ? ' is-changed' : ''}`}
                                  key={item.key}
                                >
                                  <div className="soha-runtime-config-item__info">
                                    <Text strong>{item.label || item.key}</Text>
                                    <Text code>{item.key}</Text>
                                    {item.description ? (
                                      <Text type="secondary">{item.description}</Text>
                                    ) : null}
                                  </div>
                                  <div className="soha-runtime-config-item__effective">
                                    <Text type="secondary">当前值</Text>
                                    <Text code>{displayValue(item)}</Text>
                                  </div>
                                  <div className="soha-runtime-config-item__editor">
                                    <Text type="secondary">新值</Text>
                                    <ConfigValueEditor
                                      disabled={!canManage || isLocked(item) || editingBlocked}
                                      item={item}
                                      value={draft[item.key] ?? item.effectiveValue}
                                      onChange={(value) => {
                                        updateDraft(item.key, value)
                                      }}
                                    />
                                  </div>
                                  <div className="soha-runtime-config-item__meta">
                                    {changedKeys.has(item.key) ? (
                                      <MetadataTag label="已修改" tone="gold" />
                                    ) : null}
                                    {item.source === 'runtime_override' ? (
                                      <Button
                                        disabled={!canManage || editingBlocked}
                                        icon={<UndoOutlined />}
                                        size="small"
                                        onClick={() => resetOverride(item.key)}
                                      >
                                        恢复来源值
                                      </Button>
                                    ) : null}
                                    <MetadataTag label={SOURCE_LABELS[item.source]} />
                                    <MetadataTag
                                      label={APPLY_MODE_LABELS[item.applyMode]}
                                      tone={item.applyMode === 'restart' ? 'orange' : 'blue'}
                                    />
                                    <StatusTag value={isLocked(item) ? 'locked' : 'active'} />
                                  </div>
                                </article>
                              ))}
                            </div>
                          ),
                        },
                      ]}
                    />
                  ) : null}
                </div>
              </section>
            )
          })
        )}
      </div>
    </>
  )

  const historyView = (
    <SettingsAdminTable
      columns={historyColumns}
      dataSource={readableQueryData(historyQuery) ?? []}
      error={historyQuery.error}
      hasReadData={readableQueryData(historyQuery) !== undefined}
      onRetry={canView ? () => void historyQuery.refetch() : undefined}
      loading={historyQuery.isLoading}
      pageSize={20}
      rowKey="id"
    />
  )

  const resourceView = <RuntimeResourceOverview query={resourceQuery} />

  return (
    <div className="soha-page soha-runtime-config-page">
      <Tabs
        activeKey={activeTab}
        className="soha-resource-tabs"
        items={[
          { key: 'configuration', label: '运行时配置', children: configurationView },
          { key: 'resources', label: '服务资源', children: resourceView },
          { key: 'history', label: '变更历史', children: historyView },
        ]}
        onChange={(key) => setActiveTab(key as RuntimeConfigurationTab)}
      />
      <Modal
        title={`应用 ${applyReview?.changes.length ?? 0} 项配置变更？`}
        open={!denied && Boolean(applyReview)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: false }}
        cancelButtonProps={{ disabled: blocked }}
        okButtonProps={{
          disabled:
            blocked || !canApply || Boolean(applyMutation.data && resultError(applyMutation.data)),
        }}
        confirmLoading={validateMutation.isPending || applyMutation.isPending}
        okText="应用配置"
        cancelText="取消"
        onCancel={() => setApplyReview(null)}
        onOk={() => void submitApply()}
      >
        <div className="soha-runtime-config-confirm">
          <Text>系统会再次校验版本与配置。请确认生效影响：</Text>
          <Space wrap>
            {Object.entries(impactSummary)
              .filter(([, count]) => count > 0)
              .map(([mode, count]) => (
                <MetadataTag
                  key={mode}
                  label={`${APPLY_MODE_LABELS[mode as RuntimeConfigApplyMode]} ${count} 项`}
                />
              ))}
          </Space>
          <p>原始配置版本 v{applyReview?.expectedVersion}</p>
        </div>
        {validateMutation.feedback}
        {applyMutation.feedback}
        {applyMutation.data ? (
          <Button
            onClick={() => {
              setActiveRevision(applyMutation.data.revision)
              setApplyReview(null)
            }}
          >
            查看返回版本
          </Button>
        ) : null}
      </Modal>
      <Modal
        title={`回滚到版本 v${rollbackReview?.revision.version ?? '-'}？`}
        open={!denied && Boolean(rollbackReview)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: false }}
        cancelButtonProps={{ disabled: blocked }}
        okButtonProps={{
          danger: true,
          disabled:
            blocked ||
            !canRollback ||
            Boolean(rollbackMutation.data && resultError(rollbackMutation.data)),
        }}
        confirmLoading={rollbackMutation.isPending}
        okText="确认回滚"
        cancelText="取消"
        onCancel={() => setRollbackReview(null)}
        onOk={() => void submitRollback()}
      >
        <p>回滚会创建新的配置版本，并按当前运行时规则应用。</p>
        {rollbackMutation.feedback}
        {rollbackMutation.data ? (
          <Button
            onClick={() => {
              setActiveRevision(rollbackMutation.data.revision)
              setRollbackReview(null)
            }}
          >
            查看返回版本
          </Button>
        ) : null}
      </Modal>
      <RuntimeConfigurationHistoryDrawer
        applicationId={
          [applyMutation.data, rollbackMutation.data].find(
            (result) => result && result.revision.id === activeRevision?.id,
          )?.application.id
        }
        canRollback={
          canRollback && !editingBlocked && activeRevision?.version !== observedSnapshot?.version
        }
        items={runtimeSnapshot?.items ?? []}
        open={!denied && !queryAccessDenied(historyQuery) && Boolean(activeRevision)}
        revision={activeRevision}
        rollingBack={rollbackMutation.isPending}
        onClose={() => setActiveRevision(null)}
        onRollback={handleRollback}
      />
    </div>
  )
}
