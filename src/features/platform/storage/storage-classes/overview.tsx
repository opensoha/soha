import { QuestionCircleOutlined } from '@ant-design/icons'
import { Button, Card, Tooltip } from 'antd'
import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { StorageClassDetail } from './types'
import type { ScConfiguration } from './detail-model'

export function ScOverview({
  detail,
  configuration,
  clusterId,
  onTab,
}: {
  detail: StorageClassDetail
  configuration?: ScConfiguration
  clusterId: string
  onTab: (tab: string) => void
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const unavailable = zh ? '未提供' : 'Not provided'
  const help = (label: string, description: string) => (
    <span className="soha-sc-section-title">
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
  return (
    <WorkloadOverview
      className="soha-sc-overview"
      aside={
        <Card className="soha-detail-card" title={zh ? '置备策略' : 'Provisioning policies'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: 'Provisioner', children: detail.provisioner || unavailable },
              {
                label: zh ? '回收策略' : 'Reclaim policy',
                children: detail.reclaimPolicy || unavailable,
              },
              {
                label: zh ? '绑定模式' : 'Binding mode',
                children: detail.volumeBindingMode || unavailable,
              },
              {
                label: zh ? '允许扩容' : 'Allow expansion',
                children: (
                  <BooleanTag
                    value={detail.allowVolumeExpansion}
                    trueLabel={zh ? '允许' : 'Allowed'}
                    falseLabel={zh ? '不允许' : 'Not allowed'}
                    trueColor="default"
                  />
                ),
              },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : unavailable,
              },
              { label: zh ? '集群' : 'Cluster', children: clusterId },
            ]}
          />
        </Card>
      }
    >
      <Card
        className="soha-detail-card"
        title={help(
          zh ? '默认存储类' : 'Default storage class',
          zh
            ? '依据稳定或 beta 默认存储类注解值是否为 true；仅表示默认选择语义，不代表存储后端健康或可用。'
            : 'Based on the stable or beta default-class annotation being true. This describes default selection, not backend health or availability.',
        )}
      >
        {configuration ? (
          <MetadataTag
            label={
              configuration.isDefault ? (zh ? '默认' : 'Default') : zh ? '非默认' : 'Not default'
            }
          />
        ) : (
          <span className="soha-sc-muted">
            {zh
              ? '补充配置未提供，默认状态未知'
              : 'Configuration unavailable; default status unknown'}
          </span>
        )}
      </Card>
      <Card
        className="soha-detail-card"
        title={help(
          zh ? '当前返回的关系' : 'Returned relationships',
          zh
            ? '仅统计详情 API 当前返回的 PV 与 PVC 清单。权限过滤、读取失败或截断可能影响结果；零条不代表全局未使用，关系也不代表挂载或后端健康。'
            : 'Counts cover only PVs and PVCs returned by the detail API. Permission filtering, read failures or truncation may affect results. Zero does not prove global non-use; relationships do not prove mounts or backend health.',
        )}
      >
        <div className="soha-sc-relations-summary">
          {(['volumes', 'claims'] as const).map((kind) => (
            <section key={kind}>
              <dl>
                <dt>{kind === 'volumes' ? 'PVs' : 'PVCs'}</dt>
                <dd>{detail[kind] === undefined ? '—' : detail[kind]?.length}</dd>
              </dl>
              <span className="soha-sc-muted">
                {detail[kind] === undefined ? unavailable : zh ? '当前返回' : 'Currently returned'}
              </span>
              {detail[kind === 'volumes' ? 'volumesTruncated' : 'claimsTruncated'] ? (
                <span className="soha-sc-warning">{zh ? '结果已截断' : 'Results truncated'}</span>
              ) : null}
              <Button onClick={() => onTab(kind)}>
                {kind === 'volumes'
                  ? zh
                    ? '查看 PVs'
                    : 'View PVs'
                  : zh
                    ? '查看 PVCs'
                    : 'View PVCs'}
              </Button>
            </section>
          ))}
        </div>
      </Card>
      <Card className="soha-detail-card" title={zh ? '置备参数' : 'Provisioning parameters'}>
        {detail.parameters === undefined ? (
          <span className="soha-sc-muted">{unavailable}</span>
        ) : Object.keys(detail.parameters).length ? (
          <WorkloadOverviewConfiguration
            items={Object.entries(detail.parameters).map(([label, value]) => ({
              label,
              children: <code>{value || '""'}</code>,
            }))}
          />
        ) : (
          <span className="soha-sc-muted">{zh ? '未设置参数' : 'No parameters configured'}</span>
        )}
      </Card>
      {configuration?.allowedTopologies?.length ? (
        <Card
          className="soha-detail-card"
          title={help(
            zh ? '允许的拓扑' : 'Allowed topologies',
            zh
              ? '置备拓扑约束：组间 OR，同组表达式 AND；不代表对应节点当前可用。'
              : 'Provisioning constraints: OR between terms, AND within a term. These do not establish current node availability.',
          )}
        >
          <div className="soha-sc-topologies">
            {configuration.allowedTopologies.map((term, index) => (
              <section key={index}>
                <h3>{zh ? `匹配组 ${index + 1}` : `Term ${index + 1}`}</h3>
                {term.matchLabelExpressions.length ? (
                  <WorkloadOverviewConfiguration
                    items={term.matchLabelExpressions.map((expression) => ({
                      label: expression.key,
                      children: expression.values.length ? expression.values.join(', ') : '[]',
                    }))}
                  />
                ) : (
                  <span className="soha-sc-muted">{zh ? '空匹配组' : 'Empty term'}</span>
                )}
              </section>
            ))}
          </div>
        </Card>
      ) : null}
      {configuration?.mountOptions?.length ? (
        <Card className="soha-detail-card" title={zh ? '挂载选项' : 'Mount options'}>
          <ul className="soha-sc-mount-options">
            {configuration.mountOptions.map((option, index) => (
              <li key={index}>
                <code>{option}</code>
              </li>
            ))}
          </ul>
        </Card>
      ) : null}
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
