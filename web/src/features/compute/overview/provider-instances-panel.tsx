import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { App, Button, Card, Space, Typography } from 'antd'
import { ReloadOutlined, SyncOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import { computeMutations } from '../mutations'
import { ComputeMutationFeedback } from '../feedback'
import { computeQueries } from '../queries'

const { Text } = Typography

export function ProviderInstancesPanel({
  canViewTask = false,
  canDiscover,
  canTest,
  enabled,
  localeCode,
}: {
  canViewTask?: boolean
  canDiscover: boolean
  canTest: boolean
  enabled: boolean
  localeCode: 'zh_CN' | 'en_US'
}) {
  const { modal } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const instancesQuery = useQuery(computeQueries.providerInstances({ limit: 50 }, enabled))
  const healthMutation = useMutation(computeMutations.checkProviderHealth(queryClient))
  const discoveryMutation = useMutation(computeMutations.discoverProvider(queryClient))
  const items = readableQueryData(instancesQuery)?.items ?? []

  const openTask = (domain: string, taskId: string) => {
    navigate(
      `/compute/tasks/operations?domain=${encodeURIComponent(domain)}&taskId=${encodeURIComponent(taskId)}&view=logs`,
    )
  }

  return (
    <Card
      classNames={{ header: 'soha-compute-panel-header', body: 'soha-compute-panel-body' }}
      className="soha-overview-panel-card soha-compute-provider-instances"
      title={
        <h2 className="soha-compute-section-heading">
          {localeCode === 'zh_CN' ? '提供方实例' : 'Provider instances'}
        </h2>
      }
      extra={
        <ManagementRefreshButton
          aria-label={localeCode === 'zh_CN' ? '刷新提供方实例' : 'Refresh provider instances'}
          loading={instancesQuery.isFetching}
          tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
          onClick={() => void instancesQuery.refetch()}
        />
      }
    >
      <ReadFeedback
        query={instancesQuery}
        label={localeCode === 'zh_CN' ? '提供方实例' : 'Provider instances'}
      />
      <ComputeMutationFeedback
        operations={[
          {
            label: localeCode === 'zh_CN' ? '连接检查' : 'Connection check',
            mutation: healthMutation,
            readOnly: true,
            target: healthMutation.variables?.instanceRef,
            receipt: {
              message:
                localeCode === 'zh_CN'
                  ? `检查结果：${healthMutation.data?.data.healthy ? '连接正常' : '连接异常'}`
                  : `Check result: ${healthMutation.data?.data.healthy ? 'connected' : 'connection failed'}`,
            },
          },
          {
            label: localeCode === 'zh_CN' ? '资源发现与同步' : 'Resource discovery',
            mutation: discoveryMutation,
            target: discoveryMutation.variables?.instanceRef,
            receipt: {
              message: localeCode === 'zh_CN' ? '发现任务已提交' : 'Discovery task submitted',
              taskId: discoveryMutation.data?.data.id,
              domain: discoveryMutation.data?.data.domain,
              canViewTask,
            },
          },
        ]}
      />
      {instancesQuery.isLoading ? (
        <ManagementState bordered={false} compact kind="loading" />
      ) : instancesQuery.isError && !readableQueryData(instancesQuery) ? null : items.length ===
        0 ? (
        <ManagementState
          bordered={false}
          compact
          kind="empty"
          title={localeCode === 'zh_CN' ? '暂无提供方实例' : 'No provider instances'}
        />
      ) : (
        <div className="soha-compute-provider-instance-list">
          {items.map((instance) => {
            const canInspect = instance.snapshot.domain === 'virtualization'
            const busy =
              (healthMutation.isPending &&
                healthMutation.variables?.instanceRef === instance.instanceRef) ||
              (discoveryMutation.isPending &&
                discoveryMutation.variables?.instanceRef === instance.instanceRef)
            return (
              <div
                className="soha-compute-provider-instance-row"
                key={`${instance.snapshot.domain}:${instance.snapshot.providerKey}:${instance.instanceRef}`}
              >
                <div className="soha-compute-provider-instance-main">
                  <Space size={6} wrap>
                    <Text strong>{instance.displayName}</Text>
                    <StatusTag value={instance.health.status} />
                  </Space>
                  <Text type="secondary" className="soha-compute-provider-instance-meta">
                    {instance.snapshot.providerKey} ·{' '}
                    {instance.accessMode === 'agent_proxy'
                      ? localeCode === 'zh_CN'
                        ? 'Agent 代理'
                        : 'Agent proxy'
                      : localeCode === 'zh_CN'
                        ? '直连'
                        : 'Direct'}
                  </Text>
                </div>
                <div className="soha-compute-provider-observed">
                  <span>{localeCode === 'zh_CN' ? '最近观测' : 'Last observed'}</span>
                  <time dateTime={instance.lastObservedAt || undefined}>
                    {formatDateTime(instance.lastObservedAt)}
                  </time>
                </div>

                {canInspect && (canTest || canDiscover) ? (
                  <Space size={8} wrap className="soha-compute-provider-actions">
                    {canTest ? (
                      <Button
                        type="text"
                        aria-label={
                          localeCode === 'zh_CN' ? '检查连接健康' : 'Check connection health'
                        }
                        icon={<ReloadOutlined />}
                        disabled={healthMutation.isPending || discoveryMutation.isPending}
                        loading={busy}
                        size="small"
                        onClick={() =>
                          healthMutation.mutate(
                            {
                              domain: instance.snapshot.domain,
                              providerKey: instance.snapshot.providerKey,
                              instanceRef: instance.instanceRef,
                              input: { expectedGeneration: instance.snapshot.generation },
                            },
                            {
                              onSuccess: ({ data }) => {
                                const healthy = data.healthy
                                modal[healthy ? 'success' : 'error']({
                                  title: `${instance.displayName} · ${healthy ? (localeCode === 'zh_CN' ? '连接正常' : 'Connected') : localeCode === 'zh_CN' ? '连接异常' : 'Connection failed'}`,
                                  content: [data.message, data.reason, data.nextAction]
                                    .filter(Boolean)
                                    .join(' · '),
                                })
                              },
                            },
                          )
                        }
                      >
                        {localeCode === 'zh_CN' ? '检查' : 'Check'}
                      </Button>
                    ) : null}
                    {canDiscover ? (
                      <Button
                        type="text"
                        aria-label={
                          localeCode === 'zh_CN' ? '发现并同步资源' : 'Discover resources'
                        }
                        icon={<SyncOutlined />}
                        disabled={healthMutation.isPending || discoveryMutation.isPending}
                        loading={busy}
                        size="small"
                        onClick={() =>
                          discoveryMutation.mutate(
                            {
                              domain: instance.snapshot.domain,
                              providerKey: instance.snapshot.providerKey,
                              instanceRef: instance.instanceRef,
                              input: {
                                expectedGeneration: instance.snapshot.generation,
                                maxItems: 1000,
                              },
                            },
                            {
                              onSuccess: (result) => {
                                if (canViewTask) openTask(result.data.domain, result.data.id)
                              },
                            },
                          )
                        }
                      >
                        {localeCode === 'zh_CN' ? '同步' : 'Sync'}
                      </Button>
                    ) : null}
                  </Space>
                ) : null}
              </div>
            )
          })}
        </div>
      )}
    </Card>
  )
}
