import { ArrowLeftOutlined } from '@ant-design/icons'
import { Typography } from 'antd'
import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import './workload-detail-layout.css'

/** Presentation only: resource owners retain refresh, permissions and operation lifecycles. */
export function WorkloadDetailHeader({
  kind,
  name,
  namespace,
  namespaceContent,
  status,
  facts,
  actions,
  backTo,
  backLabel,
}: {
  kind: string
  name: string
  namespace?: string | null
  namespaceContent?: ReactNode
  status?: ReactNode
  facts?: ReactNode
  actions?: ReactNode
  backTo?: string
  backLabel?: string
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const location = useLocation()
  const params = new URLSearchParams(location.search)
  for (const key of ['tab', 'range', 'container']) params.delete(key)
  const collection = location.pathname.split('/').slice(0, -1).join('/')
  return (
    <header className="soha-workload-detail-header">
      <div className="soha-workload-detail-title-row">
        <div className="soha-workload-detail-title">
          <Link
            to={backTo ?? `${collection}${params.size ? `?${params}` : ''}`}
            aria-label={backLabel ?? (zh ? `返回${kind}列表` : `Back to ${kind} list`)}
          >
            <ArrowLeftOutlined />
          </Link>
          <h1>{name}</h1>
          <Typography.Text copyable={{ text: name }} />
        </div>
        <div className="soha-workload-detail-actions">{actions}</div>
      </div>
      <div className="soha-workload-detail-meta">
        {status}
        {namespace && namespace !== '—' && namespace !== '-' && (
          <MetadataTag
            tone="blue"
            title={namespace}
            className="soha-workload-detail-namespace"
            label={<span>{namespaceContent ?? namespace}</span>}
          />
        )}
        {facts}
      </div>
    </header>
  )
}
