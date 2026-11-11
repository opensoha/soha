import type { KubernetesManifest, NamespaceFormValues, ServiceAccountFormValues } from '../types'
import { buildMetadata, manifest } from './shared'

export function buildNamespaceManifest(values: NamespaceFormValues): KubernetesManifest {
  return manifest('v1', 'Namespace', buildMetadata(values, false))
}

export function buildServiceAccountManifest(values: ServiceAccountFormValues): KubernetesManifest {
  return manifest('v1', 'ServiceAccount', buildMetadata(values), {
    automountServiceAccountToken: values.automountServiceAccountToken,
    imagePullSecrets: values.imagePullSecrets
      ?.map((name) => name.trim())
      .filter(Boolean)
      .map((name) => ({ name })),
  })
}
