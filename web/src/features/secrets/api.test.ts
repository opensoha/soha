import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SecretMetadata, SecretVersionMetadata } from '@opensoha/contracts/gen/ts/sohaapi'
import {
  buildSecretReference,
  createSecret,
  disableSecret,
  isSecretReference,
  listSecrets,
  listSecretReferences,
  listSecretVersions,
  revokeSecretVersion,
  rotateSecret,
  updateSecret,
} from './api'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  getEnvelope: vi.fn(),
  patch: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const secret: SecretMetadata = {
  id: 'secret-1',
  name: 'Registry token',
  scopeType: 'project',
  scopeId: 'demo',
  status: 'active',
  currentVersion: 1,
  bindings: [{ targetType: 'project', targetRef: 'demo' }],
  createdBy: 'admin',
  createdAt: '2026-08-03T00:00:00Z',
  updatedAt: '2026-08-03T00:00:00Z',
}

const version: SecretVersionMetadata = {
  secretId: secret.id,
  version: 2,
  status: 'active',
  createdBy: 'admin',
  createdAt: '2026-08-03T00:01:00Z',
}

describe('secret store api', () => {
  beforeEach(() => vi.clearAllMocks())

  it('lists metadata with normalized scope filters and never expects a value', async () => {
    apiMocks.getEnvelope.mockResolvedValueOnce({ items: [secret] })

    await expect(listSecrets({ scopeType: 'project', scopeId: ' demo ' })).resolves.toEqual([
      secret,
    ])
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith('/secrets?scopeType=project&scopeId=demo')
    expect(JSON.stringify(secret)).not.toContain('opaque-secret')
  })

  it('preserves opaque write-only values only in create and rotate requests', async () => {
    const opaqueValue = '  opaque-secret  '
    const createInput = {
      name: secret.name,
      value: opaqueValue,
      scopeType: secret.scopeType,
      scopeId: secret.scopeId,
      bindings: secret.bindings,
    }
    apiMocks.post.mockResolvedValueOnce({ data: secret }).mockResolvedValueOnce({ data: version })

    await expect(createSecret(createInput)).resolves.toBe(secret)
    await expect(
      rotateSecret({ secretId: ' secret-1 ', input: { value: opaqueValue } }),
    ).resolves.toBe(version)

    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/secrets', createInput)
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/secrets/secret-1/versions', {
      value: opaqueValue,
    })
  })

  it('forwards only pinned Vault KV v2 locators for external versions', async () => {
    const vaultKv2 = { mount: 'secret', path: 'demo/app', key: 'token', version: 3 }
    const createInput = {
      name: secret.name,
      vaultKv2,
      scopeType: secret.scopeType,
      scopeId: secret.scopeId,
      bindings: secret.bindings,
    }
    apiMocks.post.mockResolvedValueOnce({ data: secret }).mockResolvedValueOnce({ data: version })

    await expect(createSecret(createInput)).resolves.toBe(secret)
    await expect(rotateSecret({ secretId: secret.id, input: { vaultKv2 } })).resolves.toBe(version)

    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/secrets', createInput)
    expect(apiMocks.post).toHaveBeenNthCalledWith(2, '/secrets/secret-1/versions', { vaultKv2 })
    expect(JSON.stringify(apiMocks.post.mock.calls)).not.toContain('vault-token')
  })

  it('encodes management paths and unwraps metadata-only responses', async () => {
    apiMocks.patch.mockResolvedValueOnce({ data: secret })
    apiMocks.delete.mockResolvedValueOnce({ data: { ...secret, status: 'disabled' } })
    apiMocks.getEnvelope.mockResolvedValueOnce({ items: [version] })
    apiMocks.post.mockResolvedValueOnce({ data: { ...version, status: 'revoked' } })

    await expect(
      updateSecret({ secretId: ' secret/id ', input: { description: 'rotated' } }),
    ).resolves.toBe(secret)
    await disableSecret(' secret/id ')
    await expect(listSecretVersions(' secret/id ')).resolves.toEqual([version])
    await revokeSecretVersion({ secretId: ' secret/id ', version: 2 })

    expect(apiMocks.patch).toHaveBeenCalledWith('/secrets/secret%2Fid', {
      description: 'rotated',
    })
    expect(apiMocks.delete).toHaveBeenCalledWith('/secrets/secret%2Fid')
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith('/secrets/secret%2Fid/versions')
    expect(apiMocks.post).toHaveBeenCalledWith('/secrets/secret%2Fid/versions/2/revoke')
  })

  it('retains actual VM reference envelopes without normalizing items into data', async () => {
    const usage = {
      targetId: 'vm-one',
      vmId: 'vm-one',
      usage: 'login_binding',
      version: 1,
      bindingRevision: 2,
      status: 'active',
    }
    apiMocks.get.mockResolvedValue({ data: [] })
    apiMocks.getEnvelope.mockResolvedValueOnce({ items: [usage] })
    await expect(listSecretReferences('secret-1')).resolves.toEqual([usage])
    expect(apiMocks.getEnvelope).toHaveBeenCalledWith('/secrets/secret-1/references')
    expect(apiMocks.get).not.toHaveBeenCalled()
  })

  it('builds only canonical latest or pinned references', () => {
    expect(buildSecretReference(' secret-1 ')).toBe('soha://secrets/secret-1')
    expect(buildSecretReference('secret-1', 2)).toBe('soha://secrets/secret-1/versions/2')
    expect(() => buildSecretReference('secret/id')).toThrow('Invalid secret ID')
    expect(() => buildSecretReference('secret-1', 0)).toThrow('Invalid secret version')
    expect(isSecretReference('soha://secrets/secret-1')).toBe(true)
    expect(isSecretReference('soha://secrets/secret-1/versions/2')).toBe(true)
    expect(isSecretReference('vault://secret-1')).toBe(false)
  })
})
