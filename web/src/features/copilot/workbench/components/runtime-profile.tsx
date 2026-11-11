import { MetadataTag, StatusTag } from '@/components/status-tag'
import { Button, Popover, Select, Space, Typography } from 'antd'
import type { WorkbenchToolCall } from '../types'
import {
  runtimeAssemblyLabels,
  runtimeAssemblyFromMetadata,
  runtimeHookOutcomeLabels,
  runtimeHookOutcomes,
  type WorkbenchRuntimeProfile,
  type WorkbenchRuntimeScope,
} from '../runtime-profile'

const assetKinds = { mcp: 'MCP', skill: 'Skill', hook: 'Hook' }
const assemblyTones = {
  preparing: 'pending',
  confirmed: 'success',
  degraded: 'warning',
  rejected: 'failed',
}
const unavailableReasons = new Map([
  ['asset_unavailable', '指定版本不可用'],
  ['asset_initialization_failed', '初始化失败'],
  ['dependency_unavailable', '所需依赖不可用'],
])

export function RuntimeProfileControl({
  profiles,
  scope,
  profileId,
  selected,
  disabled,
  loading,
  onChange,
}: {
  profiles?: WorkbenchRuntimeProfile[]
  scope: WorkbenchRuntimeScope
  profileId: string
  selected?: WorkbenchRuntimeProfile
  disabled: boolean
  loading: boolean
  onChange: (id: string) => void
}) {
  const defaultId = `${scope}-default`
  const allowed = (profiles ?? []).filter((profile) => profile.enabled && profile.scope === scope)
  const unavailable = Boolean(profiles && profileId && !selected)
  const options = [
    { value: '', label: scope === 'heya' ? 'Heya 默认配置' : 'Chat 默认配置' },
    ...allowed
      .filter((profile) => profile.id !== defaultId || profileId === defaultId)
      .map((profile) => ({ value: profile.id, label: `${profile.name} · v${profile.revision}` })),
    ...(profileId && !selected
      ? [
          {
            value: profileId,
            label: unavailable ? '当前配置不可用' : '运行配置待确认',
            disabled: true,
          },
        ]
      : []),
  ]
  return (
    <Space size={6} wrap>
      <Select
        aria-label="运行配置"
        size="small"
        style={{ maxWidth: 232 }}
        value={profileId}
        options={options}
        loading={loading}
        disabled={disabled}
        popupMatchSelectWidth={false}
        onChange={onChange}
      />
      {selected && (
        <Popover
          title={`${selected.name} · v${selected.revision}`}
          content={
            <Space orientation="vertical" size={4}>
              <Typography.Text type="secondary">已发布配置，下一轮提交时解析</Typography.Text>
              {selected.modelPreferences?.publicModel && (
                <span>配置默认模型 · {selected.modelPreferences.publicModel}</span>
              )}
              {selected.maxOutputTokens && (
                <span>输出上限 · {selected.maxOutputTokens} tokens</span>
              )}
              {selected.assets?.map((asset) => (
                <span key={`${asset.kind}:${asset.id}`}>
                  {assetKinds[asset.kind]} · {asset.id} @ {asset.version} ·{' '}
                  {asset.required ? '必需' : '可选'}
                </span>
              ))}
              {!selected.assets?.length && <span>暂无附加能力</span>}
            </Space>
          }
          trigger="click"
        >
          <Button size="small" type="text">
            配置详情
          </Button>
        </Popover>
      )}
      {unavailable && (
        <Typography.Text type="warning">请选择其他配置或恢复默认配置</Typography.Text>
      )}
    </Space>
  )
}

export function RuntimeAssemblySummary({
  assembly: value,
  hookOutcomes,
  toolExecutions,
}: {
  assembly?: unknown
  hookOutcomes?: unknown
  toolExecutions?: WorkbenchToolCall[]
}) {
  const assembly = runtimeAssemblyFromMetadata(value)
  const hooks = runtimeHookOutcomes(hookOutcomes)
  if (!assembly && !hooks.length) return null
  const identity = assembly
    ? `${assembly.profileId}${assembly.revision ? ` · v${assembly.revision}` : ''}`
    : ''
  return (
    <Space size={4} wrap>
      {assembly && (
        <StatusTag
          label={runtimeAssemblyLabels[assembly.status]}
          value={assemblyTones[assembly.status]}
        />
      )}
      <Popover
        title="本轮运行装配"
        trigger="click"
        content={
          <Space
            orientation="vertical"
            size={4}
            style={{ maxHeight: 'min(320px, 50vh)', overflowY: 'auto' }}
          >
            {identity && <Typography.Text>{identity}</Typography.Text>}
            {assembly?.assets.map((asset) => (
              <Space key={`${asset.kind}:${asset.id}`} size={4} wrap>
                <MetadataTag label={assetKinds[asset.kind]} />
                <span>
                  {asset.id} @ {asset.version}
                </span>
                <Typography.Text type={asset.status === 'unavailable' ? 'warning' : 'secondary'}>
                  {asset.status === 'loaded'
                    ? (asset.kind === 'skill' ? '目录已注册' : '已加载')
                    : (unavailableReasons.get(asset.code ?? '') ?? '不可用')}
                  {asset.kind === 'skill' && asset.status === 'loaded' && toolExecutions?.some(
                    (tool) => tool.adapterId === asset.id && tool.toolName === `skill_${asset.id.replace(/-/g, '_')}_load` && tool.status === 'completed',
                  ) && ' · 正文已加载'}
                </Typography.Text>
              </Space>
            ))}
            {assembly?.status === 'rejected' && (
              <span>运行配置或必需能力不可用，请检查配置后重试。</span>
            )}
            {assembly?.status === 'confirmed' && !assembly.assets.length && (
              <span>本轮无附加能力</span>
            )}
            {hooks.length > 0 && <Typography.Text strong>Hook 执行结果</Typography.Text>}
            {hooks.map((hook, index) => (
              <Typography.Text key={`${hook.id}:${hook.callId}:${index}`}>
                {hook.id} @ {hook.version} · {hook.toolName} ·{' '}
                {runtimeHookOutcomeLabels[hook.outcome]}
              </Typography.Text>
            ))}
          </Space>
        }
      >
        <Button type="text" size="small" aria-label="查看本轮装配详情">
          {identity || 'Hook 执行结果'}
          {hooks.length > 0 ? ` · ${hooks.length} 条 Hook 记录` : ''}
        </Button>
      </Popover>
    </Space>
  )
}
