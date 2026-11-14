import { Button } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Navigate, useParams, useSearchParams } from 'react-router-dom'
import { ManagementState } from '@/components/management-list'
import { deliveryQueries } from '../queries'
import { applicationWorkspacePath } from '../applications/workspace-navigation'

// Old workload links resolve into the application workspace instead of another navigation level.
export function ApplicationWorkloadDetailPage() {
  const { applicationId = '', applicationEnvironmentId = '', workloadName = '' } = useParams()
  const [search] = useSearchParams()
  const query = useQuery(
    deliveryQueries.workloads.runtime({ applicationId, applicationEnvironmentId, workloadName }),
  )
  const services = useQuery(deliveryQueries.applications.services(applicationId))
  if (query.isPending || services.isPending) return <ManagementState compact kind="loading" />
  if (query.isError || !query.data)
    return (
      <ManagementState
        compact
        kind="error"
        title="运行实例不可用"
        actions={<Button onClick={() => void query.refetch()}>重试</Button>}
      />
    )
  const workload = query.data.workload
  const serviceId = services.data?.find(
    (service) => service.id === workload.serviceId || service.key === workload.serviceKey,
  )?.id
  const tab = search.get('tab') || 'pods'
  const serviceTab = ['basic', 'build', 'resources', 'related-resources'].includes(tab)
    ? tab
    : 'pods'
  const next = new URLSearchParams({ tab: 'services', applicationEnvironmentId, serviceTab })
  if (serviceId) next.set('serviceId', serviceId)
  else next.set('workload', workloadName)
  const tool =
    search.get('tool') ||
    (['logs', 'terminal', 'metrics', 'events', 'yaml'].includes(tab) ? tab : '')
  const pod = search.get('pod') || (tool ? query.data.pods?.[0]?.name : '')
  if (pod) {
    next.set('pod', pod)
    next.set('tool', tool || 'details')
    next.set('podCluster', workload.clusterId)
    next.set('podNamespace', workload.namespace)
  }
  if (search.get('container')) next.set('container', search.get('container')!)
  return (
    <Navigate
      replace
      to={applicationWorkspacePath(applicationId).split('?')[0] + '?' + next.toString()}
    />
  )
}
