import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  createIdentityOIDCClient,
  createIdentityProvider,
  deleteIdentityOIDCClient,
  deleteIdentityProvider,
  getIdentityProvider,
  getIdentityProviderProtocolMetadata,
  getIdentityProviderUserMetadata,
  importSAMLLoginSourceMetadata,
  listIdentityOIDCClients,
  listIdentityProviders,
  revealIdentityOIDCClientSecret,
  rotateIdentityProviderSAMLCertificate,
  rotateIdentityProviderSigningKey,
  rotateSAMLCertificate,
  updateIdentityOIDCClient,
  updateIdentityProvider,
  validateSAMLMetadata,
} from './api'
import type {
  IdentityOIDCClient,
  IdentityOIDCClientInput,
  IdentityProvider,
  IdentityProviderInput,
} from './types'

const apiMocks = vi.hoisted(() => ({
  delete: vi.fn(),
  get: vi.fn(),
  getText: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const provider: IdentityProvider = {
  id: 'provider/id',
  applicationId: 'grafana',
  name: 'Grafana OIDC',
  type: 'oidc',
  enabled: true,
  status: 'enabled',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const providerInput: IdentityProviderInput = {
  applicationId: provider.applicationId,
  name: provider.name,
  type: provider.type,
  enabled: provider.enabled,
  config: {},
  secretRefs: {},
  status: provider.status,
}

const client: IdentityOIDCClient = {
  id: 'client/id',
  providerId: provider.id,
  clientId: 'grafana',
  clientType: 'confidential',
  redirectUris: ['https://grafana.example/login'],
  redirectUriRegexes: ['https://[^/]+\\.example\\.com/login'],
  postLogoutRedirectUris: ['https://grafana.example/logout'],
  allowedScopes: ['openid'],
  allowedGrantTypes: ['authorization_code'],
  requirePkce: true,
  accessTokenTtlSeconds: 3600,
  idTokenTtlSeconds: 300,
  refreshTokenTtlSeconds: 0,
  status: 'enabled',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const clientInput: IdentityOIDCClientInput = {
  clientId: client.clientId,
  clientType: client.clientType,
  redirectUris: client.redirectUris,
  redirectUriRegexes: client.redirectUriRegexes,
  postLogoutRedirectUris: client.postLogoutRedirectUris,
  allowedScopes: client.allowedScopes,
  allowedGrantTypes: ['authorization_code'],
  requirePkce: client.requirePkce,
  accessTokenTtlSeconds: client.accessTokenTtlSeconds,
  idTokenTtlSeconds: client.idTokenTtlSeconds,
  refreshTokenTtlSeconds: client.refreshTokenTtlSeconds,
  status: client.status,
}

describe('identity providers api', () => {
  it('loads protocol documents through the shared transport and encodes identifiers', async () => {
    apiMocks.get.mockResolvedValueOnce({ issuer: 'https://soha.example' })
    expect(await getIdentityProviderProtocolMetadata(provider)).toContain('https://soha.example')
    expect(apiMocks.get).toHaveBeenCalledWith('/provider/oidc/.well-known/openid-configuration')
    apiMocks.getText.mockResolvedValueOnce('<EntityDescriptor />')
    expect(await getIdentityProviderProtocolMetadata({ ...provider, type: 'saml' })).toBe('<EntityDescriptor />')
    expect(apiMocks.getText).toHaveBeenCalledWith('/saml2/idp/provider%2Fid/metadata')
    apiMocks.get.mockResolvedValueOnce({ data: { attributes: { sub: ['user/id'] } } })
    await getIdentityProviderUserMetadata(provider.id, 'user/id', 'client/id')
    expect(apiMocks.get).toHaveBeenCalledWith('/identity/providers/provider%2Fid/users/user%2Fid/metadata?clientId=client%2Fid')
  })
  beforeEach(() => vi.clearAllMocks())

  it('normalizes every backend list filter and unwraps provider data', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: [provider] })

    await expect(
      listIdentityProviders({
        applicationId: ' grafana ',
        type: 'oidc',
        status: 'enabled',
        limit: 25.9,
        offset: 50,
      }),
    ).resolves.toEqual([provider])
    expect(apiMocks.get).toHaveBeenCalledWith(
      '/identity/providers?applicationId=grafana&type=oidc&status=enabled&limit=25&offset=50',
    )
  })

  it('tolerates empty provider and OIDC client list envelopes', async () => {
    apiMocks.get.mockResolvedValueOnce({}).mockResolvedValueOnce({})

    await expect(listIdentityProviders()).resolves.toEqual([])
    await expect(listIdentityOIDCClients(' provider/id ')).resolves.toEqual([])
    expect(apiMocks.get).toHaveBeenNthCalledWith(1, '/identity/providers')
    expect(apiMocks.get).toHaveBeenNthCalledWith(
      2,
      '/identity/providers/provider%2Fid/oidc-clients',
    )
  })

  it('unwraps provider detail/create/update and encodes trimmed ids', async () => {
    apiMocks.get.mockResolvedValueOnce({ data: provider })
    apiMocks.post.mockResolvedValueOnce({ data: provider })
    apiMocks.put.mockResolvedValueOnce({ data: provider })

    await expect(getIdentityProvider(' provider/id ')).resolves.toBe(provider)
    await expect(createIdentityProvider(providerInput)).resolves.toBe(provider)
    await expect(
      updateIdentityProvider({ providerId: ' provider/id ', input: providerInput }),
    ).resolves.toBe(provider)

    expect(apiMocks.get).toHaveBeenCalledWith('/identity/providers/provider%2Fid')
    expect(apiMocks.post).toHaveBeenCalledWith('/identity/providers', providerInput)
    expect(apiMocks.put).toHaveBeenCalledWith('/identity/providers/provider%2Fid', providerInput)
  })

  it('preserves the one-time OIDC secret and unwraps client updates', async () => {
    const created = { client, clientSecret: 'shown-once' }
    apiMocks.post.mockResolvedValueOnce({ data: created })
    apiMocks.put.mockResolvedValueOnce({ data: client })

    await expect(
      createIdentityOIDCClient({ providerId: ' provider/id ', input: clientInput }),
    ).resolves.toBe(created)
    await expect(
      updateIdentityOIDCClient({
        providerId: provider.id,
        clientId: ' client/id ',
        input: clientInput,
      }),
    ).resolves.toBe(client)

    expect(apiMocks.post).toHaveBeenCalledWith(
      '/identity/providers/provider%2Fid/oidc-clients',
      clientInput,
    )
    expect(apiMocks.put).toHaveBeenCalledWith('/identity/oidc-clients/client%2Fid', clientInput)
  })

  it('reveals an existing OIDC Client Secret through the no-cache action endpoint', async () => {
    const revealed = {
      clientId: 'generated-client-id',
      clientSecret: 'revealable-client-secret',
      revealedAt: '2026-08-24T08:00:00Z',
    }
    apiMocks.post.mockResolvedValueOnce({ data: revealed })

    await expect(revealIdentityOIDCClientSecret(' client/id ')).resolves.toBe(revealed)
    expect(apiMocks.post).toHaveBeenCalledWith('/identity/oidc-clients/client%2Fid/secret/reveal')
  })

  it('keeps provider and client delete transport results out of the domain', async () => {
    apiMocks.delete.mockResolvedValue(undefined)

    await expect(deleteIdentityProvider(' provider/id ')).resolves.toBeUndefined()
    await expect(
      deleteIdentityOIDCClient({ providerId: provider.id, clientId: ' client/id ' }),
    ).resolves.toBeUndefined()

    expect(apiMocks.delete).toHaveBeenNthCalledWith(1, '/identity/providers/provider%2Fid')
    expect(apiMocks.delete).toHaveBeenNthCalledWith(2, '/identity/oidc-clients/client%2Fid')
  })

  it('rotates a provider signing key without sending secret material', async () => {
    const key = {
      id: 'key-1',
      providerId: provider.id,
      kid: 'kid-1',
      alg: 'ES256',
      active: true,
      createdAt: '2026-07-10T00:00:00Z',
    }
    apiMocks.post.mockResolvedValueOnce({ data: key })

    await expect(rotateIdentityProviderSigningKey(' provider/id ')).resolves.toBe(key)
    expect(apiMocks.post).toHaveBeenCalledWith(
      '/identity/providers/provider%2Fid/signing-keys/rotate',
    )
  })

  it('wires SAML metadata validation, import, and certificate rotation endpoints', async () => {
    const validation = { valid: true, entityId: 'idp', singleSignOnUrls: [], certificates: [] }
    const source = { id: 'source-1' }
    const rotation = { active: { id: 'active' }, retiring: { id: 'old' } }
    apiMocks.post
      .mockResolvedValueOnce({ data: validation })
      .mockResolvedValueOnce({ data: source })
      .mockResolvedValueOnce({ data: rotation })
      .mockResolvedValueOnce({ data: rotation })

    await expect(
      validateSAMLMetadata({ source: 'url', url: 'https://idp.example/metadata' }),
    ).resolves.toBe(validation)
    await expect(
      importSAMLLoginSourceMetadata({
        name: 'Enterprise',
        status: 'active',
        metadata: { source: 'url', url: 'https://idp.example/metadata' },
      }),
    ).resolves.toBe(source)
    await expect(
      rotateIdentityProviderSAMLCertificate({
        providerId: ' provider/id ',
        input: { overlapSeconds: 604800 },
      }),
    ).resolves.toBe(rotation)
    await expect(rotateSAMLCertificate(' cert/id ', { overlapSeconds: 3600 })).resolves.toBe(
      rotation,
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(1, '/identity/saml/metadata/validate', {
      source: 'url',
      url: 'https://idp.example/metadata',
    })
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      3,
      '/identity/providers/provider%2Fid/saml/certificate/rotate',
      { overlapSeconds: 604800 },
    )
    expect(apiMocks.post).toHaveBeenNthCalledWith(
      4,
      '/identity/saml/certificates/cert%2Fid/rotate',
      { overlapSeconds: 3600 },
    )
  })
})
