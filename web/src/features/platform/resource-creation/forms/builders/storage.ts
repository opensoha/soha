import type { KubernetesManifest, PersistentVolumeClaimFormValues } from '../types'
import { buildMetadata, compactObject, manifest } from './shared'

export function buildPersistentVolumeClaimManifest(
  values: PersistentVolumeClaimFormValues,
): KubernetesManifest {
  return manifest('v1', 'PersistentVolumeClaim', buildMetadata(values), {
    spec: compactObject({
      accessModes: values.accessModes,
      volumeMode: values.volumeMode,
      storageClassName: values.storageClassName?.trim(),
      resources: { requests: { storage: values.storage.trim() } },
    }),
  })
}
