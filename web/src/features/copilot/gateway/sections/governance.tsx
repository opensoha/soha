import { lazy, Suspense } from 'react'
import { PlusOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import { Alert, Button, Descriptions, Select, Space, Tabs } from 'antd'
import { AdminTable } from '@/components/admin-table'
import {
  ManagementQueryField,
  ManagementQueryPanel,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
  ManagementToolbarSearch,
} from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import type {
  AIClient,
  AccessPolicy,
  ApprovalFilterState,
  ApprovalRequest,
  GatewayManifest,
  GatewayTabKey,
  GovernanceFinding,
  GovernanceHealthCheck,
  GovernanceMetricCount,
  GovernanceRecommendationAction,
  GovernanceRedactionRow,
  GovernanceStatus,
  GovernanceTokenFindingRow,
  SkillBinding,
  ToolGrant,
} from '../types'
import { compactList, formatDateTime } from '../presentation'
import {
  governanceApprovalQueueRows,
  governanceCoverageRows,
  governanceRedactionRows,
  governanceRiskCountTags,
  governanceTokenFindingRows,
  governanceWindowOptions,
} from '../types'

const GatewayPoliciesSection = lazy(() =>
  import('./policies').then((module) => ({ default: module.GatewayPoliciesSection })),
)
const GatewayApprovalsSection = lazy(() =>
  import('./approvals').then((module) => ({ default: module.GatewayApprovalsSection })),
)

export interface GatewayGovernanceSectionProps {
  activeTab: GatewayTabKey
  onTabChange: (tab: GatewayTabKey) => void
  canCreateGrants: boolean
  canCreatePolicies: boolean
  canCreateSkills: boolean
  grantColumns: TableColumnsType<ToolGrant>
  grants: ToolGrant[]
  grantsLoading: boolean
  grantFilter: string
  onGrantFilterChange: (value: string) => void
  onCreateGrant: () => void
  policyColumns: TableColumnsType<AccessPolicy>
  policies: AccessPolicy[]
  policiesLoading: boolean
  policyFilter: string
  onPolicyFilterChange: (value: string) => void
  onCreatePolicy: () => void
  bindingColumns: TableColumnsType<SkillBinding>
  bindings: SkillBinding[]
  bindingsLoading: boolean
  onCreateBinding: () => void
  governanceStatus?: GovernanceStatus
  governanceLoading: boolean
  governanceFetching: boolean
  governanceWindowHours: string
  onGovernanceWindowChange: (value: string) => void
  onRefreshGovernance: () => void
  governanceHealthColumns: TableColumnsType<GovernanceHealthCheck>
  governanceCoverageColumns: TableColumnsType<ReturnType<typeof governanceCoverageRows>[number]>
  governanceFindingColumns: TableColumnsType<GovernanceFinding>
  governanceMetricColumns: TableColumnsType<GovernanceMetricCount>
  governanceRedactionColumns: TableColumnsType<GovernanceRedactionRow>
  governanceQueueColumns: TableColumnsType<ReturnType<typeof governanceApprovalQueueRows>[number]>
  governanceTokenFindingColumns: TableColumnsType<GovernanceTokenFindingRow>
  governanceRecommendationColumns: TableColumnsType<GovernanceRecommendationAction>
  approvalColumns: TableColumnsType<ApprovalRequest>
  approvals: ApprovalRequest[]
  approvalsLoading: boolean
  approvalFilters: ApprovalFilterState
  clients: AIClient[]
  manifest?: GatewayManifest
  onApprovalFiltersChange: (filters: ApprovalFilterState) => void
  onRefreshApprovals: () => void
  expandedApprovalRowRender: (record: ApprovalRequest) => React.ReactNode
}

export function GatewayGovernanceSection(props: GatewayGovernanceSectionProps) {
  const status = props.governanceStatus

  return (
    <Tabs
      activeKey={props.activeTab}
      className="soha-resource-tabs"
      onChange={(key) => props.onTabChange(key as GatewayTabKey)}
      destroyOnHidden
      items={[
        {
          key: 'grants',
          label: 'Tool Grants',
          children: (
            <AdminTable
              shellClassName="soha-management-table-shell"
              columnSettingIconOnly
              columnSettingPlacement="header"
              rowKey="id"
              tableSize="small"
              columns={props.grantColumns}
              dataSource={props.grants}
              loading={props.grantsLoading}
              scroll={{ x: 1000 }}
              title="Tool Grants"
              headerExtra={
                <ManagementTableToolbar>
                  <Button
                    type="primary"
                    size="small"
                    icon={<PlusOutlined />}
                    disabled={!props.canCreateGrants}
                    onClick={props.onCreateGrant}
                  >
                    新增 grant
                  </Button>
                  <ManagementToolbarSearch
                    placeholder="过滤 grant"
                    value={props.grantFilter}
                    onChange={props.onGrantFilterChange}
                  />
                </ManagementTableToolbar>
              }
            />
          ),
        },
        {
          key: 'policies',
          label: 'Access Policies',
          children: (
            <Suspense fallback={null}>
              <GatewayPoliciesSection
                columns={props.policyColumns}
                policies={props.policies}
                loading={props.policiesLoading}
                canCreate={props.canCreatePolicies}
                filter={props.policyFilter}
                onFilterChange={props.onPolicyFilterChange}
                onCreate={props.onCreatePolicy}
              />
            </Suspense>
          ),
        },
        {
          key: 'bindings',
          label: 'Skill Bindings',
          children: (
            <AdminTable
              shellClassName="soha-management-table-shell"
              columnSettingIconOnly
              columnSettingPlacement="header"
              rowKey="id"
              tableSize="small"
              columns={props.bindingColumns}
              dataSource={props.bindings}
              loading={props.bindingsLoading}
              scroll={{ x: 920 }}
              title="Skill Bindings"
              headerExtra={
                <ManagementTableToolbar>
                  <Button
                    type="primary"
                    size="small"
                    icon={<PlusOutlined />}
                    disabled={!props.canCreateSkills}
                    onClick={props.onCreateBinding}
                  >
                    新增 binding
                  </Button>
                </ManagementTableToolbar>
              }
            />
          ),
        },
        {
          key: 'governance',
          label: 'Governance',
          children: (
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <ManagementQueryPanel
                collapsible={false}
                actions={
                  <ManagementRefreshButton
                    aria-label="刷新 Governance"
                    tooltip="刷新"
                    loading={props.governanceFetching}
                    onClick={props.onRefreshGovernance}
                  />
                }
              >
                <ManagementQueryField label="时间窗口" width={140}>
                  <Select
                    options={governanceWindowOptions}
                    value={props.governanceWindowHours}
                    onChange={(value) => props.onGovernanceWindowChange(String(value))}
                  />
                </ManagementQueryField>
              </ManagementQueryPanel>
              {status ? (
                <>
                  <Descriptions
                    size="small"
                    column={4}
                    bordered
                    items={[
                      {
                        key: 'status',
                        label: 'Health',
                        children: <StatusTag value={status.health.status} />,
                      },
                      {
                        key: 'message',
                        label: 'Message',
                        span: 2,
                        children: status.health.message || '-',
                      },
                      { key: 'window', label: 'Window', children: `${status.windowHours}h` },
                      {
                        key: 'generatedAt',
                        label: 'Generated',
                        children: formatDateTime(status.generatedAt),
                      },
                      { key: 'calls', label: 'Calls', children: status.metrics.totalCalls },
                      { key: 'success', label: 'Success', children: status.metrics.successCount },
                      { key: 'deny', label: 'Denied', children: status.metrics.denyCount },
                      { key: 'failure', label: 'Failures', children: status.metrics.failureCount },
                      {
                        key: 'pending',
                        label: 'Pending approvals',
                        children: status.approvals.pending,
                      },
                      {
                        key: 'approvalSla',
                        label: 'Approval SLA',
                        children: `${status.approvals.overdue} overdue / ${status.approvals.dueSoon} due soon / ${status.approvals.stalePending} stale`,
                      },
                      {
                        key: 'tokens',
                        label: 'Active tokens',
                        children: `${status.tokens.personalAccessTokens.active + status.tokens.serviceAccountTokens.active} / ${status.tokens.personalAccessTokens.total + status.tokens.serviceAccountTokens.total}`,
                      },
                      {
                        key: 'clients',
                        label: 'AI clients',
                        children: `${status.clients.active} active / ${status.clients.total} total`,
                      },
                    ]}
                  />
                  <Descriptions
                    size="small"
                    column={4}
                    bordered
                    items={[
                      {
                        key: 'expiring',
                        label: 'Token expiration',
                        children: `${status.tokens.expiredActive?.length ?? 0} expired / ${status.tokens.expiringSoon?.length ?? 0} soon`,
                      },
                      {
                        key: 'stale',
                        label: 'Token usage',
                        children: `${status.tokens.stale?.length ?? 0} stale / ${status.tokens.neverUsed?.length ?? 0} never used`,
                      },
                      {
                        key: 'lastUsed',
                        label: 'last_used tracking',
                        children: <StatusTag value={status.tokens.lastUsedTrackingState} />,
                      },
                      {
                        key: 'clientApproval',
                        label: 'Client registration',
                        children: <StatusTag value={status.clients.registrationApproval} />,
                      },
                      {
                        key: 'riskCounts',
                        label: 'Risk counts',
                        span: 2,
                        children: compactList(
                          governanceRiskCountTags(status.metrics.riskCounts),
                          4,
                        ),
                      },
                      {
                        key: 'oldestPending',
                        label: 'Oldest pending',
                        children: status.approvals.oldestPendingRequestId
                          ? `${status.approvals.oldestPendingRequestId} / ${status.approvals.oldestPendingHours ?? 0}h`
                          : '-',
                      },
                      {
                        key: 'nextDue',
                        label: 'Next due',
                        children: status.approvals.nextDueRequestId
                          ? `${status.approvals.nextDueRequestId} / ${formatDateTime(status.approvals.nextDueAt)}`
                          : '-',
                      },
                      {
                        key: 'redactionHits',
                        label: 'Redaction hits',
                        children: `${status.redaction?.totalMatches ?? 0} / ${status.redaction?.auditsWithRedaction ?? 0} audits`,
                      },
                      {
                        key: 'redactionTargets',
                        label: 'Redaction targets',
                        children: `${status.redaction?.inputAudits ?? 0} input / ${status.redaction?.outputAudits ?? 0} output`,
                      },
                    ]}
                  />
                  {status.recommendationActions?.length ? (
                    <AdminTable
                      shellClassName="soha-management-table-shell"
                      columnSettingIconOnly
                      columnSettingPlacement="header"
                      rowKey={(record) =>
                        `${record.type}:${record.action}:${record.targetKind || ''}:${record.targetId || ''}`
                      }
                      tableSize="small"
                      title="Recommendation actions"
                      columns={props.governanceRecommendationColumns}
                      dataSource={status.recommendationActions}
                      pagination={{ pageSize: 6 }}
                      scroll={{ x: 1120 }}
                    />
                  ) : status.recommendations?.length ? (
                    <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                      {status.recommendations.map((item) => (
                        <Alert key={item} type="warning" showIcon title={item} />
                      ))}
                    </Space>
                  ) : (
                    <Alert
                      type="success"
                      showIcon
                      title="AI Gateway governance controls are healthy"
                    />
                  )}
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey="name"
                    tableSize="small"
                    columns={props.governanceHealthColumns}
                    dataSource={status.health.checks ?? []}
                    loading={props.governanceLoading}
                    pagination={false}
                    scroll={{ x: 860 }}
                  />
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey="key"
                    tableSize="small"
                    columns={props.governanceCoverageColumns}
                    dataSource={governanceCoverageRows(status.policyCoverage)}
                    pagination={false}
                    scroll={{ x: 760 }}
                  />
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey="key"
                    tableSize="small"
                    title="Redaction hits"
                    columns={props.governanceRedactionColumns}
                    dataSource={governanceRedactionRows(status.redaction)}
                    pagination={false}
                    scroll={{ x: 820 }}
                  />
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey="key"
                    tableSize="small"
                    title="Token findings"
                    columns={props.governanceTokenFindingColumns}
                    dataSource={governanceTokenFindingRows(status.tokens)}
                    pagination={{ pageSize: 6 }}
                    scroll={{ x: 1160 }}
                  />
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey="key"
                    tableSize="small"
                    columns={props.governanceQueueColumns}
                    dataSource={governanceApprovalQueueRows(status.approvals, status.clients)}
                    pagination={false}
                    scroll={{ x: 720 }}
                  />
                  <div className="grid gap-3 lg:grid-cols-3">
                    <AdminTable
                      shellClassName="soha-management-table-shell"
                      columnSettingIconOnly
                      columnSettingPlacement="header"
                      rowKey="key"
                      tableSize="small"
                      title="Top tools"
                      columns={props.governanceMetricColumns}
                      dataSource={status.metrics.topTools ?? []}
                      pagination={false}
                    />
                    <AdminTable
                      shellClassName="soha-management-table-shell"
                      columnSettingIconOnly
                      columnSettingPlacement="header"
                      rowKey="key"
                      tableSize="small"
                      title="Top AI clients"
                      columns={props.governanceMetricColumns}
                      dataSource={status.metrics.topAiClients ?? []}
                      pagination={false}
                    />
                    <AdminTable
                      shellClassName="soha-management-table-shell"
                      columnSettingIconOnly
                      columnSettingPlacement="header"
                      rowKey="key"
                      tableSize="small"
                      title="Top actors"
                      columns={props.governanceMetricColumns}
                      dataSource={status.metrics.topActors ?? []}
                      pagination={false}
                    />
                  </div>
                  <AdminTable
                    shellClassName="soha-management-table-shell"
                    columnSettingIconOnly
                    columnSettingPlacement="header"
                    rowKey={(record) =>
                      `${record.type}:${record.policyId || record.grantId || record.approvalRequestId || record.actorId || record.aiClientId || record.toolName || record.summary}`
                    }
                    tableSize="small"
                    columns={props.governanceFindingColumns}
                    dataSource={status.anomalies ?? []}
                    pagination={{ pageSize: 8 }}
                    scroll={{ x: 1180 }}
                  />
                </>
              ) : (
                <AdminTable
                  shellClassName="soha-management-table-shell"
                  columnSettingIconOnly
                  columnSettingPlacement="header"
                  rowKey="name"
                  tableSize="small"
                  columns={props.governanceHealthColumns}
                  dataSource={[]}
                  loading={props.governanceLoading}
                  pagination={false}
                  empty={
                    <ManagementState
                      bordered={false}
                      compact
                      title="暂无治理状态"
                      description="治理状态生成后会展示健康检查和建议动作。"
                    />
                  }
                />
              )}
            </Space>
          ),
        },
        {
          key: 'approvals',
          label: 'Approvals',
          children: (
            <Suspense fallback={null}>
              <GatewayApprovalsSection
                columns={props.approvalColumns}
                approvals={props.approvals}
                loading={props.approvalsLoading}
                filters={props.approvalFilters}
                clients={props.clients}
                manifest={props.manifest}
                expandedRowRender={props.expandedApprovalRowRender}
                onFiltersChange={props.onApprovalFiltersChange}
                onRefresh={props.onRefreshApprovals}
              />
            </Suspense>
          ),
        },
      ]}
    />
  )
}
