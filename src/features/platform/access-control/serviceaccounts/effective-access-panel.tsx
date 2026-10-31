import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey, type ApiResponse } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { SafetyCertificateOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, Tag, Typography } from 'antd'
import { accessControlKeys } from '../shared/keys'

interface AccessCheck {
  verb: string
  group?: string
  resource: string
  namespace?: string
  name?: string
}

interface AccessDecision {
  check: AccessCheck
  allowed: boolean
  denied: boolean
  reason?: string
  evaluationError?: string
}

interface EffectiveAccessRow extends AccessDecision {
  key: string
  label: string
  risk: 'read' | 'high'
}

function serviceAccountChecks(namespace: string) {
  return [
    { label: 'List Pods', risk: 'read' as const, verb: 'list', resource: 'pods', namespace },
    { label: 'Read Pod logs', risk: 'read' as const, verb: 'get', resource: 'pods/log', namespace },
    {
      label: 'Create Deployments',
      risk: 'high' as const,
      verb: 'create',
      group: 'apps',
      resource: 'deployments',
      namespace,
    },
    {
      label: 'Update Deployments',
      risk: 'high' as const,
      verb: 'update',
      group: 'apps',
      resource: 'deployments',
      namespace,
    },
    {
      label: 'Delete Deployments',
      risk: 'high' as const,
      verb: 'delete',
      group: 'apps',
      resource: 'deployments',
      namespace,
    },
    { label: 'Read Secrets', risk: 'high' as const, verb: 'get', resource: 'secrets', namespace },
    {
      label: 'Create RoleBindings',
      risk: 'high' as const,
      verb: 'create',
      group: 'rbac.authorization.k8s.io',
      resource: 'rolebindings',
      namespace,
    },
    {
      label: 'Create ClusterRoleBindings',
      risk: 'high' as const,
      verb: 'create',
      group: 'rbac.authorization.k8s.io',
      resource: 'clusterrolebindings',
    },
    { label: 'Impersonate Users', risk: 'high' as const, verb: 'impersonate', resource: 'users' },
  ]
}

export function ServiceAccountEffectiveAccessPanel({
  name,
  namespace,
}: {
  name: string
  namespace: string
}) {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const snapshot = usePermissionSnapshot().data?.data
  const canReview =
    hasPermission(snapshot, 'platform.access-control.access-reviews.execute') ||
    hasPermission(snapshot, 'platform.access-control.service-accounts.view')
  const checks = serviceAccountChecks(namespace)
  const scope = toScopeKey(clusterId, namespace)
  const query = useQuery({
    queryKey: accessControlKeys.accessReview(scope, name),
    queryFn: async () => {
      const response = await api.post<ApiResponse<{ decisions: AccessDecision[] }>>(
        `/clusters/${encodeURIComponent(clusterId!)}/access-control/access-reviews`,
        {
          subject: { kind: 'ServiceAccount', namespace, name },
          checks: checks.map(({ label: _label, risk: _risk, ...check }) => check),
        },
      )
      return response.data.decisions.map(
        (decision, index): EffectiveAccessRow => ({
          ...decision,
          key: `${decision.check.verb}/${decision.check.group || 'core'}/${decision.check.resource}`,
          label: checks[index]?.label ?? decision.check.resource,
          risk: checks[index]?.risk ?? 'read',
        }),
      )
    },
    enabled: canReview && Boolean(clusterId && namespace && name),
  })
  const visibleQueryData =
    canReview && Boolean(clusterId && namespace && name) ? readableQueryData(query) : undefined

  if (!canReview) {
    return (
      <ManagementState
        compact
        kind="unsupported"
        description="platform.access-control.access-reviews.execute"
      />
    )
  }

  const rows = visibleQueryData ?? []
  const highRiskAllowed = rows.filter((row) => row.risk === 'high' && row.allowed).length
  const columns: TableColumnsType<EffectiveAccessRow> = [
    { title: localeCode === 'zh_CN' ? '操作' : 'Operation', dataIndex: 'label' },
    {
      title: localeCode === 'zh_CN' ? '范围' : 'Scope',
      key: 'scope',
      render: (_value, row) =>
        `${row.check.group || 'core'}/${row.check.resource}${row.check.namespace ? ` · ${row.check.namespace}` : ' · cluster'}`,
    },
    {
      title: localeCode === 'zh_CN' ? '风险' : 'Risk',
      dataIndex: 'risk',
      render: (risk: EffectiveAccessRow['risk']) => (
        <Tag color={risk === 'high' ? 'red' : 'default'}>{risk}</Tag>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '决策' : 'Decision',
      key: 'decision',
      render: (_value, row) => <StatusTag value={row.allowed ? 'allowed' : 'denied'} />,
    },
    {
      title: localeCode === 'zh_CN' ? '原因' : 'Reason',
      key: 'reason',
      render: (_value, row) => (
        <Typography.Text type="secondary">
          {row.evaluationError || row.reason || '-'}
        </Typography.Text>
      ),
    },
  ]

  return (
    <div className="soha-detail-stack">
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />

      <Alert
        showIcon
        icon={<SafetyCertificateOutlined />}
        type={highRiskAllowed > 0 ? 'warning' : 'info'}
        title={
          highRiskAllowed > 0
            ? localeCode === 'zh_CN'
              ? `允许 ${highRiskAllowed} 项高风险操作`
              : `${highRiskAllowed} high-risk operations allowed`
            : localeCode === 'zh_CN'
              ? '未发现允许的高风险操作'
              : 'No high-risk operations allowed'
        }
      />
      <AdminTable
        className="soha-platform-table"
        columns={columns}
        dataSource={rows}
        loading={query.isLoading}
        rowKey="key"
        pagination={false}
        virtual
        scroll={{ x: 1000, y: 520 }}
        tableSize="small"
        localSorting
        viewportScroll
      />
    </div>
  )
}
