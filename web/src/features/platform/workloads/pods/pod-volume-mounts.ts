import type { PodVolume } from '@/types'

const readOnlySources = new Set([
  'ConfigMap',
  'Secret',
  'Projected',
  'DownwardAPI',
  'ServiceAccountToken',
])

export function podVolumeReadOnly(
  volume: Pick<PodVolume, 'type' | 'readOnly'> | undefined,
  mountReadOnly: boolean,
) {
  return mountReadOnly || Boolean(volume && (volume.readOnly || readOnlySources.has(volume.type)))
}
