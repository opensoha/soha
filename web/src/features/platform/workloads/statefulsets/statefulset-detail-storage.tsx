import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { persistentVolumeClaimQueries } from '@/features/platform/storage/persistent-volume-claims/queries'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Input, Typography } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useResourceListViewport } from '@/features/platform/shared/use-resource-list-viewport'
import type { useWorkloadRefresh } from '../shared/workload-refresh'
import { Link } from 'react-router-dom'
import { WorkloadOverviewConfiguration } from '../shared/workload-overview'
import type { StatefulSetConfiguration } from './statefulset-detail-model'
import type { StatefulSetDetail } from './types'
export function StatefulSetDetailStorage({
  scope,
  detail,
  configuration,
  refreshToken = 0,
  interval,
  onReadStateChange,
}: {
  scope: ScopeKey
  detail: StatefulSetDetail
  configuration?: StatefulSetConfiguration
  refreshToken?: number
  interval?: ReturnType<typeof useWorkloadRefresh>['interval']
  onReadStateChange?: (state: { isFetching: boolean; isError: boolean; isEnabled: boolean }) => void
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const allowed = hasPermission(permissions, 'platform.storage.persistent-volume-claims.view')
  const [selected, setSelected] = useState('')
  const [search, setSearch] = useState('')
  const names = useMemo(
    () => [
      ...new Set([
        ...(configuration?.referencedClaims || []),
        ...(detail.pods || []).flatMap((p) => p.persistentVolumeClaims || []),
      ]),
    ],
    [configuration, detail.pods],
  )
  const listOptions = persistentVolumeClaimQueries.list(scope)
  const list = useQuery({
    ...listOptions,
    refetchInterval: interval,
    refetchIntervalInBackground: false,
    enabled: Boolean(listOptions.enabled && allowed && names.length),
  })
  const readOptions = persistentVolumeClaimQueries.detail(scope, selected)
  const query = useQuery({
    ...readOptions,
    refetchInterval: interval,
    refetchIntervalInBackground: false,
    enabled: Boolean(readOptions.enabled && allowed && selected && names.includes(selected)),
  })
  const returned = allowed ? readableQueryData(list) : undefined
  const rows = names.map((name) => ({
    name,
    claim: returned?.find((v) => v.namespace === scope.namespace && v.name === name),
    pods: (detail.pods || [])
      .filter((p) => p.persistentVolumeClaims?.includes(name))
      .map((p) => p.name),
  }))
  const filtered = rows.filter((row) =>
    `${row.name} ${row.claim?.volumeName || ''} ${row.claim?.storageClass || ''} ${row.pods.join(' ')}`
      .toLowerCase()
      .includes(search.trim().toLowerCase()),
  )
  const viewport = useResourceListViewport(
    filtered,
    (row) => row.name,
    JSON.stringify([scope.clusterId, scope.namespace, search]),
  )
  const previousRefresh = useRef(refreshToken)
  const refresh = useRef(() => {})
  refresh.current = () => {
    if (list.isEnabled) void list.refetch()
    if (query.isEnabled) void query.refetch()
  }
  useEffect(() => {
    if (previousRefresh.current !== refreshToken) {
      previousRefresh.current = refreshToken
      refresh.current()
    }
  }, [refreshToken])
  useEffect(() => {
    onReadStateChange?.({
      isFetching: list.isFetching || query.isFetching,
      isError: list.isError || query.isError,
      isEnabled: list.isEnabled || query.isEnabled,
    })
  }, [
    list.isFetching,
    list.isError,
    list.isEnabled,
    query.isFetching,
    query.isError,
    query.isEnabled,
    onReadStateChange,
  ])
  const rawClaim =
    allowed && selected && names.includes(selected) ? readableQueryData(query) : undefined
  const claim =
    rawClaim?.name === selected && rawClaim.namespace === scope.namespace ? rawClaim : undefined
  const pvcPath = (name: string) =>
    `/storage/persistentvolumeclaims/${encodeURIComponent(name)}?clusterId=${encodeURIComponent(scope.clusterId || '')}&namespace=${encodeURIComponent(scope.namespace || '')}`
  return (
    <div className="soha-sts-stack">
      <Card
        className="soha-detail-card"
        title={zh ? '卷声明模板与保留策略' : 'Claim templates and retention'}
      >
        <WorkloadOverviewConfiguration
          items={[
            {
              label: zh ? '删除负载时' : 'When deleted',
              children: configuration?.retention.whenDeleted || '—',
            },
            {
              label: zh ? '缩容时' : 'When scaled',
              children: configuration?.retention.whenScaled || '—',
            },
          ]}
        />
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          rowKey="name"
          dataSource={configuration?.claims || []}
          scroll={{ x: 650 }}
          columns={[
            { title: zh ? '模板' : 'Template', dataIndex: 'name', width: 180 },
            { title: zh ? '申请量' : 'Requested', dataIndex: 'requested', width: 100 },
            {
              title: 'StorageClass',
              dataIndex: 'storageClass',
              width: 160,
              render: (v?: string) => v ?? (zh ? '集群默认' : 'Cluster default'),
            },
            {
              title: zh ? '访问模式' : 'Access modes',
              dataIndex: 'accessModes',
              render: (v: string[]) => v.join(', '),
            },
            {
              title: zh ? '卷模式' : 'Volume mode',
              dataIndex: 'volumeMode',
              render: (v?: string) => v || 'Filesystem',
            },
          ]}
          empty={
            <ManagementState
              compact
              title={
                zh
                  ? configuration
                    ? '未声明卷模板'
                    : '配置尚未读取'
                  : configuration
                    ? 'No claim templates'
                    : 'Configuration not read'
              }
            />
          }
        />
      </Card>
      <Card
        className="soha-detail-card"
        title={zh ? '实际引用的 PVC' : 'Referenced PVCs'}
        extra={
          <Button onClick={() => refresh.current()} disabled={!allowed || !names.length}>
            {zh ? '刷新存储' : 'Refresh storage'}
          </Button>
        }
      >
        {!allowed && (
          <ManagementState
            compact
            kind="no-permission"
            title={zh ? '无权读取 PVC 状态' : 'No permission to read PVC status'}
          />
        )}
        <ReadFeedback canRead={allowed} query={list} label="PVC" />
        <Typography.Paragraph type="secondary">
          {zh
            ? '按当前受控 Pod 的真实声明引用与 Pod 模板直接引用展示，不用名称推测归属；保留但已不再被引用的 PVC 请到存储菜单查看。'
            : 'References come from controlled Pods and direct template references. Retained unreferenced PVCs remain in the storage inventory.'}
        </Typography.Paragraph>
        <Input
          aria-label={zh ? '搜索 PVC' : 'Search PVCs'}
          allowClear
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={
            zh ? '搜索 PVC / PV / StorageClass / Pod' : 'Search PVC / PV / StorageClass / Pod'
          }
        />
        <AdminTable
          pagination={false}
          enableColumnSelection={false}
          rowKey="name"
          dataSource={filtered}
          tableRef={viewport.tableRef}
          onScroll={viewport.onScroll}
          onChange={(_pagination, _filters, _sorter, extra) => {
            if (extra.action === 'sort') viewport.resetPosition()
          }}
          virtual={filtered.length > 20}
          scroll={{ x: 900, y: 440, scrollToFirstRowOnChange: false }}
          columns={[
            {
              title: 'PVC',
              dataIndex: 'name',
              width: 250,
              sorter: (a: (typeof rows)[number], b: (typeof rows)[number]) =>
                a.name.localeCompare(b.name),
              ellipsis: true,
              render: (name: string) =>
                allowed ? (
                  <Link to={pvcPath(name)} title={name}>
                    {name}
                  </Link>
                ) : (
                  name
                ),
            },
            {
              title: zh ? '状态' : 'Status',
              key: 'status',
              width: 140,
              render: (_: unknown, row: (typeof rows)[number]) =>
                row.claim ? (
                  <StatusTag value={row.claim.status} />
                ) : (
                  <Typography.Text type="secondary">
                    {zh ? '尚未确认' : 'Unconfirmed'}
                  </Typography.Text>
                ),
            },
            {
              title: zh ? '申请量' : 'Requested',
              key: 'requested',
              width: 100,
              render: (_: unknown, row: (typeof rows)[number]) => row.claim?.requested || '—',
            },
            {
              title: 'StorageClass',
              key: 'class',
              width: 180,
              render: (_: unknown, row: (typeof rows)[number]) => row.claim?.storageClass || '—',
            },
            {
              title: 'PV',
              key: 'volume',
              width: 190,
              render: (_: unknown, row: (typeof rows)[number]) =>
                row.claim?.volumeName &&
                hasPermission(permissions, 'platform.storage.persistent-volumes.view') ? (
                  <Link
                    to={`/storage/persistentvolumes/${encodeURIComponent(row.claim.volumeName)}?clusterId=${encodeURIComponent(scope.clusterId || '')}`}
                  >
                    {row.claim.volumeName}
                  </Link>
                ) : (
                  row.claim?.volumeName || '—'
                ),
            },
            {
              title: zh ? '引用 Pod' : 'Referencing Pods',
              key: 'pods',
              width: 200,
              render: (_: unknown, row: (typeof rows)[number]) =>
                row.pods.join(', ') || (zh ? '模板直接引用' : 'Template reference'),
            },
            {
              title: '',
              key: 'details',
              width: 90,
              fixed: 'right',
              render: (_: unknown, row: (typeof rows)[number]) => (
                <Button type="link" disabled={!allowed} onClick={() => setSelected(row.name)}>
                  {zh ? '详情' : 'Details'}
                </Button>
              ),
            },
          ]}
          empty={
            <ManagementState
              compact
              title={zh ? '当前没有已确认的 PVC 引用' : 'No confirmed PVC references'}
            />
          }
        />
      </Card>
      {selected && names.includes(selected) && (
        <Card
          className="soha-detail-card"
          title={selected}
          extra={
            <Button type="text" onClick={() => setSelected('')}>
              {zh ? '关闭' : 'Close'}
            </Button>
          }
        >
          <ReadFeedback canRead={allowed} query={query} label="PVC" />
          {rawClaim && !claim && (
            <ManagementState
              compact
              kind="error"
              title={
                zh
                  ? 'PVC 详情返回的资源身份不匹配'
                  : 'PVC detail identity does not match the selected claim'
              }
            />
          )}
          {claim && (
            <WorkloadOverviewConfiguration
              items={[
                { label: zh ? '容量' : 'Capacity', children: claim.capacity || '—' },
                { label: zh ? '申请量' : 'Requested', children: claim.requested || '—' },
                {
                  label: zh ? '访问模式' : 'Access modes',
                  children: claim.accessModes?.join(', ') || '—',
                },
                { label: zh ? '卷模式' : 'Volume mode', children: claim.volumeMode || '—' },
                { label: 'PV', children: claim.volumeName || '—' },
              ]}
            />
          )}
        </Card>
      )}
    </div>
  )
}
