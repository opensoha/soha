import type {
  CompanionInteractionReceipt,
  CompanionPackManifest,
  CompanionProfile,
  InstalledPlugin,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type CompanionVisualState =
  | 'disabled'
  | 'error'
  | 'dragging'
  | 'thinking'
  | 'speaking'
  | 'listening'
  | 'hover'
  | 'idle'

type CompanionAsset = CompanionPackManifest['assets'][number]

export interface CompanionRenderManifest extends Omit<CompanionPackManifest, 'assets' | 'license'> {
  assets: Array<Pick<CompanionAsset, 'path'> & Partial<Omit<CompanionAsset, 'path'>>>
  license?: CompanionPackManifest['license']
}

export interface CompanionPackSelection {
  manifest: CompanionRenderManifest
  pluginId: string
  version: string
  installed?: InstalledPlugin
  loadAsset?: (path: string) => Promise<Blob>
  local?: boolean
}

export type { CompanionInteractionReceipt, CompanionPackManifest, CompanionProfile }
