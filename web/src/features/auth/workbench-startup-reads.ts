import type { ApplicationWorkbenchRefreshEnvelope } from '@opensoha/contracts/gen/ts/sohaapi'
import { isCurrentAuthSession, type AuthSessionOwner } from './auth-session'

type WorkbenchRefresh = ApplicationWorkbenchRefreshEnvelope['data']
type WorkbenchEntry = NonNullable<WorkbenchRefresh['entry']>

const startupReadLifetimeMs = 30_000
let prepared:
  | {
      owner: AuthSessionOwner
      expiresAt: number
      permissionSnapshot?: WorkbenchRefresh['permissionSnapshot']
      entry?: Partial<WorkbenchEntry>
      applicationId: string
    }
  | undefined

export function clearWorkbenchReads() {
  prepared = undefined
}

export function prepareWorkbenchReads(
  owner: AuthSessionOwner,
  applicationId: string,
  response: Pick<WorkbenchRefresh, 'permissionSnapshot' | 'entry'>,
) {
  prepared = {
    owner,
    expiresAt: Date.now() + startupReadLifetimeMs,
    permissionSnapshot: response.permissionSnapshot,
    entry: response.entry?.application.id === applicationId ? { ...response.entry } : undefined,
    applicationId,
  }
}

function currentReads() {
  if (prepared && (!isCurrentAuthSession(prepared.owner) || Date.now() >= prepared.expiresAt)) {
    prepared = undefined
  }
  return prepared
}

export function takeWorkbenchPermissionSnapshot() {
  const current = currentReads()
  const snapshot = current?.permissionSnapshot
  if (current) delete current.permissionSnapshot
  return snapshot
}

export function takeWorkbenchEntryRead<Key extends keyof WorkbenchEntry>(
  applicationId: string,
  key: Key,
  signal: AbortSignal,
) {
  signal.throwIfAborted()
  const current = currentReads()
  if (!current?.entry || current.applicationId !== applicationId) return undefined
  const value = current.entry[key]
  delete current.entry[key]
  return value
}
