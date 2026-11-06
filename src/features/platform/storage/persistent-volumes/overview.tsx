import { QuestionCircleOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Tooltip } from 'antd'
import { Link } from 'react-router-dom'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { PersistentVolumeDetail } from './types'
import type { PvConfiguration } from './detail-model'

export function PvOverview({
  detail,
  configuration,
  clusterId,
  onYaml,
}: {
  detail: PersistentVolumeDetail
  configuration?: PvConfiguration
  clusterId: string
  onYaml: () => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const unavailable = zh ? '未提供' : 'Not provided'
  const help = (label: string, description: string) => (
    <span className="soha-pv-section-title">
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
  const storageClass = detail.storageClass || configuration?.storageClassName
  const claim =
    detail.claimName && detail.claimNamespace
      ? { name: detail.claimName, namespace: detail.claimNamespace }
      : undefined
  const claimLabel = detail.claimRef || (claim ? `${claim.namespace}/${claim.name}` : '')
  const claimValue =
    claim && hasPermission(permissions, 'platform.storage.persistent-volume-claims.view') ? (
      <Link
        to={`/storage/persistentvolumeclaims/${encodeURIComponent(claim.name)}?${new URLSearchParams({ clusterId, namespace: claim.namespace })}`}
      >
        {claimLabel}
      </Link>
    ) : (
      claimLabel || (zh ? '未返回声明引用' : 'No claim reference returned')
    )
  const source = configuration?.sources
  return (
    <WorkloadOverview
      className="soha-pv-overview"
      aside={
        <Card className="soha-detail-card" title={zh ? '卷配置' : 'Volume configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: 'StorageClass',
                children: storageClass ? (
                  hasPermission(permissions, 'platform.storage.storage-classes.view') ? (
                    <Link
                      to={`/storage/storageclasses/${encodeURIComponent(storageClass)}?${new URLSearchParams({ clusterId })}`}
                    >
                      {storageClass}
                    </Link>
                  ) : (
                    storageClass
                  )
                ) : storageClass === '' ? (
                  zh ? (
                    '不使用存储类'
                  ) : (
                    'No storage class'
                  )
                ) : (
                  unavailable
                ),
              },
              {
                label: zh ? '回收策略' : 'Reclaim policy',
                children: detail.reclaimPolicy || unavailable,
              },
              { label: 'VolumeMode', children: detail.volumeMode || unavailable },
              {
                label: zh ? '访问模式' : 'Access modes',
                children: detail.accessModes?.length ? detail.accessModes.join(', ') : unavailable,
              },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : unavailable,
              },
              ...(configuration?.lastPhaseTransitionTime
                ? [
                    {
                      label: zh ? '状态变更' : 'Phase changed',
                      children: formatDateTime(configuration.lastPhaseTransitionTime),
                    },
                  ]
                : []),
            ]}
          />
        </Card>
      }
    >
      {detail.status === 'Released' ? (
        <Alert
          showIcon
          type="warning"
          title={
            zh
              ? 'PV 已释放，ClaimRef 可能保留原声明引用'
              : 'PV released; ClaimRef may retain the previous claim'
          }
        />
      ) : null}
      {detail.status === 'Failed' ? (
        <Alert showIcon type="error" title={zh ? 'PV 状态为 Failed' : 'PV phase is Failed'} />
      ) : null}
      <Card
        className="soha-detail-card"
        title={help(
          zh ? '绑定与容量' : 'Binding and capacity',
          zh
            ? '容量为 PV 声明容量，不是磁盘使用率。ClaimRef 是对象引用，不代表 PVC 当前仍存在或挂载正常；删除后的数据处理取决于实际回收策略与存储后端。'
            : 'Capacity is the PV declared capacity, not disk usage. ClaimRef does not prove the claim still exists or mounts are healthy. Data handling depends on the reclaim policy and storage backend.',
        )}
      >
        <div className="soha-pv-capacity">
          <dl>
            <dt>{zh ? '卷容量' : 'Volume capacity'}</dt>
            <dd>{detail.capacity || unavailable}</dd>
          </dl>
          <StatusTag value={detail.status} />
        </div>
        <WorkloadOverviewConfiguration
          items={[
            { label: 'ClaimRef', children: claimValue },
            ...(configuration?.reason
              ? [{ label: zh ? '原因' : 'Reason', children: configuration.reason }]
              : []),
            ...(configuration?.message
              ? [{ label: zh ? '消息' : 'Message', children: configuration.message }]
              : []),
          ]}
        />
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '存储源' : 'Volume source'}
        extra={
          <Button type="link" onClick={onYaml}>
            {zh ? '查看 YAML' : 'View YAML'}
          </Button>
        }
      >
        {source?.length ? (
          <div className="soha-pv-sources">
            {source.map((item) => (
              <section key={item.kind}>
                <h3>{item.kind === 'csi' ? 'CSI' : item.kind === 'nfs' ? 'NFS' : item.kind}</h3>
                <WorkloadOverviewConfiguration
                  items={item.fields.map(({ label, value }) => ({
                    label,
                    children: value || '""',
                  }))}
                />
              </section>
            ))}
          </div>
        ) : (
          <span className="soha-pv-muted">
            {configuration
              ? zh
                ? '当前未解析到存储源，可从 YAML 查看实际配置。'
                : 'No source was parsed. Inspect the actual configuration in YAML.'
              : zh
                ? '读取补充配置后展示存储源'
                : 'Read configuration to view the volume source'}
          </span>
        )}
      </Card>
      <Card
        className="soha-detail-card"
        title={help(
          zh ? '节点约束' : 'Node constraints',
          zh
            ? '来自 PV nodeAffinity.required；组间为 OR，同组标签与字段条件为 AND。配置约束不代表节点当前可用或卷已经挂载。'
            : 'From PV nodeAffinity.required. Terms are OR; requirements within each term are AND. Configuration does not prove node availability or a mounted volume.',
        )}
      >
        {configuration?.requiredNodeTerms?.length ? (
          <div className="soha-pv-node-terms">
            {configuration.requiredNodeTerms.map((term, index) => (
              <section key={index}>
                <h3>{zh ? `匹配组 ${index + 1}` : `Term ${index + 1}`}</h3>
                {[
                  ...term.matchExpressions.map((entry) => ({ ...entry, field: false })),
                  ...term.matchFields.map((entry) => ({ ...entry, field: true })),
                ].map((entry, position) => (
                  <div className="soha-pv-node-requirement" key={position}>
                    <span className="soha-pv-muted">
                      {entry.field ? (zh ? '字段' : 'Field') : zh ? '标签' : 'Label'}
                    </span>
                    <code>{entry.key}</code>
                    <span>{entry.operator}</span>
                    {entry.values ? (
                      <span>{entry.values.length ? entry.values.join(', ') : '[]'}</span>
                    ) : null}
                  </div>
                ))}
                {!term.matchExpressions.length && !term.matchFields.length ? (
                  <span className="soha-pv-muted">{zh ? '空匹配组' : 'Empty term'}</span>
                ) : null}
              </section>
            ))}
          </div>
        ) : (
          <span className="soha-pv-muted">
            {configuration
              ? configuration.requiredNodeTerms
                ? 'nodeSelectorTerms: []'
                : zh
                  ? '未设置'
                  : 'Not set'
              : unavailable}
          </span>
        )}
      </Card>
      <Card className="soha-detail-card" title={zh ? '挂载选项' : 'Mount options'}>
        {configuration?.mountOptions?.length ? (
          <ul className="soha-pv-mount-options">
            {configuration.mountOptions.map((option, index) => (
              <li key={index}>
                <code>{option}</code>
              </li>
            ))}
          </ul>
        ) : (
          <span className="soha-pv-muted">
            {configuration ? (zh ? '未设置' : 'Not set') : unavailable}
          </span>
        )}
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
