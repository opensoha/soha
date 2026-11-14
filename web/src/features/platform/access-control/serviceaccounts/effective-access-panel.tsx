import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { TableCellText } from '@/components/table-cell-content'
import {
  QuestionCircleOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Alert, Button, Card, Tag, Tooltip } from 'antd'
import {
  accessDecisionState,
  serviceAccountAccessReviewOptions,
  type EffectiveAccessRow,
} from './access-review'

export function ServiceAccountEffectiveAccessPanel({
  name,
  namespace,
  clusterId: explicitClusterId,
}: {
  name: string
  namespace: string
  clusterId?: string
}) {
  const { localeCode } = useI18n()
  const scopeClusterId = usePlatformScopeStore((state) => state.clusterId)
  const clusterId = explicitClusterId ?? scopeClusterId
  const snapshot = usePermissionSnapshot().data?.data
  const canReview =
    hasPermission(snapshot, 'platform.access-control.access-reviews.execute') ||
    hasPermission(snapshot, 'platform.access-control.service-accounts.view')
  const query = useQuery(
    serviceAccountAccessReviewOptions(clusterId ?? undefined, namespace, name, canReview),
  )
  const visibleQueryData =
    canReview && !query.isError && Boolean(clusterId && namespace && name)
      ? readableQueryData(query)
      : undefined

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
  const highRiskAllowed = rows.filter(
    (row) => row.risk === 'high' && accessDecisionState(row) === 'allowed',
  ).length
  const inconclusive =
    query.isError ||
    new Set(rows.map((row) => row.key)).size !== 9 ||
    rows.some(
      (row) => ['unknown', 'error'].includes(accessDecisionState(row)) || row.risk === 'unknown',
    )
  const columns: TableColumnsType<EffectiveAccessRow> = [
    {
      title: localeCode === 'zh_CN' ? '操作' : 'Operation',
      key: 'operation',
      width: 220,
      render: (_value, row) => <TableCellText value={`${row.check.verb} · ${row.label}`} />,
    },
    {
      title: localeCode === 'zh_CN' ? '范围' : 'Scope',
      key: 'scope',
      width: 320,
      render: (_value, row) => (
        <TableCellText
          value={`${row.check.group || 'core'}/${row.check.resource}${row.check.namespace ? ` · ${row.check.namespace}` : ' · cluster'}${row.check.name ? ` · ${row.check.name}` : ''}`}
        />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '风险' : 'Risk',
      dataIndex: 'risk',
      width: 100,
      render: (risk: EffectiveAccessRow['risk']) => (
        <Tag color={risk === 'high' ? 'red' : 'default'}>
          {localeCode === 'zh_CN'
            ? risk === 'high'
              ? '敏感操作'
              : risk === 'read'
                ? '读取'
                : '未知'
            : risk}
        </Tag>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '决策' : 'Decision',
      key: 'decision',
      width: 100,
      render: (_value, row) => <StatusTag value={accessDecisionState(row)} />,
    },
    {
      title: localeCode === 'zh_CN' ? '原因' : 'Reason',
      key: 'reason',
      width: 280,
      render: (_value, row) => (
        <TableCellText
          value={
            row.evaluationError ||
            (row.allowed && row.denied
              ? localeCode === 'zh_CN'
                ? '决策冲突'
                : 'Conflicting decisions'
              : row.reason) ||
            (accessDecisionState(row) === 'unknown'
              ? localeCode === 'zh_CN'
                ? '未返回允许或明确拒绝'
                : 'Neither allowed nor explicitly denied'
              : '—')
          }
        />
      ),
    },
  ]

  return (
    <div className="soha-detail-stack">
      <ReadFeedback canRead={query.isEnabled} query={query} label="资源记录" />

      <Card
        title={
          <span className="soha-sa-section-title">
            {localeCode === 'zh_CN' ? '权限检查' : 'Access checks'}
            <Tooltip
              trigger={['hover', 'focus', 'click']}
              title={
                localeCode === 'zh_CN'
                  ? '仅覆盖当前九项抽样检查，不代表完整权限或安全评估。未知表示未返回允许或明确拒绝；评估错误单独展示。'
                  : 'Only nine sampled checks; not a complete security assessment. Unknown means neither allowed nor explicitly denied. Evaluation errors are separate.'
              }
            >
              <Button
                type="text"
                size="small"
                icon={<QuestionCircleOutlined />}
                aria-label={localeCode === 'zh_CN' ? '权限检查说明' : 'Access check information'}
              />
            </Tooltip>
          </span>
        }
        extra={
          <Button
            size="small"
            icon={<ReloadOutlined />}
            onClick={() => void query.refetch()}
            loading={query.isFetching}
            disabled={!query.isEnabled}
          >
            {localeCode === 'zh_CN' ? '刷新检查' : 'Refresh checks'}
          </Button>
        }
      >
        <Alert
          showIcon
          style={{ marginBottom: 16 }}
          icon={<SafetyCertificateOutlined />}
          type={highRiskAllowed > 0 ? 'warning' : 'info'}
          title={
            highRiskAllowed > 0
              ? localeCode === 'zh_CN'
                ? `允许 ${highRiskAllowed} 项高风险操作`
                : `${highRiskAllowed} high-risk operations allowed`
              : inconclusive
                ? localeCode === 'zh_CN'
                  ? '检查不完整，无法确认高风险授权'
                  : 'Checks incomplete; high-risk access is unknown'
                : localeCode === 'zh_CN'
                  ? '本次检查未返回允许的高风险操作'
                  : 'No high-risk operations allowed among these checks'
          }
        />
        <AdminTable
          enableColumnSelection={false}
          className="soha-platform-table"
          columns={columns}
          dataSource={rows}
          loading={query.isLoading}
          rowKey="key"
          pagination={false}
          scroll={{ x: 1020 }}
          tableSize="small"
          localSorting
        />
      </Card>
    </div>
  )
}
