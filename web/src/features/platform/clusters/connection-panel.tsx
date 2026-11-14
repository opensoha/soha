import { Card } from 'antd'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { WorkloadOverviewConfiguration } from '../workloads/shared/workload-overview'
import { formatClusterRuntimeValue, formatConnectionMode } from './presentation'
import type { ClusterDetail } from './types'

export function ClusterConnectionPanel({
  detail,
  localeCode,
}: {
  detail: ClusterDetail
  localeCode: string
}) {
  const zh = localeCode === 'zh_CN'
  const { connection, diagnostics } = detail

  return (
    <>
      <Card className="soha-detail-card" title={zh ? '连接配置' : 'Connection configuration'}>
        <WorkloadOverviewConfiguration
          items={[
            {
              label: zh ? '连接模式' : 'Connection mode',
              children: formatConnectionMode(connection.mode, localeCode),
            },
            {
              label: zh ? '凭据类型' : 'Credential type',
              children: formatClusterRuntimeValue(connection.credentialType, localeCode),
            },
            {
              label: zh ? '来源类型' : 'Source type',
              children: formatClusterRuntimeValue(connection.sourceType, localeCode),
            },
            { label: 'Context', children: connection.context || '—' },
            { label: 'Endpoint', children: connection.endpoint || '—' },
          ]}
        />
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '运行与缓存诊断' : 'Runtime and cache diagnostics'}
      >
        <WorkloadOverviewConfiguration
          items={[
            {
              label: zh ? '连接状态' : 'Connection state',
              children: (
                <StatusTag
                  value={diagnostics.connectionState}
                  label={formatClusterRuntimeValue(diagnostics.connectionState, localeCode)}
                />
              ),
            },
            {
              label: zh ? '同步策略' : 'Sync strategy',
              children: formatClusterRuntimeValue(diagnostics.syncStrategy, localeCode),
            },
            {
              label: 'Informer Cache',
              children: (
                <BooleanTag
                  value={connection.usesInformerCache}
                  trueLabel={zh ? '是' : 'Yes'}
                  falseLabel={zh ? '否' : 'No'}
                />
              ),
            },
            {
              label: zh ? '缓存状态' : 'Cache status',
              children: (
                <StatusTag
                  value={diagnostics.cacheStatus}
                  label={formatClusterRuntimeValue(diagnostics.cacheStatus, localeCode)}
                />
              ),
            },
            { label: zh ? '诊断信息' : 'Diagnostic message', children: diagnostics.message || '—' },
          ]}
        />
      </Card>
    </>
  )
}
