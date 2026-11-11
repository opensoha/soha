import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { DeploymentUnitOutlined, RightOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Space, Typography } from 'antd'
import { useNavigate } from 'react-router-dom'
import { platformSharedKeys } from './keys'

interface ManagedResource {
  kind: string
  namespace?: string
  name: string
  fields?: Array<{ path: string }>
}

export interface GitOpsDeployment {
  id: string
  packageId: string
  generation: number
  spec: {
    desiredRevision: number
    reconcilePolicy: string
    driftPolicy: string
  }
  status: {
    phase: string
    appliedRevision?: number
    inventory?: ManagedResource[]
    drift?: { drifted: boolean; resources: ManagedResource[] }
  }
}

interface ResourceIdentity {
  kind: string
  namespace?: string | null
  name: string
}

export interface ResourceGitOpsStatus {
  appliedRevision?: number
  deploymentId: string
  desiredRevision: number
  drifted: boolean
  driftPolicy: string
  fieldCount: number
  packageId: string
  phase: string
  reconcilePolicy: string
}

function sameResource(resource: ManagedResource, target: ResourceIdentity) {
  return (
    resource.kind.localeCompare(target.kind, undefined, { sensitivity: 'accent' }) === 0 &&
    (resource.namespace?.trim() || '') === (target.namespace?.trim() || '') &&
    resource.name === target.name
  )
}

export function selectResourceGitOpsStatus(
  deployments: GitOpsDeployment[],
  target: ResourceIdentity,
): ResourceGitOpsStatus | null {
  for (const deployment of deployments) {
    const drift = deployment.status.drift?.resources.find((item) => sameResource(item, target))
    if (drift) {
      return {
        deploymentId: deployment.id,
        packageId: deployment.packageId,
        phase: deployment.status.phase,
        desiredRevision: deployment.spec.desiredRevision,
        appliedRevision: deployment.status.appliedRevision,
        reconcilePolicy: deployment.spec.reconcilePolicy,
        driftPolicy: deployment.spec.driftPolicy,
        drifted: true,
        fieldCount: drift.fields?.length ?? 0,
      }
    }
  }
  for (const deployment of deployments) {
    if (deployment.status.inventory?.some((item) => sameResource(item, target))) {
      return {
        deploymentId: deployment.id,
        packageId: deployment.packageId,
        phase: deployment.status.phase,
        desiredRevision: deployment.spec.desiredRevision,
        appliedRevision: deployment.status.appliedRevision,
        reconcilePolicy: deployment.spec.reconcilePolicy,
        driftPolicy: deployment.spec.driftPolicy,
        drifted: false,
        fieldCount: 0,
      }
    }
  }
  return null
}

async function loadResourceGitOpsStatus(
  clusterId: string,
  namespace: string,
  target: ResourceIdentity,
) {
  const search = new URLSearchParams({ clusterId, page: '1', pageSize: '100' })
  if (namespace) search.set('namespace', namespace)
  const response = await api.get<
    ApiResponse<{ items: GitOpsDeployment[]; total: number; page: number; pageSize: number }>
  >(`/delivery/manifest-deployments?${search.toString()}`)
  return selectResourceGitOpsStatus(response.data.items ?? [], target)
}

export function ResourceGitOpsStatus({
  clusterId,
  kind,
  name,
  namespace,
}: ResourceIdentity & { clusterId?: string | null }) {
  const { localeCode } = useI18n()
  const navigate = useNavigate()
  const snapshot = usePermissionSnapshot().data?.data
  const canView = hasPermission(snapshot, 'delivery.applications.view')
  const normalizedCluster = clusterId?.trim() || ''
  const normalizedNamespace = namespace?.trim() || ''
  const query = useQuery({
    queryKey: platformSharedKeys.resourceGitOpsStatus(
      normalizedCluster,
      normalizedNamespace,
      kind,
      name,
    ),
    queryFn: () =>
      loadResourceGitOpsStatus(normalizedCluster, normalizedNamespace, {
        kind,
        namespace: normalizedNamespace,
        name,
      }),
    enabled: canView && Boolean(normalizedCluster && kind && name),
    retry: false,
  })
  const visibleQueryData =
    canView && Boolean(normalizedCluster && kind && name) ? readableQueryData(query) : undefined
  const status = visibleQueryData
  if (!status) return null

  return (
    <>
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />
      <Alert
        showIcon
        icon={<DeploymentUnitOutlined />}
        type={status.drifted ? 'warning' : 'info'}
        title={
          <Space size={8} wrap>
            <span>GitOps</span>
            <StatusTag value={status.drifted ? 'drifted' : status.phase} />
          </Space>
        }
        description={
          <Space size={12} wrap>
            <Typography.Text type="secondary">
              {localeCode === 'zh_CN'
                ? `期望 revision ${status.desiredRevision} · 已应用 ${status.appliedRevision ?? '-'} · ${status.reconcilePolicy}`
                : `Desired revision ${status.desiredRevision} · Applied ${status.appliedRevision ?? '-'} · ${status.reconcilePolicy}`}
            </Typography.Text>
            {status.drifted ? (
              <Typography.Text type="warning">
                {localeCode === 'zh_CN'
                  ? `${status.fieldCount} 个字段发生漂移`
                  : `${status.fieldCount} drifted fields`}
              </Typography.Text>
            ) : null}
            <Button
              icon={<RightOutlined />}
              iconPlacement="end"
              size="small"
              type="link"
              onClick={() => navigate('/delivery/manifests')}
            >
              {localeCode === 'zh_CN' ? '打开 Manifest 运维' : 'Open Manifest operations'}
            </Button>
          </Space>
        }
      />
    </>
  )
}
