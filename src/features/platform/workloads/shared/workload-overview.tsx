import type { ReactNode } from 'react'
import { Collapse } from 'antd'
import { useI18n } from '@/i18n'
import './workload-overview.css'

/** Shared reading layout; each workload retains ownership of its facts and actions. */
export function WorkloadOverview({
  children,
  aside,
  className = '',
}: {
  children: ReactNode
  aside: ReactNode
  className?: string
}) {
  return (
    <div className={`soha-workload-overview ${className}`}>
      <div className="soha-workload-overview-stack">{children}</div>
      <aside className="soha-workload-overview-stack">{aside}</aside>
    </div>
  )
}

export interface WorkloadOverviewFact {
  key: string
  label: string
  value?: number
  target?: number
  showTarget?: boolean
  tone?: 'primary' | 'success' | 'warning'
}

export function WorkloadOverviewFacts({ facts }: { facts: WorkloadOverviewFact[] }) {
  return (
    <dl className="soha-workload-overview-facts">
      {facts.map(({ key, label, value, target, showTarget, tone = 'success' }) => (
        <div key={key}>
          <dt>{label}</dt>
          <dd>
            <strong>
              {value ?? '—'}
              {showTarget && target !== undefined && <small>{` / ${target}`}</small>}
            </strong>
            {target !== undefined && (
              <div className="soha-workload-overview-track" aria-hidden="true">
                <i
                  style={{
                    width: `${value !== undefined && target > 0 ? Math.max(0, Math.min(100, (value / target) * 100)) : 0}%`,
                    background: `var(--soha-${tone})`,
                  }}
                />
              </div>
            )}
          </dd>
        </div>
      ))}
    </dl>
  )
}

export function WorkloadOverviewConfiguration({
  items,
}: {
  items: { label: string; children: ReactNode }[]
}) {
  return (
    <dl className="soha-workload-overview-config">
      {items.map(({ label, children }) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{children}</dd>
        </div>
      ))}
    </dl>
  )
}

export function WorkloadOverviewMetadata({
  labels,
  annotations,
}: {
  labels?: Record<string, string>
  annotations?: Record<string, string>
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  return (
    <Collapse
      className="soha-k8s-detail-metadata"
      items={[
        { key: 'labels', label: zh ? '标签' : 'Labels', entries: labels },
        { key: 'annotations', label: zh ? '注解' : 'Annotations', entries: annotations },
      ].map(({ key, label, entries }) => ({
        key,
        label: `${label} · ${Object.keys(entries || {}).length}`,
        children: (
          <dl className="soha-workload-overview-metadata">
            {Object.entries(entries || {}).map(([name, value]) => (
              <div key={name}>
                <dt>{name}:</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
        ),
      }))}
    />
  )
}
