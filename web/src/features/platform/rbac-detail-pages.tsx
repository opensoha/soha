import { lazy, Suspense } from 'react'
import type { ReactNode } from 'react'
import { Button, Card, Space, Spin, Tabs, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ManagementDetailHeader, ManagementState } from '@/components/management-list'
import { BooleanTag } from '@/components/status-tag'
import { ResourceMetaOverview, useResourceYAMLState } from '@/features/platform/configuration-detail-pages'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { useI18n } from '@/i18n'
import type {
  ApiResponse,
  Cluster,
  ClusterRoleBindingDetail,
  ClusterRoleDetail,
  RoleBindingDetail,
  RoleDetail,
  ServiceAccountDetail,
} from '@/types'
import './platform-pages.css'

const { Text, Paragraph } = Typography

const K8sYamlEditor = lazy(async () => {
  const mod = await import('@/components/k8s-yaml-editor')
  return { default: mod.K8sYamlEditor }
})

function useResolvedNamespace() {
  const [searchParams] = useSearchParams()
  const { namespace } = usePlatformScopeStore()
  return (namespace && namespace !== '') ? namespace : (searchParams.get('namespace') || '')
}

function useCurrentCluster() {
  const { clusterId } = usePlatformScopeStore()
  return useQuery({
    queryKey: ['clusters'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
    enabled: !!clusterId,
  })
}

function renderStringList(values: string[] | undefined, emptyLabel: string) {
  if (!values || values.length === 0) {
    return <Text type="secondary">{emptyLabel}</Text>
  }
  return (
    <div className="soha-tag-list">
      {values.map((value) => <Tag key={value}>{value}</Tag>)}
    </div>
  )
}

function renderRuleSummaries(values: string[] | undefined, emptyLabel: string) {
  if (!values || values.length === 0) {
    return <Text type="secondary">{emptyLabel}</Text>
  }
  return (
    <Space orientation="vertical" style={{ width: '100%' }} size={8}>
      {values.map((value) => (
        <Card key={value} className="soha-detail-card" bodyStyle={{ padding: 12 }}>
          <Paragraph style={{ margin: 0 }}>{value}</Paragraph>
        </Card>
      ))}
    </Space>
  )
}

function yamlUnsupportedDescription(localeCode: 'zh_CN' | 'en_US') {
  return localeCode === 'zh_CN'
    ? '当前 agent 集群暂不支持 YAML 查看、编辑或删除。'
    : 'YAML view, edit, and delete are not supported for agent-connected clusters yet.'
}

function useClusterConnectionMode() {
  const { clusterId } = usePlatformScopeStore()
  const clustersQuery = useCurrentCluster()
  return (clustersQuery.data?.data ?? []).find((item) => item.id === clusterId)?.connectionMode || ''
}

interface RBACDetailPageProps<T> {
  backPath: string
  detail: T | undefined
  detailDescription: string
  detailTitle: string
  emptyDescription: string
  isAgentCluster: boolean
  localeCode: 'zh_CN' | 'en_US'
  namespace?: string
  resourceName: string
  renderOverview: (detail: T) => ReactNode
  yamlPath: string | null
  yamlResourceKey: string
  yamlTitle: string
}

function RBACDetailPage<T>({
  backPath,
  detail,
  detailDescription,
  detailTitle,
  emptyDescription,
  isAgentCluster,
  localeCode,
  namespace = '',
  resourceName,
  renderOverview,
  yamlPath,
  yamlResourceKey,
  yamlTitle,
}: RBACDetailPageProps<T>) {
  const navigate = useNavigate()
  const yamlState = useResourceYAMLState(yamlPath, yamlResourceKey, resourceName, namespace)

  if (!detail) {
    return <div className="soha-page"><ManagementState kind="not-found" description={emptyDescription} /></div>
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={detailTitle}
        description={detailDescription}
        actions={<Button onClick={() => navigate(backPath)}>{localeCode === 'zh_CN' ? '返回列表' : 'Back to list'}</Button>}
      />
      <Tabs
        defaultActiveKey="overview"
        items={[
          {
            key: 'overview',
            label: localeCode === 'zh_CN' ? '概览' : 'Overview',
            children: renderOverview(detail),
          },
          {
            key: 'yaml',
            label: yamlTitle,
            children: isAgentCluster || !yamlPath ? (
              <ManagementState kind="unsupported" description={yamlUnsupportedDescription(localeCode)} />
            ) : (
              <Suspense fallback={<Card className="soha-detail-card"><Spin size="large" /></Card>}>
                <div style={{ height: 620 }}>
                  <K8sYamlEditor
                    value={yamlState.draft}
                    onChange={yamlState.setDraft}
                    onReset={() => yamlState.setDraft(yamlState.serverValue)}
                    onSave={() => void 0}
                    onApply={() => yamlState.applyMutation.mutate()}
                    saveDisabled
                    applyDisabled={!yamlPath || !yamlState.draft.trim()}
                    applying={yamlState.applyMutation.isPending}
                  />
                </div>
              </Suspense>
            ),
          },
        ]}
      />
    </div>
  )
}

export function PlatformAccessControlServiceAccountDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const name = params.name as string
  const namespace = useResolvedNamespace()
  const { clusterId } = usePlatformScopeStore()
  const isAgentCluster = useClusterConnectionMode() === 'agent'

  const detailPath = clusterId && namespace ? `/clusters/${clusterId}/access-control/serviceaccounts/${name}/detail?namespace=${encodeURIComponent(namespace)}` : null
  const yamlPath = clusterId && namespace ? `/clusters/${clusterId}/access-control/serviceaccounts/${name}/yaml?namespace=${encodeURIComponent(namespace)}` : null
  const detailQuery = useQuery({
    queryKey: ['serviceaccounts', 'detail', name, namespace],
    queryFn: () => api.get<ApiResponse<ServiceAccountDetail>>(detailPath!),
    enabled: !!detailPath,
  })
  const detail = detailQuery.data?.data

  if (detailQuery.isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }

  return (
    <RBACDetailPage<ServiceAccountDetail>
      backPath="/platform-access-control/serviceaccounts"
      detail={detail}
      detailTitle={`ServiceAccount: ${name}`}
      detailDescription={localeCode === 'zh_CN' ? '查看 ServiceAccount 的引用对象、自动挂载与 YAML。' : 'Inspect ServiceAccount references, automount setting, and YAML.'}
      emptyDescription={localeCode === 'zh_CN' ? 'ServiceAccount 未找到' : 'ServiceAccount not found'}
      isAgentCluster={isAgentCluster}
      localeCode={localeCode}
      namespace={namespace}
      resourceName={name}
      renderOverview={(item) => (
        <>
          <ResourceMetaOverview
            name={item.name}
            namespace={item.namespace}
            createdAt={item.createdAt}
            labels={item.labels}
            annotations={item.annotations}
            extra={[
              { key: localeCode === 'zh_CN' ? 'Secrets' : 'Secrets', value: renderStringList(item.secrets, localeCode === 'zh_CN' ? '暂无关联 Secrets' : 'No secrets') },
              { key: localeCode === 'zh_CN' ? '镜像拉取密钥' : 'Image Pull Secrets', value: renderStringList(item.imagePullSecrets, localeCode === 'zh_CN' ? '暂无 imagePullSecrets' : 'No image pull secrets') },
              { key: localeCode === 'zh_CN' ? '自动挂载 Token' : 'Automount Token', value: <BooleanTag value={item.automountServiceAccountToken} trueLabel={localeCode === 'zh_CN' ? '是' : 'Yes'} falseLabel={localeCode === 'zh_CN' ? '否' : 'No'} /> },
            ]}
          />
        </>
      )}
      yamlPath={yamlPath}
      yamlResourceKey="serviceaccounts"
      yamlTitle="YAML"
    />
  )
}

export function PlatformAccessControlRoleDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const name = params.name as string
  const namespace = useResolvedNamespace()
  const { clusterId } = usePlatformScopeStore()
  const isAgentCluster = useClusterConnectionMode() === 'agent'
  const detailPath = clusterId && namespace ? `/clusters/${clusterId}/access-control/roles/${name}/detail?namespace=${encodeURIComponent(namespace)}` : null
  const yamlPath = clusterId && namespace ? `/clusters/${clusterId}/access-control/roles/${name}/yaml?namespace=${encodeURIComponent(namespace)}` : null
  const detailQuery = useQuery({
    queryKey: ['roles', 'detail', name, namespace],
    queryFn: () => api.get<ApiResponse<RoleDetail>>(detailPath!),
    enabled: !!detailPath,
  })
  const detail = detailQuery.data?.data

  if (detailQuery.isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }

  return (
    <RBACDetailPage<RoleDetail>
      backPath="/platform-access-control/roles"
      detail={detail}
      detailTitle={`Role: ${name}`}
      detailDescription={localeCode === 'zh_CN' ? '查看 Role 的规则摘要与 YAML。' : 'Inspect Role rule summaries and YAML.'}
      emptyDescription={localeCode === 'zh_CN' ? 'Role 未找到' : 'Role not found'}
      isAgentCluster={isAgentCluster}
      localeCode={localeCode}
      namespace={namespace}
      resourceName={name}
      renderOverview={(item) => (
        <>
          <ResourceMetaOverview
            name={item.name}
            namespace={item.namespace}
            createdAt={item.createdAt}
            labels={item.labels}
            annotations={item.annotations}
            extra={[
              { key: localeCode === 'zh_CN' ? '规则数' : 'Rules', value: item.rules },
            ]}
          />
          <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '规则摘要' : 'Rule Summaries'}>
            {renderRuleSummaries(item.ruleSummaries, localeCode === 'zh_CN' ? '暂无规则摘要' : 'No rule summaries')}
          </Card>
        </>
      )}
      yamlPath={yamlPath}
      yamlResourceKey="roles"
      yamlTitle="YAML"
    />
  )
}

export function PlatformAccessControlRoleBindingDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const name = params.name as string
  const namespace = useResolvedNamespace()
  const { clusterId } = usePlatformScopeStore()
  const isAgentCluster = useClusterConnectionMode() === 'agent'
  const detailPath = clusterId && namespace ? `/clusters/${clusterId}/access-control/rolebindings/${name}/detail?namespace=${encodeURIComponent(namespace)}` : null
  const yamlPath = clusterId && namespace ? `/clusters/${clusterId}/access-control/rolebindings/${name}/yaml?namespace=${encodeURIComponent(namespace)}` : null
  const detailQuery = useQuery({
    queryKey: ['rolebindings', 'detail', name, namespace],
    queryFn: () => api.get<ApiResponse<RoleBindingDetail>>(detailPath!),
    enabled: !!detailPath,
  })
  const detail = detailQuery.data?.data

  if (detailQuery.isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }

  return (
    <RBACDetailPage<RoleBindingDetail>
      backPath="/platform-access-control/rolebindings"
      detail={detail}
      detailTitle={`RoleBinding: ${name}`}
      detailDescription={localeCode === 'zh_CN' ? '查看 RoleBinding 的 subject 与 YAML。' : 'Inspect RoleBinding subjects and YAML.'}
      emptyDescription={localeCode === 'zh_CN' ? 'RoleBinding 未找到' : 'RoleBinding not found'}
      isAgentCluster={isAgentCluster}
      localeCode={localeCode}
      namespace={namespace}
      resourceName={name}
      renderOverview={(item) => (
        <ResourceMetaOverview
          name={item.name}
          namespace={item.namespace}
          createdAt={item.createdAt}
          labels={item.labels}
          annotations={item.annotations}
          extra={[
            { key: 'RoleRef', value: item.roleRef },
            { key: localeCode === 'zh_CN' ? 'Subjects' : 'Subjects', value: renderStringList(item.subjects, localeCode === 'zh_CN' ? '暂无主体' : 'No subjects') },
          ]}
        />
      )}
      yamlPath={yamlPath}
      yamlResourceKey="rolebindings"
      yamlTitle="YAML"
    />
  )
}

export function PlatformAccessControlClusterRoleDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const name = params.name as string
  const { clusterId } = usePlatformScopeStore()
  const isAgentCluster = useClusterConnectionMode() === 'agent'
  const detailPath = clusterId ? `/clusters/${clusterId}/access-control/clusterroles/${name}/detail` : null
  const yamlPath = clusterId ? `/clusters/${clusterId}/access-control/clusterroles/${name}/yaml` : null
  const detailQuery = useQuery({
    queryKey: ['clusterroles', 'detail', name],
    queryFn: () => api.get<ApiResponse<ClusterRoleDetail>>(detailPath!),
    enabled: !!detailPath,
  })
  const detail = detailQuery.data?.data

  if (detailQuery.isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }

  return (
    <RBACDetailPage<ClusterRoleDetail>
      backPath="/platform-access-control/clusterroles"
      detail={detail}
      detailTitle={`ClusterRole: ${name}`}
      detailDescription={localeCode === 'zh_CN' ? '查看 ClusterRole 的规则摘要与 YAML。' : 'Inspect ClusterRole rule summaries and YAML.'}
      emptyDescription={localeCode === 'zh_CN' ? 'ClusterRole 未找到' : 'ClusterRole not found'}
      isAgentCluster={isAgentCluster}
      localeCode={localeCode}
      resourceName={name}
      renderOverview={(item) => (
        <>
          <ResourceMetaOverview
            name={item.name}
            namespace={localeCode === 'zh_CN' ? '集群级' : 'Cluster-scoped'}
            createdAt={item.createdAt}
            labels={item.labels}
            annotations={item.annotations}
            extra={[
              { key: localeCode === 'zh_CN' ? '规则数' : 'Rules', value: item.rules },
              { key: localeCode === 'zh_CN' ? '聚合规则' : 'Aggregation', value: item.aggregationRules },
            ]}
          />
          <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? '规则摘要' : 'Rule Summaries'}>
            {renderRuleSummaries(item.ruleSummaries, localeCode === 'zh_CN' ? '暂无规则摘要' : 'No rule summaries')}
          </Card>
        </>
      )}
      yamlPath={yamlPath}
      yamlResourceKey="clusterroles"
      yamlTitle="YAML"
    />
  )
}

export function PlatformAccessControlClusterRoleBindingDetailPage() {
  const { localeCode } = useI18n()
  const params = useParams()
  const name = params.name as string
  const { clusterId } = usePlatformScopeStore()
  const isAgentCluster = useClusterConnectionMode() === 'agent'
  const detailPath = clusterId ? `/clusters/${clusterId}/access-control/clusterrolebindings/${name}/detail` : null
  const yamlPath = clusterId ? `/clusters/${clusterId}/access-control/clusterrolebindings/${name}/yaml` : null
  const detailQuery = useQuery({
    queryKey: ['clusterrolebindings', 'detail', name],
    queryFn: () => api.get<ApiResponse<ClusterRoleBindingDetail>>(detailPath!),
    enabled: !!detailPath,
  })
  const detail = detailQuery.data?.data

  if (detailQuery.isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }

  return (
    <RBACDetailPage<ClusterRoleBindingDetail>
      backPath="/platform-access-control/clusterrolebindings"
      detail={detail}
      detailTitle={`ClusterRoleBinding: ${name}`}
      detailDescription={localeCode === 'zh_CN' ? '查看 ClusterRoleBinding 的 subject 与 YAML。' : 'Inspect ClusterRoleBinding subjects and YAML.'}
      emptyDescription={localeCode === 'zh_CN' ? 'ClusterRoleBinding 未找到' : 'ClusterRoleBinding not found'}
      isAgentCluster={isAgentCluster}
      localeCode={localeCode}
      resourceName={name}
      renderOverview={(item) => (
        <ResourceMetaOverview
          name={item.name}
          namespace={localeCode === 'zh_CN' ? '集群级' : 'Cluster-scoped'}
          createdAt={item.createdAt}
          labels={item.labels}
          annotations={item.annotations}
          extra={[
            { key: 'RoleRef', value: item.roleRef },
            { key: localeCode === 'zh_CN' ? 'Subjects' : 'Subjects', value: renderStringList(item.subjects, localeCode === 'zh_CN' ? '暂无主体' : 'No subjects') },
          ]}
        />
      )}
      yamlPath={yamlPath}
      yamlResourceKey="clusterrolebindings"
      yamlTitle="YAML"
    />
  )
}
