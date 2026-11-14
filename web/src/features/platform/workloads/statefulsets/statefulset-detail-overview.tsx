import { Alert, Button, Card, Typography } from 'antd'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import type { ScopeKey } from '@/types'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewFacts,
  WorkloadOverviewMetadata,
} from '../shared/workload-overview'
import { WorkloadPodsTable } from '../shared/workload-pods-table'
import type { StatefulSetDetail } from './types'
import { statefulSetState, type StatefulSetConfiguration } from './statefulset-detail-model'
export function StatefulSetDetailOverview({
  detail,
  configuration,
  scope,
  onTabChange,
}: {
  detail: StatefulSetDetail
  configuration?: StatefulSetConfiguration
  scope: ScopeKey
  onTabChange: (key: string) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const state = statefulSetState(detail, configuration),
    desired = detail.desiredReplicas
  return (
    <WorkloadOverview
      aside={
        <>
          <Card className="soha-detail-card" title={zh ? '运行配置' : 'Configuration'}>
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
                {
                  label: zh ? '更新策略' : 'Update strategy',
                  children: configuration?.strategy || detail.updateStrategy || '—',
                },
                { label: 'Partition', children: configuration?.partition ?? '—' },
                {
                  label: zh ? 'Pod 管理策略' : 'Pod management',
                  children: configuration?.podManagementPolicy || '—',
                },
                {
                  label: 'Service',
                  children: configuration?.serviceName || detail.serviceName || '—',
                },
                { label: zh ? '创建时间' : 'Created', children: formatDateTime(detail.createdAt) },
                {
                  label: zh ? '选择器' : 'Selector',
                  children:
                    Object.entries(detail.selector || {})
                      .map(([key, value]) => `${key}=${value}`)
                      .join(', ') || '—',
                },
              ]}
            />
          </Card>
          <Card className="soha-detail-card" title={zh ? '版本' : 'Revisions'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '当前版本' : 'Current revision',
                  children: detail.currentRevision || '—',
                },
                {
                  label: zh ? '目标版本' : 'Target revision',
                  children: detail.updateRevision || '—',
                },
                {
                  label: zh ? '历史保留数' : 'Revision retention',
                  children: configuration?.revisionHistoryLimit ?? '—',
                },
              ]}
            />
            <Button type="link" onClick={() => onTabChange('revisions')}>
              {zh ? '查看版本历史' : 'View revision history'}
            </Button>
          </Card>
          <Card className="soha-detail-card" title={zh ? '存储保留策略' : 'PVC retention'}>
            <WorkloadOverviewConfiguration
              items={[
                {
                  label: zh ? '删除负载时' : 'When deleted',
                  children: configuration?.retention.whenDeleted || '—',
                },
                {
                  label: zh ? '缩容时' : 'When scaled',
                  children: configuration?.retention.whenScaled || '—',
                },
              ]}
            />
            <Button type="link" onClick={() => onTabChange('storage')}>
              {zh ? '查看存储与 PVC' : 'View storage and PVCs'}
            </Button>
          </Card>
        </>
      }
    >
      <Alert
        showIcon
        type={state === 'healthy' ? 'success' : state === 'unknown' ? 'info' : 'warning'}
        title={
          zh
            ? state === 'healthy'
              ? 'StatefulSet 当前副本已就绪'
              : state === 'scaled-to-zero'
                ? 'StatefulSet 已缩容至 0'
                : state === 'progressing'
                  ? 'StatefulSet 正在更新'
                  : 'StatefulSet 副本状态待检查'
            : `StatefulSet · ${state}`
        }
        action={
          <Button type="link" onClick={() => onTabChange('events')}>
            {zh ? '查看事件' : 'View events'}
          </Button>
        }
      />
      <Card className="soha-detail-card" title={zh ? '副本与发布' : 'Replicas and rollout'}>
        <WorkloadOverviewFacts
          facts={[
            { key: 'desired', label: zh ? '期望' : 'Desired', value: desired, target: desired },
            {
              key: 'current',
              label: zh ? '当前' : 'Current',
              value: detail.currentReplicas,
              target: desired,
              showTarget: true,
            },
            {
              key: 'updated',
              label: zh ? '已更新' : 'Updated',
              value: configuration?.updatedReplicas,
              target: desired,
              showTarget: true,
              tone: 'primary',
            },
            {
              key: 'ready',
              label: zh ? '就绪' : 'Ready',
              value: detail.readyReplicas,
              target: desired,
              showTarget: true,
            },
          ]}
        />
        <div className="soha-workload-overview-facts-footer">
          <span>
            {configuration?.strategy || detail.updateStrategy}{' '}
            {configuration?.partition !== undefined
              ? ` · Partition ${configuration.partition}`
              : ''}
          </span>
          <span>
            Generation {configuration?.observedGeneration ?? '—'} /{' '}
            {configuration?.generation ?? '—'}
          </span>
          <Button type="link" onClick={() => onTabChange('pods')}>
            {zh ? '查看 Pods' : 'View Pods'}
          </Button>
        </div>
        {configuration?.strategy === 'OnDelete' && (
          <Typography.Paragraph type="secondary">
            {zh
              ? 'OnDelete 策略不会自动替换已有 Pod，模板更新后需按运维流程处理 Pod。'
              : 'OnDelete does not automatically replace existing Pods after a template update.'}
          </Typography.Paragraph>
        )}
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '当前模板' : 'Current template'}
        extra={
          configuration
            ? `${configuration.containers.length} ${zh ? '个容器' : 'containers'}`
            : undefined
        }
      >
        {configuration?.containers.map((c) => (
          <div className="soha-sts-template" key={c.name}>
            <strong>
              {c.name} {c.init && <Typography.Text type="secondary">init</Typography.Text>}
            </strong>
            <Typography.Text copyable>{c.image}</Typography.Text>
          </div>
        ))}
        {!configuration && (
          <Typography.Text type="secondary">
            {zh
              ? '配置尚未读取，请查看上方读取状态。'
              : 'Configuration has not been read; see the read status above.'}
          </Typography.Text>
        )}
      </Card>
      <WorkloadPodsTable
        compact
        pods={detail.pods || []}
        scope={scope}
        onShowAll={() => onTabChange('pods')}
      />
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
