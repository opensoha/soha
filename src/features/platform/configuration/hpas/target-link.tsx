import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { buildRelatedResourcePath } from '@/features/platform/workloads-model'
import { readableQueryData } from '@/utils/query-feedback'
import { Link } from 'react-router-dom'
const targets: Record<string, { apiVersion: string; permission: string }> = {
  Deployment: { apiVersion: 'apps/v1', permission: 'platform.deployment.view' },
  StatefulSet: { apiVersion: 'apps/v1', permission: 'platform.workloads.stateful-sets.view' },
  ReplicaSet: { apiVersion: 'apps/v1', permission: 'platform.workloads.replica-sets.view' },
  ReplicationController: {
    apiVersion: 'v1',
    permission: 'platform.workloads.replication-controllers.view',
  },
}
export function HPATargetLink({
  target,
  namespace,
  clusterId,
}: {
  target: { apiVersion: string; kind: string; name: string }
  namespace: string
  clusterId: string
}) {
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const rule = targets[target.kind],
    label = target.kind + '/' + target.name
  const path =
    rule && rule.apiVersion === target.apiVersion && hasPermission(permissions, rule.permission)
      ? buildRelatedResourcePath(target, namespace, clusterId)
      : null
  return path ? (
    <Link className="soha-hpa-target-link" to={path}>
      <TableCellText value={label} />
    </Link>
  ) : (
    <TableCellText value={label} />
  )
}
