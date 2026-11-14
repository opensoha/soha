import type {
  PersistentVolume as PlatformPersistentVolume,
  PersistentVolumeDetail as PlatformPersistentVolumeDetail,
} from '@/types'
import type { StorageTarget } from '../shared/types'

export type PersistentVolume = PlatformPersistentVolume
export type PersistentVolumeDetail = PlatformPersistentVolumeDetail
export type PersistentVolumeTarget = StorageTarget
