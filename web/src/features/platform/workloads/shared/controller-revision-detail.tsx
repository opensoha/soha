import { ReadFeedback } from '@/components/request-feedback'
import { YamlDraftDiffEditor } from '@/components/yaml-draft-diff-editor'
import { useI18n } from '@/i18n'
import type { RolloutHistory, ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Select, Spin, Typography } from 'antd'
import { useState } from 'react'
import { workloadKeys } from './keys'
import type { UseQueryOptions } from '@tanstack/react-query'
import './controller-revision.css'
export function ControllerRevisionDetail({
  scope,
  name,
  record,
  records,
  currentName,
  revisionOptions,
}: {
  scope: ScopeKey
  name: string
  record: RolloutHistory
  records: RolloutHistory[]
  currentName?: string
  revisionOptions: (
    scope: ScopeKey,
    name: string,
    record: RolloutHistory,
  ) => UseQueryOptions<
    { template: string },
    Error,
    { template: string },
    readonly [...ReturnType<typeof workloadKeys.detail>, 'revision', string, string]
  >
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const others = records.filter((v) => v.name !== record.name)
  const [baseName, setBaseName] = useState(
    others.find((v) => v.name === currentName)?.name || others[0]?.name || '',
  )
  const base = others.find((v) => v.name === baseName)
  const selected = useQuery(revisionOptions(scope, name, record))
  const baseOptions = revisionOptions(scope, name, base || record)
  const baseline = useQuery({ ...baseOptions, enabled: Boolean(base && baseOptions.enabled) })
  const value = readableQueryData(selected),
    other = base ? readableQueryData(baseline) : undefined
  return (
    <div className="soha-controller-revision">
      <Typography.Text>
        {record.namespace} / {record.name}
      </Typography.Text>
      <ReadFeedback canRead query={selected} label={zh ? '所选版本模板' : 'Selected revision'} />
      {base && (
        <ReadFeedback
          canRead
          query={baseline}
          label={zh ? '对比版本模板' : 'Comparison revision'}
        />
      )}
      <Select
        aria-label={zh ? '对比版本' : 'Compare revision'}
        value={baseName}
        onChange={setBaseName}
        options={[
          { value: '', label: zh ? '仅查看所选模板' : 'Selected template only' },
          ...others.map((v) => ({ value: v.name, label: `${v.revision} · ${v.name}` })),
        ]}
      />
      <Typography.Text type="secondary">
        {zh
          ? '只读 ControllerRevision 中保留的 Pod 模板；历史受集群保留策略限制。'
          : 'Read-only retained ControllerRevision Pod templates. History depends on cluster retention.'}
      </Typography.Text>
      {(selected.isLoading || (base && baseline.isLoading)) && <Spin />}
      {value && (!base || other) && (
        <YamlDraftDiffEditor
          editable={false}
          title={zh ? 'Pod 模板' : 'Pod template'}
          modified={value.template}
          original={other?.template || value.template}
          leftLabel={`${zh ? '所选版本' : 'Selected revision'} ${record.revision}`}
          rightLabel={
            base
              ? `${zh ? '对比版本' : 'Comparison revision'} ${base.revision}`
              : zh
                ? '模板差异'
                : 'Template differences'
          }
        />
      )}
    </div>
  )
}
