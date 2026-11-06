import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
} from '@/features/platform/workloads/shared/workload-overview'
import { Card, Collapse, Typography } from 'antd'
import type { CRD, CRDResourceInstance } from './types'
import { getServedVersions } from './utils'

export function CRDInstanceOverview({
  crd,
  resource,
}: {
  crd: CRD
  resource: CRDResourceInstance
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const labels = Object.entries(resource.labels ?? {})
  const summary = Object.entries(resource.summary ?? {})
  return (
    <WorkloadOverview
      className="soha-crd-overview"
      aside={
        <Card title={zh ? '资源信息' : 'Resource information'} size="small">
          <WorkloadOverviewConfiguration
            items={[
              { label: 'Kind', children: resource.kind || crd.kind },
              {
                label: zh ? 'API 版本' : 'API version',
                children: resource.apiVersion || `${crd.group}/${crd.version}`,
              },
              {
                label: zh ? '命名空间' : 'Namespace',
                children: resource.namespace ? (
                  <MetadataTag tone="blue" title={resource.namespace} label={resource.namespace} />
                ) : zh ? (
                  '集群范围'
                ) : (
                  'Cluster scope'
                ),
              },
              {
                label: 'UID',
                children: resource.uid ? (
                  <Typography.Text copyable>{resource.uid}</Typography.Text>
                ) : (
                  '—'
                ),
              },
              { label: 'Generation', children: resource.generation ?? '—' },
              {
                label: zh ? '创建时间' : 'Created',
                children: resource.createdAt
                  ? new Date(resource.createdAt).toLocaleString(
                      localeCode === 'zh_CN' ? 'zh-CN' : 'en-US',
                    )
                  : '—',
              },
            ]}
          />
        </Card>
      }
    >
      <Card title={zh ? '资源摘要' : 'Resource summary'} size="small">
        <WorkloadOverviewConfiguration
          items={[
            {
              label: zh ? '状态' : 'Status',
              children:
                resource.status || resource.deletingAt ? (
                  <StatusTag value={resource.deletingAt ? 'deleting' : resource.status!} />
                ) : zh ? (
                  '未返回'
                ) : (
                  'Not provided'
                ),
            },
            ...summary.map(([label, value]) => ({
              label,
              children: value == null ? '—' : String(value),
            })),
          ]}
        />
      </Card>
      <Card title={zh ? '资源定义' : 'Resource definition'} size="small">
        <WorkloadOverviewConfiguration
          items={[
            { label: 'CRD', children: crd.name },
            { label: 'API Group', children: crd.group },
            { label: zh ? '作用域' : 'Scope', children: crd.scope },
            {
              label: zh ? '支持版本' : 'Served versions',
              children: (
                <div className="soha-crd-inline-tags">
                  {getServedVersions(crd).map((version) => (
                    <MetadataTag key={version} label={version} />
                  ))}
                </div>
              ),
            },
          ]}
        />
      </Card>
      <Collapse
        items={[
          {
            key: 'labels',
            label: `${zh ? '标签' : 'Labels'} · ${labels.length}`,
            children: labels.length ? (
              <dl className="soha-workload-overview-metadata">
                {labels.map(([name, value]) => (
                  <div key={name}>
                    <dt>{name}</dt>
                    <dd>
                      <Typography.Text copyable>{value}</Typography.Text>
                    </dd>
                  </div>
                ))}
              </dl>
            ) : (
              <span className="soha-crd-resource-secondary">
                {zh ? '未返回标签' : 'No labels returned'}
              </span>
            ),
          },
          {
            key: 'finalizers',
            label: `Finalizers · ${resource.finalizers?.length ?? 0}`,
            children: resource.finalizers?.length ? (
              <div className="soha-crd-resource-finalizers">
                {resource.finalizers.map((value) => (
                  <Typography.Text key={value} copyable>
                    {value}
                  </Typography.Text>
                ))}
              </div>
            ) : (
              '—'
            ),
          },
        ]}
      />
    </WorkloadOverview>
  )
}
