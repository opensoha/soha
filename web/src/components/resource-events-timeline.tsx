import { Card, Timeline, Typography } from 'antd'
import { ManagementState } from '@/components/management-list'
import './resource-events-timeline.css'
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

function resolveTimelineColor(event: ResourceEvent) {
  const type = resolveTimelineType(event)
  switch (type) {
    case 'warning':
      return '#faad14'
    case 'error':
      return '#ff4d4f'
    case 'success':
      return '#52c41a'
    default:
      return '#1677ff'
  }
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
    <Card className="soha-detail-card" title={title} loading={loading}>
      {events.length === 0 ? (
        <ManagementState bordered={false} compact title={emptyDescription || (localeCode === 'zh_CN' ? '暂无事件' : 'No events')} />
      ) : (
        <div className="soha-events-timeline-shell">
          <Timeline
            mode="left"
            items={events.map((event) => ({
              color: resolveTimelineColor(event),
              children: (
                <div className="soha-events-timeline-item">
                  <div className="soha-events-timeline-summary">
                    <Text strong>{event.message || event.reason}</Text>
                    <Text type="secondary" className="text-xs">{formatAgeSeconds(event.ageSeconds)}</Text>
                  </div>
                  <div className="soha-events-timeline-meta">
                    <div className="soha-events-timeline-row">
                      <Text type="secondary" className="soha-events-timeline-label text-xs">{localeCode === 'zh_CN' ? '时间' : 'Time'}</Text>
                      <Text className="soha-events-timeline-value text-xs">{formatDateTime(new Date(Date.now() - event.ageSeconds * 1000).toISOString())}</Text>
                    </div>
                    {event.namespace ? (
                      <div className="soha-events-timeline-row">
                        <Text type="secondary" className="soha-events-timeline-label text-xs">{localeCode === 'zh_CN' ? '命名空间' : 'Namespace'}</Text>
                        <Text className="soha-events-timeline-value text-xs">{event.namespace}</Text>
                      </div>
                    ) : null}
                    <div className="soha-events-timeline-row">
                      <Text type="secondary" className="soha-events-timeline-label text-xs">{localeCode === 'zh_CN' ? '原因' : 'Reason'}</Text>
                      <Text className="soha-events-timeline-value text-xs">{event.reason}</Text>
                    </div>
                    <div className="soha-events-timeline-row">
                      <Text type="secondary" className="soha-events-timeline-label text-xs">{localeCode === 'zh_CN' ? '次数' : 'Count'}</Text>
                      <Text className="soha-events-timeline-value text-xs">{event.count}</Text>
                    </div>
                    {event.involvedKind || event.involvedName ? (
                      <div className="soha-events-timeline-row">
                        <Text type="secondary" className="soha-events-timeline-label text-xs">{localeCode === 'zh_CN' ? '对象' : 'Object'}</Text>
                        <Text className="soha-events-timeline-value text-xs">
                          {`${event.involvedKind || '-'} / ${event.involvedName || '-'}`}
                        </Text>
                      </div>
                    ) : null}
                  </div>
                </div>
              ),
            }))}
          />
        </div>
      )}
    </Card>
  )
}
