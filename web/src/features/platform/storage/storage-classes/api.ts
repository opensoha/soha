import {
  createStorageResource,
  deleteStorageResource,
  getStorageDetail,
  getStorageYAML,
  listStorageResources,
  updateStorageYAML,
} from '../shared/api'
import type { CreateStorageVariables, UpdateStorageYAMLVariables } from '../shared/types'
import type { StorageClass, StorageClassDetail, StorageClassTarget } from './types'

export const storageClassKind = 'storageclasses' as const

export const listStorageClasses = (scope: StorageClassTarget['scope']) =>
  listStorageResources<StorageClass>(storageClassKind, scope)
export const getStorageClassDetail = (target: StorageClassTarget) =>
  getStorageDetail<StorageClassDetail>(storageClassKind, target)
export const getStorageClassYAML = (target: StorageClassTarget) =>
  getStorageYAML(storageClassKind, target)
export const createStorageClass = (variables: CreateStorageVariables) =>
  createStorageResource(storageClassKind, variables)
export const updateStorageClassYAML = (variables: UpdateStorageYAMLVariables) =>
  updateStorageYAML(storageClassKind, variables)
export const deleteStorageClass = (target: StorageClassTarget) =>
  deleteStorageResource(storageClassKind, target)
