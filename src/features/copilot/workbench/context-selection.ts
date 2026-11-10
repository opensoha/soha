import type { WorkbenchTextAttachment } from '@opensoha/contracts/gen/ts/sohaapi'

export const textAttachmentAccept = '.txt,.log,.md,.json,.yaml,.yml,.toml,.ini,.conf,.cfg,.csv,.xml'

export async function readTextAttachment(file: File): Promise<WorkbenchTextAttachment> {
  const extension = file.name.slice(file.name.lastIndexOf('.')).toLowerCase()
  if (!textAttachmentAccept.split(',').includes(extension) || file.size > 65_536) {
    throw new Error('请选择不超过 64 KiB 的文本、日志或配置文件。')
  }
  const content = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer())
  if (content.includes('\0')) throw new Error('不支持二进制文件。')
  return { id: crypto.randomUUID(), name: file.name, content }
}

type ContextRow = { label: string; value: string }

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function tokenCount(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : undefined
}

export function formatTokens(value: number) {
  return value >= 1000
    ? `${new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 }).format(value / 1000)}K`
    : value.toLocaleString('zh-CN')
}

export function contextSnapshotSummary(value: unknown): ContextRow[] {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return []
  const snapshot = record(value)
  const budget = record(snapshot.budgetUsage)
  const citations = Array.isArray(snapshot.citations) ? snapshot.citations : []
  const toolSources = Array.isArray(snapshot.toolSources) ? snapshot.toolSources : []
  const sources = [...citations, ...toolSources]
    .slice(0, 30)
    .map((item) => {
      const source = record(item)
      return [source.documentTitle, source.title, source.id].find(
        (text) => typeof text === 'string',
      )
    })
    .filter(Boolean)
    .join('、')
  const truncations = Array.isArray(snapshot.truncations)
    ? snapshot.truncations
        .filter((item) => typeof item === 'string')
        .map((item) =>
          item === 'memory_unavailable' ? '长期记忆暂不可用，本次未使用不可用的记忆' : item,
        )
        .join('；')
    : ''
  return [
    { label: '来源', value: sources || '没有额外引用资料' },
    { label: '证据条目', value: `${tokenCount(budget.evidenceItems) ?? 0} 条` },
    {
      label: '证据用量',
      value:
        tokenCount(budget.evidenceTokens) === undefined
          ? '未知'
          : `估算 ${formatTokens(budget.evidenceTokens as number)} tokens`,
    },
    { label: '资料裁剪', value: truncations || '无裁剪记录' },
    {
      label: '历史裁剪',
      value: snapshot.historyTruncated ? '历史已按消息数或字符预算裁剪' : '无裁剪记录',
    },
  ]
}

export function modelContextSummary(value: unknown) {
  const usage = record(value)
  const measured = usage.measurement === 'reported' || usage.measurement === 'estimated'
  const input = measured ? tokenCount(usage.inputTokens) : undefined
  const output = measured ? tokenCount(usage.outputTokens) : undefined
  const capacity = tokenCount(usage.contextWindowTokens) || undefined
  const estimated = usage.measurement === 'estimated'
  const model = typeof usage.model === 'string' ? usage.model : undefined
  const measurement = estimated ? '按输入与输出估算' : measured ? '模型上游回报' : '尚无用量回报'
  const tokens = (count: number | undefined) =>
    count === undefined ? '未知' : `${estimated ? '估算 ' : ''}${formatTokens(count)} tokens`
  const label = `上下文 ${input === undefined ? '未知' : (estimated ? '约 ' : '') + formatTokens(input)}${capacity ? ' / ' + formatTokens(capacity) : ''}`
  const percent = input !== undefined && capacity ? (input / capacity) * 100 : undefined
  const rows: ContextRow[] = [
    { label: '本次模型', value: model || '未记录' },
    { label: '输入用量', value: tokens(input) },
    { label: '输出用量', value: tokens(output) },
    {
      label: '模型窗口',
      value: capacity ? `${formatTokens(capacity)} tokens · 路由配置` : '容量未配置',
    },
    { label: '计量来源', value: measurement },
  ]
  return { label, rows, input, capacity, estimated, percent, model }
}

export function taskUsageSummary(value: unknown): ContextRow[] {
  const usage = record(value)
  const total = tokenCount(usage.totalTokens)
  if (total === undefined) return []
  const limit = tokenCount(usage.taskTokenLimit)
  return [
    {
      label: '累计消耗',
      value: `${usage.estimatedTokens ? '估算 ' : ''}${formatTokens(total)} tokens`,
    },
    ...(limit ? [{ label: '任务预算', value: `${formatTokens(limit)} tokens` }] : []),
    { label: '模型调用', value: `${tokenCount(usage.steps) ?? '未知'} 次` },
  ]
}
