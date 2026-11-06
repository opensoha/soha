import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { YamlDraftDiffEditor } from '@/components/yaml-draft-diff-editor'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import type { RolloutHistory, ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Button, Select, Spin, Typography } from 'antd'
import { useState } from 'react'
import { deploymentQueries } from './queries'

export function DeploymentDetailRevision({
  record,
  records,
  currentRevision,
  scope,
}: {
  record: RolloutHistory
  records: RolloutHistory[]
  currentRevision?: string
  scope: ScopeKey
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const allowed = hasPermission(permissions, 'platform.workloads.replica-sets.view')
  const others = records.filter((item) => item.name !== record.name)
  const [baseName, setBaseName] = useState(
    () => others.find((item) => item.revision === currentRevision)?.name || others[0]?.name || '',
  )
  const base = others.find((item) => item.name === baseName)
  const selectedOptions = deploymentQueries.revisionTemplate(scope, record)
  const baselineOptions = deploymentQueries.revisionTemplate(scope, base || record)
  const selected = useQuery({
    ...selectedOptions,
    enabled: Boolean(allowed && selectedOptions.enabled),
  })
  const baseline = useQuery({
    ...baselineOptions,
    enabled: Boolean(allowed && base && baselineOptions.enabled),
  })
  const selectedData = allowed && selectedOptions.enabled ? readableQueryData(selected) : undefined
  const baselineData =
    allowed && base && baselineOptions.enabled ? readableQueryData(baseline) : undefined
  if (!allowed)
    return (
      <ManagementState
        compact
        kind="no-permission"
        title={
          zh
            ? '需要 ReplicaSet 查看权限才能读取历史模板'
            : 'ReplicaSet view permission is required to read revision templates'
        }
      />
    )
  return (
    <div className="soha-deployment-stack">
      <Typography.Text>
        {record.namespace} / {record.name}
      </Typography.Text>
      <ReadFeedback
        canRead
        query={selected}
        label={zh ? '所选版本模板' : 'Selected revision template'}
      />
      {base && (
        <ReadFeedback
          canRead
          query={baseline}
          label={zh ? '对比版本模板' : 'Comparison template'}
        />
      )}
      {selected.isLoading && <Spin />}
      {selectedData && (
        <>
          <Typography.Paragraph>
            <strong>{zh ? '变更说明' : 'Change cause'}：</strong>
            {selectedData.changeCause ||
              (zh ? '此修订未记录变更说明' : 'No change cause recorded for this revision')}
          </Typography.Paragraph>
          <div className="soha-deployment-toolbar">
            <label htmlFor="deployment-revision-base">{zh ? '对比版本' : 'Compare with'}</label>
            <Select
              id="deployment-revision-base"
              aria-label={zh ? '对比版本' : 'Compare with'}
              value={baseName}
              onChange={setBaseName}
              options={[
                { value: '', label: zh ? '仅查看所选模板' : 'Selected template only' },
                ...others.map((item) => ({
                  value: item.name,
                  label: `${zh ? '版本' : 'Revision'} ${item.revision} · ${item.name}`,
                })),
              ]}
            />
            <Button
              onClick={() => {
                void selected.refetch()
                if (base) void baseline.refetch()
              }}
            >
              {zh ? '刷新模板' : 'Refresh templates'}
            </Button>
          </div>
          <Typography.Text type="secondary">
            {zh
              ? '只读 Pod 模板；不含控制器生成的 pod-template-hash 标签。历史受 ReplicaSet 保留策略限制。'
              : 'Read-only Pod templates, excluding the controller-generated pod-template-hash label. History depends on retained ReplicaSets.'}
          </Typography.Text>
          {base && baseline.isLoading && <Spin />}
          {(!base || baselineData) && (
            <YamlDraftDiffEditor
              title={zh ? 'Pod 模板' : 'Pod template'}
              editable={false}
              modified={selectedData.template}
              original={base ? baselineData!.template : selectedData.template}
              leftLabel={`${zh ? '所选版本' : 'Selected revision'} ${record.revision}`}
              rightLabel={
                base
                  ? `${zh ? '对比版本' : 'Comparison revision'} ${base.revision}`
                  : zh
                    ? '模板差异'
                    : 'Template difference'
              }
            />
          )}
        </>
      )}
    </div>
  )
}
