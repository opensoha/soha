import type { ScopeKey } from '@/types'
import { workloadKeys } from '../shared/keys'

export const deploymentDetailKeys = {
  revisionTemplate: (scope: ScopeKey, name: string, revision: string) =>
    [...workloadKeys.yaml('replicasets', scope, name), 'revision-template', revision] as const,
}
