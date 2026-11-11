import type { ConfigMapFormValues, KubernetesManifest, SecretFormValues } from '../types'
import { buildMetadata, entriesToRecord, manifest } from './shared'

export function buildConfigMapManifest(values: ConfigMapFormValues): KubernetesManifest {
  return manifest('v1', 'ConfigMap', buildMetadata(values), {
    data: entriesToRecord(values.data) ?? {},
  })
}

function encodeBase64(value: string) {
  const bytes = new TextEncoder().encode(value)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

export function buildSecretManifest(values: SecretFormValues): KubernetesManifest {
  const stringData = entriesToRecord(values.data) ?? {}
  return manifest('v1', 'Secret', buildMetadata(values), {
    type: values.type,
    immutable: values.immutable || undefined,
    data: Object.fromEntries(
      Object.entries(stringData).map(([key, value]) => [key, encodeBase64(value)]),
    ),
  })
}
