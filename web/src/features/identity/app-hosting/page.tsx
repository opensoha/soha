import { ContextFeedback } from '@/components/context-feedback'
import { ManagementState } from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Tabs } from 'antd'
import { useState } from 'react'
import { VisibilityApprovalsPanel } from './components/approvals-panel'
import { GlobalAppLedgerPanel } from './components/ledger-panel'
import { GovernanceSettingsPanel } from './components/settings-panel'
import { appHostingAdminQueries } from './queries'
import './styles.css'

export type AppHostingGovernanceTabKey = 'ledger' | 'approvals' | 'settings'

function resolveInitialTab(): AppHostingGovernanceTabKey {
  if (typeof window === 'undefined') return 'ledger'
  const tab = new URLSearchParams(window.location.search).get('tab')
  if (tab === 'approvals' || tab === 'settings' || tab === 'ledger') {
    return tab
  }
  return 'ledger'
}

export function InternalWorkbenchAppHostingPage() {
  const snapshotQuery = usePermissionSnapshot()
  const snapshot = snapshotQuery.data?.data
  const currentUserId = useAuthStore((state) => state.user?.userId ?? '')

  const canView =
    hasPermission(snapshot, 'apphosting.admin.view') ||
    hasPermission(snapshot, 'apphosting.admin.manage')
  const canManage = hasPermission(snapshot, 'apphosting.admin.manage')
  const canViewAudit =
    hasPermission(snapshot, 'system.audit.view') || hasPermission(snapshot, 'identity.audit.view')
  const canViewStorageIntegrations = hasPermission(snapshot, 'settings.system-integrations.view')

  const [activeTab, setActiveTab] = useState<AppHostingGovernanceTabKey>(resolveInitialTab)

  const pendingSummaryQuery = useQuery(
    appHostingAdminQueries.approvals({ status: 'pending', limit: 1, offset: 0 }, canView),
  )
  const pendingTotal =
    (pendingSummaryQuery.isEnabled === false ? undefined : readableQueryData(pendingSummaryQuery))
      ?.total ?? 0

  if (snapshotQuery.isLoading) {
    return <ManagementState kind="loading" />
  }

  if (!canView) {
    return (
      <ManagementState
        kind="no-permission"
        title="无权限访问应用托管治理"
        description="当前账号缺少应用托管后台权限（apphosting.admin.view 或 apphosting.admin.manage）。"
      />
    )
  }

  return (
    <div className="soha-identity-app-hosting-page">
      {!canManage ? (
        <ContextFeedback
          type="info"
          title="当前为只读治理视角"
          description="您拥有查看权限（apphosting.admin.view），可查阅全局应用台账、公开审批记录与平台策略；强制封停、归属转交、审批处置与策略修改需要 apphosting.admin.manage 权限。"
        />
      ) : null}

      <Tabs
        activeKey={activeTab}
        className="soha-resource-tabs is-header-only"
        items={[
          {
            key: 'ledger',
            label: '全局应用台账',
          },
          {
            key: 'approvals',
            label: pendingTotal > 0 ? `全公司公开审批（${pendingTotal}）` : '全公司公开审批',
          },
          {
            key: 'settings',
            label: '基础设施与策略设置',
          },
        ]}
        onChange={(nextKey) => setActiveTab(nextKey as AppHostingGovernanceTabKey)}
      />

      {activeTab === 'ledger' ? (
        <GlobalAppLedgerPanel canManage={canManage} canView={canView} canViewAudit={canViewAudit} />
      ) : null}

      {activeTab === 'approvals' ? (
        <VisibilityApprovalsPanel
          canManage={canManage}
          canView={canView}
          currentUserId={currentUserId}
        />
      ) : null}

      {activeTab === 'settings' ? (
        <GovernanceSettingsPanel
          canManage={canManage}
          canView={canView}
          canViewStorageIntegrations={canViewStorageIntegrations}
        />
      ) : null}
    </div>
  )
}
