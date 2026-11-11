import type {
  PersistentVolumeClaim as PlatformPersistentVolumeClaim,
  PersistentVolumeClaimDetail as PlatformPersistentVolumeClaimDetail,
} from '@/types'
import type { StorageTarget } from '../shared/types'

export type PersistentVolumeClaim = PlatformPersistentVolumeClaim
export type PersistentVolumeClaimDetail = PlatformPersistentVolumeClaimDetail
export type PersistentVolumeClaimTarget = StorageTarget
