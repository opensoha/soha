import { Space } from 'antd'
import { StatusTag } from '@/components/status-tag'
import { localeText, useI18n } from '@/i18n'

export function NodeStateTags({
  status,
  cordoned,
  conditions,
  draining = false,
}: {
  status: string
  cordoned: boolean
  conditions?: readonly { type: string; status: string }[] | null
  draining?: boolean
}) {
  const { localeCode } = useI18n()
  const ready =
    status === 'True'
      ? 'ready'
      : status === 'False'
        ? 'not_ready'
        : status === 'Unknown'
          ? 'unknown'
          : status
  return (
    <Space size={4} wrap>
      <StatusTag value={ready} />
      {cordoned && (
        <StatusTag value="warning" label={localeText(localeCode, '禁止调度', 'Cordoned')} />
      )}
      {draining && (
        <StatusTag
          value="running"
          label={localeText(localeCode, '排水中（本次操作）', 'Draining (this operation)')}
        />
      )}
      {[
        ['MemoryPressure', '内存压力', 'Memory pressure'],
        ['DiskPressure', '磁盘压力', 'Disk pressure'],
        ['PIDPressure', 'PID 压力', 'PID pressure'],
        ['NetworkUnavailable', '网络未就绪', 'Network unavailable'],
      ].map(([type, zh, en]) => {
        const condition = conditions?.find((item) => item.type === type)
        return condition && condition.status !== 'False' ? (
          <StatusTag
            key={type}
            value={condition.status === 'True' ? 'warning' : 'unknown'}
            label={`${localeText(localeCode, zh!, en!)}${condition.status === 'True' ? '' : localeText(localeCode, '未知', ' unknown')}`}
          />
        ) : null
      })}
    </Space>
  )
}
