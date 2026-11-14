import { stringify } from 'yaml'
import type { IdentityProvider } from '../providers'
import type { IdentityOutpost } from './types'

export function providersForOutpost(providers: IdentityProvider[], outpostId: string) {
  return providers.filter(
    (provider) =>
      provider.type === 'proxy' &&
      String(provider.config?.outpostId ?? provider.config?.outpost_id ?? '') === outpostId,
  )
}

export function outpostDeploymentConfig(outpost: IdentityOutpost): string | undefined {
  const deployment = outpost.deployment
  if (
    outpost.mode === 'embedded' ||
    !deployment?.controlPlaneUrl ||
    !deployment.trustKeyId ||
    !deployment.trustPublicKey
  )
    return undefined
  return stringify({
    app: { name: 'soha-outpost', env: 'production' },
    http: { addr: ':18080', base_path: '/api/v1', read_timeout: '15s', write_timeout: '15s' },
    auth: { bearer_token_file: '/var/run/secrets/soha/agent-token' },
    control_plane: {
      enabled: true,
      base_url: deployment.controlPlaneUrl,
      bearer_token_file: '/var/run/secrets/soha/control-plane-token',
      outpost: {
        enabled: true,
        agent_id: outpost.id,
        protocol_version: deployment.protocolVersion,
        trust_key_id: deployment.trustKeyId,
        trust_public_key: deployment.trustPublicKey,
        poll_interval: '5s',
        heartbeat_interval: '15s',
      },
    },
    kubernetes: { enabled: false },
  })
}
