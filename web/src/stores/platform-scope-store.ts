import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface PlatformScopeState {
  clusterId: string | null
  namespace: string | null
  setClusterId: (id: string | null) => void
  setNamespace: (ns: string | null) => void
}

export const usePlatformScopeStore = create<PlatformScopeState>()(
  persist(
    (set) => ({
      clusterId: null,
      namespace: null,
      setClusterId: (clusterId) => set({ clusterId, namespace: null }),
      setNamespace: (namespace) => set({ namespace }),
    }),
    { name: 'soha-scope' },
  ),
)
