import type { ReactNode } from 'react'
import { Card, Collapse, Descriptions, Tooltip, Typography } from 'antd'
import { useI18n } from '@/i18n'
import { formatAgeSeconds, formatRelativeTime } from '@/utils/time'

const { Text } = Typography

export interface PlatformResourceOverviewFact {
  readonly key: string
  readonly label: ReactNode
  readonly value: ReactNode
}

function ResourceMetadataSection({
  items,
  title,
}: {
  items?: Record<string, string>
  title: ReactNode
}) {
  const entries = Object.entries(items ?? {}).filter(([key]) => key.trim())
  if (entries.length === 0) return null

  return (
    <div className="soha-workload-metadata-section">
      <Text strong className="soha-workload-metadata-title">
        {title}
      </Text>
      <div className="soha-workload-kv-grid">
        {entries.map(([key, value]) => {
          const displayValue = value || '-'
          return (
            <Tooltip
              key={key}
              classNames={{ root: 'soha-workload-kv-tooltip' }}
              placement="topLeft"
              title={`${key}: ${displayValue}`}
            >
              <div className="soha-workload-kv-item">
                <span className="soha-workload-kv-key">{`${key}:`}</span>
                <span className="soha-workload-kv-value">{displayValue}</span>
              </div>
            </Tooltip>
          )
        })}
      </div>
    </div>
  )
}

export function PlatformResourceOverview({
  ageSeconds,
  annotations,
  createdAt,
  createdAtLabel,
  facts = [],
  labels,
  name,
  namespace,
  namespaceLabel,
  compact = false,
}: {
  readonly ageSeconds?: number
  readonly annotations?: Record<string, string>
  readonly createdAt?: string
  readonly createdAtLabel?: ReactNode
  readonly facts?: readonly PlatformResourceOverviewFact[]
  readonly labels?: Record<string, string>
  readonly name: ReactNode
  readonly namespace?: ReactNode
  readonly namespaceLabel?: ReactNode
  readonly compact?: boolean
}) {
  const { t, localeCode } = useI18n()
  const hasMetadata = Boolean(
    (labels && Object.keys(labels).length > 0) ||
    (annotations && Object.keys(annotations).length > 0),
  )

  if (compact && !hasMetadata && !facts.length) return null
  return (
    <Card className="soha-detail-card">
      {(!compact || facts.length > 0) && (
        <Descriptions
          column={{ xs: 1, sm: 2, md: 3 }}
          size="small"
          items={[
            ...(!compact ? [{ key: 'name', label: t('common.name', 'Name'), children: name }] : []),
            ...(namespace === undefined || compact
              ? []
              : [
                  {
                    key: 'namespace',
                    label: namespaceLabel ?? t('common.namespace', 'Namespace'),
                    children: namespace,
                  },
                ]),
            ...(!compact
              ? [
                  {
                    key: 'createdAt',
                    label: createdAtLabel ?? t('common.createdAt', 'Created At'),
                    children: createdAt
                      ? formatRelativeTime(createdAt)
                      : formatAgeSeconds(ageSeconds),
                  },
                ]
              : []),
            ...facts.map((fact) => ({
              key: fact.key,
              label: fact.label,
              children: fact.value,
            })),
          ]}
        />
      )}
      {hasMetadata ? (
        compact ? (
          <Collapse
            ghost
            className="soha-k8s-detail-metadata"
            items={[
              {
                key: 'labels',
                label: `${t('common.labels', 'Labels')} · ${Object.keys(labels || {}).length}`,
                children: (
                  <ResourceMetadataSection items={labels} title={t('common.labels', 'Labels')} />
                ),
              },
              {
                key: 'annotations',
                label: `${localeCode === 'zh_CN' ? '注解' : 'Annotations'} · ${Object.keys(annotations || {}).length}`,
                children: (
                  <ResourceMetadataSection
                    items={annotations}
                    title={localeCode === 'zh_CN' ? '注解' : 'Annotations'}
                  />
                ),
              },
            ]}
          />
        ) : (
          <div className="soha-workload-metadata-stack">
            <ResourceMetadataSection items={labels} title={t('common.labels', 'Labels')} />
            <ResourceMetadataSection
              items={annotations}
              title={localeCode === 'zh_CN' ? '注解' : 'Annotations'}
            />
          </div>
        )
      ) : null}
    </Card>
  )
}
