import { Alert, Card, Typography } from 'antd'
import './manifest-deployment-state.css'
import { Link } from 'react-router-dom'
import { StatusTag } from '@/components/status-tag'
import type { ApplicationRuntimeEnvironment } from '../types'
import { manifestDeploymentRuntimeStatus } from './runtime-status'
import { ManifestResourceInventoryTable } from '../manifests/resource-inventory'

export function ManifestDeploymentState({
  deployment,
}: {
  deployment: NonNullable<ApplicationRuntimeEnvironment['manifestDeployments']>[number]
}) {
  const status = manifestDeploymentRuntimeStatus(deployment)
  return (
    <Card
      size="small"
      title="配置与资源状态"
      extra={<StatusTag value={status.tone} label={status.label} />}
    >
      <div className="manifest-deployment-state__content">
        <div className="manifest-deployment-state__summary">
          <div className="manifest-deployment-state__fact">
            <Typography.Text type="secondary">期望版本</Typography.Text>
            <Typography.Text strong>{deployment.spec.desiredRevision}</Typography.Text>
          </div>
          <div className="manifest-deployment-state__fact">
            <Typography.Text type="secondary">已应用版本</Typography.Text>
            <Typography.Text strong>{deployment.status.appliedRevision || '暂无'}</Typography.Text>
          </div>
          {deployment.status.lastExecutionTaskId ? (
            <Link
              className="manifest-deployment-state__task"
              to={`/delivery/execution-tasks/${deployment.status.lastExecutionTaskId}`}
            >
              查看最近执行任务 →
            </Link>
          ) : null}
        </div>
        <details className="manifest-deployment-state__digest">
          <summary>
            期望配置摘要
            <span className="manifest-deployment-state__digest-preview">
              {deployment.spec.desiredDigest
                ? `${deployment.spec.desiredDigest.slice(0, 12)}…`
                : '未提供'}
            </span>
          </summary>
          <Typography.Text
            copyable={{ text: deployment.spec.desiredDigest, tooltips: ['复制完整摘要', '已复制'] }}
          >
            {deployment.spec.desiredDigest || '未提供'}
          </Typography.Text>
        </details>
        {deployment.status.lastErrorMessage ? (
          <Alert showIcon type="error" title={deployment.status.lastErrorMessage} />
        ) : null}
        <ManifestResourceInventoryTable items={deployment.status.inventory ?? []} />
      </div>
    </Card>
  )
}
