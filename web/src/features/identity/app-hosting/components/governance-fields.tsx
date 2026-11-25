import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { Button, Card, Divider, Form, Input, InputNumber, Select, Space, Typography } from 'antd'
import { useRef } from 'react'
import type { AppHostingSettings } from '../types'

type Governance = NonNullable<AppHostingSettings['governance']>
type QuotaOverride = Governance['quotaOverrides'][number]
type OverrideDraft = QuotaOverride & { rowKey: string }

export type GovernanceDraft = Omit<Governance, 'quotaOverrides'> & { quotaOverrides: OverrideDraft[] }

const MAX_APPS = 1000
const MAX_OVERRIDES = 100
const RUNTIME_FIELDS = [
  { key: 'cpuMilli', label: 'CPU 上限（mCPU）', min: 50, max: 4000 },
  { key: 'memoryMiB', label: '内存上限（MiB）', min: 64, max: 4096 },
  { key: 'maxReplicas', label: '最大副本数', min: 1, max: 10 },
  { key: 'scaleDownSeconds', label: '缩容等待（秒）', min: 60, max: 3600 },
] as const

export function buildGovernanceDraft(settings: AppHostingSettings): GovernanceDraft {
  const governance = settings.governance
  return {
    dataCapacityWarningMiB: governance?.dataCapacityWarningMiB ?? 200,
    maxDynamicAppsPerOwner: governance?.maxDynamicAppsPerOwner ?? Math.min(settings.maxAppsPerOwner || 20, 5),
    quotaOverrides: (governance?.quotaOverrides ?? []).map((item, index) => ({
      ...item,
      rowKey: `saved-${index}`,
    })),
    runtimeLimits: { ...(governance?.runtimeLimits ?? {
      cpuMilli: 500, memoryMiB: 512, maxReplicas: 2, scaleDownSeconds: 600,
    }) },
  }
}

function integerInRange(value: number, min: number, max: number) {
  return Number.isInteger(value) && value >= min && value <= max
}

export function governanceDraftError(draft: GovernanceDraft): string | undefined {
  if (!integerInRange(draft.maxDynamicAppsPerOwner, 1, MAX_APPS)) {
    return '每作者动态应用配额须为 1 到 1000 的整数'
  }
  for (const { key, label, min, max } of RUNTIME_FIELDS) {
    if (!integerInRange(draft.runtimeLimits[key], min, max)) return `${label}须为 ${min} 到 ${max} 的整数`
  }
  if (!integerInRange(draft.dataCapacityWarningMiB, 1, 1_048_576)) {
    return '数据库容量告警阈值须为 1 到 1048576 MiB 的整数'
  }
  if (draft.quotaOverrides.length > MAX_OVERRIDES) return '最多支持 100 条额度覆盖'

  const seen = new Set<string>()
  for (const [index, item] of draft.quotaOverrides.entries()) {
    const id = item.id.trim()
    const key = `${item.scope}:${id}`
    if (!id || new TextEncoder().encode(id).length > 128 || seen.has(key)) {
      return `第 ${index + 1} 条覆盖需要唯一且有效的部门或角色标识`
    }
    seen.add(key)
    const caps = [item.maxStaticApps, item.maxDynamicApps].filter((cap) => cap !== undefined)
    if (!caps.length || caps.some((cap) => !integerInRange(cap, 1, MAX_APPS))) {
      return `第 ${index + 1} 条覆盖至少设置一项额度，上限为 1000`
    }
  }
}

export function buildGovernancePayload({ maxDynamicAppsPerOwner, quotaOverrides, runtimeLimits, dataCapacityWarningMiB }: GovernanceDraft): Governance {
  return {
    maxDynamicAppsPerOwner,
    dataCapacityWarningMiB,
    runtimeLimits,
    quotaOverrides: quotaOverrides.map(({ id, scope, maxStaticApps, maxDynamicApps }) => ({
      id: id.trim(), scope, maxStaticApps, maxDynamicApps,
    })),
  }
}

export function GovernanceSettingsFields({
  draft,
  disabled,
  error,
  onChange,
}: {
  draft: GovernanceDraft
  disabled: boolean
  error?: string
  onChange: (next: GovernanceDraft) => void
}) {
  const nextRow = useRef(0)
  const updateRow = (rowKey: string, patch: Partial<QuotaOverride>) => {
    onChange({
      ...draft,
      quotaOverrides: draft.quotaOverrides.map((item) => item.rowKey === rowKey ? { ...item, ...patch } : item),
    })
  }

  return (
    <>
      <Divider titlePlacement="start">动态应用配额与运行规格</Divider>
      <div className="soha-identity-app-hosting-form-grid soha-identity-app-hosting-quota-grid">
        <Form.Item label="每作者动态应用配额上限">
          <InputNumber
            aria-label="每作者动态应用配额上限"
            disabled={disabled}
            min={1}
            max={MAX_APPS}
            precision={0}
            value={draft.maxDynamicAppsPerOwner}
            onChange={(value) => onChange({ ...draft, maxDynamicAppsPerOwner: value ?? 1 })}
          />
        </Form.Item>
        <Form.Item label="单库容量告警（MiB）">
          <InputNumber
            aria-label="单库容量告警（MiB）"
            disabled={disabled}
            min={1}
            max={1_048_576}
            precision={0}
            value={draft.dataCapacityWarningMiB}
            onChange={(value) => onChange({ ...draft, dataCapacityWarningMiB: value ?? 200 })}
          />
        </Form.Item>
        {RUNTIME_FIELDS.map(({ key, label, min, max }) => (
          <Form.Item key={key} label={label}>
            <InputNumber
              aria-label={label}
              disabled={disabled}
              min={min}
              max={max}
              precision={0}
              value={draft.runtimeLimits[key]}
              onChange={(value) => onChange({
                ...draft, runtimeLimits: { ...draft.runtimeLimits, [key]: value ?? min },
              })}
            />
          </Form.Item>
        ))}
      </div>
      <Divider titlePlacement="start">部门和角色配额覆盖</Divider>
      <Space orientation="vertical" style={{ width: '100%' }}>
        <Typography.Text type="secondary">
          同一类别的应用额度命中多条覆盖时取最小值。该类别未命中时沿用默认额度。
        </Typography.Text>
        {draft.quotaOverrides.map(({ rowKey, scope, id, maxStaticApps, maxDynamicApps }, index) => (
          <Card
            key={rowKey}
            size="small"
            extra={
              <Button
                aria-label={`移除第 ${index + 1} 条覆盖`}
                disabled={disabled}
                icon={<DeleteOutlined />}
                type="text"
                onClick={() => onChange({
                  ...draft, quotaOverrides: draft.quotaOverrides.filter((item) => item.rowKey !== rowKey),
                })}
              />
            }
          >
            <div className="soha-identity-app-hosting-form-grid">
              <Form.Item label="范围">
                <Select
                  aria-label={`第 ${index + 1} 条覆盖范围`}
                  disabled={disabled}
                  options={[{ label: '部门', value: 'team' }, { label: '角色', value: 'role' }]}
                  value={scope}
                  onChange={(value) => updateRow(rowKey, { scope: value })}
                />
              </Form.Item>
              <Form.Item label="部门或角色 ID">
                <Input
                  aria-label={`第 ${index + 1} 条覆盖目标 ID`}
                  disabled={disabled}
                  value={id}
                  maxLength={128}
                  onChange={(event) => updateRow(rowKey, { id: event.target.value })}
                />
              </Form.Item>
              {([
                ['maxStaticApps', '静态应用上限', maxStaticApps],
                ['maxDynamicApps', '动态应用上限', maxDynamicApps],
              ] as const).map(([key, label, value]) => (
                <Form.Item key={key} label={label}>
                  <InputNumber
                    aria-label={`第 ${index + 1} 条覆盖${label}`}
                    disabled={disabled}
                    min={1}
                    max={MAX_APPS}
                    precision={0}
                    value={value}
                    onChange={(next) => updateRow(rowKey, { [key]: next ?? undefined })}
                  />
                </Form.Item>
              ))}
            </div>
          </Card>
        ))}
        <Button
          disabled={disabled || draft.quotaOverrides.length >= MAX_OVERRIDES}
          icon={<PlusOutlined />}
          onClick={() => onChange({
            ...draft,
            quotaOverrides: [...draft.quotaOverrides, {
              id: '', scope: 'team', rowKey: `new-${nextRow.current++}`,
            }],
          })}
        >
          添加覆盖
        </Button>
        {error ? <Typography.Text type="danger" role="alert">{error}</Typography.Text> : null}
      </Space>
    </>
  )
}
