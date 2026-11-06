import { Card, Typography } from 'antd'
import { Link } from 'react-router-dom'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import type { DeploymentRolloutStatus, RolloutHistory, ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { ReplicaControllerOverview } from '../shared/replica-controller-overview'
import {
  replicaSetOwner,
  replicaSetRevision,
  replicaSetTemplateState,
  type ReplicaSetConfiguration,
} from './replicaset-detail-model'
import type { ReplicaSetDetail } from './types'

export function ReplicaSetDetailOverview({
  detail,
  scope,
  configuration,
  ownerHistory,
  parentStatus,
  onTabChange,
}: {
  detail: ReplicaSetDetail
  scope: ScopeKey
  configuration?: ReplicaSetConfiguration
  ownerHistory?: RolloutHistory[]
  parentStatus?: DeploymentRolloutStatus
  onTabChange: (tab: string) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canReadParent = hasPermission(permissions, 'platform.deployment.view')
  const owner = replicaSetOwner(detail),
    revision = replicaSetRevision(detail),
    state = replicaSetTemplateState(detail, ownerHistory, parentStatus)
  const parentPath = owner
    ? buildWorkloadDetailPath(
        'deployments',
        owner.name,
        scope.namespace,
        owner.namespace,
        scope.clusterId,
      )
    : undefined
  const labels = {
    current: zh ? 'Deployment 当前修订' : 'Current Deployment revision',
    retained: zh ? 'Deployment 历史修订' : 'Historical Deployment revision',
    unknown: zh ? '模板归属未确认' : 'Template status unconfirmed',
    standalone: zh ? '未关联 Deployment' : 'No Deployment owner',
  }
  return (
    <ReplicaControllerOverview
      kind="ReplicaSet"
      detail={detail}
      scope={scope}
      configuration={configuration}
      currentReplicas={configuration?.currentReplicas}
      onTabChange={onTabChange}
      summaryNote={labels[state]}
      summaryExtra={
        revision ? (
          <span title="deployment.kubernetes.io/revision">
            {zh ? '版本' : 'Revision'} {revision}
          </span>
        ) : undefined
      }
      asideExtra={
        <Card className="soha-detail-card" title={zh ? '所属 Deployment' : 'Owning Deployment'}>
          {owner ? (
            <>
              <Typography.Paragraph>
                {canReadParent && parentPath ? (
                  <Link to={parentPath}>{owner.name}</Link>
                ) : (
                  owner.name
                )}
              </Typography.Paragraph>
              <Typography.Paragraph type="secondary">{labels[state]}</Typography.Paragraph>
              {canReadParent && parentPath ? (
                <div className="soha-replicaset-parent-links">
                  <Link to={`${parentPath}&tab=revisions`}>
                    {zh ? '查看 Deployment 版本历史' : 'Deployment revision history'}
                  </Link>
                  {hasPermission(permissions, 'delivery.applications.view') && (
                    <Link to={`${parentPath}&tab=delivery`}>
                      {zh ? '查看 Deployment 交付联动' : 'Deployment delivery integration'}
                    </Link>
                  )}
                </div>
              ) : (
                <Typography.Text type="secondary">
                  {zh
                    ? '需要 Deployment 查看权限以确认版本和访问上游'
                    : 'Deployment view permission is required to confirm template status and open its owner'}
                </Typography.Text>
              )}
            </>
          ) : (
            <Typography.Text type="secondary">
              {zh
                ? '未关联 Deployment，不推测其版本历史或交付来源。'
                : 'No Deployment owner; revision history or delivery is not inferred.'}
            </Typography.Text>
          )}
        </Card>
      }
    />
  )
}
