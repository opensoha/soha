import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons'
import { useIsFetching, useQueryClient, type QueryKey } from '@tanstack/react-query'
import { Button, Tabs, Typography, type TabsProps } from 'antd'
import type { ReactNode } from 'react'
import { Link, useLocation, useSearchParams } from 'react-router-dom'
import { formatAgeSeconds, formatRelativeTime } from '@/utils/time'
import { resourceCollectionPath } from './resource-collection-path'
import './resource-detail.css'

/** Resource identity and navigation only; owners supply facts, query keys and permitted actions. */
export function ResourceDetailHeader({
  kind,
  name,
  namespace,
  status,
  facts,
  actions,
  queryKey,
  onRefresh,
  refreshing,
  createdAt,
  ageSeconds,
  backTo,
  hideBack = false,
}: {
  kind: string
  name: string
  namespace?: string | null
  status?: string | null
  facts?: ReactNode
  actions?: ReactNode
  queryKey?: QueryKey
  onRefresh?: () => unknown
  refreshing?: boolean
  createdAt?: string
  ageSeconds?: number
  backTo?: string
  hideBack?: boolean
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const location = useLocation()
  const client = useQueryClient()
  const fetching = useIsFetching({
    queryKey: queryKey ?? ['platform', 'detail-header', 'inactive'],
  })
  const { namespace: listNamespace } = usePlatformScopeStore()
  const collection = backTo ?? location.pathname.split('/').slice(0, -1).join('/')
  const href = resourceCollectionPath(
    collection,
    location.search,
    namespace && namespace !== '-' && namespace !== '—' ? listNamespace : null,
  )
  return (
    <header className="soha-k8s-detail-header">
      <div className="soha-k8s-detail-title-row">
        <div className="soha-k8s-detail-title">
          {!hideBack && (
            <Link to={href} aria-label={zh ? `返回${kind}列表` : `Back to ${kind} list`}>
              <ArrowLeftOutlined />
            </Link>
          )}
          <h1>{name}</h1>
          <Typography.Text copyable={{ text: name }} />
        </div>
        <div className="soha-k8s-detail-actions">
          {actions}
          {(queryKey || onRefresh) && (
            <Button
              icon={<ReloadOutlined />}
              aria-label={zh ? `刷新 ${kind}` : `Refresh ${kind}`}
              loading={refreshing ?? Boolean(queryKey && fetching)}
              onClick={() => {
                if (onRefresh) void onRefresh()
                else if (queryKey) void client.invalidateQueries({ queryKey })
              }}
            >
              {zh ? '刷新' : 'Refresh'}
            </Button>
          )}
        </div>
      </div>
      <div className="soha-k8s-detail-meta">
        {status && <StatusTag value={status} />}
        <span>{kind}</span>
        {namespace && namespace !== '-' && <MetadataTag tone="blue" label={namespace} />}
        {facts}
        {(createdAt || ageSeconds != null) && (
          <Typography.Text type="secondary">
            {createdAt ? formatRelativeTime(createdAt) : formatAgeSeconds(ageSeconds)}
          </Typography.Text>
        )}
      </div>
    </header>
  )
}

/** The URL carries the visible leaf; feature query owners still decide when data is read. */
export function useResourceDetailTab(initial = 'overview') {
  const [params, setParams] = useSearchParams()
  const active = params.get('tab') || initial
  const change = (key: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous)
        next.set('tab', key)
        return next
      },
      { replace: true },
    )
  return [active, change] as const
}

/** A stale or unavailable leaf must not leave an otherwise readable detail page blank. */
export function ResourceDetailTabs({ activeKey, items, className, ...props }: TabsProps) {
  const visibleKey = items?.some((item) => item.key === activeKey && !item.disabled)
    ? activeKey
    : items?.find((item) => !item.disabled)?.key
  return (
    <Tabs
      size="small"
      tabBarGutter={32}
      indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
      {...props}
      className={`soha-resource-tabs soha-workload-detail-tabs ${className ?? ''}`}
      items={items}
      activeKey={visibleKey}
    />
  )
}
