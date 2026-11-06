import {
  ResourceDetailHeader,
  ResourceDetailTabs,
  useResourceDetailTab,
} from '@/features/platform/shared/resource-detail-header'
import { AdminTable } from '@/components/admin-table'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
import { TableCellText } from '@/components/table-cell-content'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { useAIPageContext } from '@/features/copilot'
import { ResourceGitOpsStatus } from '@/features/platform/shared/resource-gitops-status'
import { PlatformResourceOverview } from '@/features/platform/shared/resource-overview'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType, TabsProps } from 'antd'
import { App, Card, Spin } from 'antd'
import type { ReactNode } from 'react'
import { lazy, Suspense, useEffect, useState } from 'react'
import '../styles.css'
import { configurationMutations } from './mutations'
import { configurationQueries } from './queries'
import type {
  ConfigurationDetailBase,
  ConfigurationKind,
  ConfigurationReference,
  ConfigurationScopeMode,
  ConfigurationTarget,
} from './types'

const K8sYamlEditor = lazy(async () => {
  const module = await import('@/components/k8s-yaml-editor')
  return { default: module.K8sYamlEditor }
})

export function ConfigurationResourceOverview({
  detail,
  extra,
}: {
  detail: ConfigurationDetailBase
  extra?: Array<{ key: string; value: ReactNode }>
}) {
  return (
    <PlatformResourceOverview
      compact
      ageSeconds={detail.ageSeconds}
      annotations={detail.annotations}
      createdAt={detail.createdAt}
      facts={(extra ?? []).map((item) => ({ ...item, label: item.key }))}
      labels={detail.labels}
      name={detail.name}
      namespace={detail.namespace || undefined}
    />
  )
}

function ConfigurationReferencesTab({
  kind,
  target,
}: {
  kind: ConfigurationKind
  target: ConfigurationTarget
}) {
  const { localeCode } = useI18n()
  const referencesQuery = useQuery(configurationQueries.references(kind, target.scope, target.name))
  const visibleReferencesQueryData = readableQueryData(referencesQuery)
  const columns: TableColumnsType<ConfigurationReference> = [
    {
      title: localeCode === 'zh_CN' ? '资源类型' : 'Kind',
      dataIndex: 'kind',
      width: 150,
      render: (value: string) => <MetadataTag label={value} tone="blue" />,
    },
    {
      title: localeCode === 'zh_CN' ? '名称' : 'Name',
      dataIndex: 'name',
      ellipsis: { showTitle: false },
      render: (value: string) => <TableCellText value={value} />,
    },
    {
      title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
      dataIndex: 'namespace',
      width: 180,
    },
    {
      title: localeCode === 'zh_CN' ? '引用位置' : 'Reference',
      dataIndex: 'path',
      ellipsis: { showTitle: false },
      render: (value: string) => <TableCellText value={value} />,
    },
  ]
  return (
    <Card className="soha-detail-card">
      <ReadFeedback canRead={referencesQuery.isEnabled} query={referencesQuery} label="资源引用" />

      <AdminTable
        enableColumnSelection={false}
        scroll={{ x: '100%' }}
        className="soha-platform-table soha-config-reference-table"
        columns={columns}
        dataSource={visibleReferencesQueryData ?? []}
        loading={referencesQuery.isLoading}
        empty={
          <ManagementState
            bordered={false}
            compact
            description={localeCode === 'zh_CN' ? '暂无关联资源' : 'No referencing resources'}
          />
        }
        pagination={false}
        rowKey={(record) => `${record.kind}/${record.namespace}/${record.name}/${record.path}`}
        tableSize="small"
        tableLayout="fixed"
      />
    </Card>
  )
}

interface ConfigurationYAMLWrite {
  blocked: boolean
  isPending: boolean
  apply: (content: string, onSuccess: (content: string) => void) => void
}

interface ConfigurationYAMLTabProps {
  active?: boolean
  canUpdate?: boolean
  kind: ConfigurationKind
  scopeMode: ConfigurationScopeMode
  target: ConfigurationTarget
}

function DefaultConfigurationYAMLTab(props: ConfigurationYAMLTabProps) {
  const queryClient = useQueryClient()
  const mutation = usePlatformMutation(configurationMutations.updateYAML(props.kind, queryClient), {
    label: ['更新资源', 'Update resource'],
    target: platformResourceTarget,
  })
  return (
    <ConfigurationYAMLTab
      {...props}
      feedback={mutation.feedback}
      write={{
        blocked: mutation.blocked,
        isPending: mutation.isPending,
        apply: (content, onSuccess) =>
          mutation.mutate({ ...props.target, content }, { onSuccess: (yaml) => onSuccess(yaml.content) }),
      }}
    />
  )
}

function ConfigurationYAMLTab({
  active = true,
  canUpdate = true,
  feedback,
  kind,
  scopeMode,
  target,
  write,
}: ConfigurationYAMLTabProps & { feedback?: ReactNode; write: ConfigurationYAMLWrite }) {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const options = configurationQueries.yaml(kind, target.scope, target.name, scopeMode)
  const yamlQuery = useQuery({
    ...options,
    ...(kind === 'secrets' ? { gcTime: 0 } : {}),
    enabled: Boolean(options.enabled) && active,
  })
  const visibleYamlQueryData = readableQueryData(yamlQuery)
  const yamlState = useYamlDraft(
    JSON.stringify([kind, target.scope.clusterId, target.scope.namespace, target.name]),
    visibleYamlQueryData?.content,
  )
  const { value: draft, setValue: setDraft } = yamlState
  const applyDisabled =
    !canUpdate ||
    !active ||
    write.blocked ||
    !draft.trim() ||
    yamlQuery.isFetching ||
    Boolean(yamlQuery.error) ||
    !visibleYamlQueryData ||
    yamlState.original !== visibleYamlQueryData.content ||
    Boolean(queryClient.getQueryState(options.queryKey)?.isInvalidated)

  return (
    <Suspense
      fallback={
        <Card className="soha-detail-card">
          <Spin size="large" />
        </Card>
      }
    >
      <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />

      {feedback}

      {yamlQuery.isError && visibleYamlQueryData === undefined ? null : (
        <K8sYamlEditor
          key={yamlState.resourceKey}
          value={draft}
          original={yamlState.original}
          serverChanged={yamlState.serverChanged}
          onCompareLatest={yamlState.compareLatest}
          loading={yamlQuery.isPending}
          error={yamlQuery.error}
          onChange={setDraft}
          onReset={yamlState.reset}
          onSave={() =>
            void message.info(
              localeCode === 'zh_CN' ? '暂不支持本地草稿' : 'Local draft save disabled here',
            )
          }
          onApply={() => {
            const current = queryClient.getQueryState(options.queryKey)
            if (
              !applyDisabled &&
              current?.fetchStatus === 'idle' &&
              !current.error &&
              !current.isInvalidated &&
              current.data === visibleYamlQueryData
            ) {
              write.apply(draft, yamlState.applied)
            }
          }}
          saveDisabled
          applyDisabled={applyDisabled}
          applying={write.isPending}
        />
      )}
    </Suspense>
  )
}

export function ConfigurationDetailShell<TDetail extends ConfigurationDetailBase>({
  dataTab,
  detail,
  kind,
  label,
  overviewExtra,
  overviewContent,
  scopeMode = 'namespace',
  showReferences = true,
  target,
  header,
  overview,
  relatedResources,
  additionalTabs,
  activeTabKey: controlledTab,
  onTabChange,
  canUpdateYAML = true,
  preserveYaml = false,
  canReadYAML = true,
  yamlWrite,
}: {
  canReadYAML?: boolean
  yamlWrite?: ConfigurationYAMLWrite
  header?: ReactNode
  overview?: ReactNode
  relatedResources?: ReactNode
  additionalTabs?: NonNullable<TabsProps['items']>
  activeTabKey?: string
  onTabChange?: (key: string) => void
  canUpdateYAML?: boolean
  preserveYaml?: boolean
  dataTab?: ReactNode
  detail: TDetail
  kind: ConfigurationKind
  label: string
  overviewExtra?: Array<{ key: string; value: ReactNode }>
  overviewContent?: ReactNode
  scopeMode?: ConfigurationScopeMode
  showReferences?: boolean
  target: ConfigurationTarget
}) {
  const { t, localeCode } = useI18n()
  const [urlTab, setUrlTab] = useResourceDetailTab()
  const activeTabKey = controlledTab ?? urlTab
  const [visitedYaml, setVisitedYaml] = useState(activeTabKey === 'yaml')
  useEffect(() => {
    if (activeTabKey === 'yaml') setVisitedYaml(true)
  }, [activeTabKey])
  const changeTab = (key: string) => {
    if (key === 'yaml') setVisitedYaml(true)
    if (onTabChange) onTabChange(key)
    else setUrlTab(key)
  }
  useAIPageContext({
    sourceWorkbench: 'platform',
    sourceTitle: label,
    entityKind: label,
    entityName: detail.name,
    clusterId: target.scope.clusterId || undefined,
    namespace: detail.namespace || undefined,
  })
  const yamlProps = { active: activeTabKey === 'yaml', canUpdate: canUpdateYAML, kind, scopeMode, target }
  const items: NonNullable<TabsProps['items']> = [
    {
      key: 'overview',
      label: t('common.overview', 'Overview'),
      children: overview ?? (
        <div className="soha-detail-stack">
          <ConfigurationResourceOverview detail={detail} extra={overviewExtra} />
          <ResourceGitOpsStatus
            clusterId={target.scope.clusterId}
            kind={label}
            name={detail.name}
            namespace={detail.namespace}
          />
          {overviewContent}
        </div>
      ),
    },
    ...(additionalTabs ?? []),
    ...(dataTab === undefined
      ? []
      : [
          {
            key: 'data',
            label: localeCode === 'zh_CN' ? '数据' : 'Data',
            children: dataTab,
          },
        ]),
    ...(showReferences
      ? [
          {
            key: 'relationships',
            label: localeCode === 'zh_CN' ? '关联资源' : 'Related resources',
            children:
              activeTabKey === 'relationships'
                ? (relatedResources ?? <ConfigurationReferencesTab kind={kind} target={target} />)
                : null,
          },
        ]
      : []),
    {
      key: 'yaml',
      label: t('common.yaml', 'YAML'),
      children: !canReadYAML ? (
        <ManagementState
          kind="no-permission"
          description={
            localeCode === 'zh_CN'
              ? '查看 Secret YAML 需要敏感数据查看权限。'
              : 'Secret data permission is required to read YAML.'
          }
        />
      ) : activeTabKey === 'yaml' || (preserveYaml && visitedYaml) ? (
        yamlWrite ? (
          <ConfigurationYAMLTab {...yamlProps} write={yamlWrite} />
        ) : (
          <DefaultConfigurationYAMLTab {...yamlProps} />
        )
      ) : null,
    },
  ]

  return (
    <div className="soha-page soha-workload-detail-page soha-k8s-detail-page">
      {header ?? (
        <ResourceDetailHeader
          kind={label}
          name={detail.name}
          namespace={detail.namespace}
          createdAt={detail.createdAt}
          ageSeconds={detail.ageSeconds}
          queryKey={
            configurationQueries.detail(kind, target.scope, target.name, scopeMode).queryKey
          }
        />
      )}
      <ResourceDetailTabs
        activeKey={activeTabKey}
        className="soha-resource-tabs soha-workload-detail-tabs"
        indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
        items={items}
        onChange={changeTab}
        size="small"
        tabBarGutter={32}
      />
    </div>
  )
}

export function ConfigurationQueryDetailPage<TDetail extends ConfigurationDetailBase>({
  kind,
  label,
  name,
  namespace,
  overviewExtra,
  renderOverview,
  scopeMode = 'namespace',
}: {
  kind: ConfigurationKind
  label: string
  name: string
  namespace?: string
  overviewExtra?: (detail: TDetail) => Array<{ key: string; value: ReactNode }>
  renderOverview?: (detail: TDetail) => ReactNode
  scopeMode?: ConfigurationScopeMode
}) {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const scope = toScopeKey(clusterId, scopeMode === 'namespace' ? namespace : null)
  const detailQuery = useQuery(configurationQueries.detail<TDetail>(kind, scope, name, scopeMode))
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const detail = visibleDetailQueryData
  const scopeMissing = !clusterId || (scopeMode === 'namespace' && !namespace)

  if (scopeMissing) {
    return (
      <div className="soha-page">
        <ManagementState
          compact
          kind="select-scope"
          description={
            localeCode === 'zh_CN'
              ? scopeMode === 'namespace'
                ? '请选择集群和命名空间'
                : '请选择集群'
              : scopeMode === 'namespace'
                ? 'Select a cluster and namespace'
                : 'Select a cluster'
          }
        />
      </div>
    )
  }

  if (detailQuery.isLoading) {
    return (
      <ManagementState
        compact
        kind="loading"
        description={localeCode === 'zh_CN' ? '加载中' : 'Loading'}
      />
    )
  }
  if (detailQuery.isError && visibleDetailQueryData === undefined) {
    return <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源记录" />
  }
  if (detailQuery.isError && !detail)
    return <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
  if (!detail) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="not-found"
          description={localeCode === 'zh_CN' ? `${label} 未找到` : `${label} not found`}
        />
      </div>
    )
  }

  const target = {
    scope: toScopeKey(clusterId, scopeMode === 'namespace' ? detail.namespace : null),
    name: detail.name,
  }
  return (
    <>
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
      <ConfigurationDetailShell
        detail={detail}
        kind={kind}
        label={label}
        overviewExtra={overviewExtra?.(detail)}
        overviewContent={renderOverview?.(detail)}
        scopeMode={scopeMode}
        showReferences={false}
        target={target}
      />
    </>
  )
}
