import { stringify } from 'yaml'
import type { IdentityProvider, IdentityProviderSetup } from './types'
import { proxyHeaderNamesFor } from './provider-form-model.ts'

export const proxySetupTargets = [
  'nginx-ingress',
  'nginx-proxy-manager',
  'nginx-standalone',
  'traefik-ingress',
  'traefik-compose',
  'traefik-standalone',
  'caddy-standalone',
] as const
export type ProxySetupTarget = (typeof proxySetupTargets)[number]
const tokenPlaceholder = 'REPLACE_WITH_AGENT_HTTP_TOKEN'

export interface ProxySetupContext {
  authURL: string
  loginURL: string
  host: string
  upstreamURL: string
  requiresOutpostToken: boolean
  identityHeaders: string[]
}

export function proxySetupContext(
  provider: IdentityProvider,
  setup: IdentityProviderSetup,
): ProxySetupContext | undefined {
  if (
    setup.providerId !== provider.id ||
    setup.configurationStatus !== 'complete' ||
    !setup.endpoints.forwardAuthUrl ||
    !setup.endpoints.loginUrl
  )
    return undefined
  const hosts = provider.config?.externalHosts
  const host = Array.isArray(hosts) ? hosts.find((value) => typeof value === 'string') : undefined
  if (typeof host !== 'string' || !/^[a-zA-Z0-9.*:_-]+$/.test(host)) return undefined
  const upstream = provider.config?.upstreamUrl
  const identityHeaders = proxyHeaderNamesFor(provider)
  if (identityHeaders.some((header) => !/^[a-zA-Z0-9_-]+$/.test(header))) return undefined
  return {
    identityHeaders,
    authURL: setup.endpoints.forwardAuthUrl,
    loginURL: setup.endpoints.loginUrl,
    host,
    upstreamURL: typeof upstream === 'string' && upstream ? upstream : 'http://upstream:8080',
    requiresOutpostToken: setup.requiresOutpostToken,
  }
}

function authURL(context: ProxySetupContext, nginx: boolean) {
  const url = new URL(context.authURL)
  if (nginx && context.requiresOutpostToken) url.searchParams.set('mode', 'nginx')
  if (!nginx && !context.requiresOutpostToken) url.searchParams.set('redirect', 'true')
  return url.toString()
}
function nginxQuote(value: string) {
  return JSON.stringify(value).replace(/\$/g, '\\$')
}
function nginxLocations(context: ProxySetupContext) {
  const identity = context.identityHeaders
    .map(
      (
        header,
        index,
      ) => `    auth_request_set $soha_identity_${index} $upstream_http_${header.toLowerCase().replace(/-/g, '_')};
    proxy_set_header ${header} $soha_identity_${index};`,
    )
    .join('\n')
  return `  location / {
    auth_request /_soha_auth;
    auth_request_set $soha_login $upstream_http_x_soha_login_url;
    error_page 401 = @soha_signin;
${identity}
    proxy_set_header X-Soha-Outpost-Token "";
    proxy_set_header X-Soha-Session-Token "";
    proxy_set_header Host $host;
    proxy_pass ${nginxQuote(context.upstreamURL)};
  }
  location = /_soha_auth {
    internal;
    proxy_pass ${nginxQuote(authURL(context, true))};
    proxy_pass_request_body off;
    proxy_set_header Content-Length "";
    proxy_set_header Cookie $http_cookie;
    proxy_set_header X-Forwarded-Host $http_host;
    proxy_set_header X-Forwarded-Uri $request_uri;
    proxy_set_header X-Original-URL $scheme://$http_host$request_uri;
    proxy_set_header X-Forwarded-Method $request_method;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Soha-Session-Token "";
    proxy_set_header X-Soha-Outpost-Token ${context.requiresOutpostToken ? nginxQuote(tokenPlaceholder) : '""'};
  }
  location @soha_signin {
    return 302 $soha_login;
  }`
}
function traefikHeaders(context: ProxySetupContext) {
  return Object.fromEntries([
    ...context.identityHeaders.map((header) => [header, '']),
    ['X-Soha-Session-Token', ''],
    ['X-Soha-Outpost-Token', context.requiresOutpostToken ? tokenPlaceholder : ''],
  ])
}
function traefikForwardAuth(context: ProxySetupContext) {
  return {
    address: authURL(context, false),
    trustForwardHeader: false,
    authRequestHeaders: [
      'Cookie',
      'X-Forwarded-Host',
      'X-Forwarded-Uri',
      'X-Forwarded-Method',
      'X-Forwarded-Proto',
      'X-Forwarded-For',
      'X-Soha-Outpost-Token',
    ],
    authResponseHeaders: context.identityHeaders,
  }
}
export function proxySetupSnippet(target: ProxySetupTarget, context: ProxySetupContext) {
  const { host, upstreamURL, identityHeaders } = context
  const forwardAuth = traefikForwardAuth(context)
  const clearToken = { 'X-Soha-Outpost-Token': '', 'X-Soha-Session-Token': '' }
  switch (target) {
    case 'nginx-ingress':
      return (
        stringify({
          apiVersion: 'v1',
          kind: 'ConfigMap',
          metadata: { name: 'soha-outpost-auth', namespace: 'default' },
          data: {
            'X-Soha-Outpost-Token': context.requiresOutpostToken ? tokenPlaceholder : '',
            'X-Soha-Session-Token': '',
          },
        }) +
        '---\n' +
        stringify({
          apiVersion: 'networking.k8s.io/v1',
          kind: 'Ingress',
          metadata: {
            name: 'protected-application',
            namespace: 'default',
            annotations: {
              'nginx.ingress.kubernetes.io/auth-url': authURL(context, true),
              'nginx.ingress.kubernetes.io/auth-signin': `${context.loginURL}&return_to=$scheme://$host$escaped_request_uri`,
              // Empty auth response values also remove client-supplied internal tokens.
              'nginx.ingress.kubernetes.io/auth-response-headers': [
                ...identityHeaders,
                'X-Soha-Outpost-Token',
                'X-Soha-Session-Token',
              ].join(','),
              'nginx.ingress.kubernetes.io/auth-proxy-set-headers': 'default/soha-outpost-auth',
            },
          },
          spec: {
            rules: [
              {
                host,
                http: {
                  paths: [
                    {
                      path: '/',
                      pathType: 'Prefix',
                      backend: { service: { name: 'protected-application', port: { number: 80 } } },
                    },
                  ],
                },
              },
            ],
          },
        })
      )
    case 'nginx-proxy-manager':
      return `# Paste this entire block into Proxy Host > Advanced. Do not add another / in Custom locations.
# Keep the Proxy Host TLS settings; set the actual upstream below.
${nginxLocations(context)}`
    case 'nginx-standalone':
      return `# Add your deployment's TLS listener/certificate settings.
server {
  listen 80;
  server_name ${host};
${nginxLocations(context)}
}`
    case 'traefik-ingress':
      return (
        [
          {
            name: 'soha-auth-input',
            spec: { headers: { customRequestHeaders: traefikHeaders(context) } },
          },
          { name: 'soha-forward-auth', spec: { forwardAuth } },
          { name: 'soha-auth-cleanup', spec: { headers: { customRequestHeaders: clearToken } } },
        ]
          .map(({ name, spec }) =>
            stringify({
              apiVersion: 'traefik.io/v1alpha1',
              kind: 'Middleware',
              metadata: { name, namespace: 'default' },
              spec,
            }),
          )
          .join('---\n') +
        '# Protected Ingress annotation:\n# traefik.ingress.kubernetes.io/router.middlewares: default-soha-auth-input@kubernetescrd,default-soha-forward-auth@kubernetescrd,default-soha-auth-cleanup@kubernetescrd'
      )
    case 'traefik-compose': {
      const labels = {
        'traefik.enable': 'true',
        'traefik.http.routers.protected.rule': `Host(\`${host}\`)`,
        'traefik.http.routers.protected.middlewares': 'soha-auth-input,soha-auth,soha-auth-cleanup',
        ...Object.fromEntries(
          Object.entries(traefikHeaders(context)).map(([key, value]) => [
            `traefik.http.middlewares.soha-auth-input.headers.customrequestheaders.${key}`,
            value,
          ]),
        ),
        ...Object.fromEntries(
          Object.entries(forwardAuth).map(([key, value]) => [
            `traefik.http.middlewares.soha-auth.forwardauth.${key}`,
            Array.isArray(value) ? value.join(',') : String(value),
          ]),
        ),
        ...Object.fromEntries(
          Object.entries(clearToken).map(([key, value]) => [
            `traefik.http.middlewares.soha-auth-cleanup.headers.customrequestheaders.${key}`,
            value,
          ]),
        ),
      }
      return (
        '# Merge into the protected service; configure its image, port and network.\n' +
        stringify({ services: { 'protected-application': { labels } } })
      )
    }
    case 'traefik-standalone':
      return stringify({
        http: {
          middlewares: {
            'soha-auth-input': { headers: { customRequestHeaders: traefikHeaders(context) } },
            'soha-auth': { forwardAuth },
            'soha-auth-cleanup': { headers: { customRequestHeaders: clearToken } },
          },
          routers: {
            protected: {
              rule: `Host(\`${host}\`)`,
              middlewares: ['soha-auth-input', 'soha-auth', 'soha-auth-cleanup'],
              service: 'protected',
            },
          },
          services: { protected: { loadBalancer: { servers: [{ url: upstreamURL }] } } },
        },
      })
    case 'caddy-standalone': {
      const url = new URL(authURL(context, false))
      return `${host} {
  route {
${[...identityHeaders, 'X-Soha-Outpost-Token', 'X-Soha-Session-Token'].map((header) => `    request_header -${header}`).join('\n')}
    forward_auth ${url.origin} {
      uri ${url.pathname}${url.search}
      header_up X-Forwarded-Host {http.request.hostport}
      header_up X-Forwarded-Uri {http.request.uri}
      header_up X-Original-URL {http.request.scheme}://{http.request.hostport}{http.request.uri}
      header_up X-Forwarded-Method {http.request.method}
      header_up Cookie {http.request.header.Cookie}${context.requiresOutpostToken ? `\n      header_up X-Soha-Outpost-Token ${tokenPlaceholder}` : ''}
      copy_headers ${identityHeaders.join(' ')}
    }
    reverse_proxy ${upstreamURL}
  }
}`
    }
  }
}
