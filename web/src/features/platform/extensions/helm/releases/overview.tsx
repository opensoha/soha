import { Alert, Card, Tooltip } from 'antd'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
} from '../../../workloads/shared/workload-overview'
import type { HelmReleaseDetail } from '../types'

export function HelmReleaseOverview({
  detail,
  capabilityReason,
}: {
  detail: HelmReleaseDetail
  capabilityReason?: string
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const unavailable = zh ? '未提供' : 'Not provided'

  return (
    <WorkloadOverview
      className="soha-helm-release-overview"
      aside={
        <Card className="soha-detail-card" title={zh ? '发布信息' : 'Release information'}>
          <WorkloadOverviewConfiguration
            items={[
              {
                label: zh ? '命名空间' : 'Namespace',
                children: detail.namespace ? (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span className="soha-helm-release-namespace" tabIndex={0}>
                      <MetadataTag label={detail.namespace} tone="blue" title={detail.namespace} />
                    </span>
                  </Tooltip>
                ) : (
                  unavailable
                ),
              },
              {
                label: zh ? '存储驱动' : 'Storage driver',
                children: detail.storageDriver || unavailable,
              },
              {
                label: zh ? '更新时间' : 'Updated',
                children: detail.updatedAt ? formatDateTime(detail.updatedAt) : unavailable,
              },
            ]}
          />
        </Card>
      }
    >
      <Card className="soha-detail-card" title={zh ? '当前发布' : 'Current release'}>
        <WorkloadOverviewConfiguration
          items={[
            { label: 'Chart', children: detail.chart || unavailable },
            {
              label: zh ? '应用版本' : 'App version',
              children: detail.appVersion || unavailable,
            },
            { label: 'Revision', children: detail.revision || unavailable },
            {
              label: zh ? '发布状态' : 'Release status',
              children: detail.status ? <StatusTag value={detail.status} /> : unavailable,
            },
          ]}
        />
      </Card>
      <Card className="soha-detail-card" title={zh ? '发布说明' : 'Release description'}>
        <p className="soha-helm-release-description">
          {detail.description || (zh ? '未提供发布说明' : 'No release description provided')}
        </p>
      </Card>
      {capabilityReason ? (
        <Alert
          type="warning"
          showIcon
          title={zh ? 'Helm 写入受限' : 'Helm writes limited'}
          description={capabilityReason}
        />
      ) : null}
    </WorkloadOverview>
  )
}
