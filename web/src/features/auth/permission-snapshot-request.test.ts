/** @vitest-environment jsdom */

import { beforeEach, describe, expect, it, vi } from 'vitest'

const { fetchPermissionSnapshot } = vi.hoisted(() => ({ fetchPermissionSnapshot: vi.fn() }))
vi.mock('./auth-api', () => ({ fetchPermissionSnapshot, getStoredAccessToken: () => 'token' }))

describe('startup permission snapshot', () => {
  let requests: typeof import('./permission-snapshot-request')
  let auth: typeof import('@/stores/auth-store')

  beforeEach(async () => {
    vi.resetModules()
    fetchPermissionSnapshot.mockReset()
    requests = await import('./permission-snapshot-request')
    auth = await import('@/stores/auth-store')
  })

  it('joins the startup request once for the current session', async () => {
    const snapshot = { permissionKeys: ['delivery.applications.view'] }
    const fallback = vi.fn().mockResolvedValue({ data: snapshot })
    fetchPermissionSnapshot.mockResolvedValue(snapshot)
    requests.preparePermissionSnapshot()

    await expect(requests.readPermissionSnapshot(fallback)).resolves.toEqual({ data: snapshot })
    expect(fetchPermissionSnapshot).toHaveBeenCalledOnce()
    expect(fallback).not.toHaveBeenCalled()

    await requests.readPermissionSnapshot(fallback)
    expect(fallback).toHaveBeenCalledOnce()
  })

  it('discards a startup read after the session changes', async () => {
    const oldSnapshot = { permissionKeys: ['delivery.applications.view'] }
    const newSnapshot = { data: { permissionKeys: [] } }
    const fallback = vi.fn().mockResolvedValue(newSnapshot)
    fetchPermissionSnapshot.mockResolvedValueOnce(oldSnapshot)
    requests.preparePermissionSnapshot()
    auth.useAuthStore.getState().clearAuth()

    await expect(requests.readPermissionSnapshot(fallback)).resolves.toBe(newSnapshot)
    expect(fallback).toHaveBeenCalledOnce()
  })

  it('keeps the normal request when the auth guard starts it first', async () => {
    const fallback = vi.fn().mockResolvedValue({ data: { permissionKeys: [] } })
    const normal = requests.readPermissionSnapshot(fallback)
    requests.preparePermissionSnapshot()

    await normal
    expect(fetchPermissionSnapshot).not.toHaveBeenCalled()
    expect(fallback).toHaveBeenCalledOnce()
  })

  it('uses the normal request path if the startup read fails', async () => {
    const snapshot = { data: { permissionKeys: [] } }
    const fallback = vi.fn().mockResolvedValue(snapshot)
    fetchPermissionSnapshot.mockRejectedValue(new Error('request unavailable'))
    requests.preparePermissionSnapshot()

    await expect(requests.readPermissionSnapshot(fallback)).resolves.toBe(snapshot)
    expect(fallback).toHaveBeenCalledOnce()
  })
})
