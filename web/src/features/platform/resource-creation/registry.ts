import type { ResourceFormDefinition } from './forms'

const formKinds = new Set([
  'Deployment',
  'StatefulSet',
  'DaemonSet',
  'Job',
  'CronJob',
  'Service',
  'Ingress',
  'ConfigMap',
  'Secret',
  'PersistentVolumeClaim',
  'Namespace',
  'ServiceAccount',
])

export function hasResourceCreateForm(kind?: string) {
  return Boolean(kind && formKinds.has(kind))
}

export async function loadResourceCreateForm(
  kind: string,
): Promise<ResourceFormDefinition | undefined> {
  const forms = await import('./forms')
  return forms.getResourceFormDefinition(kind)
}
