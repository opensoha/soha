import type { ScopeKey } from '@/types'

export type StorageKind = 'persistentvolumeclaims' | 'persistentvolumes' | 'storageclasses'

export interface StorageTarget {
  readonly scope: ScopeKey
  readonly name: string
}

export interface StorageYAMLInput {
  readonly content: string
}

export interface CreateStorageVariables {
  readonly scope: ScopeKey
  readonly content: string
}

export interface UpdateStorageYAMLVariables extends StorageTarget {
  readonly content: string
}
