import {
  createStorageResource,
  deleteStorageResource,
  getStorageDetail,
  getStorageYAML,
  listStorageResources,
  updateStorageYAML,
} from '../shared/api'
import type { CreateStorageVariables, UpdateStorageYAMLVariables } from '../shared/types'
import type { PersistentVolume, PersistentVolumeDetail, PersistentVolumeTarget } from './types'

export const persistentVolumeKind = 'persistentvolumes' as const

export const listPersistentVolumes = (scope: PersistentVolumeTarget['scope']) =>
  listStorageResources<PersistentVolume>(persistentVolumeKind, scope)
export const getPersistentVolumeDetail = (target: PersistentVolumeTarget) =>
  getStorageDetail<PersistentVolumeDetail>(persistentVolumeKind, target)
export const getPersistentVolumeYAML = (target: PersistentVolumeTarget) =>
  getStorageYAML(persistentVolumeKind, target)
export const createPersistentVolume = (variables: CreateStorageVariables) =>
  createStorageResource(persistentVolumeKind, variables)
export const updatePersistentVolumeYAML = (variables: UpdateStorageYAMLVariables) =>
  updateStorageYAML(persistentVolumeKind, variables)
export const deletePersistentVolume = (target: PersistentVolumeTarget) =>
  deleteStorageResource(persistentVolumeKind, target)
