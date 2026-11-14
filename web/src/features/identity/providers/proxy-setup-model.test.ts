import { describe, expect, it } from 'vitest'
import { parse, parseAllDocuments } from 'yaml'
import type { IdentityProviderSetup } from './types'
import { proxySetupContext, proxySetupSnippet, proxySetupTargets } from './proxy-setup-model'

const provider = {
  id: 'proxy-grafana',
  applicationId: 'grafana',
  name: 'Grafana Proxy',
  type: 'proxy' as const,
  enabled: true,
  status: 'enabled' as const,
  config: {
    externalHosts: ['grafana.example.com'],
    upstreamUrl: 'http://grafana:3000',
  },
  createdAt: '2026-07-13T00:00:00Z',
  updatedAt: '2026-07-13T00:00:00Z',
}

describe('proxy setup model', () => {
  const setup: IdentityProviderSetup = {
    providerId: provider.id,
    configurationStatus: 'complete',
    issues: [],
    requiresOutpostToken: false,
    migrationRequired: false,
    endpoints: {
      forwardAuthUrl:
        'https://soha.example.com/api/v1/provider/proxy/auth?provider_id=proxy-grafana',
      loginUrl: 'https://soha.example.com/api/v1/provider/proxy/start?provider_id=proxy-grafana',
    },
  }
  it.each([false, true])(
    'uses the saved endpoint and proxy-specific authentication, remote=%s',
    (remote) => {
      const selected = remote
        ? {
            ...setup,
            requiresOutpostToken: true,
            endpoints: {
              ...setup.endpoints,
              forwardAuthUrl: 'https://edge.example.com/custom/outpost/forward-auth',
            },
          }
        : setup
      const context = proxySetupContext(provider, selected)!
      expect(proxySetupTargets).toHaveLength(7)
      expect(context.authURL).toBe(selected.endpoints.forwardAuthUrl)
      for (const target of proxySetupTargets) {
        const snippet = proxySetupSnippet(target, context)
        expect(snippet.toLowerCase()).toContain('soha')
        expect(snippet).toContain(new URL(context.authURL).host)
        expect(snippet).toContain(new URL(context.authURL).pathname)
        expect(snippet.includes('REPLACE_WITH_AGENT_HTTP_TOKEN')).toBe(remote)
        expect(snippet).not.toContain('/identity/outposts/')
        if (target.includes('ingress'))
          expect(parseAllDocuments(snippet).every((document) => document.errors.length === 0)).toBe(
            true,
          )
      }
      expect(proxySetupSnippet('traefik-compose', context)).toContain('grafana.example.com')
      const nginxIngress = proxySetupSnippet('nginx-ingress', context)
      expect(nginxIngress).not.toContain('redirect=true')
      expect(nginxIngress).toContain('return_to=$scheme://$host$escaped_request_uri')
      expect(
        parseAllDocuments(nginxIngress)[1].toJS().metadata.annotations[
          'nginx.ingress.kubernetes.io/auth-response-headers'
        ],
      ).toContain('X-Soha-Outpost-Token,X-Soha-Session-Token')
      const nginx = proxySetupSnippet('nginx-standalone', context)
      expect(nginx).toContain('auth_request /_soha_auth;')
      expect(nginx).toContain('proxy_pass_request_body off;')
      expect(nginx).toContain('return 302 $soha_login;')
      expect(nginx).toContain('proxy_set_header Cookie $http_cookie;')
      expect(nginx.includes('mode=nginx')).toBe(remote)
      const traefik = parse(proxySetupSnippet('traefik-standalone', context)).http
      expect(traefik.routers.protected.middlewares).toEqual([
        'soha-auth-input',
        'soha-auth',
        'soha-auth-cleanup',
      ])
      expect(traefik.middlewares['soha-auth'].forwardAuth.trustForwardHeader).toBe(false)
      expect(traefik.middlewares['soha-auth'].forwardAuth.authRequestHeaders).not.toContain(
        'X-Soha-Session-Token',
      )
      expect(
        traefik.middlewares['soha-auth-cleanup'].headers.customRequestHeaders[
          'X-Soha-Outpost-Token'
        ],
      ).toBe('')
      expect(
        parse(proxySetupSnippet('traefik-compose', context)).services['protected-application']
          .labels['traefik.enable'],
      ).toBe('true')
      expect(proxySetupSnippet('caddy-standalone', context)).toContain(
        'request_header -X-Soha-User',
      )
    },
  )
  it('withholds templates for missing configuration or a different provider', () => {
    expect(
      proxySetupContext(provider, { ...setup, configurationStatus: 'incomplete' }),
    ).toBeUndefined()
    expect(
      proxySetupContext(provider, { ...setup, endpoints: { loginUrl: setup.endpoints.loginUrl } }),
    ).toBeUndefined()
    expect(proxySetupContext(provider, { ...setup, providerId: 'other' })).toBeUndefined()
  })
  it('uses saved identity header mappings and rejects unsafe template syntax', () => {
    const custom = {
      ...provider,
      config: {
        ...provider.config,
        headerMappings: { userId: 'X-Auth-User', email: 'X-Auth-Email' },
      },
    }
    const context = proxySetupContext(custom, setup)!
    for (const target of proxySetupTargets) {
      const snippet = proxySetupSnippet(target, context)
      expect(snippet).toContain('X-Auth-User')
      expect(snippet).toContain('X-Auth-Email')
      expect(snippet).toContain('X-Soha-Projects')
      expect(snippet).toContain('X-Soha-Tags')
    }
    expect(
      proxySetupContext(
        {
          ...custom,
          config: { ...custom.config, headerMappings: { userId: 'X-Auth; return 200' } },
        },
        setup,
      ),
    ).toBeUndefined()
  })
})
