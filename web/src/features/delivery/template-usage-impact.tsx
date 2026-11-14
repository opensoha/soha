import { Alert, Button, Space, Tag, Typography } from 'antd'
import { LinkOutlined } from '@ant-design/icons'
import type {
  TemplateUsageApplication,
  TemplateUsageBinding,
  TemplateUsageBuildSource,
  TemplateUsageRiskLevel,
  TemplateUsageRuntimeItem,
  TemplateUsageSummary,
} from '@/types'
import { runtimeEvidencePath } from '@/features/delivery/template-usage-runtime-links'

const { Text } = Typography

type LocaleCode = 'zh_CN' | 'en_US'
type NavigateFn = (path: string) => void

interface TemplateUsageImpactPanelProps {
  usage?: TemplateUsageSummary
  loading?: boolean
  localeCode?: LocaleCode
  onNavigate?: NavigateFn
}

function riskColor(risk?: TemplateUsageRiskLevel) {
  switch (risk) {
    case 'high':
      return 'red'
    case 'medium':
      return 'gold'
    case 'low':
      return 'green'
    default:
      return 'default'
  }
}

function alertType(risk?: TemplateUsageRiskLevel) {
  switch (risk) {
    case 'high':
      return 'error' as const
    case 'medium':
      return 'warning' as const
    default:
      return 'info' as const
  }
}

function riskText(risk: TemplateUsageRiskLevel | undefined, localeCode: LocaleCode) {
  if (localeCode === 'en_US') {
    if (risk === 'high') return 'High risk'
    if (risk === 'medium') return 'Medium risk'
    return 'Low risk'
  }
  if (risk === 'high') return '高风险'
  if (risk === 'medium') return '中风险'
  return '低风险'
}

function compactApplicationLabel(app?: TemplateUsageApplication) {
  return app?.name || app?.key || app?.id || ''
}

function uniqueApplications(usage: TemplateUsageSummary) {
  const items = new Map<string, TemplateUsageApplication>()
  for (const app of usage.applications ?? []) {
    if (app.id) items.set(app.id, app)
  }
  for (const binding of usage.bindings ?? []) {
    if (binding.application?.id) items.set(binding.application.id, binding.application)
  }
  for (const source of usage.buildSources ?? []) {
    if (source.application?.id) items.set(source.application.id, source.application)
  }
  return Array.from(items.values()).filter((item) => compactApplicationLabel(item))
}

function bindingLabel(binding: TemplateUsageBinding) {
  const app = compactApplicationLabel(binding.application) || binding.applicationId || '-'
  const env =
    binding.environment?.name ||
    binding.environment?.key ||
    binding.environmentKey ||
    binding.environmentId ||
    '-'
  return `${app} / ${env}`
}

function buildSourceLabel(source: TemplateUsageBuildSource) {
  return source.buildSourceName || source.buildSourceId
}

function fileKindLabels(usage: TemplateUsageSummary) {
  return Object.entries(usage.fileKindCounts ?? {})
    .filter(([, count]) => count > 0)
    .map(([kind, count]) => `${kind} x ${count}`)
}

function usageTitle(usage: TemplateUsageSummary, localeCode: LocaleCode) {
  if (localeCode === 'en_US') {
    return `${riskText(usage.riskLevel, localeCode)} template impact`
  }
  return `${riskText(usage.riskLevel, localeCode)}模板影响面`
}

function runtimeStateColor(state: string) {
  switch (state) {
    case 'failed':
      return 'red'
    case 'running':
      return 'blue'
    case 'pending':
      return 'gold'
    case 'succeeded':
      return 'green'
    default:
      return 'default'
  }
}

function runtimeItemLabel(item: TemplateUsageRuntimeItem, localeCode: LocaleCode) {
  const kind = item.kind.replace(/_/g, ' ')
  const prefix =
    localeCode === 'en_US'
      ? kind
      : ({
          build: '构建',
          workflow: '工作流',
          release: '发布',
          release_bundle: '版本包',
          execution_task: '执行任务',
        }[item.kind] ?? kind)
  const target = item.workflowName || item.version || item.sourceSystem || item.taskKind || item.id
  return `${prefix}: ${target}`
}

function runtimeLinkForItem(item: TemplateUsageRuntimeItem) {
  return runtimeEvidencePath(item)
}

function runtimeEvidence(
  usage: TemplateUsageSummary,
  localeCode: LocaleCode,
  onNavigate?: NavigateFn,
) {
  const summary = usage.lastExecutionSummary
  const stateCounts = summary?.stateCounts ?? {}
  const statusCounts = summary?.statusCounts ?? {}
  const items = summary?.items ?? []
  const latest = summary?.latest
  const hasRuntimeEvidence =
    items.length > 0 || Object.values(stateCounts).some((count) => count > 0)
  if (!summary || (!hasRuntimeEvidence && !summary.note)) return null
  const latestPath = latest ? runtimeLinkForItem(latest) : ''
  return (
    <div className="soha-template-usage-impact__runtime">
      <Space wrap size={[8, 8]}>
        <Tag color={runtimeStateColor('succeeded')}>
          {localeCode === 'en_US'
            ? `Succeeded ${stateCounts.succeeded ?? 0}`
            : `成功 ${stateCounts.succeeded ?? 0}`}
        </Tag>
        <Tag color={runtimeStateColor('failed')}>
          {localeCode === 'en_US'
            ? `Failed ${stateCounts.failed ?? 0}`
            : `失败 ${stateCounts.failed ?? 0}`}
        </Tag>
        <Tag color={runtimeStateColor('running')}>
          {localeCode === 'en_US'
            ? `Running ${stateCounts.running ?? 0}`
            : `运行中 ${stateCounts.running ?? 0}`}
        </Tag>
        <Tag color={runtimeStateColor('pending')}>
          {localeCode === 'en_US'
            ? `Pending ${stateCounts.pending ?? 0}`
            : `等待 ${stateCounts.pending ?? 0}`}
        </Tag>
        {Object.entries(statusCounts)
          .slice(0, 4)
          .map(([status, count]) => (
            <Tag key={status}>
              {status} {count}
            </Tag>
          ))}
      </Space>
      {latest ? (
        <Text className="soha-template-usage-impact__line">
          {localeCode === 'en_US' ? 'Latest evidence: ' : '最近证据：'}
          {runtimeItemLabel(latest, localeCode)}
          {latest.status ? ` / ${latest.status}` : ''}
          {latest.observedAt ? ` / ${latest.observedAt}` : ''}
          {onNavigate && latestPath ? (
            <Button
              className="soha-template-usage-impact__inline-action"
              icon={<LinkOutlined />}
              size="small"
              type="link"
              onClick={() => onNavigate(latestPath)}
            >
              {localeCode === 'en_US' ? 'Open' : '打开'}
            </Button>
          ) : null}
        </Text>
      ) : null}
      {items.length > 0 ? (
        <Space wrap size={[6, 6]}>
          {items.slice(0, 5).map((item) => {
            const path = runtimeLinkForItem(item)
            return (
              <Button
                key={`${item.kind}:${item.id}`}
                disabled={!onNavigate || !path}
                icon={<LinkOutlined />}
                size="small"
                onClick={() => path && onNavigate?.(path)}
              >
                {runtimeItemLabel(item, localeCode)}
              </Button>
            )
          })}
          {items.length > 5 ? <Tag>+{items.length - 5}</Tag> : null}
        </Space>
      ) : summary.note ? (
        <Text type="secondary" className="soha-template-usage-impact__line">
          {summary.note}
        </Text>
      ) : null}
    </div>
  )
}

function staticJumpButtons(
  usage: TemplateUsageSummary,
  localeCode: LocaleCode,
  onNavigate?: NavigateFn,
) {
  if (!onNavigate) return null
  const applications = uniqueApplications(usage)
  const bindings = usage.bindings ?? []
  const buildSources = usage.buildSources ?? []
  const buttons: Array<{ key: string; label: string; path: string }> = []
  for (const app of applications.slice(0, 3)) {
    if (!app.id) continue
    buttons.push({
      key: `app:${app.id}`,
      label: compactApplicationLabel(app),
      path: `/applications/${encodeURIComponent(app.id)}`,
    })
  }
  for (const binding of bindings.slice(0, 3)) {
    const applicationId = binding.applicationId || binding.application?.id
    if (!binding.id || !applicationId) continue
    buttons.push({
      key: `binding:${binding.id}`,
      label: bindingLabel(binding),
      path: `/applications/${encodeURIComponent(applicationId)}?tab=services&applicationEnvironmentId=${encodeURIComponent(binding.id)}`,
    })
  }
  for (const source of buildSources.slice(0, 3)) {
    if (!source.applicationId) continue
    buttons.push({
      key: `source:${source.applicationId}:${source.buildSourceId}`,
      label: buildSourceLabel(source),
      path: `/applications/${encodeURIComponent(source.applicationId)}`,
    })
  }
  if (usage.templateId) {
    const metadataKey = 'usageSnapshot.templateId'
    const metadataValue = encodeURIComponent(usage.templateId)
    buttons.push({
      key: `audit:${usage.templateId}`,
      label: localeCode === 'en_US' ? 'Audit usage snapshot' : '审计快照',
      path: `/system/audit?metadataKey=${encodeURIComponent(metadataKey)}&metadataValue=${metadataValue}&usageTemplateKind=${encodeURIComponent(usage.templateKind)}&usageTemplateId=${metadataValue}`,
    })
    buttons.push({
      key: `operation:${usage.templateId}`,
      label: localeCode === 'en_US' ? 'Operation usage snapshot' : '操作快照',
      path: `/system/operations?metadataKey=${encodeURIComponent(metadataKey)}&metadataValue=${metadataValue}&usageTemplateKind=${encodeURIComponent(usage.templateKind)}&usageTemplateId=${metadataValue}`,
    })
  }
  if (buttons.length === 0) return null
  return (
    <Space wrap size={[6, 6]}>
      <Text type="secondary">{localeCode === 'en_US' ? 'Jump: ' : '跳转：'}</Text>
      {buttons.slice(0, 6).map((item) => (
        <Button
          key={item.key}
          icon={<LinkOutlined />}
          size="small"
          onClick={() => onNavigate(item.path)}
        >
          {item.label}
        </Button>
      ))}
      {buttons.length > 6 ? <Tag>+{buttons.length - 6}</Tag> : null}
    </Space>
  )
}

function usageDescription(
  usage: TemplateUsageSummary,
  localeCode: LocaleCode,
  onNavigate?: NavigateFn,
) {
  const applications = uniqueApplications(usage)
  const bindings = usage.bindings ?? []
  const buildSources = usage.buildSources ?? []
  const fileKinds = fileKindLabels(usage)
  return (
    <div className="soha-template-usage-impact">
      <Space wrap size={[8, 8]}>
        <Tag color={riskColor(usage.riskLevel)}>{riskText(usage.riskLevel, localeCode)}</Tag>
        <Tag>
          {localeCode === 'en_US' ? `Usage ${usage.usageCount}` : `使用 ${usage.usageCount}`}
        </Tag>
        <Tag>
          {localeCode === 'en_US'
            ? `Apps ${usage.applicationCount}`
            : `应用 ${usage.applicationCount}`}
        </Tag>
        <Tag>
          {localeCode === 'en_US'
            ? `Envs ${usage.environmentCount}`
            : `环境 ${usage.environmentCount}`}
        </Tag>
        <Tag color={usage.productionEnvironmentCount > 0 ? 'red' : 'default'}>
          {localeCode === 'en_US'
            ? `Prod ${usage.productionEnvironmentCount}`
            : `生产 ${usage.productionEnvironmentCount}`}
        </Tag>
        <Tag color={usage.approvalBindingCount > 0 ? 'gold' : 'default'}>
          {localeCode === 'en_US'
            ? `Approvals ${usage.approvalBindingCount}`
            : `审批 ${usage.approvalBindingCount}`}
        </Tag>
        <Tag>
          {localeCode === 'en_US' ? `Targets ${usage.targetCount}` : `目标 ${usage.targetCount}`}
        </Tag>
      </Space>
      {usage.riskReasons?.length ? (
        <Text type="secondary" className="soha-template-usage-impact__line">
          {usage.riskReasons.join(' / ')}
        </Text>
      ) : null}
      {applications.length > 0 ? (
        <Text className="soha-template-usage-impact__line">
          {localeCode === 'en_US' ? 'Applications: ' : '影响应用：'}
          {applications.slice(0, 5).map(compactApplicationLabel).join(' / ')}
          {applications.length > 5 ? ` +${applications.length - 5}` : ''}
        </Text>
      ) : null}
      {bindings.length > 0 ? (
        <Text className="soha-template-usage-impact__line">
          {localeCode === 'en_US' ? 'Bindings: ' : '环境绑定：'}
          {bindings.slice(0, 5).map(bindingLabel).join(' / ')}
          {bindings.length > 5 ? ` +${bindings.length - 5}` : ''}
        </Text>
      ) : null}
      {buildSources.length > 0 ? (
        <Text className="soha-template-usage-impact__line">
          {localeCode === 'en_US' ? 'Build sources: ' : '构建源：'}
          {buildSources.slice(0, 5).map(buildSourceLabel).join(' / ')}
          {buildSources.length > 5 ? ` +${buildSources.length - 5}` : ''}
        </Text>
      ) : null}
      {fileKinds.length > 0 ? (
        <Text className="soha-template-usage-impact__line">
          {localeCode === 'en_US' ? 'Spec files: ' : '规范文件：'}
          {fileKinds.join(' / ')}
        </Text>
      ) : null}
      {staticJumpButtons(usage, localeCode, onNavigate)}
      {runtimeEvidence(usage, localeCode, onNavigate)}
    </div>
  )
}

export function TemplateUsageImpactPanel({
  usage,
  loading,
  localeCode = 'zh_CN',
  onNavigate,
}: TemplateUsageImpactPanelProps) {
  if (loading) {
    return (
      <Alert
        showIcon
        title={localeCode === 'en_US' ? 'Loading template impact' : '正在加载模板影响面'}
        type="info"
      />
    )
  }
  if (!usage) return null
  return (
    <Alert
      showIcon
      title={usageTitle(usage, localeCode)}
      description={usageDescription(usage, localeCode, onNavigate)}
      type={alertType(usage.riskLevel)}
    />
  )
}

export function shouldConfirmTemplateUsageSave(usage?: TemplateUsageSummary) {
  return usage?.riskLevel === 'high' && usage.usageCount > 0
}

export function templateUsageConfirmText(
  templateName: string,
  usage?: TemplateUsageSummary,
  localeCode: LocaleCode = 'zh_CN',
) {
  if (!usage) {
    return localeCode === 'en_US'
      ? `${templateName} has no usage summary yet. Continue saving this template directly?`
      : `${templateName} 还没有可用的影响面摘要。确认继续直接保存该模板？`
  }
  if (localeCode === 'en_US') {
    return [
      `${templateName} is high risk and affects ${usage.applicationCount} applications, ${usage.environmentCount} environments, and ${usage.targetCount} targets.`,
      'Copy the template before editing when you need to isolate production bindings.',
      'Continue saving this template directly?',
    ].join('\n')
  }
  return [
    `${templateName} 当前为高风险模板，影响 ${usage.applicationCount} 个应用、${usage.environmentCount} 个环境、${usage.targetCount} 个发布目标。`,
    '建议复制模板后编辑，以避免直接影响线上绑定。',
    '确认继续直接保存该模板？',
  ].join('\n')
}
