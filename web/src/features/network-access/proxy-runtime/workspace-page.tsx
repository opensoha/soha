import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Tabs, Typography } from 'antd'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import type { NetworkProxyInstance } from '@opensoha/contracts/gen/ts/sohaapi'
import { proxyQueries } from './queries'
import { workspaceKeys } from './workspace-queries'
import { SourcesPanel } from './sources-panel'
import { RuntimePanel } from './runtime-panel'
import { RulesPanel } from './rules-panel'
import { DeploymentPanel } from './deployment-panel'
import './styles.css'
export default function ProxyWorkspacePage() {
  const { instanceID = '' } = useParams()
  const permissions = usePermissionSnapshot()
  const canRead = hasPermission(permissions.data?.data, 'network_access.proxy_instances.view')
  const canUpdate = hasPermission(permissions.data?.data, 'network_access.proxy_instances.update')
  const client = useQueryClient()
  const instances = useQuery(proxyQueries.instances({}, canRead))
  useEffect(() => {
    if (!canRead) {
      void client.cancelQueries({ queryKey: workspaceKeys.all })
      client.removeQueries({ queryKey: workspaceKeys.all })
    }
  }, [canRead, client])
  const selected = canRead
    ? readableQueryData(instances)?.find((i) => i.id === instanceID)
    : undefined
  if (!canRead)
    return <ManagementState kind={permissions.isLoading ? 'loading' : 'no-permission'} />
  if (instances.isLoading) return <ManagementState kind="loading" />
  if (!selected)
    return (
      <>
        <ReadFeedback canRead query={instances} label="代理实例" />
        <ManagementState
          kind={instances.isError ? 'error' : 'empty'}
          description="未找到可访问的代理实例"
          actions={<Button onClick={() => void instances.refetch()}>重试</Button>}
        />
      </>
    )
  return (
    <main className="soha-server-proxy-page soha-proxy-workspace">
      <ReadFeedback canRead query={instances} label="代理实例" />
      <div className="soha-server-proxy-head">
        <div>
          <h1>{selected.name}</h1>
          <Typography.Text type="secondary">
            {selected.engine === 'external'
              ? (selected.engineDescriptor?.engineId ?? '外部内核')
              : selected.engine}{' '}
            · {selected.engineVersion ?? '版本未知'} · {selected.status}
          </Typography.Text>
        </div>
        <Link to="/network-access/proxy-instances">返回代理实例</Link>
      </div>
      {!canUpdate && <Alert type="info" title="当前仅可查看，配置与发布操作需要修改权限" />}
      <Workspace key={`${instanceID}:${canUpdate}`} instance={selected} canUpdate={canUpdate} />
    </main>
  )
}
function Workspace({
  instance,
  canUpdate,
}: {
  instance: NetworkProxyInstance
  canUpdate: boolean
}) {
  const [tab, setTab] = useState('sources')
  return (
    <Tabs
      style={{ minWidth: 0 }}
      activeKey={tab}
      onChange={setTab}
      items={[
        {
          key: 'sources',
          label: '来源与发布',
          children: <SourcesPanel id={instance.id} canUpdate={canUpdate} />,
        },
        {
          key: 'runtime',
          label: '节点与策略',
          children: <RuntimePanel instance={instance} canUpdate={canUpdate} />,
        },
        {
          key: 'rules',
          label: '组织规则',
          children: <RulesPanel id={instance.id} canUpdate={canUpdate} />,
        },
        {
          key: 'diagnostics',
          label: 'DNS 与诊断',
          children: <RuntimePanel instance={instance} canUpdate={canUpdate} diagnostics />,
        },
        {
          key: 'deployment',
          label: '部署',
          children: <DeploymentPanel id={instance.id} canUpdate={canUpdate} />,
        },
      ]}
    />
  )
}
