import type { ReactNode } from 'react'
import { PlatformResourceOverview } from '@/features/platform/shared/resource-overview'
import { useI18n } from '@/i18n'
import type { AccessControlDetailBase } from './types'

export function AccessControlMetadataOverview({
  detail,
  extra,
  scopeLabel,
}: {
  detail: AccessControlDetailBase
  extra?: Array<{ key: string; value: ReactNode }>
  scopeLabel?: string
}) {
  const { t } = useI18n()
  const namespace = detail.namespace || scopeLabel
  return (
    <PlatformResourceOverview
      compact
      ageSeconds={detail.ageSeconds}
      annotations={detail.annotations}
      createdAt={detail.createdAt}
      facts={(extra ?? []).map((item) => ({ ...item, label: item.key }))}
      labels={detail.labels}
      name={detail.name}
      namespace={namespace || undefined}
      namespaceLabel={detail.namespace ? t('common.namespace', 'Namespace') : 'Scope'}
    />
  )
}
