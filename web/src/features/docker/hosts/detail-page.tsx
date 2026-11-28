import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'

import { useState } from 'react'
import { Button, Card, Descriptions, Space, Tabs } from 'antd'
import { EditOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useParams } from 'react-router-dom'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import {
  ComputeRelationsPanel,
  computeQueries,
  latestTaskForResource,
  ResourceTaskActions,
} from '@/features/compute'
import { useAIPageContext } from '@/features/copilot'
import { localeText, useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { dockerQueries } from '../queries'
import {
  architectureTag,
  DockerTableHeader,
  formatBytes,
  statusTag,
  useDockerPermissions,
} from '../shared/ui'
import { RuntimeHostModal } from './create-page'

export function DockerHostDetailPage() {
  const { id = '' } = useParams()
  const { localeCode } = useI18n()
  const [editing, setEditing] = useState(false)
  const { dockerModuleEnabled, canUpdateHosts, canViewOperations } = useDockerPermissions()
  const hostQuery = useQuery(dockerQueries.host(id, dockerModuleEnabled))
  const tasksQuery = useQuery({
    ...computeQueries.tasks({
      domain: 'container_runtime',
      resourceKind: 'runtime_host',
      resourceId: id,
      limit: 15,
    }),
    enabled: dockerModuleEnabled && canViewOperations && Boolean(id),
  })
  const host = readableQueryData(hostQuery)
  const latestTask = latestTaskForResource(
    canViewOperations ? (readableQueryData(tasksQuery)?.items ?? []) : [],
    'runtime_host',
    id,
  )

  useAIPageContext({
    sourceWorkbench: 'compute',
    sourceTitle: host?.name
      ? localeText(localeCode, `运行时主机 ${host.name}`, `Runtime host ${host.name}`)
      : localeText(localeCode, '运行时主机详情', 'Runtime host details'),
    entityKind: 'docker.host',
    entityName: host?.name ?? id,
    dockerHostId: id,
    pinnedData: {
      status: host?.status,
      environment: host?.environment,
      vmId: host?.vmId,
    },
  })

  if (!id) return <Navigate to="/compute/runtimes/hosts" replace />

  return (
    <div className="soha-page soha-docker-page">
      <ReadFeedback
        query={hostQuery}
        label={localeText(localeCode, '运行时主机', 'Runtime host')}
      />
      <DockerTableHeader
        title={host?.name || localeText(localeCode, '运行时主机', 'Runtime host')}
        status={host?.status}
        tone={host?.status === 'online' || host?.status === 'ready' ? 'success' : 'warning'}
        meta={[
          host?.environment || localeText(localeCode, '未设置环境', 'Environment not set'),
          host?.endpoint || host?.ipAddress || id,
        ]}
        actions={
          <Space>
            <ManagementRefreshButton
              tooltip={localeText(localeCode, '刷新', 'Refresh')}
              aria-label={localeText(localeCode, '刷新主机', 'Refresh host')}
              loading={hostQuery.isFetching}
              onClick={() => void hostQuery.refetch()}
            />
            {canUpdateHosts && host ? (
              <Button icon={<EditOutlined />} onClick={() => setEditing(true)}>
                {localeText(localeCode, '编辑', 'Edit')}
              </Button>
            ) : null}
          </Space>
        }
      />
      {!host && hostQuery.isError ? null : !host && !hostQuery.isLoading ? (
        <Card size="small" variant="outlined" className="soha-management-panel-card">
          <ManagementState
            compact
            kind="not-found"
            title={localeText(localeCode, '未找到运行时主机', 'Runtime host not found')}
            description={localeText(
              localeCode,
              '目标主机不存在，或当前账号无法访问该资源。',
              'The host does not exist or your account cannot access it.',
            )}
          />
        </Card>
      ) : (
        <Tabs
          size="small"
          className="soha-resource-tabs soha-workload-detail-tabs"
          items={[
            {
              key: 'overview',
              label: localeText(localeCode, '概览', 'Overview'),
              children: (
                <div className="soha-detail-stack">
                  <Card size="small" className="soha-detail-card" loading={hostQuery.isLoading}>
                    <dl className="soha-docker-spec-tiles">
                      <div>
                        <dt>CPU</dt>
                        <dd>
                          {host?.cpuCoreCount
                            ? localeText(
                                localeCode,
                                `${host.cpuCoreCount} 核`,
                                `${host.cpuCoreCount} cores`,
                              )
                            : '-'}
                        </dd>
                      </div>
                      <div>
                        <dt>{localeText(localeCode, '内存', 'Memory')}</dt>
                        <dd>{formatBytes(host?.memoryBytes)}</dd>
                      </div>
                      <div>
                        <dt>{localeText(localeCode, '磁盘', 'Disk')}</dt>
                        <dd>{formatBytes(host?.diskBytes)}</dd>
                      </div>
                      <div>
                        <dt>{localeText(localeCode, '端口池', 'Port pool')}</dt>
                        <dd>
                          {host?.availablePortStart && host?.availablePortEnd
                            ? `${host.availablePortStart}-${host.availablePortEnd}`
                            : '-'}
                        </dd>
                      </div>
                    </dl>
                  </Card>
                  <div className="soha-docker-overview-grid">
                    <Card
                      size="small"
                      className="soha-detail-card"
                      loading={hostQuery.isLoading}
                      title={localeText(localeCode, '主机与节点归属', 'Host & node ownership')}
                    >
                      <Descriptions size="small" column={1}>
                        <Descriptions.Item label={localeText(localeCode, '状态', 'Status')}>
                          {statusTag(host?.status)}
                        </Descriptions.Item>
                        <Descriptions.Item label={localeText(localeCode, '架构', 'Architecture')}>
                          {architectureTag(host?.architecture)}
                        </Descriptions.Item>
                        <Descriptions.Item label="Endpoint">
                          {host?.endpoint || host?.ipAddress || '-'}
                        </Descriptions.Item>
                        <Descriptions.Item
                          label={localeText(localeCode, '虚拟机', 'Virtual machine')}
                        >
                          {host?.vmId ? (
                            <Link
                              to={`/compute/virtualization/vms/${encodeURIComponent(host.vmId)}`}
                            >
                              {host.vmName || host.vmId}
                            </Link>
                          ) : (
                            '-'
                          )}
                        </Descriptions.Item>
                        <Descriptions.Item label={localeText(localeCode, '环境', 'Environment')}>
                          {host?.environment || '-'}
                        </Descriptions.Item>
                        <Descriptions.Item label={localeText(localeCode, '归属', 'Owner')}>
                          {host?.owner || host?.team || '-'}
                        </Descriptions.Item>
                      </Descriptions>
                    </Card>
                    <Card
                      size="small"
                      className="soha-detail-card"
                      loading={hostQuery.isLoading}
                      title={localeText(
                        localeCode,
                        '引擎版本与连接心跳',
                        'Runtime engine & heartbeat',
                      )}
                    >
                      <Descriptions size="small" column={1}>
                        <Descriptions.Item label="Docker">
                          {host?.dockerVersion || '-'}
                        </Descriptions.Item>
                        <Descriptions.Item label="Compose">
                          {host?.composeVersion || '-'}
                        </Descriptions.Item>
                        <Descriptions.Item label="Agent">
                          {host?.agentVersion || '-'}
                        </Descriptions.Item>
                        <Descriptions.Item
                          label={localeText(localeCode, '虚拟化连接', 'Virtualization connection')}
                        >
                          {host?.virtualizationConnectionId || '-'}
                        </Descriptions.Item>
                        <Descriptions.Item
                          label={localeText(localeCode, '最近心跳', 'Last heartbeat')}
                        >
                          {formatDateTime(host?.lastHeartbeatAt)}
                        </Descriptions.Item>
                        <Descriptions.Item label={localeText(localeCode, '主机 ID', 'Host ID')}>
                          {host?.id || id}
                        </Descriptions.Item>
                      </Descriptions>
                    </Card>
                  </div>
                </div>
              ),
            },
            {
              key: 'relations',
              label: localeText(localeCode, '资源关系', 'Relations'),
              children: (
                <Card size="small" className="soha-detail-card">
                  <ComputeRelationsPanel
                    domain="container_runtime"
                    kind="runtime_host"
                    resourceId={id}
                  />
                </Card>
              ),
            },
            ...(canViewOperations
              ? [
                  {
                    key: 'tasks',
                    label: localeText(localeCode, '最近任务', 'Latest task'),
                    children: (
                      <Card
                        size="small"
                        className="soha-detail-card"
                        extra={
                          <Link
                            to={`/compute/tasks/operations?domain=container_runtime&resourceKind=runtime_host&resourceId=${encodeURIComponent(id)}`}
                          >
                            {localeText(localeCode, '查看任务中心', 'Open task center')}
                          </Link>
                        }
                      >
                        <ReadFeedback
                          query={tasksQuery}
                          label={localeText(localeCode, '最近任务', 'Latest tasks')}
                        />
                        <ResourceTaskActions
                          task={latestTask}
                          resourceKind="runtime_host"
                          resourceId={id}
                        />
                      </Card>
                    ),
                  },
                ]
              : []),
          ]}
        />
      )}
      <RuntimeHostModal
        editing={host ?? null}
        open={editing && Boolean(host) && canUpdateHosts}
        onClose={() => setEditing(false)}
      />
    </div>
  )
}
