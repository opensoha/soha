import { useEffect, useState, type ReactNode } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useAuthStore } from '@/stores/auth-store'

export function SessionQueryProvider({ children }: { children: ReactNode }) {
  const generation = useAuthStore((state) => state.sessionGeneration)
  return (
    <SessionQueries key={generation} generation={generation}>
      {children}
    </SessionQueries>
  )
}

function SessionQueries({ children, generation }: { children: ReactNode; generation: number }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 30_000 },
        },
      }),
  )

  useEffect(
    () => () => {
      // StrictMode also cleans up effects while the session is still current.
      if (useAuthStore.getState().sessionGeneration !== generation) client.clear()
    },
    [client, generation],
  )

  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}
