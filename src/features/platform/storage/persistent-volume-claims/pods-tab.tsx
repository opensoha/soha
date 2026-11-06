import { useState } from 'react'
import { Link } from 'react-router-dom'
import { Card, Tooltip, Button, type TableColumnsType } from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { AdminTable } from '@/components/admin-table'
import { ManagementTableToolbar, ManagementToolbarSearch } from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import type { PersistentVolumeClaimDetail, StoragePodReference } from '@/types/platform'
import { readableQueryData } from '@/utils/query-feedback'
import './styles.css'

export function PvcPodsTab({
  detail,
  clusterId,
}: {
  detail: PersistentVolumeClaimDetail
  clusterId: string
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const [search, setSearch] = useState('')
  const pods = detail.pods ?? []
  const query = search.trim().toLocaleLowerCase()
  const rows = pods.filter((pod) =>
    [pod.name, pod.namespace, pod.phase, pod.nodeName].some((value) =>
      value?.toLocaleLowerCase().includes(query),
    ),
  )
  const columns: TableColumnsType<StoragePodReference> = [
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 240,
      render: (name: string, pod) =>
        hasPermission(permissions, 'platform.pods.view') ? (
          <Link
            to={`/workloads/pods/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId, namespace: pod.namespace })}`}
          >
            <TableCellText value={name} />
          </Link>
        ) : (
          <TableCellText value={name} />
        ),
    },
    {
      title: 'Namespace',
      dataIndex: 'namespace',
      width: 180,
      render: (value: string) => (
        <Tooltip title={value} trigger={['hover', 'focus', 'click']}>
          <span className="soha-pvc-namespace-trigger" tabIndex={0}>
            <MetadataTag tone="blue" label={<span>{value}</span>} />
          </span>
        </Tooltip>
      ),
    },
    {
      title: 'Phase',
      dataIndex: 'phase',
      width: 120,
      render: (value?: string) => (value ? <StatusTag value={value} /> : '—'),
    },
    {
      title: zh ? '节点' : 'Node',
      dataIndex: 'nodeName',
      width: 220,
      render: (value?: string) =>
        value ? (
          hasPermission(permissions, 'platform.nodes.view') ? (
            <Link
              to={`/cluster-resources/nodes/${encodeURIComponent(value)}?${new URLSearchParams({ clusterId })}`}
            >
              <TableCellText value={value} />
            </Link>
          ) : (
            <TableCellText value={value} />
          )
        ) : (
          '—'
        ),
    },
  ]
  return (
    <Card
      className="soha-detail-card soha-pvc-pods"
      title={
        <span className="soha-pvc-section-title">
          {zh ? 'Pod 引用' : 'Pod references'}
          <Tooltip
            title={
              zh
                ? '此处仅展示当前 API 返回的 Pod 引用；权限或关系来源失败可能影响结果，不表示挂载成功或全局使用情况。'
                : 'Only Pod references returned by the API are shown. Permissions or relationship source failures may affect results; these are not proof of mounts or global use.'
            }
            trigger={['hover', 'focus', 'click']}
          >
            <Button
              type="text"
              size="small"
              icon={<QuestionCircleOutlined />}
              aria-label={zh ? 'Pod 引用说明' : 'Pod reference information'}
            />
          </Tooltip>
        </span>
      }
    >
      <div className="soha-pvc-pods-toolbar">
        <ManagementTableToolbar>
          <ManagementToolbarSearch
            aria-label={zh ? '搜索当前返回的 Pod 引用' : 'Search returned Pod references'}
            placeholder={
              zh ? '搜索名称、Namespace、Phase、节点' : 'Search name, namespace, phase, node'
            }
            value={search}
            onChange={setSearch}
          />
        </ManagementTableToolbar>
        <span className="soha-pvc-muted">
          {detail.pods === undefined
            ? zh
              ? '未提供引用清单'
              : 'References not provided'
            : zh
              ? `显示 ${rows.length} / 当前返回 ${pods.length} 条`
              : `${rows.length} shown / ${pods.length} returned`}
          {detail.podsTruncated ? (zh ? ' · 结果已截断' : ' · Results truncated') : ''}
        </span>
      </div>
      <AdminTable
        columns={columns}
        dataSource={rows}
        pagination={false}
        enableColumnSelection={false}
        tableSize="small"
        rowKey={(pod) => `${pod.namespace}/${pod.name}`}
        scroll={{ x: 760 }}
        empty={
          detail.pods === undefined
            ? zh
              ? '未提供 Pod 引用清单'
              : 'Pod references were not provided'
            : query
              ? zh
                ? '当前返回范围内没有匹配引用'
                : 'No matching references in the returned scope'
              : zh
                ? '当前未返回 Pod 引用'
                : 'No Pod references returned'
        }
      />
    </Card>
  )
}
