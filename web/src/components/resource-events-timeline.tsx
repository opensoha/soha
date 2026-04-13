import { Card, Empty, Timeline, Typography } from '@douyinfe/semi-ui'
import { useI18n } from '@/i18n'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'

const { Text } = Typography

interface ResourceEvent {
  name: string
  namespace?: string
  type: string
  reason: string
  involvedKind?: string
  involvedName?: string
  message: string
  count: number
  ageSeconds: number
}

function resolveTimelineType(event: ResourceEvent): 'default' | 'ongoing' | 'success' | 'warning' | 'error' {
  const normalizedType = (event.type || '').toLowerCase()
  const normalizedReason = (event.reason || '').toLowerCase()
  if (normalizedType === 'warning') return 'warning'
  if (normalizedReason.includes('failed') || normalizedReason.includes('fail') || normalizedReason.includes('error')) return 'error'
  if (normalizedReason.includes('success') || normalizedReason.includes('complete')) return 'success'
  return 'ongoing'
}

export function ResourceEventsTimeline({
  title,
  events,
  loading,
  emptyDescription,
}: {
  title: string
  events: ResourceEvent[]
  loading?: boolean
  emptyDescription?: string
}) {
  const { localeCode } = useI18n()

  return (
    <Card className="kc-detail-card" title={title} loading={loading}>
      {events.length === 0 ? (
        <Empty description={emptyDescription || (localeCode === 'zh_CN' ? '暂无事件' : 'No events')} />
      ) : (
        <Timeline aria-label={title} mode="left">
          {events.map((event) => (
            <Timeline.Item
              key={`${event.name}:${event.reason}:${event.ageSeconds}`}
              time={formatAgeSeconds(event.ageSeconds)}
              type={resolveTimelineType(event)}
              extra={(
                <div className="flex flex-col gap-1">
                  <Text type="tertiary" size="small">{`${localeCode === 'zh_CN' ? '原因' : 'Reason'}: ${event.reason}`}</Text>
                  <Text type="tertiary" size="small">{`${localeCode === 'zh_CN' ? '次数' : 'Count'}: ${event.count}`}</Text>
                  {event.involvedKind || event.involvedName ? (
                    <Text type="tertiary" size="small">
                      {`${localeCode === 'zh_CN' ? '对象' : 'Object'}: ${event.involvedKind || '-'} / ${event.involvedName || '-'}`}
                    </Text>
                  ) : null}
                </div>
              )}
            >
              <div className="flex flex-col gap-1">
                <Text strong>{event.message || event.reason}</Text>
                <Text type="tertiary" size="small">
                  {event.namespace
                    ? `${localeCode === 'zh_CN' ? '命名空间' : 'Namespace'}: ${event.namespace}`
                    : `${localeCode === 'zh_CN' ? '时间' : 'Time'}: ${formatDateTime(new Date(Date.now() - event.ageSeconds * 1000).toISOString())}`}
                </Text>
              </div>
            </Timeline.Item>
          ))}
        </Timeline>
      )}
    </Card>
  )
}
