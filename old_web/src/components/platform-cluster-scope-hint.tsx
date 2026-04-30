import { useMemo } from 'react'
import { Card, Space, Tag, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type { ApiResponse, ApplicationEnvironment, BusinessLine, DeliveryEnvironment } from '@/types'

const { Text } = Typography

interface ApplicationSummary {
  id: string
  name: string
  businessLineId?: string
}

function uniqueLabels(values: string[]) {
  return [...new Set(values.filter(Boolean))].sort((left, right) => left.localeCompare(right))
}

export function PlatformClusterScopeHint({ resourceLabel }: { resourceLabel: string }) {
  const { clusterId, namespace } = usePlatformScopeStore()

  const bindingsQuery = useQuery({
    queryKey: ['application-environments'],
    queryFn: () => api.get<ApiResponse<ApplicationEnvironment[]>>('/application-environments'),
    enabled: !!clusterId,
  })
  const businessLinesQuery = useQuery({
    queryKey: ['business-lines'],
    queryFn: () => api.get<ApiResponse<BusinessLine[]>>('/business-lines'),
    enabled: !!clusterId,
  })
  const environmentsQuery = useQuery({
    queryKey: ['delivery-environments'],
    queryFn: () => api.get<ApiResponse<DeliveryEnvironment[]>>('/delivery-environments'),
    enabled: !!clusterId,
  })
  const applicationsQuery = useQuery({
    queryKey: ['applications'],
    queryFn: () => api.get<ApiResponse<ApplicationSummary[]>>('/applications'),
    enabled: !!clusterId,
  })

  const scopeSummary = useMemo(() => {
    if (!clusterId) return null
    const appMap = Object.fromEntries((applicationsQuery.data?.data ?? []).map((item) => [item.id, item]))
    const businessLineMap = Object.fromEntries((businessLinesQuery.data?.data ?? []).map((item) => [item.id, item.name]))
    const environmentMap = Object.fromEntries((environmentsQuery.data?.data ?? []).map((item) => [item.id, item.name]))
    const bindings = (bindingsQuery.data?.data ?? []).filter((binding) =>
      (binding.targets ?? []).some((target) => target.enabled !== false && target.clusterId === clusterId),
    )

    const businessLines = uniqueLabels(bindings.map((binding) => {
      const businessLineId = binding.businessLineId || appMap[binding.applicationId]?.businessLineId
      return businessLineMap[businessLineId || ''] || businessLineId || ''
    }))
    const environments = uniqueLabels(bindings.map((binding) => environmentMap[binding.environmentId] || binding.environmentKey || binding.environmentId))
    const applications = uniqueLabels(bindings.map((binding) => appMap[binding.applicationId]?.name || binding.applicationId))

    return {
      bindingsCount: bindings.length,
      businessLines,
      environments,
      applications,
    }
  }, [applicationsQuery.data, bindingsQuery.data, businessLinesQuery.data, clusterId, environmentsQuery.data])

  if (!clusterId) {
    return null
  }

  return (
    <Card className="kc-scope-hint-card">
      <Space direction="vertical" align="start" size={4}>
        <Text strong>{`${resourceLabel} 业务线范围说明`}</Text>
        <Text type="secondary">
          {`${resourceLabel} 属于集群级共享资源，不会按单个 workload 再拆分。页面可见性由你在当前集群已绑定的发布目标范围决定。`}
        </Text>
        {namespace ? (
          <Text type="secondary">当前页是集群级视图，已选择的命名空间筛选不会影响这类资源结果。</Text>
        ) : null}
        {scopeSummary && scopeSummary.bindingsCount > 0 ? (
          <>
            <Text type="secondary">
              {`当前集群命中了 ${scopeSummary.bindingsCount} 条应用环境绑定，覆盖 ${scopeSummary.businessLines.length} 条业务线、${scopeSummary.environments.length} 个环境、${scopeSummary.applications.length} 个应用。`}
            </Text>
            <div className="kc-scope-hint-tags">
              {scopeSummary.businessLines.map((item) => <Tag key={`bl:${item}`} color="blue">{item}</Tag>)}
              {scopeSummary.environments.map((item) => <Tag key={`env:${item}`} color="green">{item}</Tag>)}
            </div>
          </>
        ) : (
          <Text type="warning">
            当前集群没有找到应用环境绑定，无法进一步映射到具体业务线；这类资源仅按基础访问控制决定是否可见。
          </Text>
        )}
      </Space>
    </Card>
  )
}
