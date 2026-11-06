import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import type { ManifestDeployment, ManifestResourceInventory } from '@/features/delivery'
import { NamespaceScopeTag } from '@/features/platform/shared/namespace-scope-tag'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import type { TableColumnsType } from 'antd'
import { Alert, Button, Card, Descriptions, Drawer, Space, Tabs, Tooltip, Typography } from 'antd'
import { FullscreenExitOutlined, FullscreenOutlined } from '@ant-design/icons'
import { useState } from 'react'
import type { UseQueryResult } from '@tanstack/react-query'
import { formatManifestValue, manifestDiffRows, type ManifestRuntimeRow } from './runtime-model'
import './styles.css'

const { Text } = Typography

export function ManifestRuntimeDrawer({
  selected,
  onClose,
  applicationName,
  clusterName,
  initialTab = 'overview',
  deploymentQuery,
}: {
  initialTab?: 'overview' | 'desired'
  deploymentQuery?: UseQueryResult<ManifestDeployment[], Error>
  clusterName?: string
  applicationName?: string
  selected: ManifestRuntimeRow | undefined
  onClose: () => void
}) {
  const { localeCode } = useI18n()
  const [expanded, setExpanded] = useState(false)
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const deployment = selected?.deployment
  const ready = selected?.readState === 'ready'
  const inventory = ready ? deployment?.status.inventory : undefined
  const drift = ready ? deployment?.status.drift : undefined
  const differences = manifestDiffRows(ready ? deployment : undefined)
  const readLabel =
    selected?.readState === 'loading'
      ? text('正在读取', 'Loading')
      : selected?.readState === 'error'
        ? text('读取失败', 'Read failed')
        : text('未获取', 'Not available')
  const unknown = text('未知', 'Unknown')
  const desiredRevision = selected
    ? `v${deployment?.spec.desiredRevision ?? selected.manifest.currentRevision}`
    : '-'
  const appliedRevision =
    ready && deployment?.status.appliedRevision ? `v${deployment.status.appliedRevision}` : unknown
  const driftLabel = drift
    ? drift.drifted
      ? text('存在差异', 'Drift detected')
      : text('未发现差异', 'No drift detected')
    : ready
      ? text('尚无差异检测记录', 'No drift observation')
      : readLabel
  const noObservation = (label: string) => (
    <div className="soha-platform-manifest-unknown" role="status">
      <Text type="secondary">{label}</Text>
    </div>
  )
  const inventoryColumns: TableColumnsType<ManifestResourceInventory> = [
    {
      title: text('资源', 'Resource'),
      key: 'resource',
      render: (_value, item) => (
        <div className="soha-platform-manifest-resource">
          <Text>{item.name}</Text>
          <Text type="secondary">{`${item.kind} · ${item.apiVersion}`}</Text>
        </div>
      ),
    },
    {
      title: text('命名空间', 'Namespace'),
      dataIndex: 'namespace',
      render: (value: string) => <NamespaceScopeTag namespace={value} />,
    },
    {
      title: text('健康状态', 'Health'),
      dataIndex: 'health',
      render: (value: string) => <StatusTag value={value || 'unknown'} />,
    },
  ]

  return (
    <Drawer
      open={Boolean(selected)}
      size={expanded ? '100vw' : 'min(1040px, 100vw)'}
      classNames={{
        header: 'soha-platform-manifest-drawer-header',
        title: 'soha-platform-manifest-drawer-title',
        body: 'soha-platform-manifest-drawer-body',
      }}
      extra={
        <Tooltip
          title={expanded ? text('恢复宽度', 'Restore width') : text('放大查看', 'Expand view')}
          trigger={['hover', 'focus']}
        >
          <Button
            type="text"
            aria-label={
              expanded
                ? text('恢复抽屉宽度', 'Restore drawer width')
                : text('放大抽屉', 'Expand drawer')
            }
            aria-pressed={expanded}
            icon={expanded ? <FullscreenExitOutlined /> : <FullscreenOutlined />}
            onClick={() => setExpanded((value) => !value)}
          />
        </Tooltip>
      }
      title={
        selected ? (
          <div className="soha-platform-manifest-identity">
            <Space size={12} wrap>
              <Text strong copyable={{ text: selected.manifest.name }}>
                {selected.manifest.name}
              </Text>
              <StatusTag
                value={ready ? deployment?.status.phase || selected.status || 'unknown' : 'unknown'}
                label={!ready ? readLabel : undefined}
              />
            </Space>
            <div className="soha-platform-manifest-context">
              <span>{`${text('期望', 'Desired')} ${desiredRevision}`}</span>
              <span>{`${text('实际', 'Applied')} ${appliedRevision}`}</span>
              <MetadataTag label={selected.environmentKey} />
              <NamespaceScopeTag namespace={selected.namespace} />
            </div>
          </div>
        ) : undefined
      }
      onClose={onClose}
    >
      {selected ? (
        <div className="soha-platform-manifest-detail">
          {deploymentQuery?.isError ? (
            <ReadFeedback query={deploymentQuery} label={text('部署记录', 'Deployment records')} />
          ) : !ready ? (
            <Alert
              showIcon
              type={selected.readState === 'error' ? 'error' : 'info'}
              title={
                selected.readState === 'loading'
                  ? text('正在读取部署记录', 'Loading deployment records')
                  : selected.readState === 'error'
                    ? text('部署记录读取失败', 'Deployment records could not be read')
                    : text('部署记录尚未获取', 'Deployment records are unavailable')
              }
              description={
                selected.readError ||
                text(
                  '实际版本、资源清单和差异状态尚不可确认。',
                  'The applied revision, resource inventory and drift status are unconfirmed.',
                )
              }
            />
          ) : null}
          {ready && deployment?.status.lastErrorMessage ? (
            <Alert
              type="error"
              showIcon
              title={
                deployment.status.lastErrorCode || text('最近协调异常', 'Latest reconcile error')
              }
              description={deployment.status.lastErrorMessage}
            />
          ) : null}
          <Tabs
            key={`${selected.manifest.id}:${selected.id || selected.applicationEnvironmentId}`}
            defaultActiveKey={initialTab}
            className="soha-resource-tabs soha-platform-manifest-tabs"
            classNames={{
              body: 'soha-platform-manifest-tabs-body',
              content: 'soha-platform-manifest-tab-content',
            }}
            items={[
              {
                key: 'overview',
                label: text('概览', 'Overview'),
                children: (
                  <div className="soha-platform-manifest-overview">
                    <div className="soha-platform-manifest-main">
                      <Card size="small" title={text('部署配置', 'Deployment configuration')}>
                        <Descriptions
                          size="small"
                          column={{ xs: 1, sm: 2 }}
                          items={[
                            {
                              key: 'application',
                              label: text('应用', 'Application'),
                              children: applicationName || selected.manifest.applicationId,
                            },
                            {
                              key: 'service',
                              label: text('服务', 'Service'),
                              children:
                                selected.manifest.serviceId ||
                                text('应用级扩展资源', 'Application resources'),
                            },
                            {
                              key: 'cluster',
                              label: text('集群', 'Cluster'),
                              children: (
                                <Tooltip title={selected.clusterId} trigger={['hover', 'focus']}>
                                  <Text
                                    className="soha-platform-manifest-value"
                                    tabIndex={0}
                                    copyable={{ text: selected.clusterId }}
                                  >
                                    {clusterName || `${selected.clusterId.slice(0, 8)}…`}
                                  </Text>
                                </Tooltip>
                              ),
                            },
                            {
                              key: 'namespace',
                              label: text('命名空间', 'Namespace'),
                              children: <NamespaceScopeTag namespace={selected.namespace} />,
                            },
                            {
                              key: 'environment',
                              label: text('环境', 'Environment'),
                              children: selected.environmentKey,
                            },
                            {
                              key: 'renderer',
                              label: text('渲染器', 'Renderer'),
                              children: selected.manifest.renderer,
                            },
                            {
                              key: 'desired',
                              label: text('期望版本', 'Desired revision'),
                              children: desiredRevision,
                            },
                            {
                              key: 'applied',
                              label: text('实际版本', 'Applied revision'),
                              children: appliedRevision,
                            },
                          ]}
                        />
                        {selected.manifest.description ? (
                          <Text type="secondary">{selected.manifest.description}</Text>
                        ) : null}
                      </Card>
                      <Card size="small" title={text('运行观测', 'Runtime observation')}>
                        <Descriptions
                          size="small"
                          column={1}
                          items={[
                            {
                              key: 'inventory',
                              label: text('已记录资源', 'Recorded resources'),
                              children:
                                inventory !== undefined
                                  ? inventory.length
                                  : ready
                                    ? text('暂无资源清单记录', 'No inventory record')
                                    : readLabel,
                            },
                            {
                              key: 'drift',
                              label: text('差异检测', 'Drift observation'),
                              children: (
                                <StatusTag
                                  value={
                                    drift ? (drift.drifted ? 'warning' : 'healthy') : 'unknown'
                                  }
                                  label={driftLabel}
                                />
                              ),
                            },
                            {
                              key: 'observedAt',
                              label: text('差异观测时间', 'Drift observed at'),
                              children: drift?.observedAt
                                ? formatDateTime(drift.observedAt)
                                : unknown,
                            },
                            {
                              key: 'reconciledAt',
                              label: text('最近协调时间', 'Last reconciled at'),
                              children:
                                ready && deployment?.status.lastReconciledAt
                                  ? formatDateTime(deployment.status.lastReconciledAt)
                                  : unknown,
                            },
                          ]}
                        />
                      </Card>
                    </div>
                    <div className="soha-platform-manifest-sidebar">
                      <Card size="small" title={text('清单包', 'Manifest package')}>
                        <Descriptions
                          size="small"
                          column={1}
                          items={[
                            {
                              key: 'status',
                              label: text('状态', 'Status'),
                              children: <StatusTag value={selected.manifest.status} />,
                            },
                            {
                              key: 'revision',
                              label: text('当前版本', 'Current revision'),
                              children: `v${selected.manifest.currentRevision}`,
                            },
                            {
                              key: 'files',
                              label: text('文件数', 'Files'),
                              children: selected.manifest.files.length,
                            },
                            {
                              key: 'updated',
                              label: text('记录更新时间', 'Record updated at'),
                              children: formatDateTime(selected.manifest.updatedAt),
                            },
                          ]}
                        />
                      </Card>
                      <Card size="small" title={text('协调策略', 'Reconcile policy')}>
                        {ready && deployment ? (
                          <Descriptions
                            size="small"
                            column={1}
                            items={[
                              {
                                key: 'reconcile',
                                label: text('协调方式', 'Reconcile'),
                                children: deployment.spec.reconcilePolicy,
                              },
                              {
                                key: 'drift',
                                label: text('差异策略', 'Drift'),
                                children: deployment.spec.driftPolicy,
                              },
                              {
                                key: 'delete',
                                label: text('删除策略', 'Deletion'),
                                children: deployment.spec.deletionPolicy,
                              },
                            ]}
                          />
                        ) : (
                          <Text type="secondary">
                            {ready ? text('暂无部署记录', 'No deployment record') : readLabel}
                          </Text>
                        )}
                      </Card>
                    </div>
                  </div>
                ),
              },
              {
                key: 'desired',
                label: text('清单文件', 'Manifest files'),
                children: selected.manifest.files.length ? (
                  <Tabs
                    size="small"
                    className="soha-platform-manifest-file-tabs"
                    classNames={{
                      body: 'soha-platform-manifest-tabs-body',
                      content: 'soha-platform-manifest-file-content',
                    }}
                    items={selected.manifest.files.map((file) => ({
                      key: file.path,
                      label: file.path,
                      children: (
                        <div className="soha-platform-manifest-file-panel">
                          <div className="soha-platform-manifest-file-toolbar">
                            <Text type="secondary">YAML</Text>
                            <Text
                              copyable={{
                                text: file.content,
                                tooltips: [
                                  text('复制文件内容', 'Copy file content'),
                                  text('已复制', 'Copied'),
                                ],
                              }}
                            >
                              {text('复制', 'Copy')}
                            </Text>
                          </div>
                          <pre
                            className="soha-platform-manifest-yaml"
                            tabIndex={0}
                            aria-label={file.path}
                          >
                            {file.content}
                          </pre>
                        </div>
                      ),
                    }))}
                  />
                ) : (
                  noObservation(text('该清单包没有文件', 'This package contains no files'))
                ),
              },
              {
                key: 'actual',
                label: `${text('实际资源', 'Actual resources')}${inventory !== undefined ? ` (${inventory.length})` : ''}`,
                children:
                  inventory !== undefined ? (
                    <AdminTable
                      enableColumnSelection={false}
                      scroll={{ x: 680, y: 520 }}
                      tableSize="small"
                      pagination={false}
                      rowKey={(item) =>
                        `${item.apiVersion}:${item.kind}:${item.namespace}:${item.name}`
                      }
                      dataSource={inventory}
                      columns={inventoryColumns}
                      empty={noObservation(
                        text('部署记录中没有资源', 'No resources in the deployment record'),
                      )}
                    />
                  ) : (
                    noObservation(
                      ready
                        ? text(
                            '尚无实际资源清单记录',
                            'No actual resource inventory has been recorded',
                          )
                        : readLabel,
                    )
                  ),
              },
              {
                key: 'diff',
                label: `${text('差异', 'Differences')}${drift ? ` (${differences.length})` : ''}`,
                children: drift ? (
                  <div className="soha-platform-manifest-detail">
                    <Space wrap>
                      <StatusTag value={drift.drifted ? 'warning' : 'healthy'} label={driftLabel} />
                      <Text type="secondary">{`${text('观测时间', 'Observed at')} ${formatDateTime(drift.observedAt)}`}</Text>
                    </Space>
                    <AdminTable
                      enableColumnSelection={false}
                      scroll={{ x: 760, y: 520 }}
                      tableSize="small"
                      pagination={false}
                      rowKey="key"
                      dataSource={differences}
                      columns={[
                        { title: text('资源', 'Resource'), dataIndex: 'resource' },
                        { title: text('字段', 'Field'), dataIndex: 'path' },
                        {
                          title: text('期望值', 'Desired value'),
                          dataIndex: 'desiredValue',
                          render: formatManifestValue,
                        },
                        {
                          title: text('实际值', 'Actual value'),
                          dataIndex: 'observedValue',
                          render: formatManifestValue,
                        },
                      ]}
                      empty={noObservation(
                        drift.drifted
                          ? text(
                              '报告标记存在差异，未提供字段详情',
                              'The report indicates drift without field details',
                            )
                          : text(
                              '本次观测未发现字段差异',
                              'No field differences in this observation',
                            ),
                      )}
                    />
                  </div>
                ) : (
                  noObservation(driftLabel)
                ),
              },
            ]}
          />
        </div>
      ) : null}
    </Drawer>
  )
}
