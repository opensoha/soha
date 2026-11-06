import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import {
  buildRelatedResourcePath,
  buildVolumeDetailPath,
  formatContainerStateLabel,
} from '@/features/platform/workloads-model'
import type { PodVolume, ScopeKey, WorkloadContainer } from '@/types'
import { formatDateTime } from '@/utils/time'
import { Button, Descriptions, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type {
  ContainerConfiguration,
  ContainerReference,
  ContainerRuntime,
} from './pod-container-configuration'
import { podVolumeReadOnly } from './pod-volume-mounts'

const { Text, Paragraph } = Typography
const wrappingCell = () => ({
  style: {
    whiteSpace: 'normal' as const,
    overflowWrap: 'anywhere' as const,
    wordBreak: 'break-word' as const,
  },
})

function Arguments({ values, zh }: { values: string[]; zh: boolean }) {
  return values.length ? (
    <Paragraph className="soha-pod-container-code" copyable={{ text: JSON.stringify(values) }}>
      {JSON.stringify(values, null, 2)}
    </Paragraph>
  ) : (
    <Text type="secondary">
      {zh ? '未显式配置，沿用镜像默认值' : 'Not declared; use image defaults'}
    </Text>
  )
}

function Runtime({ title, value, zh }: { title: string; value: ContainerRuntime; zh: boolean }) {
  const failed = value.type === 'terminated' && value.exitCode !== undefined && value.exitCode !== 0
  return (
    <section className={`soha-pod-container-runtime${failed ? ' is-failed' : ''}`}>
      <h4>{title}</h4>
      {value.type === 'unknown' ? (
        <Text type="secondary">{zh ? '未返回状态记录' : 'No state record returned'}</Text>
      ) : (
        <>
          <div className="soha-pod-container-state">
            <StatusTag value={value.reason || value.type} />
            {value.exitCode !== undefined && (
              <Text strong type={failed ? 'danger' : undefined}>
                Exit Code {value.exitCode}
              </Text>
            )}
            {value.signal !== undefined && <Text type="secondary">Signal {value.signal}</Text>}
          </div>
          {value.message && <p className="soha-pod-detail-break">{value.message}</p>}
          {(value.startedAt || value.finishedAt) && (
            <Text type="secondary">
              {zh ? '开始' : 'Started'} {formatDateTime(value.startedAt)}
              {value.finishedAt && (
                <>
                  {' '}
                  · {zh ? '结束' : 'Finished'} {formatDateTime(value.finishedAt)}
                </>
              )}
            </Text>
          )}
        </>
      )}
    </section>
  )
}

export function PodContainerDetails({
  container,
  configuration,
  loading,
  scope,
  volumes,
  zh,
  canViewReference,
  onViewStorage,
}: {
  container: WorkloadContainer
  configuration?: ContainerConfiguration
  loading: boolean
  scope: ScopeKey
  volumes: PodVolume[]
  zh: boolean
  canViewReference: (kind: string) => boolean
  onViewStorage: () => void
}) {
  const reference = (ref: ContainerReference): ReactNode => {
    if (ref.kind === 'Field' || ref.kind === 'Resource')
      return (
        <span>
          {ref.kind === 'Field'
            ? zh
              ? 'Pod 字段'
              : 'Pod field'
            : zh
              ? '容器资源'
              : 'Container resource'}{' '}
          · {ref.detail || '—'}
        </span>
      )
    if (ref.kind === 'Unknown')
      return <Text type="secondary">{zh ? '未识别的引用' : 'Unrecognized reference'}</Text>
    const path =
      ref.name && canViewReference(ref.kind)
        ? buildRelatedResourcePath(
            { kind: ref.kind, name: ref.name },
            scope.namespace,
            scope.clusterId,
          )
        : null
    return (
      <span>
        {ref.kind} · {path ? <Link to={path}>{ref.name}</Link> : ref.name || '—'}
        {ref.key && (
          <>
            {' '}
            / <Text code>{ref.key}</Text>
          </>
        )}
        {ref.optional && <Text type="secondary"> · {zh ? '可选' : 'Optional'}</Text>}
      </span>
    )
  }
  const envColumns: TableColumnsType<NonNullable<ContainerConfiguration['env']>[number]> = [
    { title: zh ? '名称' : 'Name', dataIndex: 'name', width: 170, onCell: wrappingCell },
    {
      title: zh ? '值 / 来源' : 'Value / source',
      width: 350,
      onCell: wrappingCell,
      render: (_, item) =>
        item.source ? (
          reference(item.source)
        ) : item.value !== undefined ? (
          <Text copyable={Boolean(item.value)}>
            {item.value === '' ? (zh ? '空字符串' : 'Empty string') : item.value}
          </Text>
        ) : (
          <Text type="secondary">{zh ? '未返回值' : 'No value returned'}</Text>
        ),
    },
  ]
  const mountColumns: TableColumnsType<NonNullable<ContainerConfiguration['mounts']>[number]> = [
    {
      title: zh ? '卷 / 来源' : 'Volume / source',
      width: 180,
      onCell: wrappingCell,
      render: (_, mount) => {
        const volume = volumes.find((volume) => volume.name === mount.name)
        const path =
          volume && canViewReference(volume.type)
            ? buildVolumeDetailPath(volume, scope.namespace, scope.clusterId)
            : null
        return (
          <div className="soha-pod-storage-source">
            <Text>{mount.name}</Text>
            {volume?.sourceName &&
              (path ? (
                <Link to={path}>{volume.sourceName}</Link>
              ) : (
                <Text type="secondary">{volume.sourceName}</Text>
              ))}
          </div>
        )
      },
    },
    {
      title: zh ? '挂载路径' : 'Mount path',
      width: 280,
      onCell: wrappingCell,
      render: (_, mount) => (
        <div className="soha-pod-storage-source">
          <Text copyable>{mount.path}</Text>
          {mount.subPath && <Text type="secondary">subPath: {mount.subPath}</Text>}
          {mount.subPathExpr && <Text type="secondary">subPathExpr: {mount.subPathExpr}</Text>}
        </div>
      ),
    },
    {
      title: zh ? '模式' : 'Mode',
      width: 70,
      onCell: wrappingCell,
      render: (_, mount) =>
        podVolumeReadOnly(
          volumes.find((volume) => volume.name === mount.name),
          mount.readOnly,
        )
          ? zh
            ? '只读'
            : 'Read only'
          : zh
            ? '读写'
            : 'Read / write',
    },
  ]
  const quantities = (values: Record<string, string>) =>
    Object.keys(values).length ? (
      <dl className="soha-pod-container-quantities">
        {Object.entries(values).map(([key, value]) => (
          <div key={key}>
            <dt>{key}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    ) : (
      <Text type="secondary">{zh ? '未设置' : 'Not set'}</Text>
    )
  return (
    <div className="soha-pod-container-panel">
      <Descriptions
        column={1}
        size="small"
        items={[
          {
            key: 'image',
            label: zh ? '镜像' : 'Image',
            children: (
              <Text copyable className="soha-pod-detail-break">
                {container.image || '—'}
              </Text>
            ),
          },
          {
            key: 'pull',
            label: 'ImagePullPolicy',
            children: configuration?.imagePullPolicy || '—',
          },
          {
            key: 'ready',
            label: zh ? '就绪' : 'Ready',
            children: zh ? (container.ready ? '是' : '否') : container.ready ? 'Yes' : 'No',
          },
          {
            key: 'started',
            label: zh ? '启动时间' : 'Started',
            children: formatDateTime(container.startedAt),
          },
          {
            key: 'id',
            label: 'Container ID',
            children: (
              <Text copyable={Boolean(container.containerId)} className="soha-pod-detail-break">
                {container.containerId || '—'}
              </Text>
            ),
          },
          ...(!configuration
            ? [
                {
                  key: 'state',
                  label: zh ? '上次状态' : 'Previous state',
                  children: formatContainerStateLabel(container.lastState),
                },
              ]
            : []),
        ]}
      />
      {loading && !configuration ? (
        <ManagementState
          compact
          kind="loading"
          title={zh ? '正在读取容器配置与退出记录' : 'Reading container configuration and state'}
        />
      ) : !configuration ? (
        <Text type="secondary">
          {zh ? '容器配置与退出记录暂不可用' : 'Container configuration and state are unavailable'}
        </Text>
      ) : (
        <>
          <div className="soha-pod-container-runtime-grid">
            <Runtime
              title={zh ? '当前状态' : 'Current state'}
              value={configuration.current}
              zh={zh}
            />
            <Runtime
              title={zh ? '上次终止' : 'Previous termination'}
              value={configuration.previous}
              zh={zh}
            />
          </div>
          <section className="soha-pod-container-section">
            <h4>{zh ? '资源配额' : 'Resources'}</h4>
            <div className="soha-pod-container-resource-grid">
              <div>
                <Text type="secondary">Requests</Text>
                {quantities(configuration.requests)}
              </div>
              <div>
                <Text type="secondary">Limits</Text>
                {quantities(configuration.limits)}
              </div>
            </div>
          </section>
          <section className="soha-pod-container-section">
            <h4>{zh ? '启动命令与参数' : 'Command and arguments'}</h4>
            <Descriptions
              column={1}
              size="small"
              items={[
                {
                  key: 'command',
                  label: 'Command',
                  children: <Arguments values={configuration.command} zh={zh} />,
                },
                {
                  key: 'args',
                  label: 'Args',
                  children: <Arguments values={configuration.args} zh={zh} />,
                },
              ]}
            />
          </section>
          <section className="soha-pod-container-section">
            <h4>{zh ? '环境变量' : 'Environment'}</h4>
            {configuration.env.length ? (
              <AdminTable
                columns={envColumns}
                dataSource={configuration.env.map((item, index) => ({
                  ...item,
                  key: `${item.name}:${index}`,
                }))}
                rowKey="key"
                pagination={false}
                enableColumnSelection={false}
                tableLayout="fixed"
                scroll={{ x: 520 }}
              />
            ) : (
              <Text type="secondary">
                {zh ? '未声明独立环境变量' : 'No individual variables declared'}
              </Text>
            )}
            {Boolean(configuration.envFrom.length) && (
              <div className="soha-pod-container-env-from">
                <Text type="secondary">
                  {zh ? '批量导入（EnvFrom）' : 'Imported sources (EnvFrom)'}
                </Text>
                {configuration.envFrom.map((item, index) => (
                  <div key={index}>
                    {reference(item.source)}
                    {item.prefix && <Text type="secondary"> · prefix: {item.prefix}</Text>}
                  </div>
                ))}
              </div>
            )}
          </section>
          <section className="soha-pod-container-section">
            <div className="soha-pod-container-section-heading">
              <h4>{zh ? '挂载' : 'Mounts'}</h4>
              <Button type="link" size="small" onClick={onViewStorage}>
                {zh ? '查看全部卷' : 'View all volumes'}
              </Button>
            </div>
            {configuration.mounts.length ? (
              <AdminTable
                columns={mountColumns}
                dataSource={configuration.mounts}
                rowKey={(item) => `${item.name}:${item.path}`}
                pagination={false}
                enableColumnSelection={false}
                tableLayout="fixed"
                scroll={{ x: 530 }}
              />
            ) : (
              <Text type="secondary">{zh ? '未声明卷挂载' : 'No volume mounts declared'}</Text>
            )}
          </section>
          <section className="soha-pod-container-section">
            <h4>{zh ? '声明的端口' : 'Declared ports'}</h4>
            {configuration.ports.length ? (
              <ul className="soha-pod-container-ports">
                {configuration.ports.map((port, index) => (
                  <li key={index}>
                    {port.name && <strong>{port.name} · </strong>}
                    {port.port ?? '—'}/{port.protocol || '—'}
                    {port.hostPort !== undefined && (
                      <Text type="secondary">
                        {' '}
                        · Host {port.hostIP || '*'}:{port.hostPort}
                      </Text>
                    )}
                  </li>
                ))}
              </ul>
            ) : (
              <Text type="secondary">{zh ? '未声明端口' : 'No ports declared'}</Text>
            )}
          </section>
          <section className="soha-pod-container-section">
            <h4>{zh ? '健康检查' : 'Health probes'}</h4>
            {configuration.probes.length ? (
              <div className="soha-pod-container-probes">
                {configuration.probes.map(({ name, probe }) => (
                  <div key={name}>
                    <strong>
                      {zh
                        ? { liveness: '存活探针', readiness: '就绪探针', startup: '启动探针' }[name]
                        : name}
                    </strong>
                    <p>
                      {probe.type}
                      {probe.target && ` · ${probe.target}`}
                    </p>
                    {probe.command && <Arguments values={probe.command} zh={zh} />}
                    {probe.headers.map((header, index) => (
                      <div key={index}>
                        {header.name}: {header.value}
                      </div>
                    ))}
                    <Text type="secondary">
                      {Object.entries(probe.settings)
                        .map(
                          ([key, value]) => `${key}=${value}${key.endsWith('Seconds') ? 's' : ''}`,
                        )
                        .join(' · ')}
                    </Text>
                  </div>
                ))}
              </div>
            ) : (
              <Text type="secondary">{zh ? '未配置探针' : 'No probes configured'}</Text>
            )}
          </section>
        </>
      )}
    </div>
  )
}
