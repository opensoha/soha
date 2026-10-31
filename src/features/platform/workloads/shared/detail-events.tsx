import { ManagementState } from '@/components/management-list'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { ReloadOutlined, SearchOutlined, WarningOutlined } from '@ant-design/icons'
import {
  Button,
  Card,
  Input,
  Segmented,
  Select,
  Space,
  Tag,
  Timeline,
  Tooltip,
  Typography,
} from 'antd'
import { useMemo, useState } from 'react'
import type { WorkloadEvent } from './types'
import './detail-observability.css'

export function workloadEventTimestamp(event: WorkloadEvent, observedAt: number) {
  const explicit = Date.parse(event.lastTimestamp || '')
  if (Number.isFinite(explicit)) return explicit
  return Number.isFinite(event.ageSeconds) && event.ageSeconds >= 0
    ? observedAt - event.ageSeconds * 1000
    : null
}

export function WorkloadDetailEvents({
  events,
  observedAt,
  loading,
  error,
  onRefresh,
  resourceLabel = 'Pod',
}: {
  events: WorkloadEvent[]
  observedAt: number
  loading: boolean
  error?: string
  onRefresh: () => void
  resourceLabel?: string
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const [filter, setFilter] = useState('all')
  const [search, setSearch] = useState('')
  const [order, setOrder] = useState('desc')
  const rows = useMemo(
    () =>
      events
        .map((event) => ({ event, timestamp: workloadEventTimestamp(event, observedAt) }))
        .filter(
          ({ event }) =>
            (filter === 'all' || event.type.toLowerCase() === filter) &&
            `${event.reason} ${event.message} ${event.involvedName || ''}`
              .toLowerCase()
              .includes(search.trim().toLowerCase()),
        )
        .sort((a, b) => ((a.timestamp ?? 0) - (b.timestamp ?? 0)) * (order === 'asc' ? 1 : -1)),
    [events, observedAt, filter, search, order],
  )
  const day = (timestamp: number | null) =>
    timestamp === null
      ? zh
        ? '时间未知'
        : 'Unknown time'
      : new Date(timestamp).toLocaleDateString(localeCode === 'zh_CN' ? 'zh-CN' : 'en-US')
  const days = [...new Set(rows.map((row) => day(row.timestamp)))]
  return (
    <Card
      className="soha-pod-detail-card soha-pod-detail-events"
      title={zh ? '事件时间轴' : 'Event timeline'}
      loading={loading}
      extra={
        <Space wrap>
          <Typography.Text type="secondary">
            {loading || error ? '—' : events.length} {zh ? '条返回记录' : 'returned records'}
          </Typography.Text>
          <Button
            icon={<ReloadOutlined />}
            aria-label={zh ? `刷新 ${resourceLabel} 事件` : `Refresh ${resourceLabel} events`}
            loading={loading}
            onClick={onRefresh}
          >
            {zh ? '刷新' : 'Refresh'}
          </Button>
        </Space>
      }
    >
      <div className="soha-pod-detail-event-toolbar">
        <Segmented
          aria-label={zh ? '事件类型' : 'Event type'}
          value={filter}
          onChange={(value) => setFilter(String(value))}
          options={[
            { value: 'all', label: zh ? '全部' : 'All' },
            { value: 'warning', label: 'Warning' },
            { value: 'normal', label: 'Normal' },
          ]}
        />
        <Input
          aria-label={zh ? '搜索事件' : 'Search events'}
          prefix={<SearchOutlined />}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          allowClear
          placeholder={zh ? '搜索原因、消息或对象' : 'Search reason, message or object'}
        />
        <Select
          aria-label={zh ? '事件排序' : 'Event order'}
          value={order}
          onChange={setOrder}
          options={[
            { value: 'desc', label: zh ? '最新在前' : 'Newest first' },
            { value: 'asc', label: zh ? '最早在前' : 'Oldest first' },
          ]}
        />
      </div>
      {error ? (
        <ManagementState
          compact
          kind="error"
          title={error}
          actions={<Button onClick={onRefresh}>{zh ? '重试' : 'Retry'}</Button>}
        />
      ) : rows.length === 0 ? (
        <ManagementState
          compact
          title={
            zh
              ? `当前返回记录中没有匹配的 ${resourceLabel} 事件`
              : `No matching ${resourceLabel} events in the returned records`
          }
        />
      ) : (
        <div className="soha-pod-detail-timeline">
          {days.map((date) => (
            <section key={date}>
              <h3>{date}</h3>
              <Timeline
                titleSpan="88px"
                classNames={{ item: 'soha-pod-detail-timeline-item' }}
                items={rows
                  .filter((row) => day(row.timestamp) === date)
                  .map(({ event, timestamp }) => ({
                    key: `${event.namespace}/${event.name}`,
                    color:
                      event.type.toLowerCase() === 'warning'
                        ? 'var(--soha-warning)'
                        : 'var(--soha-primary)',
                    icon: event.type.toLowerCase() === 'warning' ? <WarningOutlined /> : undefined,
                    title: (
                      <time
                        dateTime={
                          timestamp === null ? undefined : new Date(timestamp).toISOString()
                        }
                      >
                        {timestamp === null
                          ? '—'
                          : new Date(timestamp).toLocaleTimeString(
                              localeCode === 'zh_CN' ? 'zh-CN' : 'en-US',
                              { hour12: false },
                            )}
                      </time>
                    ),
                    content: (
                      <article>
                        <div className="soha-pod-detail-event-heading">
                          <strong>{event.reason || '—'}</strong>
                          <Tag color={event.type.toLowerCase() === 'warning' ? 'orange' : 'blue'}>
                            {event.type}
                          </Tag>
                          <Tooltip
                            title={
                              zh
                                ? '同一条事件记录的累计次数；接口未提供首次发生时间'
                                : 'Occurrences of this event record; first occurrence time was not provided'
                            }
                          >
                            <span>
                              {event.count} {zh ? '次' : 'occurrences'}
                            </span>
                          </Tooltip>
                        </div>
                        <p>{event.message || event.reason}</p>
                        <Typography.Text type="secondary">
                          {event.involvedKind} {event.involvedName}
                          {timestamp === null
                            ? ''
                            : ` · ${zh ? '最近' : 'Last'} ${formatDateTime(new Date(timestamp).toISOString())}`}
                        </Typography.Text>
                      </article>
                    ),
                  }))}
              />
            </section>
          ))}
        </div>
      )}
    </Card>
  )
}
