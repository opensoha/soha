import { readableQueryData } from '@/utils/query-feedback'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import { useResourceListViewport } from '@/features/platform/shared/use-resource-list-viewport'
import { KubernetesResourceActionsMenu } from '@/features/platform/shared/kubernetes-resource-actions-menu'
import { buildWorkloadDetailPath } from '@/features/platform/workloads-model'
import { useI18n } from '@/i18n'
import type { Pod, ScopeKey } from '@/types'
import { formatAgeSeconds } from '@/utils/time'
import { SearchOutlined } from '@ant-design/icons'
import {
  Button,
  Card,
  Input,
  Segmented,
  Tooltip,
  Typography,
  type TableProps,
  type TableColumnsType,
} from 'antd'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { PodContainersCell } from '../pods/pod-containers-cell'
import { PodResourceCell } from '../pods/pod-resource-cell'
import './workload-pods-table.css'

export function WorkloadPodsTable({
  pods,
  scope,
  model,
  currentRevision,
  compact = false,
  onShowAll,
  factColumns = [],
  revisionWidth = 90,
}: {
  pods: Pod[]
  scope: ScopeKey
  model?: { podOwners: Map<string, { record?: { revision: string } }> }
  currentRevision?: string
  compact?: boolean
  onShowAll?: () => void
  factColumns?: TableColumnsType<Pod>
  revisionWidth?: number
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const logs = useClusterCapability('pod.logs', localeCode)
  const exec = useClusterCapability('pod.exec', localeCode)
  const navigate = useNavigate()
  const [search, setSearch] = useState('')
  const [filter, setFilter] = useState('all')
  const owner = (pod: Pod) => model?.podOwners.get(`${pod.namespace}/${pod.name}`)
  const path = (pod: Pod, tab?: string) =>
    `${buildWorkloadDetailPath('pods', pod.name, scope.namespace, pod.namespace, scope.clusterId)}${tab ? `&tab=${tab}` : ''}`
  const abnormal = (pod: Pod) =>
    !['Running', 'Succeeded'].includes(pod.phase) ||
    (pod.phase === 'Running' &&
      pod.readyContainers.split('/')[0] !== pod.readyContainers.split('/')[1])
  const filtered = compact
    ? pods.slice(0, 3)
    : pods.filter(
        (pod) =>
          `${pod.name} ${pod.nodeName || ''} ${owner(pod)?.record?.revision || ''}`
            .toLowerCase()
            .includes(search.trim().toLowerCase()) &&
          (filter === 'all' ||
            (filter === 'current'
              ? Boolean(currentRevision && owner(pod)?.record?.revision === currentRevision)
              : abnormal(pod))),
      )
  const rowKey = (pod: Pod) => `${pod.namespace}/${pod.name}`
  const viewport = useResourceListViewport(
    filtered,
    rowKey,
    JSON.stringify([scope.clusterId, scope.namespace, search, filter]),
  )
  const onChange: NonNullable<TableProps<Pod>['onChange']> = (
    _pagination,
    _filters,
    _sorter,
    extra,
  ) => {
    if (extra.action === 'sort' || extra.action === 'filter') viewport.resetPosition()
  }
  const canView = hasPermission(permissions, 'platform.pods.view')
  const columns: TableColumnsType<Pod> = [
    {
      title: 'Pod',
      key: 'name',
      dataIndex: 'name',
      width: compact ? 300 : 320,
      fixed: 'left',
      ellipsis: true,
      sorter: (a, b) => a.name.localeCompare(b.name),
      render: (name: string, pod) =>
        canView ? (
          <Link title={name} to={path(pod)}>
            {name}
          </Link>
        ) : (
          <span title={name}>{name}</span>
        ),
    },
    ...(!compact ? factColumns : []),
    {
      title: zh ? '状态' : 'Status',
      key: 'phase',
      width: 150,
      render: (_, pod) => <StatusTag value={pod.phase} />,
    },
    ...(!compact
      ? [
          {
            title: zh ? '容器' : 'Containers',
            key: 'containers',
            width: 100,
            render: (_: unknown, pod: Pod) => (
              <PodContainersCell
                statuses={pod.containerStatuses}
                readyContainers={pod.readyContainers}
                zh={zh}
              />
            ),
          },
          ...(model
            ? [
                {
                  title: zh ? '版本' : 'Revision',
                  key: 'revision',
                  width: revisionWidth,
                  ellipsis: true,
                  render: (_: unknown, pod: Pod) => (
                    <span title={owner(pod)?.record?.revision}>
                      {owner(pod)?.record?.revision || '—'}
                    </span>
                  ),
                },
              ]
            : []),
        ]
      : []),
    {
      title: zh ? '节点' : 'Node',
      dataIndex: 'nodeName',
      key: 'node',
      width: 200,
      ellipsis: true,
      render: (node: string) =>
        node && hasPermission(permissions, 'platform.nodes.view') ? (
          <Link
            title={node}
            to={`/cluster-resources/nodes/${encodeURIComponent(node)}?clusterId=${encodeURIComponent(scope.clusterId || '')}`}
          >
            {node}
          </Link>
        ) : (
          node || '—'
        ),
    },
    {
      title: zh ? '重启' : 'Restarts',
      dataIndex: 'restarts',
      key: 'restarts',
      width: 85,
      sorter: (a, b) => a.restarts - b.restarts,
      render: (value: number) => (
        <Typography.Text type={value > 0 ? 'warning' : undefined}>{value}</Typography.Text>
      ),
    },
    ...(!compact
      ? [
          {
            title: 'CPU',
            key: 'cpu',
            width: 140,
            render: (_: unknown, pod: Pod) => (
              <PodResourceCell pod={pod} resource="cpu" localeCode={localeCode} />
            ),
          },
          {
            title: zh ? '内存' : 'Memory',
            key: 'memory',
            width: 140,
            render: (_: unknown, pod: Pod) => (
              <PodResourceCell pod={pod} resource="memory" localeCode={localeCode} />
            ),
          },
          {
            title: zh ? '时长' : 'Age',
            key: 'age',
            width: 100,
            dataIndex: 'ageSeconds',
            sorter: (a: Pod, b: Pod) => a.ageSeconds - b.ageSeconds,
            render: (value: number) => formatAgeSeconds(value),
          },
        ]
      : []),
    {
      title: '',
      key: 'actions',
      width: 64,
      fixed: 'right',
      render: (_, pod) => (
        <KubernetesResourceActionsMenu
          resourceLabel="Pod"
          name={pod.name}
          localeCode={localeCode}
          items={[
            ...(canView ? [{ key: 'detail', label: zh ? '查看 Pod' : 'View Pod' }] : []),
            ...(hasPermission(permissions, 'platform.pods.logs') &&
            hasAllowedAction(pod.allowedActions, 'logs')
              ? [{ key: 'logs', label: zh ? '查看日志' : 'View logs', disabled: logs.disabled }]
              : []),
            ...(hasPermission(permissions, 'platform.pods.exec') &&
            hasAllowedAction(pod.allowedActions, 'exec')
              ? [
                  {
                    key: 'terminal',
                    label: (
                      <Tooltip title={exec.reason}>{zh ? '打开终端' : 'Open terminal'}</Tooltip>
                    ),
                    disabled: exec.disabled || exec.status === 'partial',
                  },
                ]
              : []),
          ]}
          onSelect={(key) => navigate(path(pod, key === 'detail' ? undefined : key))}
        />
      ),
    },
  ]
  return (
    <Card
      className="soha-detail-card soha-workload-pods-table soha-deployment-pods"
      title={
        <>
          {zh ? 'Pods' : 'Pods'}{' '}
          <Typography.Text type="secondary">
            {compact ? pods.length : `${filtered.length} / ${pods.length}`}
          </Typography.Text>
        </>
      }
      extra={
        compact ? (
          <Button type="link" onClick={onShowAll}>
            {zh ? '查看全部' : 'View all'}
          </Button>
        ) : undefined
      }
    >
      {!compact && (
        <div className="soha-k8s-detail-toolbar">
          <Input
            aria-label={zh ? '搜索 Pod' : 'Search Pods'}
            prefix={<SearchOutlined />}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            allowClear
            placeholder={zh ? '搜索 Pod / 节点 / 版本' : 'Search Pod / node / revision'}
          />
          <Segmented
            aria-label={zh ? 'Pod 筛选' : 'Pod filter'}
            value={filter}
            onChange={(value) => setFilter(String(value))}
            options={[
              { value: 'all', label: zh ? '全部' : 'All' },
              ...(model
                ? [
                    {
                      value: 'current',
                      label: (
                        <Tooltip
                          title={
                            !currentRevision || (pods.length > 0 && !model?.podOwners.size)
                              ? zh
                                ? '当前版本或 Pod 所属关系尚未返回'
                                : 'Current revision or Pod ownership is unavailable'
                              : undefined
                          }
                        >
                          {zh ? '当前版本' : 'Current revision'}
                        </Tooltip>
                      ),
                      disabled: !currentRevision || (pods.length > 0 && !model?.podOwners.size),
                    },
                  ]
                : []),
              { value: 'abnormal', label: zh ? '异常' : 'Unhealthy' },
            ]}
          />
        </div>
      )}
      <AdminTable
        columns={columns}
        dataSource={filtered}
        rowKey={rowKey}
        tableRef={viewport.tableRef}
        onScroll={viewport.onScroll}
        onChange={onChange}
        pagination={false}
        enableColumnSelection={false}
        virtual={!compact && filtered.length > 20}
        scroll={{
          x: columns.reduce((width, column) => width + (Number(column.width) || 0), 0),
          scrollToFirstRowOnChange: false,
          ...(!compact ? { y: 600 } : {}),
        }}
        empty={
          <ManagementState
            compact
            title={
              pods.length
                ? zh
                  ? '没有匹配的 Pod'
                  : 'No matching Pods'
                : zh
                  ? '当前返回数据中没有 Pod'
                  : 'No Pods returned'
            }
          />
        }
      />
    </Card>
  )
}
