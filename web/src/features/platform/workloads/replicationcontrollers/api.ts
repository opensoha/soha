import type { ScopeKey } from '@/types'
import { getWorkloadYAML } from '../shared/api'
import { readReplicaControllerConfiguration } from '../shared/replica-controller-model'
export async function getReplicationControllerConfiguration(scope: ScopeKey, name: string) {
  const yaml = await getWorkloadYAML('replicationcontrollers', scope, name)
  return readReplicaControllerConfiguration(
    yaml.content,
    name,
    scope.namespace || 'default',
    'ReplicationController',
  )
}
