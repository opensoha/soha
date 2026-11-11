import { Card, Timeline, Typography } from 'antd'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
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

function resolveTimelineType(
  event: ResourceEvent,
): 'default' | 'ongoing' | 'success' | 'warning' | 'error' {
  const normalizedType = (event.type || '').toLowerCase()
  const normalizedReason = (event.reason || '').toLowerCase()
  if (normalizedType === 'warning') return 'warning'
  if (
    normalizedReason.includes('failed') ||
    normalizedReason.includes('fail') ||
    normalizedReason.includes('error')
  )
    return 'error'
  if (normalizedReason.includes('success') || normalizedReason.includes('complete'))
    return 'success'
  return 'ongoing'
}

function resolveTimelineColor(event: ResourceEvent) {
  const type = resolveTimelineType(event)
  switch (type) {
    case 'warning':
      return 'var(--soha-warning)'
    case 'error':
      return 'var(--soha-danger)'
    case 'success':
      return 'var(--soha-success)'
    default:
      return 'var(--soha-primary)'
  }
}

export function ResourceEventsTimeline({
  title,
  events,
  loading,
  emptyDescription,
}: {
  title?: string
  events: ResourceEvent[]
  loading?: boolean
  emptyDescription?: string
}) {
  const { localeCode } = useI18n()

  return (
    <Card className="soha-detail-card" title={title} loading={loading}>
      {events.length === 0 ? (
        <ManagementState
          bordered={false}
          compact
          title={emptyDescription || (localeCode === 'zh_CN' ? '暂无事件' : 'No events')}
        />
      ) : (
        <div className="soha-events-timeline-shell">
          <Timeline
            mode="start"
            variant="filled"
            titleSpan="var(--soha-event-time-width)"
            classNames={{
              item: 'soha-events-timeline-entry',
              itemRail: 'soha-events-timeline-rail',
            }}
            items={events.map((event, index) => ({
              key: `${event.namespace ?? ''}/${event.name}/${index}`,
              color: resolveTimelineColor(event),
              title: (
                <div className="soha-events-timeline-time">
                  <time dateTime={new Date(Date.now() - event.ageSeconds * 1000).toISOString()}>
                    {formatDateTime(new Date(Date.now() - event.ageSeconds * 1000).toISOString())}
                  </time>
                  <Text type="secondary">{formatAgeSeconds(event.ageSeconds)}</Text>
                </div>
              ),
              content: (
                <div className={`soha-events-timeline-item is-${resolveTimelineType(event)}`}>
                  <div className="soha-events-timeline-summary">
                    <StatusTag value={event.type} />
                    <Text strong className="soha-events-timeline-reason">
                      {event.reason || '-'}
                    </Text>
                    <Text type="secondary" className="soha-events-timeline-count">
                      {event.count} {localeCode === 'zh_CN' ? '次' : 'occurrences'}
                    </Text>
                  </div>
                  <Text className="soha-events-timeline-message">
                    {event.message || event.reason}
                  </Text>
                  <div className="soha-events-timeline-meta">
                    {event.involvedKind || event.involvedName ? (
                      <Text type="secondary">
                        {event.involvedKind || '-'} / {event.involvedName || '-'}
                      </Text>
                    ) : null}
                    {event.namespace ? (
                      <Text type="secondary">
                        {localeCode === 'zh_CN' ? '命名空间' : 'Namespace'}: {event.namespace}
                      </Text>
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
