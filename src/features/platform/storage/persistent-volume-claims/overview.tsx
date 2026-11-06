import { QuestionCircleOutlined } from '@ant-design/icons'
import { Button, Card, Tooltip } from 'antd'
import { Link } from 'react-router-dom'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import type { PersistentVolumeClaimDetail } from '@/types/platform'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { WorkloadConditions } from '../../workloads/shared/workload-conditions'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { PvcConfiguration } from './detail-model'
import { PersistentVolumeClaimLifecyclePanel } from './lifecycle-panel'
import './styles.css'

export function PvcOverview({
  detail,
  clusterId,
  configuration,
  onPods,
}: {
  detail: PersistentVolumeClaimDetail
  clusterId: string
  configuration?: PvcConfiguration
  onPods: () => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const unavailable = zh ? '未提供' : 'Not provided'
  const help = (label: string, description: string) => (
    <span className="soha-pvc-section-title">
      {label}
      <Tooltip title={description} trigger={['hover', 'focus', 'click']}>
        <Button
          type="text"
          size="small"
          icon={<QuestionCircleOutlined />}
          aria-label={`${label}${zh ? '说明' : ' information'}`}
        />
      </Tooltip>
    </span>
  )
  const related = (kind: 'persistent-volumes' | 'storage-classes', name?: string) => {
    if (!name) return unavailable
    const segment = kind === 'persistent-volumes' ? 'persistentvolumes' : 'storageclasses'
    return hasPermission(permissions, `platform.storage.${kind}.view`) ? (
      <Link
        to={`/storage/${segment}/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId })}`}
      >
        {name}
      </Link>
    ) : (
      name
    )
  }
  const source = (reference: PvcConfiguration['dataSource']) =>
    reference ? (
      <span>
        {reference.apiGroup ? `${reference.apiGroup}/` : ''}
        {reference.kind}/{reference.name}
        {reference.namespace ? ` · ${reference.namespace}` : ''}
      </span>
    ) : configuration ? (
      zh ? (
        '未设置'
      ) : (
        'Not set'
      )
    ) : (
      unavailable
    )
  const selector = configuration?.selector
  const storageClassName = detail.storageClass || configuration?.storageClassName
  const selectorValues = [
    ...Object.entries(selector?.matchLabels ?? {}).map(([key, value]) => `${key}=${value}`),
    ...(selector?.matchExpressions ?? []).map(
      ({ key, operator, values }) =>
        `${key} ${operator}${values?.length ? ` (${values.join(', ')})` : ''}`,
    ),
  ]
  return (
    <WorkloadOverview
      className="soha-pvc-overview"
      aside={
        <Card className="soha-detail-card" title={zh ? '声明配置' : 'Claim configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '命名空间' : 'Namespace',
                children: (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span tabIndex={0} className="soha-pvc-namespace-trigger">
                      <MetadataTag tone="blue" label={<span>{detail.namespace}</span>} />
                    </span>
                  </Tooltip>
                ),
              },
              {
                label: 'VolumeMode',
                children: detail.volumeMode ?? configuration?.volumeMode ?? unavailable,
              },
              {
                label: 'AccessModes',
                children: detail.accessModes?.length ? detail.accessModes.join(', ') : unavailable,
              },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : unavailable,
              },
            ]}
          />
        </Card>
      }
    >
      <Card
        className="soha-detail-card"
        title={help(
          zh ? '绑定与容量' : 'Binding and capacity',
          zh
            ? '容量来自 PVC spec 请求与 status 报告，不是空间使用率。Bound 和 Pod 引用不能证明挂载或应用 I/O 正常。'
            : 'Request and reported capacity come from PVC spec and status, not disk usage. Bound and Pod references do not prove successful mounts or healthy I/O.',
        )}
      >
        <div className="soha-pvc-binding">
          <StatusTag value={detail.status} />
          {detail.volumeName ? (
            related('persistent-volumes', detail.volumeName)
          ) : (
            <span>{zh ? '未返回绑定 PV' : 'No bound PV returned'}</span>
          )}
        </div>
        <dl className="soha-pvc-capacity">
          <div>
            <dt>{zh ? '申请容量' : 'Requested capacity'}</dt>
            <dd>{detail.requested ?? unavailable}</dd>
          </div>
          <div>
            <dt>{zh ? '报告容量' : 'Reported capacity'}</dt>
            <dd>{detail.capacity ?? unavailable}</dd>
          </div>
        </dl>
        <WorkloadOverviewConfiguration
          items={[
            {
              label: 'StorageClass',
              children:
                storageClassName === ''
                  ? zh
                    ? '不使用存储类'
                    : 'No storage class'
                  : related('storage-classes', storageClassName),
            },
          ]}
        />
        <PersistentVolumeClaimLifecyclePanel claim={detail} />
      </Card>
      {configuration ? (
        <WorkloadConditions conditions={configuration.conditions} showTransitionTime />
      ) : (
        <Card className="soha-detail-card" title={zh ? '运行条件' : 'Conditions'}>
          <span className="soha-pvc-muted">
            {zh
              ? '配置读取后可查看原生 Conditions'
              : 'Read the configuration to view native conditions'}
          </span>
        </Card>
      )}
      {configuration &&
      (configuration.selector || configuration.dataSource || configuration.dataSourceRef) ? (
        <Card
          className="soha-detail-card"
          title={help(
            zh ? '选择与数据源' : 'Selection and data sources',
            zh
              ? '来自授权读取的同一 PVC YAML；未提供表示配置尚不可用。'
              : 'From the authorized YAML for this PVC. Not provided means configuration is unavailable.',
          )}
        >
          <WorkloadOverviewConfiguration
            items={[
              {
                label: 'Selector',
                children: configuration
                  ? selectorValues.length
                    ? selectorValues.join('; ')
                    : selector
                      ? zh
                        ? '空选择器'
                        : 'Empty selector'
                      : zh
                        ? '未设置'
                        : 'Not set'
                  : unavailable,
              },
              { label: 'DataSource', children: source(configuration?.dataSource) },
              { label: 'DataSourceRef', children: source(configuration?.dataSourceRef) },
            ]}
          />
        </Card>
      ) : null}
      <Card
        className="soha-detail-card"
        title={help(
          zh ? 'Pod 引用' : 'Pod references',
          zh
            ? '仅统计当前返回的引用；权限过滤或关系读取失败可能影响结果。引用不证明已挂载，0 条也不证明无人使用。'
            : 'Counts cover returned references only. Permission filtering or relationship read failures may affect results. References do not prove mounts; zero does not prove non-use.',
        )}
        extra={
          <Button type="link" onClick={onPods}>
            {zh ? '查看 Pods' : 'View Pods'}
          </Button>
        }
      >
        <span>
          {detail.pods === undefined
            ? unavailable
            : zh
              ? `当前返回 ${detail.pods.length} 条引用`
              : `${detail.pods.length} references returned`}
          {detail.podsTruncated ? (zh ? ' · 结果已截断' : ' · Results truncated') : ''}
        </span>
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
