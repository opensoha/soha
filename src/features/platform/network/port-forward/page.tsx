import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { useKubernetesResourceActions } from '@/features/platform/shared/use-kubernetes-resource-actions'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  capabilityActionTooltip,
  useClusterCapability,
} from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  Button,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { ReactNode } from 'react'
import { useDeferredValue, useMemo, useState } from 'react'
import '../styles.css'
import { portForwardMutations } from './mutations'
import { portForwardQueries } from './queries'
import type { PortForwardDraft, PortForwardSession } from './types'

const { Text } = Typography

function normalizeSearchKeyword(value: string) {
  return value.trim().toLowerCase()
}

function includesSearch(values: Array<string | undefined | null>, keyword: string) {
  if (!keyword) return true
  return values.some((value) => (value ?? '').toLowerCase().includes(keyword))
}

function createPortForwardDraft(namespace: string | null) {
  return {
    targetKind: 'Pod',
    targetName: '',
    namespace: namespace || 'default',
    localPort: 8080,
    remotePort: 80,
  }
}

function PlatformTableState({
  description,
  kind,
}: {
  description: ReactNode
  kind?: 'empty' | 'error' | 'select-scope'
}) {
  return (
    <ManagementState bordered={false} compact description={description} kind={kind ?? 'empty'} />
  )
}

export function NetworkPortForwardPage() {
  const { localeCode } = useI18n()
  const queryClient = useQueryClient()
  const { clusterId, namespace } = usePlatformScopeStore()
  const scope = toScopeKey(clusterId, null)
  const portForwardCapability = useClusterCapability('port.forward', localeCode)
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canCreate = hasPermission(permissionSnapshot, 'platform.network.port-forwards.create')
  const canDelete = hasPermission(permissionSnapshot, 'platform.network.port-forwards.delete')
  const [modalVisible, setModalVisible] = useState(false)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const [form, setForm] = useState<Omit<PortForwardDraft, 'scope'>>(() =>
    createPortForwardDraft(namespace),
  )

  const query = useQuery(portForwardQueries.list(scope))
  const visibleQueryData = readableQueryData(query)
  const registerMutation = usePlatformMutation(portForwardMutations.register(queryClient), {
    label: ['建立端口转发', 'Create port forward'],
    target: platformResourceTarget,
  })
  const stopMutation = usePlatformMutation(portForwardMutations.stop(queryClient), {
    label: ['停止端口转发', 'Stop port forward'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = registerMutation.blocked || stopMutation.blocked

  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeSearchKeyword(deferredSearchKeyword)
  const rawItems = visibleQueryData ?? []
  const scopedItems = useMemo(
    () => (namespace ? rawItems.filter((item) => item.namespace === namespace) : rawItems),
    [rawItems, namespace],
  )
  const filteredItems = useMemo(
    () =>
      scopedItems.filter((item) =>
        includesSearch(
          [
            item.sessionId,
            item.namespace,
            item.targetKind,
            item.targetName,
            item.status,
            String(item.localPort),
            String(item.remotePort),
          ],
          normalizedKeyword,
        ),
      ),
    [normalizedKeyword, scopedItems],
  )
  const effectiveEmpty = !clusterId
    ? localeCode === 'zh_CN'
      ? '请选择集群'
      : 'Select a cluster'
    : normalizedKeyword && rawItems.length > 0
      ? localeCode === 'zh_CN'
        ? '没有匹配的 Port Forward'
        : 'No matching port forward sessions'
      : localeCode === 'zh_CN'
        ? '当前集群没有登记的 Port Forward'
        : 'No port forward sessions registered'
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const portForwardUnsupported = portForwardCapability.status === 'unsupported'
  const canShowActions = canDelete && !portForwardUnsupported && rawItems.length > 0
  const portForwardCapabilityReason = portForwardCapability.reason
  const createPortForwardLabel = localeCode === 'zh_CN' ? '新建 Port Forward' : 'New Port Forward'
  const stopPortForwardLabel = localeCode === 'zh_CN' ? '停止 Port Forward' : 'Stop port forward'

  const resourceActions = useKubernetesResourceActions({
    resourceKey: 'port-forwards',
    resourceLabel: 'Port Forward',
    records: rawItems,
    getName: (item) => item.sessionId,
    getKey: (item) => item.sessionId,
    actions: (item) =>
      canDelete && !portForwardUnsupported
        ? [
            {
              key: 'stop',
              label: stopPortForwardLabel,
              icon: <DeleteOutlined />,
              danger: true,
              disabled: platformWriteBlocked,
              loading:
                stopMutation.isPending && stopMutation.variables?.sessionId === item.sessionId,
              prepare: () => stopMutation.reset(),
              feedback: stopMutation.feedback,
              confirm: {
                title:
                  localeCode === 'zh_CN' ? '确认停止该 Port Forward？' : 'Stop this port forward?',
                description:
                  localeCode === 'zh_CN'
                    ? '这只会停止 Soha 中登记的转发会话记录。'
                    : 'This stops the registered forward session record in Soha.',
              },
              run: () => stopMutation.mutateAsync({ scope, sessionId: item.sessionId }),
            },
          ]
        : [],
  })

  const columns: TableColumnsType<PortForwardSession> = [
    {
      title: localeCode === 'zh_CN' ? '会话' : 'Session',
      dataIndex: 'sessionId',
      render: (value: string) => <Text code>{value.slice(0, 8)}</Text>,
    },
    { title: 'Namespace', dataIndex: 'namespace' },
    {
      title: localeCode === 'zh_CN' ? '目标' : 'Target',
      dataIndex: 'targetName',
      render: (_: unknown, record: PortForwardSession) =>
        `${record.targetKind}/${record.targetName}`,
    },
    { title: localeCode === 'zh_CN' ? '本地端口' : 'Local', dataIndex: 'localPort' },
    { title: localeCode === 'zh_CN' ? '远端端口' : 'Remote', dataIndex: 'remotePort' },
    {
      title: localeCode === 'zh_CN' ? '状态' : 'Status',
      dataIndex: 'status',
      render: (value: string) => (
        <Tag color={value === 'active' ? 'green' : 'default'}>{value}</Tag>
      ),
    },
    {
      title: localeCode === 'zh_CN' ? '创建时间' : 'Created',
      dataIndex: 'createdAt',
      render: (value: string) => formatDateTime(value),
    },
    resourceActions.column,
  ]

  return (
    <ManagementDataPage
      query={{
        onFinish: () => undefined,
        actions: (
          <ManagementQueryActions
            disabledReset={!searchKeyword.trim()}
            resetLabel={localeCode === 'zh_CN' ? '重置' : 'Reset'}
            submitLabel={localeCode === 'zh_CN' ? '查询' : 'Search'}
            onReset={() => setSearchKeyword('')}
          />
        ),
        children: (
          <ManagementKeywordField
            label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
            value={searchKeyword}
            onChange={setSearchKeyword}
            placeholder={
              localeCode === 'zh_CN'
                ? '搜索会话 / Namespace / 目标 / 状态 / 端口'
                : 'Search session / namespace / target / status / port'
            }
            inputProps={{
              className: 'soha-platform-compact-field soha-workload-search-input',
              size: 'small',
            }}
          />
        ),
      }}
      tableNode={
        <KubernetesResourceTable
          resourceKey="port-forwards"
          resourceLabel="Port Forwards"
          allRows={scopedItems}
          filterKey={normalizedKeyword}
          {...{
            error: query.isEnabled === false ? null : query.error,
            hasReadData: query.data !== undefined,
            onRetry: query.isEnabled === false ? undefined : () => query.refetch(),
            className: 'soha-platform-table',
            columnSettingIconOnly: true,
            columnSettingPlacement: 'header',
            headerExtra: (
              <ManagementTableToolbar>
                {canCreate ? (
                  <Tooltip
                    title={
                      !clusterId
                        ? localeCode === 'zh_CN'
                          ? '请先选择集群'
                          : 'Select a cluster first'
                        : capabilityActionTooltip(createPortForwardLabel, portForwardCapability)
                    }
                  >
                    <span>
                      <Button
                        autoInsertSpace={false}
                        size="small"
                        type="primary"
                        icon={<PlusOutlined />}
                        disabled={!clusterId || portForwardUnsupported}
                        onClick={() => {
                          setForm(createPortForwardDraft(namespace))
                          setModalVisible(true)
                        }}
                      >
                        {createPortForwardLabel}
                      </Button>
                    </span>
                  </Tooltip>
                ) : null}
                <ManagementDensityButton
                  aria-label={densityLabel}
                  title={densityLabel}
                  tooltip={densityLabel}
                  onClick={() =>
                    setTableSize((current) => (current === 'middle' ? 'small' : 'middle'))
                  }
                />
                <ManagementRefreshButton
                  aria-label={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                  disabled={!clusterId}
                  loading={query.isFetching}
                  tooltip={localeCode === 'zh_CN' ? '刷新' : 'Refresh'}
                  onClick={() => {
                    if (clusterId) {
                      void query.refetch()
                    }
                  }}
                />
              </ManagementTableToolbar>
            ),
            columns: canShowActions
              ? columns
              : columns.filter((column) => column.key !== 'actions'),
            dataSource: clusterId ? filteredItems : [],
            rowKey: 'sessionId',
            loading: query.isLoading,
            localSorting: true,
            tableSize,
            scroll: { x: 'max-content' },
            empty: (
              <PlatformTableState
                description={effectiveEmpty}
                kind={!clusterId ? 'select-scope' : 'empty'}
              />
            ),
          }}
        />
      }
    >
      {registerMutation.feedback}
      {resourceActions.isConfirming ? null : stopMutation.feedback}
      {resourceActions.confirmation}

      {portForwardCapabilityReason ? (
        <Alert
          showIcon
          type={portForwardUnsupported ? 'warning' : 'info'}
          style={{ marginBottom: 12 }}
          title={
            localeCode === 'zh_CN' ? 'Port Forward 连接模式说明' : 'Port forward connection mode'
          }
          description={portForwardCapabilityReason}
        />
      ) : null}
      <Modal
        okButtonProps={{ disabled: platformWriteBlocked }}
        title={localeCode === 'zh_CN' ? '新建 Port Forward' : 'New Port Forward'}
        open={modalVisible}
        onOk={() =>
          registerMutation.mutate(
            { scope, ...form },
            {
              onSuccess: () => {
                setModalVisible(false)
              },
            },
          )
        }
        onCancel={() => {
          if (!platformWriteBlocked) (() => setModalVisible(false))?.()
        }}
        confirmLoading={registerMutation.isPending}
      >
        {registerMutation.feedback}
        {stopMutation.feedback}

        <Space orientation="vertical" align="start" style={{ width: '100%' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '目标类型' : 'Target kind'}</Text>
            <Select
              value={form.targetKind}
              onChange={(value) => setForm((prev) => ({ ...prev, targetKind: String(value) }))}
              style={{ flex: 1 }}
              options={[
                { value: 'Pod', label: 'Pod' },
                { value: 'Service', label: 'Service' },
              ]}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>Namespace</Text>
            <Input
              value={form.namespace}
              onChange={(event) => setForm((prev) => ({ ...prev, namespace: event.target.value }))}
              style={{ flex: 1 }}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '目标名称' : 'Target name'}</Text>
            <Input
              value={form.targetName}
              onChange={(event) => setForm((prev) => ({ ...prev, targetName: event.target.value }))}
              style={{ flex: 1 }}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '本地端口' : 'Local port'}</Text>
            <InputNumber
              value={form.localPort}
              min={1}
              max={65535}
              onChange={(v) => setForm((prev) => ({ ...prev, localPort: Number(v) || 0 }))}
              style={{ flex: 1 }}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%' }}>
            <Text style={{ width: 96 }}>{localeCode === 'zh_CN' ? '远端端口' : 'Remote port'}</Text>
            <InputNumber
              value={form.remotePort}
              min={1}
              max={65535}
              onChange={(v) => setForm((prev) => ({ ...prev, remotePort: Number(v) || 0 }))}
              style={{ flex: 1 }}
            />
          </div>
        </Space>
      </Modal>
    </ManagementDataPage>
  )
}
