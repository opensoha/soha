import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { User } from '@/types'

interface AuthState {
  user: User | null
  accessToken: string | null
  sessionGeneration: number
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
      sessionGeneration: 0,
      setUser: (user) =>
        set((state) => ({
          user,
          sessionGeneration:
            state.user?.userId === user.userId
              ? state.sessionGeneration
              : state.sessionGeneration + 1,
        })),
      setTokens: (accessToken) =>
        set((state) => ({
          accessToken,
          sessionGeneration:
            !state.accessToken && accessToken
              ? state.sessionGeneration + 1
              : state.sessionGeneration,
        })),
      clearAuth: () =>
        set((state) => ({
          user: null,
          accessToken: null,
          sessionGeneration: state.sessionGeneration + 1,
        })),
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
