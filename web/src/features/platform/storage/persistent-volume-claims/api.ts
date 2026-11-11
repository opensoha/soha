import {
  createStorageResource,
  deleteStorageResource,
  getStorageDetail,
  getStorageYAML,
  listStorageResources,
  updateStorageYAML,
} from '../shared/api'
import type { CreateStorageVariables, UpdateStorageYAMLVariables } from '../shared/types'
import type {
  PersistentVolumeClaim,
  PersistentVolumeClaimDetail,
  PersistentVolumeClaimTarget,
} from './types'

export const persistentVolumeClaimKind = 'persistentvolumeclaims' as const

export const listPersistentVolumeClaims = (scope: PersistentVolumeClaimTarget['scope']) =>
  listStorageResources<PersistentVolumeClaim>(persistentVolumeClaimKind, scope)
export const getPersistentVolumeClaimDetail = (target: PersistentVolumeClaimTarget) =>
  getStorageDetail<PersistentVolumeClaimDetail>(persistentVolumeClaimKind, target)
export const getPersistentVolumeClaimYAML = (target: PersistentVolumeClaimTarget) =>
  getStorageYAML(persistentVolumeClaimKind, target)
export const createPersistentVolumeClaim = (variables: CreateStorageVariables) =>
  createStorageResource(persistentVolumeClaimKind, variables)
export const updatePersistentVolumeClaimYAML = (variables: UpdateStorageYAMLVariables) =>
  updateStorageYAML(persistentVolumeClaimKind, variables)
export const deletePersistentVolumeClaim = (target: PersistentVolumeClaimTarget) =>
  deleteStorageResource(persistentVolumeClaimKind, target)
