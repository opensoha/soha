import type { KubernetesSecurityFinding } from '@opensoha/contracts/gen/ts/sohaapi'
import { Alert, Space, Spin, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import type { TableColumnsType } from 'antd'
import { AdminTable } from '@/components/admin-table'
import { ManagementState, ManagementTableToolbar } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { buildKubernetesResourcePath } from '@/features/platform/shared/resource-ref'
import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { resourceInsightQueries } from './queries'

export function SecurityPosturePanel({ scope }: { scope: ScopeKey }) {
  const { localeCode } = useI18n()
  const postureQuery = useQuery(resourceInsightQueries.posture(scope))
  const posture = postureQuery.data

  if (postureQuery.isLoading) {
    return (
      <div className="flex items-center justify-center h-48">
        <Spin size="large" />
      </div>
    )
  }
  if (postureQuery.error || !posture) {
    return (
      <Alert
        showIcon
        type="warning"
        title={localeCode === 'zh_CN' ? '安全态势暂不可用' : 'Security posture unavailable'}
        description={postureQuery.error?.message}
      />
    )
  }
  if (posture.status === 'unsupported') {
    return (
      <Alert
        showIcon
        type="info"
        title={localeCode === 'zh_CN' ? '未检测到 Kubescape' : 'Kubescape was not detected'}
        description={
          localeCode === 'zh_CN'
            ? '安装 Kubescape Operator 后，此处会读取配置与漏洞扫描摘要。'
            : posture.message ||
              'Install the Kubescape Operator to expose configuration and vulnerability summaries.'
        }
      />
    )
  }

  const columns: TableColumnsType<KubernetesSecurityFinding> = [
    {
      title: localeCode === 'zh_CN' ? '级别' : 'Severity',
      dataIndex: 'severity',
      width: 100,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '类别' : 'Category',
      dataIndex: 'category',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '资源' : 'Resource',
      key: 'resource',
      render: (_, finding) => {
        if (!finding.resource) return '-'
        const label = `${finding.resource.kind} / ${finding.resource.namespace ? `${finding.resource.namespace}/` : ''}${finding.resource.name}`
        const path = buildKubernetesResourcePath(finding.resource)
        return path ? <Link to={path}>{label}</Link> : label
      },
    },
    {
      title: localeCode === 'zh_CN' ? '发现' : 'Finding',
      dataIndex: 'title',
      ellipsis: { showTitle: false },
      render: (value: string, finding) => (
        <TableCellText value={finding.message ? `${value}: ${finding.message}` : value} />
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
  ]

  const severityCounts = [
    ['critical', posture.counts.critical],
    ['high', posture.counts.high],
    ['medium', posture.counts.medium],
    ['low', posture.counts.low],
  ] as const

  return (
    <div className="soha-detail-stack">
      {posture.status !== 'available' || posture.warnings.length ? (
        <Alert showIcon type="warning" title={posture.message || posture.warnings.join(' ')} />
      ) : null}
      <AdminTable
        className="soha-platform-table"
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        headerExtra={
          <ManagementTableToolbar>
            <Typography.Text strong>
              {localeCode === 'zh_CN' ? '集群安全态势' : 'Cluster security posture'}
            </Typography.Text>
            <StatusTag label={`Kubescape · ${posture.status}`} value={posture.status} />
            <Space size={4} wrap>
              {severityCounts.map(([severity, count]) => (
                <StatusTag key={severity} label={`${severity} ${count}`} value={severity} />
              ))}
            </Space>
            <Typography.Text type="secondary">
              {new Date(posture.generatedAt).toLocaleString()}
            </Typography.Text>
          </ManagementTableToolbar>
        }
        columns={columns}
        dataSource={posture.findings}
        empty={
          <ManagementState
            compact
            kind="empty"
            title={localeCode === 'zh_CN' ? '当前没有安全发现' : 'No security findings'}
          />
        }
        localSorting
        pagination={false}
        virtual
        rowKey="id"
        scroll={{ x: 1000, y: 520 }}
        viewportScroll
      />
    </div>
  )
}
