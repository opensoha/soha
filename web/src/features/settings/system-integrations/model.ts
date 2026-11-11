import type {
  SystemIntegration,
  SystemIntegrationCreateRequest,
  SystemIntegrationUpdateRequest,
} from './types'
import { API_BASE_URL } from '@/features/auth'

export type GitLabAuthMode = 'access_token' | 'oauth'

export interface GitLabFormValues {
  name: string
  description?: string
  enabled: boolean
  baseUrl: string
  groupId?: string
  perPage: number
  timeout: string
  authMode: GitLabAuthMode
  token?: string
  clientId?: string
  clientSecret?: string
  oauthRedirectUri?: string
  gitAllowedEndpoints?: string
  gitAllowedCidrs?: string
  gitCaCertificate?: string
  privateKey?: string
  knownHosts?: string
}

function configurationValue(item: SystemIntegration, key: string) {
  return item.configuration.find((field) => field.key === key)?.value ?? ''
}

export function gitLabFormValues(item?: SystemIntegration): GitLabFormValues {
  const authMode = item ? configurationValue(item, 'auth_mode') : 'access_token'
  return {
    name: item?.name ?? 'GitLab',
    description: item?.description ?? '',
    enabled: item?.enabled ?? true,
    baseUrl: item ? configurationValue(item, 'base_url') : 'https://gitlab.com/api/v4',
    groupId: item ? configurationValue(item, 'group_id') : '',
    perPage: Number(item ? configurationValue(item, 'per_page') : '100') || 100,
    timeout: item ? configurationValue(item, 'timeout') || '15s' : '15s',
    authMode: authMode === 'oauth' ? 'oauth' : 'access_token',
    token: '',
    privateKey: '',
    knownHosts: '',
    gitAllowedEndpoints: item ? configurationValue(item, 'git_allowed_endpoints') : '',
    gitAllowedCidrs: item ? configurationValue(item, 'git_allowed_cidrs') : '',
    gitCaCertificate: item ? configurationValue(item, 'git_ca_certificate') : '',
    clientId: item ? configurationValue(item, 'client_id') : '',
    clientSecret: '',
    oauthRedirectUri: item
      ? configurationValue(item, 'oauth_redirect_uri')
      : gitLabOAuthCallbackURL(),
  }
}

export function gitLabOAuthCallbackURL(origin?: string) {
  const baseOrigin = origin ?? globalThis.location?.origin ?? 'http://localhost'
  return new URL(`${API_BASE_URL}/system-integrations/oauth/gitlab/callback`, baseOrigin).toString()
}

export function gitLabOAuthReturnURL(origin?: string) {
  return origin ?? globalThis.location?.origin ?? 'http://localhost'
}

function gitLabConfiguration(values: GitLabFormValues) {
  return [
    { key: 'base_url', value: values.baseUrl.trim() },
    { key: 'group_id', value: values.groupId?.trim() ?? '' },
    { key: 'per_page', value: String(values.perPage) },
    { key: 'timeout', value: values.timeout.trim() },
    { key: 'auth_mode', value: values.authMode },
    { key: 'client_id', value: values.clientId?.trim() ?? '' },
    { key: 'oauth_redirect_uri', value: values.oauthRedirectUri?.trim() ?? '' },
    { key: 'oauth_return_uri', value: gitLabOAuthReturnURL() },
    { key: 'git_allowed_endpoints', value: values.gitAllowedEndpoints?.trim() ?? '' },
    { key: 'git_allowed_cidrs', value: values.gitAllowedCidrs?.trim() ?? '' },
    { key: 'git_ca_certificate', value: values.gitCaCertificate?.trim() ?? '' },
  ]
}

function gitLabCredentials(values: GitLabFormValues) {
  const entries = [
    {
      key: values.authMode === 'oauth' ? 'client_secret' : 'token',
      value: (values.authMode === 'oauth' ? values.clientSecret : values.token)?.trim() ?? '',
    },
    { key: 'private_key', value: values.privateKey?.trim() ?? '' },
    { key: 'known_hosts', value: values.knownHosts?.trim() ?? '' },
  ].filter((entry) => entry.value)
  return entries.length ? entries : undefined
}

export function createGitLabIntegration(values: GitLabFormValues): SystemIntegrationCreateRequest {
  return {
    category: 'source_control',
    providerType: 'gitlab',
    name: values.name.trim(),
    description: values.description?.trim() || undefined,
    enabled: values.enabled,
    configuration: gitLabConfiguration(values),
    credentials: gitLabCredentials(values),
  }
}

export function updateGitLabIntegration(
  item: SystemIntegration,
  values: GitLabFormValues,
): SystemIntegrationUpdateRequest {
  return {
    expectedVersion: item.version,
    name: values.name.trim(),
    description: values.description?.trim() ?? '',
    enabled: values.enabled,
    configuration: gitLabConfiguration(values),
    credentials: gitLabCredentials(values),
    clearCredentialKeys:
      values.authMode === 'oauth' ? ['token'] : ['client_secret', 'access_token', 'refresh_token'],
  }
}
