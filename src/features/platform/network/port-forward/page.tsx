import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { useKubernetesResourceActions } from '@/features/platform/shared/use-kubernetes-resource-actions'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryField,
  ManagementQueryActions,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  capabilityActionTooltip,
  useClusterCapability,
} from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import {
  WorkloadRefresh,
  useWorkloadRefresh,
} from '@/features/platform/workloads/shared/workload-refresh'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { PauseCircleOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Select, Tooltip, Typography, type TableColumnsType } from 'antd'
import { useDeferredValue, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import '../styles.css'
import './styles.css'
import { networkResourcePath } from '../shared/resource-links'
import { CreateForwardForm } from './create-form'
import { sessionCanStop } from './model'
import { portForwardMutations } from './mutations'
import { portForwardQueries } from './queries'
import type { PortForwardSession } from './types'

export function NetworkPortForwardPage() {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const client = useQueryClient(),
    { clusterId, namespace } = usePlatformScopeStore()
  const scope = toScopeKey(clusterId, null),
    capability = useClusterCapability('port.forward', localeCode)
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const permitted = (a: string) => hasPermission(permissions, `platform.network.port-forwards.${a}`)
  const canRead = permitted('view'),
    canCreate = permitted('create'),
    canStop = permitted('delete')
  const [modal, setModal] = useState<{ clusterId: string; namespace: string | null }>()
  const [keyword, setKeyword] = useState(''),
    [status, setStatus] = useState<string>(),
    [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const search = useDeferredValue(keyword).trim().toLowerCase(),
    refresh = useWorkloadRefresh()
  const options = portForwardQueries.list(scope)
  const query = useQuery({
    ...options,
    enabled: Boolean(options.enabled) && canRead,
    refetchInterval: refresh.interval,
  })
  const raw = canRead ? readableQueryData(query) || [] : []
  const scoped = useMemo(
    () => raw.filter((r) => r.clusterId === clusterId && (!namespace || r.namespace === namespace)),
    [raw, clusterId, namespace],
  )
  const filtered = useMemo(
    () =>
      scoped.filter(
        (r) =>
          (!status || r.status === status) &&
          (!search ||
            [
              r.sessionId,
              r.namespace,
              r.targetKind,
              r.targetName,
              r.status,
              r.createdBy,
              String(r.localPort),
              String(r.remotePort),
            ].some((v) => v?.toLowerCase().includes(search))),
      ),
    [scoped, search, status],
  )
  const register = usePlatformMutation(portForwardMutations.register(client), {
    label: ['创建转发会话', 'Create forward session'],
    target: platformResourceTarget,
  })
  const stop = usePlatformMutation(portForwardMutations.stop(client), {
    label: ['停止转发会话', 'Stop forward session'],
    target: platformResourceTarget,
  })
  const blocked = register.blocked || stop.blocked
  const actions = useKubernetesResourceActions<PortForwardSession>({
    resourceKey: 'port-forwards',
    resourceLabel: 'Port Forward',
    records: scoped,
    getName: (r) => r.sessionId,
    getKey: (r) => r.sessionId,
    scopeKey: JSON.stringify([clusterId, namespace]),
    actions: (r) =>
      canStop && sessionCanStop(r.status)
        ? [
            {
              key: 'stop',
              label: zh ? '停止会话' : 'Stop session',
              icon: <PauseCircleOutlined />,
              danger: true,
              disabled: blocked,
              loading: stop.isPending && stop.variables?.sessionId === r.sessionId,
              prepare: () => stop.reset(),
              feedback: stop.feedback,
              confirm: {
                title: zh ? '停止此端口转发会话？' : 'Stop this port forward session?',
                description: (
                  <div className="soha-forward-wrap">
                    <p>
                      {r.namespace} · {r.targetKind}/{r.targetName}
                    </p>
                    <p>
                      Core 127.0.0.1:{r.localPort} → {r.remotePort}
                    </p>
                    <p>{r.sessionId}</p>
                    <span>
                      {zh
                        ? '将请求后端停止此会话并释放其监听；经此会话的连接会中断，目标资源不被删除。'
                        : 'Requests backend session shutdown and listener release. Connections through this session will close; the target resource is retained.'}
                    </span>
                  </div>
                ),
              },
              run: () => {
                if (r.clusterId !== clusterId) throw new Error('Port forward scope changed')
                return stop.mutateAsync({ scope, sessionId: r.sessionId })
              },
            },
          ]
        : [],
  })
  const target = (r: PortForwardSession) => {
    const path = ['Pod', 'Service'].includes(r.targetKind)
      ? networkResourcePath(permissions, {
          clusterId: r.clusterId,
          namespace: r.namespace,
          kind: r.targetKind,
          name: r.targetName,
        })
      : null
    return (
      <div className="soha-forward-target">
        <MetadataTag label={r.targetKind} />
        {path ? (
          <Link className="soha-forward-wrap" to={path}>
            {r.targetName}
          </Link>
        ) : (
          <TableCellText value={r.targetName} />
        )}
      </div>
    )
  }
  const columns: TableColumnsType<PortForwardSession> = [
    {
      title: zh ? '会话 ID' : 'Session ID',
      dataIndex: 'sessionId',
      width: 320,
      minWidth: 260,
      render: (v: string) => (
        <Typography.Text className="soha-forward-wrap" copyable={{ text: v }}>
          {v}
        </Typography.Text>
      ),
    },
    { title: zh ? '命名空间' : 'Namespace', dataIndex: 'namespace', width: 176 },
    {
      title: zh ? '目标' : 'Target',
      dataIndex: 'targetName',
      width: 280,
      minWidth: 200,
      render: (_: unknown, r: PortForwardSession) => target(r),
    },
    {
      title: zh ? 'Core 监听端口' : 'Core listener',
      dataIndex: 'localPort',
      width: 185,
      minWidth: 160,
      render: (v: number) => (
        <Typography.Text copyable={{ text: `127.0.0.1:${v}` }}>127.0.0.1:{v}</Typography.Text>
      ),
    },
    { title: zh ? '目标端口' : 'Target port', dataIndex: 'remotePort', width: 110 },
    {
      title: zh ? '会话状态' : 'Session status',
      dataIndex: 'status',
      width: 130,
      render: (v: string) => <StatusTag value={v} label={v} />,
    },
    {
      title: zh ? '创建者' : 'Created by',
      dataIndex: 'createdBy',
      width: 180,
      minWidth: 140,
      render: (v?: string) => <TableCellText value={v} />,
    },
    {
      title: zh ? '创建时间' : 'Created',
      dataIndex: 'createdAt',
      width: 180,
      render: (v: string) => formatDateTime(v),
    },
    ...(actions.hasActions ? [actions.column] : []),
  ]
  return (
    <>
      <ReadFeedback canRead={canRead} query={query} label="Port Forwards" />
      {actions.confirmation}
      {!actions.isConfirming && stop.feedback}
      {!modal && register.feedback}
      {canRead && (
        <ManagementDataPage
          query={{
            onFinish: () => undefined,
            actions: (
              <ManagementQueryActions
                disabledReset={!keyword && !status}
                onReset={() => {
                  setKeyword('')
                  setStatus(undefined)
                }}
                resetLabel={zh ? '重置' : 'Reset'}
                submitLabel={zh ? '查询' : 'Search'}
              />
            ),
            children: (
              <>
                <ManagementKeywordField
                  label={zh ? '关键词' : 'Keyword'}
                  value={keyword}
                  onChange={setKeyword}
                  placeholder={
                    zh
                      ? '搜索会话 / 目标 / 创建者 / 端口'
                      : 'Search session / target / creator / port'
                  }
                  inputProps={{
                    className: 'soha-platform-compact-field soha-workload-search-input',
                    size: 'small',
                  }}
                />
                <ManagementQueryField label={zh ? '状态' : 'Status'} width={240} minWidth={220}>
                  <Select
                    aria-label={zh ? '会话状态筛选' : 'Session status filter'}
                    size="small"
                    allowClear
                    placeholder={zh ? '全部状态' : 'All states'}
                    value={status}
                    onChange={setStatus}
                    options={[...new Set(scoped.map((r) => r.status))].map((s) => ({
                      value: s,
                      label: s,
                    }))}
                  />
                </ManagementQueryField>
              </>
            ),
          }}
          tableNode={
            <KubernetesResourceTable
              resourceKey="port-forwards"
              resourceLabel="Port Forwards"
              scopeMode="namespace"
              allRows={scoped}
              dataSource={clusterId ? filtered : []}
              filterKey={JSON.stringify([search, status])}
              rowKey="sessionId"
              columns={columns}
              localSorting
              loading={query.isLoading}
              tableSize={tableSize}
              error={query.isEnabled ? query.error : null}
              hasReadData={query.data !== undefined}
              onRetry={query.isEnabled ? () => query.refetch() : undefined}
              className="soha-platform-table"
              columnSettingIconOnly
              columnSettingPlacement="header"
              headerExtra={
                <ManagementTableToolbar>
                  {canCreate && (
                    <Tooltip
                      title={capabilityActionTooltip(
                        zh ? '创建会话' : 'Create session',
                        capability,
                      )}
                    >
                      <span>
                        <Button
                          autoInsertSpace={false}
                          size="small"
                          type="primary"
                          icon={<PlusOutlined />}
                          disabled={!clusterId || capability.disabled || blocked}
                          onClick={() => {
                            if (clusterId) {
                              register.reset()
                              setModal({ clusterId, namespace })
                            }
                          }}
                        >
                          {zh ? '新建转发' : 'New forward'}
                        </Button>
                      </span>
                    </Tooltip>
                  )}
                  <ManagementDensityButton
                    aria-label={zh ? '切换表格密度' : 'Toggle density'}
                    tooltip={zh ? '切换表格密度' : 'Toggle density'}
                    onClick={() => setTableSize((v) => (v === 'small' ? 'middle' : 'small'))}
                  />
                  <WorkloadRefresh
                    compact
                    disabled={!clusterId}
                    settings={refresh}
                    queries={[query]}
                    resourceLabel="Port Forwards"
                    onRefresh={() => query.refetch()}
                  />
                </ManagementTableToolbar>
              }
              empty={
                <ManagementState
                  compact
                  bordered={false}
                  kind={!clusterId ? 'select-scope' : 'empty'}
                  description={
                    !clusterId
                      ? zh
                        ? '请选择集群'
                        : 'Select a cluster'
                      : search || status
                        ? zh
                          ? '没有匹配的会话'
                          : 'No matching sessions'
                        : zh
                          ? '本次读取范围没有转发会话'
                          : 'No forward sessions returned in this scope'
                  }
                />
              }
            />
          }
        >
          <Alert
            showIcon
            type="info"
            title={zh ? 'Core 主机端口转发' : 'Core-host port forwarding'}
            description={
              zh
                ? '监听位于 Core 进程的 127.0.0.1；部署到 K8s 后位于 Core Pod 内，不会自动改为 Pod IP。此地址不是浏览器电脑的本地端口。刷新只重读，不自动重连。'
                : 'Listeners bind to the Core process’s 127.0.0.1, inside the Core Pod on Kubernetes, not its Pod IP or the browser computer. Refresh only rereads sessions; it does not reconnect.'
            }
          />
          {capability.status !== 'available' && capability.reason && (
            <Alert
              showIcon
              type={capability.status === 'unsupported' ? 'warning' : 'info'}
              title={zh ? '当前模式能力' : 'Connection-mode capability'}
              description={capability.reason}
            />
          )}
        </ManagementDataPage>
      )}
      {modal && (
        <CreateForwardForm
          key={modal.clusterId}
          clusterId={modal.clusterId}
          currentClusterId={clusterId}
          namespace={modal.namespace}
          items={raw.filter((r) => r.clusterId === modal.clusterId)}
          blocked={blocked}
          submitDisabled={!canCreate || capability.disabled}
          pending={register.isPending}
          feedback={register.feedback}
          onClose={() => setModal(undefined)}
          onSubmit={(values) => {
            if (canCreate && !capability.disabled && clusterId === modal.clusterId)
              register.mutate(
                { scope: toScopeKey(modal.clusterId, null), ...values },
                { onSuccess: () => setModal(undefined) },
              )
          }}
        />
      )}
    </>
  )
}
