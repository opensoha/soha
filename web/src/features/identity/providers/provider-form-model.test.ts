import { describe, expect, it } from 'vitest'
import {
  defaultOIDCClientValues,
  defaultProviderValues,
  oidcClientInputFromValues,
  oidcClientValuesFor,
  providerInputFromValues,
  providerValuesFor,
  proxyModeOptions,
} from './provider-form-model'
import type { IdentityOIDCClient, IdentityProvider } from './types'

const proxyProvider: IdentityProvider = {
  id: 'proxy-1',
  applicationId: 'grafana',
  name: 'Grafana Proxy',
  type: 'proxy',
  enabled: true,
  status: 'enabled',
  config: {
    external_hosts: [' grafana.example.com ', 'grafana.example.com'],
    upstream_url: ' http://grafana:3000 ',
    protected_path_prefix: '/grafana',
    outpost_id: 'edge-1',
    skip_auth_paths: '/healthz, /public',
    websocket_enabled: true,
    allow_private_upstream: true,
    header_mappings: {
      user: 'X-User',
      email: 'X-Email',
    },
    customSetting: 'preserved',
  },
  configuredSecretAliases: ['CLIENT_SECRET'],
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const client: IdentityOIDCClient = {
  id: 'client-1',
  providerId: 'provider-1',
  clientId: 'grafana',
  clientType: 'confidential',
  redirectUris: ['https://grafana.example/login'],
  redirectUriRegexes: ['https://[^/]+\\.example\\.com/callback'],
  postLogoutRedirectUris: ['https://grafana.example/logout'],
  allowedScopes: ['openid', 'email'],
  allowedGrantTypes: ['authorization_code'],
  requirePkce: true,
  accessTokenTtlSeconds: 3600,
  idTokenTtlSeconds: 300,
  refreshTokenTtlSeconds: 0,
  status: 'enabled',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

describe('provider form model', () => {
  it('keeps provider defaults aligned with current runtime behavior', () => {
    expect(defaultProviderValues()).toMatchObject({
      enabled: true,
      proxyMode: 'forward_auth',
      proxyAllowPrivateUpstream: false,
      proxyPathPrefix: '/',
      proxyWebsocketEnabled: true,
      status: 'enabled',
      type: 'oidc',
    })
    expect(proxyModeOptions).toContainEqual({ label: 'Reverse proxy', value: 'reverse_proxy' })
  })

  it('reads legacy proxy keys and writes one canonical config without losing advanced values', () => {
    const values = providerValuesFor(proxyProvider)
    expect(values).toMatchObject({
      proxyExternalHosts: ['grafana.example.com'],
      proxyHeaderEmail: 'X-Email',
      proxyHeaderUser: 'X-User',
      proxyAllowPrivateUpstream: true,
      proxyOutpostId: 'edge-1',
      proxyPathPrefix: '/grafana',
      proxySkipAuthPaths: ['/healthz', '/public'],
      proxyUpstreamUrl: 'http://grafana:3000',
      proxyWebsocketEnabled: true,
      type: 'proxy',
    })

    expect(providerInputFromValues(values)).toEqual({
      applicationId: 'grafana',
      name: 'Grafana Proxy',
      type: 'proxy',
      enabled: true,
      status: 'enabled',
      secretRefs: undefined,
      config: expect.objectContaining({
        customSetting: 'preserved',
        externalHosts: ['grafana.example.com'],
        headerMappings: expect.objectContaining({ user: 'X-User', email: 'X-Email' }),
        outpostId: 'edge-1',
        pathPrefix: '/grafana',
        skipAuthPaths: ['/healthz', '/public'],
        upstreamUrl: 'http://grafana:3000',
        websocketEnabled: true,
        allowPrivateUpstream: true,
      }),
    })
  })

  it('rejects non-object provider JSON before a mutation', () => {
    expect(() => providerInputFromValues({ ...defaultProviderValues(), configJson: '[]' })).toThrow(
      'Config 必须是 JSON object',
    )
    expect(() =>
      providerInputFromValues({ ...defaultProviderValues(), secretRefsJson: 'null' }),
    ).toThrow('Secret refs 必须是 JSON object')
  })

  it('submits explicitly entered secret references without reading existing values', () => {
    const values = providerValuesFor(proxyProvider)
    expect(values.secretRefsJson).toBe('')
    values.secretRefsJson = '{"CLIENT_SECRET":"soha://secrets/oidc-client"}'

    expect(providerInputFromValues(values).secretRefs).toEqual({
      CLIENT_SECRET: 'soha://secrets/oidc-client',
    })
  })

  it('submits OIDC providers without unmounted SAML fields', () => {
    const values = defaultProviderValues()
    values.applicationId = 'oidc-app'
    values.name = 'OIDC Provider'
    Reflect.deleteProperty(values, 'samlEntityId')
    Reflect.deleteProperty(values, 'samlAttributeMappingsJson')

    expect(providerInputFromValues(values)).toMatchObject({
      applicationId: 'oidc-app',
      config: {},
      name: 'OIDC Provider',
      type: 'oidc',
    })
  })

  it('builds a typed SAML service provider config instead of proxy config', () => {
    expect(
      providerInputFromValues({
        ...defaultProviderValues(),
        type: 'saml',
        samlEntityId: ' https://grafana.example/saml/metadata ',
        samlAcsUrls: [' https://grafana.example/saml/acs '],
        samlAttributeMappingsJson: '[{"source":"email","target":"email","required":true}]',
      }),
    ).toMatchObject({
      type: 'saml',
      config: {
        entityId: 'https://grafana.example/saml/metadata',
        acsUrls: ['https://grafana.example/saml/acs'],
        nameIdFormat: 'persistent',
        wantAuthnRequestsSigned: false,
        attributeMappings: [{ source: 'email', target: 'email', required: true }],
      },
    })
  })

  it('normalizes OIDC arrays and leaves an empty edit secret undefined', () => {
    const values = oidcClientValuesFor(client)
    expect(values.clientSecret).toBe('')
    expect(values.refreshTokenTtlSeconds).toBe(0)

    expect(
      oidcClientInputFromValues(client.providerId, {
        ...values,
        redirectRules: [
          { mode: 'strict', value: ' https://grafana.example/login ' },
          { mode: 'strict', value: 'https://grafana.example/login' },
          { mode: 'regex', value: ' https://[^/]+\\.example\\.com/callback ' },
        ],
        allowedScopes: ['openid', ' email ', 'openid'],
      }),
    ).toEqual({
      providerId: 'provider-1',
      clientId: 'grafana',
      clientSecret: undefined,
      clientType: 'confidential',
      redirectUris: ['https://grafana.example/login'],
      redirectUriRegexes: ['https://[^/]+\\.example\\.com/callback'],
      postLogoutRedirectUris: ['https://grafana.example/logout'],
      allowedScopes: ['openid', 'email'],
      allowedGrantTypes: ['authorization_code'],
      requirePkce: true,
      accessTokenTtlSeconds: 3600,
      idTokenTtlSeconds: 300,
      refreshTokenTtlSeconds: 0,
      status: 'enabled',
    })
  })

  it('omits a blank Client ID so the server can generate it', () => {
    expect(
      oidcClientInputFromValues('provider-1', {
        ...defaultOIDCClientValues(),
        clientId: '   ',
        redirectRules: [{ mode: 'strict', value: 'https://app.example.com/oauth/callback' }],
      }),
    ).not.toHaveProperty('clientId')
  })

  it('keeps new OIDC defaults on authorization code and PKCE', () => {
    expect(defaultOIDCClientValues()).toMatchObject({
      allowedGrantTypes: ['authorization_code'],
      allowedScopes: ['openid', 'profile', 'email'],
      clientType: 'confidential',
      requirePkce: true,
      accessTokenTtlSeconds: 3600,
      idTokenTtlSeconds: 300,
      redirectRules: [{ mode: 'strict', value: '' }],
      refreshTokenTtlSeconds: 0,
    })
  })
})
