import { useEffect, useMemo } from 'react'
import { Select, Space, Tag, Typography } from 'antd'
import { ApartmentOutlined, KubernetesOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { ApiResponse, Cluster, Namespace, RouteMeta } from '@/types'

const { Text } = Typography

export type ScopeMode = NonNullable<RouteMeta['scopeMode']>

interface PlatformScopeToolbarProps {
  className?: string
  clusterWidth?: number
  embedded?: boolean
  namespaceWidth?: number
  showLabel?: boolean
}

interface GlobalScopeBarProps {
  className?: string
  scopeMode?: ScopeMode
}

interface PlatformScopeTriggerProps {
  className?: string
  scopeMode?: ScopeMode
}

interface ScopeSummaryProps {
  className?: string
  scopeMode?: ScopeMode
}

interface ResourceWorkspaceScopeBarProps {
  className?: string
  scopeMode?: ScopeMode
  sticky?: boolean
}

function usePlatformScopeData() {
  const { t } = useI18n()
  const { clusterId, namespace, setClusterId, setNamespace } = usePlatformScopeStore()

  const clustersQuery = useQuery({
    queryKey: ['clusters'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })

  const namespacesQuery = useQuery({
    queryKey: ['namespaces', clusterId],
    queryFn: () => api.get<ApiResponse<Namespace[]>>(`/clusters/${clusterId}/namespaces`),
    enabled: !!clusterId,
  })

  useEffect(() => {
    if (!clustersQuery.data) return
    const clusters = clustersQuery.data.data ?? []
    if (clusterId && clusters.some((cluster) => cluster.id === clusterId)) {
      return
    }
    if (clusters.length >= 1) {
      setClusterId(clusters[0].id)
      return
    }
    if (clusterId) {
      setClusterId(null)
    }
  }, [clusterId, clustersQuery.data, setClusterId])

  useEffect(() => {
    if (!namespacesQuery.data || !clusterId || namespace == null || namespace === '') return
    const namespaces = namespacesQuery.data.data ?? []
    if (namespaces.some((item) => item.name === namespace)) return
    setNamespace(null)
  }, [clusterId, namespace, namespacesQuery.data, setNamespace])

  const clusters = clustersQuery.data?.data ?? []
  const namespaces = namespacesQuery.data?.data ?? []
  const currentCluster = useMemo(
    () => clusters.find((item) => item.id === clusterId) ?? null,
    [clusterId, clusters],
  )

  const scopeLabels = useMemo(() => ({
    cluster: currentCluster?.name || t('platformScope.clusterPlaceholder', 'Select cluster'),
    namespace: namespace && namespace !== ''
      ? namespace
      : t('platformScope.allNamespaces', 'All namespaces'),
  }), [currentCluster?.name, namespace, t])

  return {
    clusterId,
    namespace,
    setClusterId,
    setNamespace,
    clusters,
    clustersLoading: clustersQuery.isLoading,
    namespaces,
    namespacesLoading: namespacesQuery.isLoading,
    currentCluster,
    scopeLabels,
  }
}

function isEditableScopeMode(scopeMode: ScopeMode) {
  return scopeMode === 'cluster' || scopeMode === 'namespace'
}

export function PlatformScopeTrigger({
  className,
  scopeMode = 'hidden',
}: PlatformScopeTriggerProps = {}) {
  const { t } = useI18n()
  const {
    clusterId,
    namespace,
    setClusterId,
    setNamespace,
    clusters,
    clustersLoading,
    namespaces,
    namespacesLoading,
  } = usePlatformScopeData()

  if (!isEditableScopeMode(scopeMode)) {
    return null
  }

  const showNamespaceSelector = scopeMode === 'namespace'

  return (
    <div
      className={[
        'soha-platform-scope-inline',
        showNamespaceSelector ? 'has-namespace' : 'is-cluster-only',
        className,
      ].filter(Boolean).join(' ')}
      role="group"
      aria-label={t('platformScope.scope', 'Resource Scope')}
    >
      {showNamespaceSelector ? (
        <Select
          aria-label={t('platformScope.namespacePlaceholder', 'Select namespace')}
          className="soha-platform-compact-field soha-platform-scope-select is-namespace"
          disabled={!clusterId}
          loading={namespacesLoading}
          optionFilterProp="label"
          options={[
            { value: '', label: t('platformScope.allNamespaces', 'All namespaces') },
            ...namespaces.map((item) => ({
              value: item.name,
              label: item.name,
            })),
          ]}
          placeholder={t('platformScope.namespacePlaceholder', 'Select namespace')}
          popupMatchSelectWidth={220}
          prefix={<ApartmentOutlined />}
          showSearch
          size="small"
          value={namespace ?? ''}
          variant="filled"
          onChange={(value) => setNamespace(typeof value === 'string' ? value : null)}
        />
      ) : null}

      <Select
        aria-label={t('platformScope.clusterPlaceholder', 'Select cluster')}
        className="soha-platform-compact-field soha-platform-scope-select is-cluster"
        loading={clustersLoading}
        optionFilterProp="label"
        options={clusters.map((cluster) => ({
          value: cluster.id,
          label: cluster.name,
        }))}
        placeholder={t('platformScope.clusterPlaceholder', 'Select cluster')}
        popupMatchSelectWidth={200}
        prefix={<KubernetesOutlined />}
        showSearch
        size="small"
        value={clusterId ?? undefined}
        variant="filled"
        onChange={(value) => setClusterId(typeof value === 'string' ? value : null)}
      />
    </div>
  )
}

export function PlatformScopeToolbar({
  className,
  clusterWidth = 220,
  embedded = false,
  namespaceWidth = 220,
  showLabel = true,
}: PlatformScopeToolbarProps = {}) {
  const { t } = useI18n()
  const { clusterId, namespace, setClusterId, setNamespace, clusters, namespaces } = usePlatformScopeData()

  return (
    <div className={['soha-scopebar', embedded ? 'is-embedded' : '', className].filter(Boolean).join(' ')}>
      {showLabel ? (
        <Text strong style={{ fontSize: 12 }}>
          {t('platformScope.scope', 'Resource Scope')}
        </Text>
      ) : null}
      <Select
        className="soha-platform-compact-field"
        size="small"
        placeholder={t('platformScope.clusterPlaceholder', 'Select cluster')}
        value={clusterId ?? undefined}
        onChange={(value) => setClusterId(typeof value === 'string' ? value : null)}
        style={{ width: clusterWidth }}
        options={clusters.map((cluster) => ({
          value: cluster.id,
          label: cluster.name,
        }))}
        allowClear
      />
      <Select
        className="soha-platform-compact-field"
        size="small"
        placeholder={t('platformScope.namespacePlaceholder', 'Select namespace')}
        value={namespace ?? undefined}
        onChange={(value) => setNamespace(typeof value === 'string' ? value : null)}
        style={{ width: namespaceWidth }}
        options={[
          { value: '', label: t('platformScope.allNamespaces', 'All namespaces') },
          ...namespaces.map((item) => ({
            value: item.name,
            label: item.name,
          })),
        ]}
        disabled={!clusterId}
        allowClear
      />
    </div>
  )
}

export function ScopeSummary({ className, scopeMode = 'passive' }: ScopeSummaryProps = {}) {
  const { t } = useI18n()
  const { scopeLabels } = usePlatformScopeData()

  if (scopeMode === 'hidden') {
    return null
  }

  return (
    <Space size={[6, 6]} wrap className={className}>
      {scopeMode === 'namespace' ? <Tag className="soha-scope-summary-tag">{`${t('common.namespace', 'Namespace')} ${scopeLabels.namespace}`}</Tag> : null}
      <Tag className="soha-scope-summary-tag">{`${t('common.cluster', 'Cluster')} ${scopeLabels.cluster}`}</Tag>
    </Space>
  )
}

export function ResourceWorkspaceScopeBar({
  className,
  scopeMode = 'hidden',
  sticky = true,
}: ResourceWorkspaceScopeBarProps = {}) {
  const { t } = useI18n()
  const { clusterId, namespace, setClusterId, setNamespace, clusters, namespaces, scopeLabels } = usePlatformScopeData()

  if (scopeMode === 'hidden') {
    return null
  }

  const showNamespaceSelector = scopeMode === 'namespace'
  const showClusterSelector = scopeMode === 'cluster' || scopeMode === 'namespace'

  return (
    <div className={['soha-page-context-bar', sticky ? 'is-sticky' : '', className].filter(Boolean).join(' ')}>
      <div className="soha-page-context-bar__header">
        <Text strong className="soha-page-context-bar__title">
          {t('platformScope.scope', 'Resource Scope')}
        </Text>
        <Text type="secondary" className="soha-page-context-bar__subtitle">
          {scopeMode === 'namespace'
            ? t('platformScope.namespaceHint', 'Use namespace and cluster as the current resource context')
            : showClusterSelector
              ? t('platformScope.clusterHint', 'Use cluster as the current resource context')
              : t('platformScope.summaryHint', 'Current page uses the selected resource context as a read-only summary')}
        </Text>
      </div>

      {showClusterSelector ? (
        <div className="soha-page-context-bar__controls">
          {showNamespaceSelector ? (
            <Select
              className="soha-platform-compact-field"
              size="small"
              placeholder={t('platformScope.namespacePlaceholder', 'Select namespace')}
              value={namespace ?? undefined}
              onChange={(value) => setNamespace(typeof value === 'string' ? value : null)}
              style={{ width: 220 }}
              options={[
                { value: '', label: t('platformScope.allNamespaces', 'All namespaces') },
                ...namespaces.map((item) => ({
                  value: item.name,
                  label: item.name,
                })),
              ]}
              disabled={!clusterId}
              allowClear
            />
          ) : null}
          <Select
            className="soha-platform-compact-field"
            size="small"
            placeholder={t('platformScope.clusterPlaceholder', 'Select cluster')}
            value={clusterId ?? undefined}
            onChange={(value) => setClusterId(typeof value === 'string' ? value : null)}
            style={{ width: 220 }}
            options={clusters.map((cluster) => ({
              value: cluster.id,
              label: cluster.name,
            }))}
            allowClear
          />
        </div>
      ) : (
        <div className="soha-page-context-bar__summary">
          <Tag className="soha-scope-summary-tag">{`${t('common.cluster', 'Cluster')} ${scopeLabels.cluster}`}</Tag>
          <Tag className="soha-scope-summary-tag">{`${t('common.namespace', 'Namespace')} ${scopeLabels.namespace}`}</Tag>
        </div>
      )}
    </div>
  )
}

export function GlobalScopeBar({ className, scopeMode = 'hidden' }: GlobalScopeBarProps = {}) {
  return <PlatformScopeTrigger className={className} scopeMode={scopeMode} />
}
