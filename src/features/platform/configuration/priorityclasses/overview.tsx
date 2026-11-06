import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Button, Card, Tooltip, Typography } from 'antd'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { PriorityClassDetail } from './priority-model'
function Help({ title, help }: { title: string; help: string }) {
  return (
    <span className="soha-priority-card-title">
      {title}
      <Tooltip title={help} trigger={['hover', 'focus', 'click']}>
        <Button
          size="small"
          type="text"
          icon={<QuestionCircleOutlined />}
          aria-label={title + ' info'}
        />
      </Tooltip>
    </span>
  )
}
export function PriorityClassOverview({ detail }: { detail: PriorityClassDetail }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  return (
    <WorkloadOverview
      aside={
        <Card title={zh ? '资源配置' : 'Resource configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '作用范围' : 'Scope', children: zh ? '集群' : 'Cluster' },
              { label: zh ? 'API 版本' : 'API version', children: 'scheduling.k8s.io/v1' },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
              },
              ...(detail.uid
                ? [
                    {
                      label: 'UID',
                      children: <Typography.Text copyable>{detail.uid}</Typography.Text>,
                    },
                  ]
                : []),
            ]}
          />
        </Card>
      }
    >
      <Card
        title={
          <Help
            title={zh ? '调度优先级' : 'Scheduling priority'}
            help={
              zh
                ? '数值越高，调度优先级越高；支持负数。该数值不表示 CPU / 内存配额或当前调度结果。'
                : 'Higher values indicate higher scheduling priority; negative values are valid. This is not a resource quota or proof of scheduling.'
            }
          />
        }
      >
        <div className="soha-priority-value">
          {new Intl.NumberFormat('en-US').format(detail.value)}
        </div>
      </Card>
      <Card
        title={
          <Help
            title={zh ? '调度策略' : 'Scheduling policy'}
            help={
              zh
                ? '全局默认用于未指定 priorityClassName 的新 Pod；不会改写已有 Pod 的优先级。'
                : 'Global default applies to new Pods without a priorityClassName; existing Pods are not reprioritized.'
            }
          />
        }
      >
        <WorkloadOverviewConfiguration
          items={[
            {
              label: zh ? '全局默认' : 'Global default',
              children: (
                <BooleanTag
                  value={detail.globalDefault}
                  trueLabel={zh ? '是' : 'Yes'}
                  falseLabel={zh ? '否' : 'No'}
                />
              ),
            },
            {
              label: zh ? '抢占策略' : 'Preemption policy',
              children: (
                <>
                  <MetadataTag label={detail.preemptionPolicy} />
                  {detail.policyDefaulted ? (
                    <small className="soha-priority-muted">{zh ? '默认' : 'Default'}</small>
                  ) : null}
                </>
              ),
            },
          ]}
        />
        <p className="soha-priority-policy-description">
          {detail.preemptionPolicy === 'Never'
            ? zh
              ? '不主动抢占低优先级 Pod；仍可能被更高优先级 Pod 抢占。'
              : 'Does not preempt lower-priority Pods; it may still be preempted by higher-priority Pods.'
            : detail.preemptionPolicy === 'PreemptLowerPriority'
              ? zh
                ? '调度时允许抢占低优先级 Pod，实际结果由调度器决定。'
                : 'Allows preemption of lower-priority Pods; the scheduler determines the outcome.'
              : zh
                ? '保留接口返回的策略值，未推断抢占行为。'
                : 'The policy value is shown as returned; no preemption behavior is inferred.'}
        </p>
      </Card>
      <Card title={zh ? '说明' : 'Description'}>
        <p className="soha-priority-description">
          {detail.description?.length
            ? detail.description
            : zh
              ? '未提供说明'
              : 'No description provided'}
        </p>
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} copyable />
    </WorkloadOverview>
  )
}
