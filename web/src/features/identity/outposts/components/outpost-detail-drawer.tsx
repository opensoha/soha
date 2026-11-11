import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Descriptions, Drawer, Space, Tabs, Typography } from 'antd'
import { Link } from 'react-router-dom'
import { identityApplicationQueries } from '../../applications'
import { identityProviderQueries, type IdentityProvider } from '../../providers'
import { outpostDeploymentConfig, providersForOutpost } from '../deployment-model'
import { identityOutpostQueries } from '../queries'
import {
  outpostConfigurationSummary,
  outpostRuntimeLabels,
  outpostRuntimeReason,
} from '../runtime-model'
import type { IdentityOutpost } from '../types'
const nextSteps: Record<string, string> = {
  awaiting_registration: '按照部署页安装 Agent，并注入本节点的控制面 token。',
  awaiting_heartbeat: '节点已注册，等待首次配置应用与心跳；如持续未收到，请检查 Agent 日志。',
  heartbeat_stale: '检查节点进程和到控制面的网络连接。',
  heartbeat_timeout: '节点心跳已过期；检查节点进程、网络与 token。',
  configuration_rejected: '检查固定的签名公钥、协议版本和节点时间；有效配置应用前保持拒绝访问。',
  configuration_expired: '配置租约已过期，检查控制面连通性与节点时间。',
  signing_key_unconfigured: '在控制面配置 Outpost 签名密钥后再部署远程节点。',
  unsupported_protocol: '升级到支持当前协议版本的 Agent。',
  configuration_pending: '等待节点应用最新配置；如持续不收敛，查看 Agent 日志。',
  legacy_runtime: '升级节点，使用签名配置与租约观测。',
}
export function OutpostDetailDrawer({
  id,
  initialTab = 'overview',
  onClose,
  onEdit,
}: {
  id: string
  initialTab?: string
  onClose: () => void
  onEdit: (outpost: IdentityOutpost) => void
}) {
  const snapshot = usePermissionSnapshot().data?.data
  const canViewProviders = hasPermission(snapshot, 'identity.providers.view')
  const query = useQuery({
    ...identityOutpostQueries.detail(id),
    refetchInterval: (state) => (state.state.error ? false : 15000),
  })
  const visibleQueryData = query.isEnabled === false ? undefined : readableQueryData(query)
  const providers = useQuery({
    ...identityProviderQueries.list({ type: 'proxy' }),
    enabled: Boolean(id) && canViewProviders,
  })
  const visibleProvidersData =
    providers.isEnabled === false ? undefined : readableQueryData(providers)
  const apps = useQuery({
    ...identityApplicationQueries.list({}),
    enabled: Boolean(id) && hasPermission(snapshot, 'identity.applications.view'),
  })
  const visibleAppsData = apps.isEnabled === false ? undefined : readableQueryData(apps)
  const outpost = visibleQueryData
  const linked = providersForOutpost(visibleProvidersData ?? [], id)
  const config = outpost && outpostDeploymentConfig(outpost)
  return (
    <Drawer
      open={Boolean(id)}
      onClose={onClose}
      size={860}
      destroyOnHidden
      title={outpost?.name ?? '接入节点'}
      extra={
        outpost && (
          <Button
            disabled={!hasPermission(snapshot, 'identity.outposts.update')}
            onClick={() => onEdit(outpost)}
          >
            编辑
          </Button>
        )
      }
    >
      <ReadFeedback canRead={query.isEnabled} query={query} label="当前记录" />
      <ReadFeedback canRead={providers.isEnabled} query={providers} label="身份数据" />
      <ReadFeedback canRead={apps.isEnabled} query={apps} label="身份数据" />

      {!visibleQueryData && query.isError && !visibleQueryData ? null : !outpost ? (
        <ManagementState kind="loading" title="正在读取接入节点" />
      ) : (
        <Tabs
          key={`${id}:${initialTab}`}
          className="soha-resource-tabs"
          defaultActiveKey={
            ['overview', 'deploy', 'apps', 'diagnostics'].includes(initialTab)
              ? initialTab
              : 'overview'
          }
          items={[
            {
              key: 'overview',
              label: '概览',
              children: (
                <div className="soha-identity-outpost-detail-tab">
                  {outpost.runtimeReason && nextSteps[outpost.runtimeReason] && (
                    <Alert showIcon type="info" title={nextSteps[outpost.runtimeReason]} />
                  )}
                  <Descriptions
                    bordered
                    size="small"
                    styles={{ label: { width: 150 }, content: { overflowWrap: 'anywhere' } }}
                    column={1}
                    items={[
                      {
                        key: 'status',
                        label: '状态',
                        children: (
                          <StatusTag
                            value={outpost.runtimeStatus}
                            label={outpostRuntimeLabels[outpost.runtimeStatus]}
                          />
                        ),
                      },
                      {
                        key: 'id',
                        label: 'ID',
                        children: <Typography.Text copyable>{outpost.id}</Typography.Text>,
                      },
                      { key: 'mode', label: '部署方式', children: outpost.mode },
                      {
                        key: 'config',
                        label: '配置同步',
                        children: outpostConfigurationSummary(outpost),
                      },
                      ...(outpost.mode === 'embedded'
                        ? []
                        : [
                            {
                              key: 'desired',
                              label: '期望配置版本',
                              children: outpost.configurationVersion || '尚未下发',
                            },
                            {
                              key: 'applied',
                              label: '已应用配置版本',
                              children: outpost.appliedConfigurationVersion || '尚未应用',
                            },
                            {
                              key: 'heartbeat',
                              label: '最近心跳',
                              children: outpost.lastHeartbeatAt || '尚未收到',
                            },
                            {
                              key: 'expiry',
                              label: '配置租约',
                              children: outpost.configurationExpiresAt || '尚未签发',
                            },
                          ]),
                    ]}
                  />
                </div>
              ),
            },
            {
              key: 'deploy',
              label: '部署',
              children:
                outpost.mode === 'embedded' ? (
                  <Alert
                    showIcon
                    type={outpost.runtimeStatus === 'available' ? 'success' : 'warning'}
                    title="由当前 Soha 实例提供内置鉴权"
                    description={outpostRuntimeReason(outpost.runtimeReason)}
                  />
                ) : (
                  <div className="soha-identity-outpost-detail-tab">
                    <Descriptions
                      bordered
                      size="small"
                      styles={{ label: { width: 150 }, content: { overflowWrap: 'anywhere' } }}
                      column={1}
                      items={[
                        {
                          key: 'control',
                          label: '控制面地址',
                          children: outpost.deployment?.controlPlaneUrl || '尚未配置',
                        },
                        {
                          key: 'auth',
                          label: 'Forward Auth 地址',
                          children: outpost.forwardAuthUrl || '尚未配置，请编辑节点',
                        },
                        {
                          key: 'trust',
                          label: '签名 Key ID',
                          children: outpost.deployment?.trustKeyId || '尚未配置',
                        },
                        {
                          key: 'public',
                          label: '信任公钥',
                          children: outpost.deployment?.trustPublicKey ? (
                            <Typography.Text copyable style={{ overflowWrap: 'anywhere' }}>
                              {outpost.deployment.trustPublicKey}
                            </Typography.Text>
                          ) : (
                            '尚未配置'
                          ),
                        },
                      ]}
                    />
                    {!config ? (
                      <Alert
                        showIcon
                        type="warning"
                        title="部署资料不完整"
                        description="请先配置 Soha 公开地址和 Outpost 签名密钥。"
                      />
                    ) : (
                      <>
                        <ol>
                          <li>选择支持本页协议版本的 soha-agent 发布版，并固定镜像版本。</li>
                          <li>
                            通过密钥管理系统挂载两个独立文件：agent-token
                            用于边缘代理访问节点；control-plane-token 使用创建或轮换本节点时返回的
                            token。
                          </li>
                          <li>
                            保存下列配置并按安装入口启动。检查就绪状态、配置同步和心跳后，再应用
                            Provider 的代理配置。
                          </li>
                        </ol>
                        <Typography.Paragraph copyable={{ text: config }}>
                          <pre className="soha-json-block">{config}</pre>
                        </Typography.Paragraph>
                      </>
                    )}
                    <Space wrap>
                      <Typography.Link
                        href="https://github.com/opensoha/soha-agent/blob/main/deploy/kubernetes/outpost/README.md"
                        target="_blank"
                        rel="noreferrer"
                      >
                        Agent / Kubernetes 安装
                      </Typography.Link>
                      <Typography.Link
                        href="https://github.com/opensoha/soha-helm/tree/main/charts/soha-agent"
                        target="_blank"
                        rel="noreferrer"
                      >
                        Helm 安装
                      </Typography.Link>
                    </Space>
                  </div>
                ),
            },
            {
              key: 'apps',
              label: '关联应用',
              children: !canViewProviders ? (
                <ManagementState kind="no-permission" title="没有查看认证绑定的权限" />
              ) : providers.isError && !visibleProvidersData ? (
                <ManagementState
                  kind="error"
                  title="关联应用加载失败"
                  actions={
                    <Button
                      onClick={() =>
                        void (providers.isEnabled === false ? undefined : providers.refetch())
                      }
                    >
                      重试
                    </Button>
                  }
                />
              ) : providers.isPending ? (
                <ManagementState kind="loading" title="正在读取关联应用" />
              ) : linked.length ? (
                <AdminTable
                  rowKey="id"
                  pagination={false}
                  enableColumnSelection={false}
                  columns={[
                    {
                      title: '应用',
                      key: 'application',
                      render: (_: unknown, provider: IdentityProvider) => (
                        <Link
                          to={
                            '/identity/applications?application=' +
                            encodeURIComponent(provider.applicationId)
                          }
                        >
                          {visibleAppsData?.find((app) => app.id === provider.applicationId)
                            ?.name ?? provider.applicationId}
                        </Link>
                      ),
                    },
                    {
                      title: '认证接入',
                      key: 'provider',
                      render: (_: unknown, provider: IdentityProvider) => (
                        <Link
                          to={'/identity/providers?provider=' + encodeURIComponent(provider.id)}
                        >
                          {provider.name}
                        </Link>
                      ),
                    },
                  ]}
                  dataSource={linked}
                />
              ) : (
                <ManagementState kind="empty" title="暂无显式绑定的应用" />
              ),
            },
            {
              key: 'diagnostics',
              label: '诊断',
              children: (
                <Descriptions
                  bordered
                  size="small"
                  styles={{ label: { width: 150 }, content: { overflowWrap: 'anywhere' } }}
                  column={1}
                  items={[
                    ...(outpost.mode === 'embedded'
                      ? []
                      : [
                          {
                            key: 'registered',
                            label: '注册 Agent',
                            children: outpost.claimedAgentId || '尚未注册',
                          },
                          {
                            key: 'protocol',
                            label: '已报告协议',
                            children: outpost.protocolVersion || '尚未报告',
                          },
                          {
                            key: 'expectedProtocol',
                            label: '期望协议',
                            children: outpost.deployment?.protocolVersion || '尚未配置',
                          },
                          {
                            key: 'version',
                            label: '程序版本',
                            children: outpost.runtimeVersion || '尚未报告',
                          },
                        ]),
                    {
                      key: 'reason',
                      label: '运行原因',
                      children: outpostRuntimeReason(outpost.runtimeReason),
                    },
                    {
                      key: 'boundary',
                      label: '授权边界',
                      children: '节点健康不替代请求授权；配置签名、版本与租约无效时拒绝访问。',
                    },
                  ]}
                />
              ),
            },
          ]}
        />
      )}
    </Drawer>
  )
}
