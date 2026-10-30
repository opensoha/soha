import {
  kubernetesDeleteConfirmation,
  useKubernetesResourceActions,
} from '@/features/platform/shared/use-kubernetes-resource-actions'
import { KubernetesResourceTable } from '@/features/platform/shared/kubernetes-resource-table'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { hasAllowedAction } from '@/features/auth'
import type { AIPageContext } from '@/features/copilot'
import { encodeAIContextForElement, useAIPageContext } from '@/features/copilot'
import { CreateEntry } from '@/features/platform/resource-creation/components/create-entry'
import { getResourceCreateTemplate } from '@/features/platform/resource-creation/templates'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, EditOutlined } from '@ant-design/icons'
import type { UseQueryResult } from '@tanstack/react-query'
import { useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { useDeferredValue, useMemo, useState } from 'react'
import '../styles.css'
import { networkMutations } from './mutations'
import { networkTargetFromRecord } from './scope'
import type { NetworkKind, NetworkResourceRecord } from './types'

const NETWORK_ENTITY_KINDS: Record<NetworkKind, string> = {
  services: 'Service',
  ingresses: 'Ingress',
  gatewayclasses: 'GatewayClass',
  gateways: 'Gateway',
  httproutes: 'HTTPRoute',
  backendtlspolicies: 'BackendTLSPolicy',
  grpcroutes: 'GRPCRoute',
  referencegrants: 'ReferenceGrant',
  endpointslices: 'EndpointSlice',
  ingressclasses: 'IngressClass',
  networkpolicies: 'NetworkPolicy',
}

function normalizeKeyword(value: string) {
  return value.trim().toLowerCase()
}

export function NetworkResourceListPage<T extends NetworkResourceRecord>({
  buildAIPageContext,
  columns,
  deletable = true,
  emptyDescription,
  kind,
  noMatchDescription,
  onEdit,
  onRow,
  query,
  rowKey,
  searchPlaceholder,
  searchValues,
}: {
  buildAIPageContext: (items: T[], searchKeyword: string) => AIPageContext
  columns: TableColumnsType<T>
  deletable?: boolean
  emptyDescription: { en_US: string; zh_CN: string }
  kind: NetworkKind
  noMatchDescription: { en_US: string; zh_CN: string }
  onEdit?: (record: T) => void
  onRow?: (record: T) => Record<string, string>
  query: UseQueryResult<T[], Error>
  rowKey?: string | ((record: T) => string)
  searchPlaceholder: { en_US: string; zh_CN: string }
  searchValues: (record: T) => Array<string | undefined | null>
}) {
  const { localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [searchKeyword, setSearchKeyword] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const deferredSearchKeyword = useDeferredValue(searchKeyword)
  const normalizedKeyword = normalizeKeyword(deferredSearchKeyword)
  const rawItems = readableQueryData(query) ?? []
  const filteredItems = useMemo(
    () =>
      rawItems.filter((record) =>
        searchValues(record).some((value) =>
          (value ?? '').toLowerCase().includes(normalizedKeyword),
        ),
      ),
    [normalizedKeyword, rawItems, searchValues],
  )
  useAIPageContext(buildAIPageContext(rawItems, searchKeyword))
  const removeMutation = usePlatformMutation(networkMutations.remove(kind, queryClient), {
    label: ['删除资源', 'Delete resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = removeMutation.blocked
  const densityLabel = localeCode === 'zh_CN' ? '切换表格密度' : 'Toggle table density'
  const createKind = [
    'services',
    'ingresses',
    'gatewayclasses',
    'gateways',
    'httproutes',
    'grpcroutes',
  ].includes(kind)
    ? NETWORK_ENTITY_KINDS[kind]
    : null
  const createAPIVersion =
    kind === 'services'
      ? 'v1'
      : kind === 'ingresses'
        ? 'networking.k8s.io/v1'
        : 'gateway.networking.k8s.io/v1'
  const createScope = kind === 'gatewayclasses' ? 'cluster' : 'namespace'
  const effectiveEmpty = !clusterId
    ? localeCode === 'zh_CN'
      ? '请选择集群'
      : 'Select a cluster'
    : normalizedKeyword && rawItems.length > 0
      ? noMatchDescription[localeCode]
      : emptyDescription[localeCode]
  const rowProps = (record: T) => {
    const provided = onRow?.(record) ?? {}
    return {
      ...provided,
      'data-ai-context':
        provided['data-ai-context'] ??
        encodeAIContextForElement({
          sourceWorkbench: 'platform',
          entityKind: NETWORK_ENTITY_KINDS[kind],
          entityName: record.name,
          clusterId: clusterId || undefined,
          namespace: record.namespace || undefined,
          service: kind === 'services' ? record.name : undefined,
        }),
    }
  }

  const resourceActions = useKubernetesResourceActions<T>({
    resourceKey: `network:${kind}`,
    resourceLabel: NETWORK_ENTITY_KINDS[kind],
    records: rawItems,
    getName: (record) => record.name,
    getKey: (record) => `${record.namespace ?? ''}/${record.name}`,
    actions: (record) => {
      const target = networkTargetFromRecord(clusterId, record)
      return [
        ...(onEdit && hasAllowedAction(record.allowedActions, 'update')
          ? [
              {
                key: 'edit',
                label: localeCode === 'zh_CN' ? '编辑' : 'Edit',
                icon: <EditOutlined />,
                disabled: platformWriteBlocked,
                run: () => onEdit(record),
              },
            ]
          : []),
        ...(deletable && hasAllowedAction(record.allowedActions, 'delete')
          ? [
              {
                key: 'delete',
                label: localeCode === 'zh_CN' ? '删除' : 'Delete',
                icon: <DeleteOutlined />,
                danger: true,
                disabled: platformWriteBlocked,
                loading:
                  removeMutation.isPending &&
                  removeMutation.variables?.scope.clusterId === clusterId &&
                  removeMutation.variables?.name === target.name &&
                  removeMutation.variables.scope.namespace === target.scope.namespace,
                confirm: kubernetesDeleteConfirmation(record, localeCode),
                prepare: () => removeMutation.reset(),
                feedback: removeMutation.feedback,
                run: () => removeMutation.mutateAsync(target),
              },
            ]
          : []),
      ]
    },
  })

  return (
    <>
      {resourceActions.confirmation}
      {!resourceActions.isConfirming && removeMutation.feedback}
      <ManagementDataPage
        query={{
          onFinish: () => undefined,
          actions: (
            <ManagementQueryActions
              disabledReset={!searchKeyword.trim()}
              onReset={() => setSearchKeyword('')}
              resetLabel={localeCode === 'zh_CN' ? '重置' : 'Reset'}
              submitLabel={localeCode === 'zh_CN' ? '查询' : 'Search'}
            />
          ),
          children: (
            <ManagementKeywordField
              label={localeCode === 'zh_CN' ? '关键词' : 'Keyword'}
              value={searchKeyword}
              onChange={setSearchKeyword}
              placeholder={searchPlaceholder[localeCode]}
              inputProps={{
                className: 'soha-platform-compact-field soha-workload-search-input',
                size: 'small',
              }}
            />
          ),
        }}
        tableNode={
          <KubernetesResourceTable
            resourceKey={`network:${kind}`}
            resourceLabel={NETWORK_ENTITY_KINDS[kind]}
            allRows={rawItems}
            filterKey={normalizedKeyword}
            scopeMode={
              ['gatewayclasses', 'ingressclasses'].includes(kind) ? 'cluster' : 'namespace'
            }
            error={query.isEnabled === false ? null : query.error}
            hasReadData={query.data !== undefined}
            onRetry={query.isEnabled === false ? undefined : () => query.refetch()}
            className={'soha-platform-table'}
            columnSettingIconOnly={true}
            columnSettingPlacement={'header'}
            columns={resourceActions.hasActions ? [...columns, resourceActions.column] : columns}
            dataSource={clusterId ? filteredItems : []}
            rowKey={rowKey ?? ((record) => `${record.namespace}/${record.name}`)}
            onRow={rowProps}
            loading={query.isLoading}
            localSorting={true}
            tableSize={tableSize}
            scroll={{ x: 'max-content' }}
            headerExtra={
              <ManagementTableToolbar>
                {createKind ? (
                  <CreateEntry
                    context={{
                      clusterId: clusterId || '',
                      defaultNamespace:
                        createScope === 'namespace' ? namespace || undefined : undefined,
                      expectedApiVersion: createAPIVersion,
                      expectedKind: createKind,
                      resourceGroup: 'network',
                      scopeMode: createScope,
                      source: 'list',
                    }}
                    defaultTemplate={getResourceCreateTemplate(createKind)}
                    label={createKind}
                  />
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
                    if (clusterId) void query.refetch()
                  }}
                />
              </ManagementTableToolbar>
            }
            empty={
              <ManagementState
                bordered={false}
                compact
                description={effectiveEmpty}
                kind={!clusterId ? 'select-scope' : 'empty'}
              />
            }
          />
        }
      />
    </>
  )
}
