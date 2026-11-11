import type { ReactNode } from 'react'
import { Card, Descriptions, Space, Tag, Typography } from 'antd'
import { StatusTag } from '@/components/status-tag'
import { stringifyPayload } from '@/features/system/system-model'

const { Text } = Typography

const SNAPSHOT_FIELDS = [
  'usageCount',
  'applicationCount',
  'environmentCount',
  'productionEnvironmentCount',
  'approvalBindingCount',
  'targetCount',
  'riskLevel',
  'recommendedAction',
]

type SnapshotRecord = Record<string, unknown>

type DiffRow = {
  field: string
  before: unknown
  after: unknown
}

type BindingKey = string

function asRecord(value: unknown): SnapshotRecord | undefined {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as SnapshotRecord : undefined
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function usageSnapshotRoot(metadata?: SnapshotRecord) {
  return asRecord(metadata?.usageSnapshot)
}

function normalizeSnapshotValue(value: unknown) {
  if (Array.isArray(value)) return value.join(' / ')
  if (value && typeof value === 'object') return JSON.stringify(value)
  if (value === undefined || value === null || value === '') return '-'
  return String(value)
}

function parseNumeric(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  const text = String(value ?? '').trim()
  if (!text) return undefined
  const parsed = Number(text)
  return Number.isFinite(parsed) ? parsed : undefined
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    const text = String(value ?? '').trim()
    if (text) return text
  }
  return ''
}

function nestedRecord(record: SnapshotRecord, key: string) {
  return asRecord(record[key])
}

function getComparableSnapshots(snapshot?: SnapshotRecord) {
  if (!snapshot) return { before: undefined, after: undefined }
  const before = asRecord(snapshot.before) ?? asRecord(snapshot.previous)
  const after = asRecord(snapshot.after) ?? asRecord(snapshot.current)
  if (before || after) return { before, after }
  return { before: snapshot, after: undefined }
}

function isProductionEnvironment(record: SnapshotRecord) {
  return Boolean(record.isProduction ?? record.production ?? record.productionEnvironment)
}

function bindingKey(record: SnapshotRecord): BindingKey {
  const application = nestedRecord(record, 'application')
  const environment = nestedRecord(record, 'environment')
  return [
    firstString(record.applicationId, application?.id, record.applicationKey, application?.key, record.applicationName, application?.name),
    firstString(record.environmentId, environment?.id, record.environmentKey, environment?.key, record.environmentName, environment?.name),
    firstString(record.targetKey, record.targetId, record.resourceName, record.resourceKey, record.workloadName, record.namespace),
  ].join('::')
}

function bindingSummary(record: SnapshotRecord) {
  const applicationRecord = nestedRecord(record, 'application')
  const environmentRecord = nestedRecord(record, 'environment')
  const application = firstString(record.applicationName, applicationRecord?.name, record.applicationKey, applicationRecord?.key, record.applicationId)
  const environment = firstString(record.environmentName, environmentRecord?.name, record.environmentKey, environmentRecord?.key, record.environmentId)
  const targetCount = parseNumeric(record.targetCount) ?? 0
  const isProd = isProductionEnvironment(record) || Boolean(environmentRecord?.isProduction)
  const requiresApproval = Boolean(record.requiresApproval ?? environmentRecord?.requiresApproval ?? record.approvalRequired)
  const riskLevel = firstString(record.riskLevel, environmentRecord?.riskLevel)
  return {
    key: bindingKey(record),
    application,
    environment,
    targetCount,
    isProd,
    requiresApproval,
    riskLevel,
    label: [application, environment].filter(Boolean).join(' / ') || '-',
  }
}

function normalizeBindings(value: unknown) {
  return asArray(value)
    .map(asRecord)
    .filter(Boolean)
    .map((record) => bindingSummary(record as SnapshotRecord))
}

function bindingChangeType(before: ReturnType<typeof bindingSummary> | undefined, after: ReturnType<typeof bindingSummary> | undefined) {
  if (!before && after) return 'added'
  if (before && !after) return 'removed'
  return 'updated'
}

function diffBindings(beforeSnapshot?: SnapshotRecord, afterSnapshot?: SnapshotRecord) {
  const before = normalizeBindings(beforeSnapshot?.bindings)
  const after = normalizeBindings(afterSnapshot?.bindings)
  const beforeMap = new Map(before.map((item) => [item.key, item]))
  const afterMap = new Map(after.map((item) => [item.key, item]))
  const keys = Array.from(new Set([...beforeMap.keys(), ...afterMap.keys()])).sort()
  return keys.map((key) => {
    const beforeItem = beforeMap.get(key)
    const afterItem = afterMap.get(key)
    return {
      key,
      type: bindingChangeType(beforeItem, afterItem),
      before: beforeItem,
      after: afterItem,
    }
  })
}

function severityForBindingChange(type: string, binding?: ReturnType<typeof bindingSummary>) {
  if (type === 'added' && binding?.isProd) return '高风险'
  if (type === 'added' && binding?.requiresApproval) return '审批门禁'
  if (type === 'removed') return '已移除'
  return '已更新'
}

function statusBadgeColor(type: string, binding?: ReturnType<typeof bindingSummary>) {
  if (type === 'added' && binding?.isProd) return 'red'
  if (type === 'added' && binding?.requiresApproval) return 'gold'
  if (type === 'removed') return 'default'
  return 'blue'
}

function runtimeSummary(snapshot?: SnapshotRecord) {
  const summary = asRecord(snapshot?.lastExecutionSummary)
  return {
    source: firstString(summary?.source, snapshot?.source),
    note: firstString(summary?.note),
    latestAt: firstString(summary?.latestAt),
    latest: asRecord(summary?.latest),
    stateCounts: asRecord(summary?.stateCounts) ?? {},
    statusCounts: asRecord(summary?.statusCounts) ?? {},
    kindCounts: asRecord(summary?.kindCounts) ?? {},
    items: asArray(summary?.items).map(asRecord).filter(Boolean) as SnapshotRecord[],
  }
}

function runtimeEvidenceTags(runtime: ReturnType<typeof runtimeSummary>) {
  const tags: Array<{ color?: 'red' | 'gold' | 'blue' | 'green'; label: string }> = []
  const failed = parseNumeric(runtime.stateCounts.failed) ?? 0
  const running = parseNumeric(runtime.stateCounts.running) ?? 0
  const latestAt = runtime.latestAt || firstString(runtime.latest?.observedAt)
  if (failed > 0) tags.push({ color: 'red', label: `失败执行 ${failed}` })
  if (running > 0) tags.push({ color: 'blue', label: `运行中执行 ${running}` })
  if (latestAt) tags.push({ color: 'green', label: `最近执行 ${latestAt}` })
  return tags
}

function collectFieldDiffs(before?: SnapshotRecord, after?: SnapshotRecord): DiffRow[] {
  if (!after) return []
  return SNAPSHOT_FIELDS
    .filter((field) => normalizeSnapshotValue(before?.[field]) !== normalizeSnapshotValue(after[field]))
    .map((field) => ({
      field,
      before: before?.[field],
      after: after[field],
    }))
}

function diffCounts(before?: SnapshotRecord, after?: SnapshotRecord) {
  const fields = ['statusCounts', 'stateCounts', 'kindCounts'] as const
  return fields.flatMap((field) => {
    const beforeCounts = asRecord(before?.[field]) ?? {}
    const afterCounts = asRecord(after?.[field]) ?? {}
    const keys = Array.from(new Set([...Object.keys(beforeCounts), ...Object.keys(afterCounts)])).sort()
    return keys
      .filter((key) => normalizeSnapshotValue(beforeCounts[key]) !== normalizeSnapshotValue(afterCounts[key]))
      .map((key) => ({
        field: `${field}.${key}`,
        before: beforeCounts[key],
        after: afterCounts[key],
      }))
  })
}

function renderBindingLine(binding: ReturnType<typeof bindingSummary>, type: string) {
  const tags: ReactNode[] = []
  if (binding.isProd) tags.push(<Tag color="red" key="prod">生产</Tag>)
  if (binding.requiresApproval) tags.push(<Tag color="gold" key="approval">审批门禁</Tag>)
  if (binding.riskLevel) tags.push(<StatusTag key="risk" value={binding.riskLevel} />)
  tags.push(<Tag key="targets">目标 {binding.targetCount}</Tag>)
  return (
    <Space wrap size={[6, 6]}>
      <Tag color={statusBadgeColor(type, binding)}>{severityForBindingChange(type, binding)}</Tag>
      <Text strong>{binding.label}</Text>
      {tags}
    </Space>
  )
}

function renderRuntimeDiff(before?: SnapshotRecord, after?: SnapshotRecord) {
  if (!before && !after) return null
  const beforeRuntime = runtimeSummary(before)
  const afterRuntime = runtimeSummary(after)
  const tags = runtimeEvidenceTags(afterRuntime)
  const fieldDiffs = diffCounts(before, after)
  const beforeLatestAt = beforeRuntime.latestAt || firstString(beforeRuntime.latest?.observedAt)
  const afterLatestAt = afterRuntime.latestAt || firstString(afterRuntime.latest?.observedAt)
  const beforeFailed = parseNumeric(beforeRuntime.stateCounts.failed) ?? 0
  const afterFailed = parseNumeric(afterRuntime.stateCounts.failed) ?? 0
  const beforeRunning = parseNumeric(beforeRuntime.stateCounts.running) ?? 0
  const afterRunning = parseNumeric(afterRuntime.stateCounts.running) ?? 0
  return (
    <Space orientation="vertical" size={10} style={{ width: '100%' }}>
      <Space wrap size={[6, 6]}>
        {tags.length > 0 ? tags.map((item) => <Tag color={item.color} key={item.label}>{item.label}</Tag>) : <Tag>运行态摘要</Tag>}
        {beforeFailed !== afterFailed ? <Tag color={afterFailed > beforeFailed ? 'red' : 'green'}>失败执行 {beforeFailed} → {afterFailed}</Tag> : null}
        {beforeRunning !== afterRunning ? <Tag color="blue">运行中执行 {beforeRunning} → {afterRunning}</Tag> : null}
        {normalizeSnapshotValue(beforeLatestAt) !== normalizeSnapshotValue(afterLatestAt) ? <Tag color="green">最近执行时间 {normalizeSnapshotValue(beforeLatestAt)} → {normalizeSnapshotValue(afterLatestAt)}</Tag> : null}
      </Space>
      {fieldDiffs.length > 0 ? (
        <Descriptions
          bordered
          size="small"
          column={1}
          title="运行态计数变化"
          items={fieldDiffs.map((item) => ({
            key: item.field,
            label: item.field,
            children: (
              <Space wrap>
                <Tag>{normalizeSnapshotValue(item.before)}</Tag>
                <Text type="secondary">→</Text>
                <Tag color="blue">{normalizeSnapshotValue(item.after)}</Tag>
              </Space>
            ),
          }))}
        />
      ) : null}
      {beforeRuntime.note || afterRuntime.note ? (
        <Text type="secondary">{afterRuntime.note || beforeRuntime.note}</Text>
      ) : null}
    </Space>
  )
}

function renderDiffSummary(before?: SnapshotRecord, after?: SnapshotRecord) {
  if (!before && !after) return null
  const fieldDiffs = collectFieldDiffs(before, after)
  const bindingDiffs = diffBindings(before, after)
  const countsChanged = diffCounts(before, after)
  return { fieldDiffs, bindingDiffs, countsChanged }
}

export function extractUsageSnapshot(metadata?: SnapshotRecord) {
  const snapshot = usageSnapshotRoot(metadata)
  const { before, after } = getComparableSnapshots(snapshot)
  const current = after ?? before ?? snapshot
  return { snapshot, before, after, current }
}

export function usageSnapshotFilterParams(params: URLSearchParams) {
  const usageTemplateKind = params.get('usageTemplateKind')?.trim() ?? ''
  const usageTemplateId = params.get('usageTemplateId')?.trim() ?? ''
  const usageRiskLevel = params.get('usageRiskLevel')?.trim() ?? ''
  const metadataKey = params.get('metadataKey')?.trim()
    || (usageTemplateId ? 'usageSnapshot.templateId' : usageRiskLevel ? 'usageSnapshot.riskLevel' : usageTemplateKind ? 'usageSnapshot.templateKind' : '')
  const metadataValue = params.get('metadataValue')?.trim()
    || usageTemplateId
    || usageRiskLevel
    || usageTemplateKind
  return {
    metadataKey,
    metadataValue,
    usageTemplateKind,
    usageTemplateId,
    usageRiskLevel,
  }
}

function SnapshotFieldRows({ current }: { current: SnapshotRecord }) {
  return (
    <Descriptions
      bordered
      size="small"
      column={1}
      items={[
        { key: 'kind', label: '模板类型', children: current.templateKind ? <Tag>{String(current.templateKind)}</Tag> : '-' },
        { key: 'id', label: '模板 ID', children: normalizeSnapshotValue(current.templateId) },
        { key: 'risk', label: '风险等级', children: current.riskLevel ? <StatusTag value={String(current.riskLevel)} /> : '-' },
        { key: 'recommendedAction', label: '建议动作', children: normalizeSnapshotValue(current.recommendedAction) },
        { key: 'usage', label: '使用数', children: normalizeSnapshotValue(current.usageCount) },
        { key: 'applications', label: '应用数', children: normalizeSnapshotValue(current.applicationCount) },
        { key: 'environments', label: '环境数', children: normalizeSnapshotValue(current.environmentCount) },
        { key: 'production', label: '生产绑定', children: normalizeSnapshotValue(current.productionEnvironmentCount) },
        { key: 'approval', label: '审批门禁', children: normalizeSnapshotValue(current.approvalBindingCount) },
        { key: 'targets', label: '目标数', children: normalizeSnapshotValue(current.targetCount) },
      ]}
    />
  )
}

function CurrentRiskReasons({ current }: { current: SnapshotRecord }) {
  if (!Array.isArray(current.riskReasons) || current.riskReasons.length === 0) return null
  return (
    <Space wrap size={[6, 6]}>
      {current.riskReasons.map((item, index) => <Tag color="gold" key={`${item}-${index}`}>{String(item)}</Tag>)}
    </Space>
  )
}

function SnapshotBindings({ before, after }: { before?: SnapshotRecord; after?: SnapshotRecord }) {
  const diff = diffBindings(before, after)
  const current = after ?? before
  const bindings = normalizeBindings(current?.bindings)
  if (diff.length === 0 && bindings.length === 0) {
    return <Text type="secondary">未发现可展开的应用环境绑定。</Text>
  }
  return (
    <Space orientation="vertical" size={10} style={{ width: '100%' }}>
      {diff.length > 0 ? (
        <Descriptions
          bordered
          size="small"
          column={1}
          title="应用环境绑定变化"
          items={diff.map((item) => ({
            key: item.key,
            label: item.type === 'updated' ? '变更' : item.type === 'added' ? '新增' : '删除',
            children: item.after ? renderBindingLine(item.after, item.type) : item.before ? renderBindingLine(item.before, item.type) : '-',
          }))}
        />
      ) : null}
      {bindings.length > 0 ? (
        <Space wrap size={[6, 6]}>
          {bindings.slice(0, 8).map((binding) => (
            <Tag key={binding.key} color={binding.isProd ? 'red' : binding.requiresApproval ? 'gold' : undefined}>
              {binding.label} / 目标 {binding.targetCount}
            </Tag>
          ))}
        </Space>
      ) : null}
    </Space>
  )
}

function SnapshotRuntime({ before, after }: { before?: SnapshotRecord; after?: SnapshotRecord }) {
  const diff = renderRuntimeDiff(before, after)
  if (!diff) return <Text type="secondary">未发现运行态证据摘要。</Text>
  return diff
}

function StructuredDiff({ before, after }: { before?: SnapshotRecord; after?: SnapshotRecord }) {
  const diff = renderDiffSummary(before, after)
  if (!diff) return null
  const hasChanges = diff.fieldDiffs.length > 0 || diff.bindingDiffs.length > 0 || diff.countsChanged.length > 0
  if (!hasChanges) {
    return <Text type="secondary">当前记录包含单个 usage 快照；新记录会在可用时展示保存前后字段差异。</Text>
  }
  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      {diff.fieldDiffs.length > 0 ? (
        <Descriptions
          bordered
          size="small"
          column={1}
          title="模板摘要变化"
          items={diff.fieldDiffs.map((item) => ({
            key: item.field,
            label: item.field,
            children: (
              <Space wrap>
                <Tag>{normalizeSnapshotValue(item.before)}</Tag>
                <Text type="secondary">→</Text>
                <Tag color="blue">{normalizeSnapshotValue(item.after)}</Tag>
              </Space>
            ),
          }))}
        />
      ) : null}
      <SnapshotBindings before={before} after={after} />
      <SnapshotRuntime before={before} after={after} />
    </Space>
  )
}

function RawSnapshotJson({ snapshot }: { snapshot?: SnapshotRecord }) {
  if (!snapshot) return <Text type="secondary">无可展示的 JSON 数据。</Text>
  return <pre className="soha-system-json-block">{stringifyPayload(snapshot)}</pre>
}

export function UsageSnapshotSummary({ metadata }: { metadata?: SnapshotRecord }) {
  const { snapshot, current } = extractUsageSnapshot(metadata)
  if (!snapshot || !current) return null
  return (
    <Space wrap size={[6, 6]}>
      {current.templateKind ? <Tag>{String(current.templateKind)}</Tag> : null}
      {current.templateId ? <Tag>{String(current.templateId)}</Tag> : null}
      {current.riskLevel ? <StatusTag value={String(current.riskLevel)} /> : null}
      {current.productionEnvironmentCount !== undefined ? <Tag color={Number(current.productionEnvironmentCount) > 0 ? 'red' : undefined}>生产 {String(current.productionEnvironmentCount)}</Tag> : null}
      {current.approvalBindingCount !== undefined ? <Tag color={Number(current.approvalBindingCount) > 0 ? 'gold' : undefined}>审批 {String(current.approvalBindingCount)}</Tag> : null}
      {current.targetCount !== undefined ? <Tag>目标 {String(current.targetCount)}</Tag> : null}
    </Space>
  )
}

export function UsageSnapshotPanel({ metadata }: { metadata?: SnapshotRecord }) {
  const { snapshot, before, after, current } = extractUsageSnapshot(metadata)
  if (!snapshot || !current) return null
  return (
    <Card variant="outlined" className="soha-system-usage-snapshot-card" title="Usage Snapshot">
      <Space orientation="vertical" size={12} style={{ width: '100%' }}>
        <SnapshotFieldRows current={current} />
        <CurrentRiskReasons current={current} />
        <StructuredDiff before={before} after={after} />
        <Card variant="outlined" size="small" title="原始 JSON">
          <RawSnapshotJson snapshot={snapshot} />
        </Card>
      </Space>
    </Card>
  )
}

export function UsageSnapshotDiffView({ metadata }: { metadata?: SnapshotRecord }) {
  const { before, after } = extractUsageSnapshot(metadata)
  return <StructuredDiff before={before} after={after} />
}

export function UsageSnapshotRawJson({ metadata }: { metadata?: SnapshotRecord }) {
  const { snapshot } = extractUsageSnapshot(metadata)
  return <RawSnapshotJson snapshot={snapshot} />
}

export { SNAPSHOT_FIELDS, normalizeSnapshotValue, asRecord, diffCounts, diffBindings, runtimeSummary, renderRuntimeDiff, renderDiffSummary }
