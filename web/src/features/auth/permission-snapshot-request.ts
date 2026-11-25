import type { ApiResponse, PermissionSnapshot } from '@/types'
import { fetchPermissionSnapshot, getStoredAccessToken } from './auth-api'
import { captureAuthSession, isCurrentAuthSession, type AuthSessionOwner } from './auth-session'
import { takeWorkbenchPermissionSnapshot } from './workbench-startup-reads'

export const permissionSnapshotStaleTime = 30_000

let startupSnapshot:
  | {
      owner: AuthSessionOwner
      accessToken: string
      startedAt: number
      result: Promise<ApiResponse<PermissionSnapshot>>
    }
  | undefined
let startupClosed = false

export function preparePermissionSnapshot() {
  const accessToken = getStoredAccessToken()
  if (startupClosed || startupSnapshot || !accessToken) return
  const owner = captureAuthSession()
  const snapshot = takeWorkbenchPermissionSnapshot()
  const result = snapshot
    ? Promise.resolve({ data: snapshot })
    : fetchPermissionSnapshot().then((data) => ({ data }))
  startupSnapshot = { owner, accessToken, startedAt: Date.now(), result }
  void result.catch(() => undefined)
}

export function readPermissionSnapshot(fallback: () => Promise<ApiResponse<PermissionSnapshot>>) {
  startupClosed = true
  const snapshot = takeWorkbenchPermissionSnapshot()
  if (snapshot) return Promise.resolve({ data: snapshot })
  const prepared = startupSnapshot
  startupSnapshot = undefined
  if (
    prepared &&
    isCurrentAuthSession(prepared.owner) &&
    prepared.accessToken === getStoredAccessToken() &&
    Date.now() - prepared.startedAt < permissionSnapshotStaleTime
  ) {
    return prepared.result.then(
      (result) => {
        if (!isCurrentAuthSession(prepared.owner))
          throw new DOMException('Authentication context changed.', 'AbortError')
        return prepared.accessToken === getStoredAccessToken() ? result : fallback()
      },
      (error) => {
        if (!isCurrentAuthSession(prepared.owner)) throw error
        return fallback()
      },
    )
  }
  return fallback()
}
