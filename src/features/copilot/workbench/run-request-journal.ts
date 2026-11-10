// Each submission has its own entry so two windows cannot overwrite each other's recovery IDs.
// Store only opaque IDs. Request contents, attachments, tokens and context stay out of storage.
function prefix(owner: string, sessionId: string) {
  return `soha.run-request.v2:${encodeURIComponent(owner)}/${encodeURIComponent(sessionId)}/`
}

export function pendingRunRequests(owner?: string, sessionId?: string): string[] {
  if (!owner || !sessionId) return []
  const scope = prefix(owner, sessionId)
  importLegacy(owner, sessionId)
  const ids: string[] = []
  for (let index = 0; index < localStorage.length; index++) {
    const key = localStorage.key(index)
    if (
      key?.startsWith(scope) &&
      localStorage.getItem(key) !== 'done' &&
      !localStorage.getItem(key)?.startsWith('readonly:')
    ) {
      const id = decodeURIComponent(key.slice(scope.length))
      if (!id || id.length > 128) throw new Error('任务恢复标识不完整，请核对原会话')
      ids.push(id)
    }
  }
  return ids.sort()
}

export function rememberRunRequest(owner: string, sessionId: string, requestId: string) {
  if (!owner || !sessionId || !requestId || requestId.length > 128)
    throw new Error('无法保存任务恢复标识')
  importLegacy(owner, sessionId)
  const key = prefix(owner, sessionId) + encodeURIComponent(requestId)
  if (localStorage.getItem(key)?.startsWith('readonly:'))
    throw new Error('旧版任务仅供查看，请发起新的请求')
  if (localStorage.getItem(key) !== 'cancel') localStorage.setItem(key, '1')
}

export function requestRunCancellation(owner: string, sessionId: string) {
  for (const requestId of pendingRunRequests(owner, sessionId))
    localStorage.setItem(prefix(owner, sessionId) + encodeURIComponent(requestId), 'cancel')
}

export function runCancellationRequested(owner: string, sessionId: string, requestId: string) {
  importLegacy(owner, sessionId)
  return ['cancel', 'readonly:cancel'].includes(
    localStorage.getItem(prefix(owner, sessionId) + encodeURIComponent(requestId)) ?? '',
  )
}

export function pendingRunCancellations(owner?: string) {
  const entries: Array<{ sessionId: string; requestId: string }> = []
  if (!owner) return entries
  importLegacy(owner)
  const scope = `soha.run-request.v2:${encodeURIComponent(owner)}/`
  for (let index = 0; index < localStorage.length; index++) {
    const key = localStorage.key(index)
    if (!key?.startsWith(scope) || localStorage.getItem(key) !== 'cancel') continue
    const [session, request] = key.slice(scope.length).split('/')
    const sessionId = decodeURIComponent(session ?? '')
    const requestId = decodeURIComponent(request ?? '')
    if (!sessionId || !requestId || requestId.length > 128)
      throw new Error('任务取消标识不完整，请核对原会话')
    entries.push({ sessionId, requestId })
  }
  return entries.sort((a, b) => JSON.stringify(a).localeCompare(JSON.stringify(b)))
}

export function runRecoveryBatch<T>(ids: T[], page: number) {
  if (ids.length <= 64) return ids
  const offset = (page * 64) % ids.length
  return [...ids.slice(offset), ...ids.slice(0, offset)].slice(0, 64)
}

export function forgetRunRequest(owner: string, sessionId: string, requestId: string) {
  try {
    const key = prefix(owner, sessionId) + encodeURIComponent(requestId)
    if (localStorage.getItem(key) === null || localStorage.getItem(key) === 'done') return false
    // Keep a terminal receipt so retained legacy keys cannot resurrect recovery.
    localStorage.setItem(key, 'done')
    return true
  } catch {
    return false
  }
}

// v1 owner already includes the server scope. Never broaden that identity when importing.
// Old clients may still write v1: leave their keys intact and OR their cancellation latch.
function importLegacy(owner: string, sessionId?: string) {
  const scope =
    `soha.native-request.v1:${encodeURIComponent(owner)}/` +
    (sessionId ? `${encodeURIComponent(sessionId)}/` : '')
  const keys = Array.from({ length: localStorage.length }, (_, i) => localStorage.key(i))
  for (const key of keys) {
    if (!key?.startsWith(scope)) continue
    const suffix = key.slice(`soha.native-request.v1:${encodeURIComponent(owner)}/`.length)
    const parts = suffix.split('/')
    if (parts.length !== 2) throw new Error('任务恢复标识不完整，请核对原会话')
    const [session, request] = parts.map(decodeURIComponent)
    if (!session || !request || request.length > 128)
      throw new Error('任务恢复标识不完整，请核对原会话')
    const target = prefix(owner, session) + encodeURIComponent(request)
    const previous = localStorage.getItem(target)
    if (previous === 'done') continue
    if (previous?.startsWith('readonly:')) {
      if (localStorage.getItem(key) === 'cancel') localStorage.setItem(target, 'readonly:cancel')
      continue
    }
    const legacy = localStorage.getItem(key)
    if (legacy === 'cancel' && previous !== 'cancel') localStorage.setItem(target, 'cancel')
    else if (previous === null) localStorage.setItem(target, '1')
  }
}

// Reconciliation acknowledges identity only: preserve unknown outcome and the stop latch.
export function acknowledgeReadOnlyRunRequest(owner: string, sessionId: string, requestId: string) {
  const stopping = runCancellationRequested(owner, sessionId, requestId)
  localStorage.setItem(
    prefix(owner, sessionId) + encodeURIComponent(requestId),
    stopping ? 'readonly:cancel' : 'readonly:unknown',
  )
}
