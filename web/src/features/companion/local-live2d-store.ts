import { create } from 'zustand'
import type { CompanionPackSelection } from './types'

export const LOCAL_LIVE2D_PLUGIN_ID = 'local.live2d-preview'

interface LocalCompanionPackState {
  pack: CompanionPackSelection | null
  clear: () => void
  setPack: (pack: CompanionPackSelection) => void
}

export const useLocalCompanionPackStore = create<LocalCompanionPackState>((set) => ({
  pack: null,
  clear: () => set({ pack: null }),
  setPack: (pack) => set({ pack }),
}))
