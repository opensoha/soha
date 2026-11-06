import { getWorkloadYAML } from '../shared/api'
import type { ScopeKey } from '@/types'

export async function getReplicaSetConfiguration(scope: ScopeKey, name: string) {
  const yaml = await getWorkloadYAML('replicasets', scope, name)
  const { readReplicaSetConfiguration } = await import('./replicaset-detail-model')
  return readReplicaSetConfiguration(yaml.content, name, scope.namespace || 'default')
}
