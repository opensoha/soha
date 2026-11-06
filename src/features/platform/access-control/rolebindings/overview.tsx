import { useState } from 'react'
import { Button, Card, Tooltip, type TableColumnsType } from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Link } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { ManagementToolbarSearch } from '@/components/management-list'
import { TableCellText } from '@/components/table-cell-content'
import { MetadataTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import type { BindingConfiguration } from './detail-model'
import type { RoleBindingDetail } from './types'

type Subject = BindingConfiguration['subjects'][number]

// Kubernetes ServiceAccount names and namespaces are DNS subdomains and labels respectively.
function isServiceAccountTarget(subject: Subject) {
  const label = /^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$/
  return (
    subject.kind === 'ServiceAccount' &&
    !subject.apiGroup &&
    Boolean(subject.namespace && subject.namespace.length <= 63 && label.test(subject.namespace)) &&
    subject.name.length <= 253 &&
    subject.name.split('.').every((part) => part.length <= 63 && label.test(part))
  )
}

export function RoleBindingOverview({
  detail,
  configuration,
  clusterId,
}: {
  detail: RoleBindingDetail
  configuration?: BindingConfiguration
  clusterId: string
}) {
  const zh = useI18n().localeCode === 'zh_CN'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const [search, setSearch] = useState('')
  const query = search.trim().toLocaleLowerCase()
  const subjects = configuration?.subjects ?? []
  const rows = subjects.map((subject, index) => ({ ...subject, key: String(index) }))
  const shown = rows.filter((subject) =>
    [subject.kind, subject.name, subject.namespace, subject.apiGroup].some((value) =>
      value?.toLocaleLowerCase().includes(query),
    ),
  )
  const role = configuration?.roleRef
  const canOpenRole =
    clusterId &&
    (role?.kind === 'Role' || role?.kind === 'ClusterRole') &&
    role.apiGroup === 'rbac.authorization.k8s.io' &&
    Boolean(role.name) &&
    (role.kind === 'ClusterRole' || Boolean(detail.namespace)) &&
    hasPermission(
      permissions,
      role.kind === 'Role'
        ? 'platform.access-control.roles.view'
        : 'platform.access-control.cluster-roles.view',
    )
  const columns: TableColumnsType<Subject> = [
    { title: zh ? '类型' : 'Kind', dataIndex: 'kind', width: 150 },
    {
      title: zh ? '名称' : 'Name',
      dataIndex: 'name',
      width: 270,
      render: (name: string, subject) =>
        clusterId &&
        isServiceAccountTarget(subject) &&
        hasPermission(permissions, 'platform.access-control.service-accounts.view') ? (
          <Link
            to={`/platform-access-control/serviceaccounts/${encodeURIComponent(name)}?${new URLSearchParams({ clusterId, namespace: subject.namespace! })}`}
          >
            <span className="soha-rb-subject-name">{name}</span>
          </Link>
        ) : (
          <span className="soha-rb-subject-name">{name}</span>
        ),
    },
    {
      title: zh ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 180,
      render: (value: string | undefined) => <TableCellText value={value} />,
    },
    {
      title: zh ? 'API 组' : 'API group',
      dataIndex: 'apiGroup',
      width: 240,
      render: (value: string | undefined) => <TableCellText value={value} />,
    },
  ]
  return (
    <WorkloadOverview
      aside={
        <Card className="soha-detail-card" title={zh ? '资源信息' : 'Resource information'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '作用域' : 'Scope', children: zh ? '命名空间级' : 'Namespace-scoped' },
              {
                label: zh ? '命名空间' : 'Namespace',
                children: (
                  <Tooltip title={detail.namespace} trigger={['hover', 'focus', 'click']}>
                    <span className="soha-rb-namespace" tabIndex={0}>
                      <MetadataTag label={detail.namespace} tone="blue" title={detail.namespace} />
                    </span>
                  </Tooltip>
                ),
              },
              {
                label: zh ? '授权主体数' : 'Subjects',
                children: configuration ? subjects.length : zh ? '未知' : 'Unknown',
              },
              {
                label: zh ? '创建时间' : 'Created',
                children: detail.createdAt ? formatDateTime(detail.createdAt) : '—',
              },
            ]}
          />
        </Card>
      }
    >
      <Card className="soha-detail-card" title={zh ? '目标角色' : 'Target role'}>
        {role ? (
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '类型' : 'Kind', children: role.kind },
              {
                label: zh ? '名称' : 'Name',
                children: canOpenRole ? (
                  <Link
                    to={`/platform-access-control/${role.kind === 'Role' ? 'roles' : 'clusterroles'}/${encodeURIComponent(role.name)}?${new URLSearchParams(role.kind === 'Role' ? { clusterId, namespace: detail.namespace } : { clusterId })}`}
                  >
                    {role.name}
                  </Link>
                ) : (
                  role.name
                ),
              },
              ...(role.kind === 'Role'
                ? [{ label: zh ? '命名空间' : 'Namespace', children: detail.namespace }]
                : []),
              { label: zh ? 'API 组' : 'API group', children: role.apiGroup || '—' },
            ]}
          />
        ) : (
          <>
            <p className="soha-rb-source-status">
              {zh
                ? '完整 YAML 配置尚不可用；以下仅为详情 API 返回的原始 RoleRef 摘要。'
                : 'Full YAML configuration unavailable; only the raw roleRef summary from the detail API is shown.'}
            </p>
            <div className="soha-rb-raw-summary">{detail.roleRef || '—'}</div>
          </>
        )}
      </Card>
      <Card
        className="soha-detail-card"
        title={
          <span className="soha-rb-section-title">
            {zh ? '授权主体' : 'Subjects'}
            <Tooltip
              trigger={['hover', 'focus', 'click']}
              title={
                zh
                  ? '完整配置来自当前资源 YAML。User 和 Group 是原始身份名称，不推断为平台用户或组；ServiceAccount 仅在显式命名空间和名称有效且有查看权限时提供链接。绑定主体不代表完整有效权限。'
                  : 'Full configuration comes from this resource YAML. User and Group values are raw identities, not platform accounts. ServiceAccount links require a valid explicit namespace and name and view permission. Binding subjects do not establish complete effective access.'
              }
            >
              <Button
                type="text"
                size="small"
                icon={<QuestionCircleOutlined />}
                aria-label={zh ? '授权主体说明' : 'About binding subjects'}
              />
            </Tooltip>
          </span>
        }
        extra={
          configuration
            ? `${shown.length} / ${subjects.length}`
            : zh
              ? '数据不完整'
              : 'Incomplete data'
        }
      >
        {configuration ? (
          <>
            <div className="soha-rb-subject-search">
              <ManagementToolbarSearch
                value={search}
                onChange={setSearch}
                placeholder={
                  zh
                    ? '搜索类型、名称、命名空间或 API 组'
                    : 'Search kind, name, namespace or API group'
                }
                aria-label={zh ? '搜索授权主体' : 'Search binding subjects'}
              />
            </div>
            <AdminTable
              rowKey="key"
              tableSize="small"
              pagination={false}
              enableColumnSelection={false}
              enableDensity={false}
              columnSettingPlacement="hidden"
              columns={columns}
              dataSource={shown}
              scroll={{ x: 840 }}
              empty={
                zh
                  ? query
                    ? '没有匹配的主体'
                    : '当前没有授权主体'
                  : query
                    ? 'No matching subjects'
                    : 'No binding subjects'
              }
            />
          </>
        ) : (
          <>
            <p className="soha-rb-source-status">
              {zh
                ? '完整 YAML 配置尚不可用；以下仅为详情 API 返回的原始主体摘要，无法确认完整身份字段与主体总数。'
                : 'Full YAML configuration unavailable; only raw subject summaries from the detail API are shown. Complete identity fields and subject totals cannot be confirmed.'}
            </p>
            <ul className="soha-rb-raw-subjects">
              {detail.subjects?.map((subject, index) => (
                <li key={index}>{subject}</li>
              ))}
            </ul>
          </>
        )}
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
