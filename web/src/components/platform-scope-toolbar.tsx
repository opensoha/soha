import { useEffect } from 'react'
import { Select, Typography } from '@douyinfe/semi-ui'
import { useQuery } from '@tanstack/react-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { ApiResponse, Cluster, Namespace } from '@/types'

const { Text } = Typography

interface PlatformScopeToolbarProps {
  className?: string
  clusterWidth?: number
  embedded?: boolean
  namespaceWidth?: number
  showLabel?: boolean
}

export function PlatformScopeToolbar({
  className,
  clusterWidth = 220,
  embedded = false,
  namespaceWidth = 220,
  showLabel = true,
}: PlatformScopeToolbarProps = {}) {
  const { t } = useI18n()
  const { clusterId, namespace, setClusterId, setNamespace } = usePlatformScopeStore()

  const { data: clustersData } = useQuery({
    queryKey: ['clusters'],
    queryFn: () => api.get<ApiResponse<Cluster[]>>('/clusters'),
  })

  const { data: namespacesData } = useQuery({
    queryKey: ['namespaces', clusterId],
    queryFn: () => api.get<ApiResponse<Namespace[]>>(`/clusters/${clusterId}/namespaces`),
    enabled: !!clusterId,
  })

  useEffect(() => {
    if (!clustersData) return
    const clusters = clustersData?.data ?? []
    if (clusterId && clusters.some((cluster) => cluster.id === clusterId)) {
      return
    }
    if (clusters.length === 1) {
      setClusterId(clusters[0].id)
      return
    }
    if (clusterId) {
      setClusterId(null)
    }
  }, [clusterId, clustersData, setClusterId])

  useEffect(() => {
    if (!namespacesData) return
    if (!clusterId || namespace == null || namespace === '') return
    const namespaces = namespacesData?.data ?? []
    if (namespaces.some((item) => item.name === namespace)) return
    setNamespace(null)
  }, [clusterId, namespace, namespacesData, setNamespace])

  return (
    <div className={['kc-scopebar', embedded ? 'is-embedded' : '', className].filter(Boolean).join(' ')}>
      {showLabel ? (
        <Text strong size="small">
          {t('platformScope.scope', 'Resource Scope')}
        </Text>
      ) : null}
      <Select
        className="kc-platform-compact-field"
        size="small"
        placeholder={t('platformScope.clusterPlaceholder', 'Select cluster')}
        value={clusterId ?? undefined}
        onChange={(value) => setClusterId(value as string)}
        style={{ width: clusterWidth }}
        optionList={(clustersData?.data ?? []).map((cluster) => ({
          value: cluster.id,
          label: cluster.name,
        }))}
        showClear
      />
      <Select
        className="kc-platform-compact-field"
        size="small"
        placeholder={t('platformScope.namespacePlaceholder', 'Select namespace')}
        value={namespace ?? undefined}
        onChange={(value) => setNamespace(value as string)}
        style={{ width: namespaceWidth }}
        optionList={[
          { value: '', label: t('platformScope.allNamespaces', 'All namespaces') },
          ...(namespacesData?.data ?? []).map((item) => ({
            value: item.name,
            label: item.name,
          })),
        ]}
        disabled={!clusterId}
        showClear
      />
    </div>
  )
}
