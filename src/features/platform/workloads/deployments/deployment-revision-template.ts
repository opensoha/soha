import { parseDocument, stringify } from 'yaml'
import type { RolloutHistory } from '@/types'

/** A retained ReplicaSet is the source of a Deployment revision, not the live Deployment. */
export function readDeploymentRevisionTemplate(content: string, record: RolloutHistory) {
  const document = parseDocument(content)
  if (document.errors.length) throw new Error('Invalid ReplicaSet YAML')
  const value = document.toJS({ maxAliasCount: 100 })
  if (
    value?.kind !== 'ReplicaSet' ||
    value.metadata?.name !== record.name ||
    value.metadata?.namespace !== record.namespace ||
    value.metadata?.annotations?.['deployment.kubernetes.io/revision'] !== record.revision
  )
    throw new Error('ReplicaSet identity or revision does not match the selected record')
  const template = value.spec?.template
  if (!template || typeof template !== 'object' || !template.spec) {
    throw new Error('ReplicaSet Pod template is unavailable')
  }
  // This controller-generated label differs even when the user template is identical.
  if (template.metadata?.labels) delete template.metadata.labels['pod-template-hash']
  const cause = value.metadata.annotations['kubernetes.io/change-cause']
  return {
    template: stringify(template, { sortMapEntries: true }),
    changeCause: typeof cause === 'string' && cause.trim() ? cause : undefined,
  }
}
