import { parseDocument } from 'yaml'
export {
  jobState,
  jobStateLabel,
  jobEndTime,
  jobDuration,
  jobCompletionTarget,
  formatJobDuration,
} from '../shared/job-execution'

export interface JobTemplateContainer {
  name: string
  image: string
  init: boolean
  command: string[]
  args: string[]
}
export function readJobTemplate(
  content: string,
  name: string,
  namespace: string,
): JobTemplateContainer[] {
  if (!content.trim() || new TextEncoder().encode(content).byteLength > 4 * 1024 * 1024)
    throw new Error('Job YAML is unavailable or exceeds the size limit')
  const doc = parseDocument(content)
  if (doc.errors.length) throw new Error('Invalid Job YAML')
  const value = doc.toJS({ maxAliasCount: 100 })
  if (
    value?.kind !== 'Job' ||
    value?.metadata?.name !== name ||
    value?.metadata?.namespace !== namespace ||
    !value?.spec?.template?.spec
  )
    throw new Error('Job YAML identity does not match the selected resource')
  const spec = value.spec.template.spec
  const strings = (v: unknown) =>
    Array.isArray(v) ? v.filter((s): s is string => typeof s === 'string') : []
  return [true, false].flatMap((init) => {
    const containers = init ? spec.initContainers : spec.containers
    return Array.isArray(containers)
      ? containers.map((c) => ({
          name: typeof c?.name === 'string' ? c.name : '—',
          image: typeof c?.image === 'string' ? c.image : '—',
          init,
          command: strings(c?.command),
          args: strings(c?.args),
        }))
      : []
  })
}
