import { describe, expect, it } from 'vitest'
import { createGitLabIntegration, gitLabFormValues, updateGitLabIntegration } from './model'
import type { SystemIntegration } from './types'

const integration = {
  id: 'gitlab-main',
  category: 'source_control',
  providerType: 'gitlab',
  name: 'Company GitLab',
  description: 'Primary source',
  enabled: true,
  configuration: [
    { key: 'base_url', value: 'https://gitlab.example.com/api/v4' },
    { key: 'group_id', value: '42' },
    { key: 'per_page', value: '50' },
    { key: 'timeout', value: '20s' },
  ],
  credentialKeys: ['token'],
  healthStatus: 'healthy',
  version: 3,
  createdAt: '2026-07-21T00:00:00Z',
  updatedAt: '2026-07-21T00:00:00Z',
} satisfies SystemIntegration

describe('system integration model', () => {
  it('maps redacted integration configuration to the GitLab form', () => {
    expect(gitLabFormValues(integration)).toMatchObject({
      name: 'Company GitLab',
      baseUrl: 'https://gitlab.example.com/api/v4',
      groupId: '42',
      perPage: 50,
      timeout: '20s',
      authMode: 'access_token',
      token: '',
    })
  })

  it('creates GitLab payloads with a write-only token', () => {
    expect(
      createGitLabIntegration({
        name: ' GitLab ',
        enabled: true,
        baseUrl: ' https://gitlab.example.com/api/v4 ',
        groupId: ' 42 ',
        perPage: 100,
        timeout: '15s',
        authMode: 'access_token',
        token: ' secret ',
      }),
    ).toMatchObject({
      category: 'source_control',
      providerType: 'gitlab',
      name: 'GitLab',
      credentials: [{ key: 'token', value: 'secret' }],
    })
  })

  it('preserves an existing token when the edit form leaves it blank', () => {
    const values = gitLabFormValues(integration)
    expect(updateGitLabIntegration(integration, values)).toEqual({
      expectedVersion: 3,
      name: 'Company GitLab',
      description: 'Primary source',
      enabled: true,
      configuration: expect.arrayContaining([
        { key: 'auth_mode', value: 'access_token' },
        { key: 'base_url', value: 'https://gitlab.example.com/api/v4' },
      ]),
      credentials: undefined,
      clearCredentialKeys: ['client_secret', 'access_token', 'refresh_token'],
    })
  })

  it('creates OAuth application payloads without a static token', () => {
    const values = {
      ...gitLabFormValues(),
      authMode: 'oauth' as const,
      clientId: 'app-id',
      clientSecret: 'app-secret',
      oauthRedirectUri: 'https://soha.example/api/v1/system-integrations/oauth/gitlab/callback',
    }

    expect(createGitLabIntegration(values)).toMatchObject({
      configuration: expect.arrayContaining([
        { key: 'auth_mode', value: 'oauth' },
        { key: 'client_id', value: 'app-id' },
        { key: 'oauth_return_uri', value: 'http://localhost' },
      ]),
      credentials: [{ key: 'client_secret', value: 'app-secret' }],
    })
  })

  it('roundtrips Git network policy and keeps blank SSH credentials write-only', () => {
    const configured = {
      ...integration,
      credentialKeys: ['token', 'private_key', 'known_hosts'],
      configuration: [
        ...integration.configuration,
        { key: 'git_allowed_endpoints', value: 'ssh://git.example.com:2222' },
        { key: 'git_allowed_cidrs', value: '10.20.0.0/16' },
        { key: 'git_ca_certificate', value: 'certificate' },
      ],
    }
    const values = gitLabFormValues(configured)
    expect(values).toMatchObject({
      privateKey: '',
      knownHosts: '',
      gitAllowedCidrs: '10.20.0.0/16',
    })
    const update = updateGitLabIntegration(configured, values)
    expect(update.configuration).toEqual(expect.arrayContaining(configured.configuration))
    expect(update.credentials).toBeUndefined()
    expect(update.clearCredentialKeys).not.toContain('private_key')
    expect(
      updateGitLabIntegration(configured, {
        ...values,
        privateKey: ' new-key ',
        knownHosts: ' verified-host ',
      }).credentials,
    ).toEqual([
      { key: 'private_key', value: 'new-key' },
      { key: 'known_hosts', value: 'verified-host' },
    ])
  })
})
