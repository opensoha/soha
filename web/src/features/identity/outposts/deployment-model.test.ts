import { describe, expect, it } from 'vitest'
import { parse } from 'yaml'
import { outpostDeploymentConfig, providersForOutpost } from './deployment-model'
import type { IdentityOutpost } from './types'
import type { IdentityProvider } from '../providers'

const outpost: IdentityOutpost = {
  id: 'edge-1',
  name: 'Office',
  mode: 'agent',
  status: 'offline',
  configurationVersion: 0,
  createdAt: '2026-09-13T00:00:00Z',
  updatedAt: '2026-09-13T00:00:00Z',
  runtimeStatus: 'unavailable',
  token: 'must-not-be-in-config',
  deployment: {
    controlPlaneUrl: 'https://control.example/api/v1',
    protocolVersion: 'v1',
    trustKeyId: 'key-1',
    trustPublicKey: 'public-key',
  },
}

describe('Outpost deployment instructions', () => {
  it('generates readable Agent YAML with separate credential files and refuses incomplete trust material', () => {
    const yaml = outpostDeploymentConfig(outpost)!
    const config = parse(yaml)
    expect(yaml).not.toContain(outpost.token)
    expect(config.auth.bearer_token_file).toBe('/var/run/secrets/soha/agent-token')
    expect(config.control_plane.bearer_token_file).toBe('/var/run/secrets/soha/control-plane-token')
    expect(config.control_plane.outpost).toMatchObject({
      enabled: true,
      agent_id: 'edge-1',
      trust_key_id: 'key-1',
      trust_public_key: 'public-key',
    })
    expect(outpostDeploymentConfig({ ...outpost, deployment: undefined })).toBeUndefined()
    expect(outpostDeploymentConfig({ ...outpost, mode: 'embedded' })).toBeUndefined()
  })

  it('derives associations only from explicit proxy bindings, including the legacy config key', () => {
    const base = {
      createdAt: '2026-09-13T00:00:00Z',
      updatedAt: '2026-09-13T00:00:00Z',
      applicationId: 'app',
      enabled: true,
      name: 'Proxy',
      status: 'enabled',
      type: 'proxy',
    } as const
    const providers: IdentityProvider[] = [
      { ...base, id: 'current', config: { outpostId: 'edge-1' } },
      { ...base, id: 'legacy', config: { outpost_id: 'edge-1' } },
      { ...base, id: 'other', config: { outpostId: 'edge-2' } },
      { ...base, id: 'embedded', config: {} },
      { ...base, id: 'oidc', type: 'oidc', config: { outpostId: 'edge-1' } },
    ]
    expect(providersForOutpost(providers, 'edge-1').map((item) => item.id)).toEqual([
      'current',
      'legacy',
    ])
  })
})
