import { CreateEntry } from '@/features/platform/resource-creation/components/create-entry'
import { getResourceCreateTemplate } from '@/features/platform/resource-creation/templates'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'

type CreatableWorkloadKind = 'Deployment' | 'StatefulSet' | 'DaemonSet' | 'Job' | 'CronJob'

export function WorkloadCreateEntry({ kind }: { kind: CreatableWorkloadKind }) {
  const { clusterId, namespace } = usePlatformScopeStore()
  return (
    <CreateEntry
      context={{
        clusterId: clusterId || '',
        defaultNamespace: namespace || undefined,
        expectedApiVersion: kind === 'Job' || kind === 'CronJob' ? 'batch/v1' : 'apps/v1',
        expectedKind: kind,
        resourceGroup: 'workloads',
        scopeMode: 'namespace',
        source: 'list',
      }}
      defaultTemplate={getResourceCreateTemplate(kind)}
      label={kind}
    />
  )
}
