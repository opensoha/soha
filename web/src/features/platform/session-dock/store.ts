import { createStore } from 'zustand/vanilla'
import { createJSONStorage, persist } from 'zustand/middleware'
import { z } from 'zod'
import {
  normalizeRealtimeSession,
  sessionReferenceMatchesScope,
  type RealtimeSession,
  type RealtimeSessionWorkbench,
} from './types'

interface DockState {
  sessions: RealtimeSession[]
  activeSessionKeys: Record<string, string>
  selectedClusterId: string | null
}
const referenceDescriptor = z.object({
  applicationId: z.string().min(1),
  viewEnvironmentId: z.string().min(1),
  sourceEnvironmentId: z.string().min(1),
  serviceId: z.string().min(1),
  referenceId: z.string().min(1),
  referenceRevision: z.number().int().positive(),
  targetId: z.string().min(1),
  rootUid: z.string().min(1),
})
const descriptor = z.object({
  kind: z.enum(['logs', 'terminal']),
  clusterId: z.string(),
  namespace: z.string(),
  podName: z.string(),
  podUid: z.string().min(1).optional(),
  container: z.string().optional(),
  reference: referenceDescriptor.optional(),
  shell: z.string().optional(),
})
const savedState = z.object({
  sessions: z.array(z.unknown()),
  activeSessionKeys: z.record(z.string(), z.string()).optional(),
  selectedClusterId: z.string().nullable().optional(),
})

export function createSessionDockStore(
  workbench: RealtimeSessionWorkbench,
  ownerKey?: string,
  scopeKey?: string,
) {
  return createStore<DockState>()(
    persist(
      (): DockState => ({
        sessions: [],
        activeSessionKeys: {},
        selectedClusterId: null,
      }),
      {
      // Legacy mixed sessions have no workbench attribution, so cannot be restored safely.
      name:
        workbench === 'delivery'
          ? `soha-live-sessions:v3:${JSON.stringify([ownerKey || 'anonymous', workbench, scopeKey || 'unscoped'])}`
          : `soha-live-sessions:v2:${JSON.stringify([ownerKey || 'anonymous', workbench])}`,
      storage: createJSONStorage(() => ({
        getItem: (key) => {
          try {
            return ownerKey ? localStorage.getItem(key) : null
          } catch {
            return null
          }
        },
        setItem: (key, value) => {
          try {
            if (ownerKey) localStorage.setItem(key, value)
          } catch {
            /* Storage unavailable: sessions remain in memory. */
          }
        },
        removeItem: (key) => {
          try {
            if (ownerKey) localStorage.removeItem(key)
          } catch {
            /* Storage unavailable. */
          }
        },
      })),
      partialize: (state) => ({
        activeSessionKeys: state.activeSessionKeys,
        selectedClusterId: state.selectedClusterId,
        // Only allowlisted resource descriptors survive reload; never tickets, output or runtime hints.
        sessions: state.sessions.flatMap((session) => {
          const parsed = descriptor.safeParse(session)
          return parsed.success ? [parsed.data] : []
        }),
      }),
      merge: (saved, current) => {
        const result = savedState.safeParse(saved)
        if (!result.success) return current
        const parsedSessions = result.data.sessions
          .flatMap((value) => {
            const parsed = descriptor.safeParse(value)
            const session = parsed.success ? normalizeRealtimeSession(parsed.data) : null
            if (!session || !sessionReferenceMatchesScope(session.reference, workbench, scopeKey))
              return []
            return [session]
          })
          .filter(
            (session, index, list) => list.findIndex((item) => item.id === session.id) === index,
          )
        const sessions = parsedSessions
        const activeSessionKeys = Object.fromEntries(
          Object.entries(result.data.activeSessionKeys ?? {}).filter(([cluster, id]) =>
            sessions.some((item) => item.clusterId === cluster && item.id === id),
          ),
        )
        return {
          sessions,
          activeSessionKeys,
          selectedClusterId: sessions.some(
            (session) => session.clusterId === result.data.selectedClusterId,
          )
            ? result.data.selectedClusterId!
            : (sessions[0]?.clusterId ?? null),
        }
      },
    }),
  )
}
