import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { User } from '@/types'

interface AuthState {
  user: User | null
  accessToken: string | null
  setUser: (user: User) => void
  setTokens: (accessToken: string) => void
  clearAuth: () => void
  isAuthenticated: () => boolean
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      user: null,
      accessToken: null,
      setUser: (user) => set({ user }),
      setTokens: (accessToken) => set({ accessToken }),
      clearAuth: () => set({ user: null, accessToken: null }),
      isAuthenticated: () => !!get().accessToken,
    }),
    {
      name: 'soha-auth',
      merge: (persistedState, currentState) => {
        const persisted = persistedState as Partial<Pick<AuthState, 'user'>>
        return {
          ...currentState,
          accessToken: null,
          user: persisted.user ?? null,
        }
      },
      partialize: (state) => ({
        user: state.user,
      }),
    },
  ),
)
