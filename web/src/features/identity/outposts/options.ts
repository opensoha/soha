import type { IdentityOutpostMode, IdentityOutpostStatus } from './types'

export const identityOutpostModeOptions: Array<{
  label: string
  value: IdentityOutpostMode
}> = [
  { label: 'Embedded', value: 'embedded' },
  { label: 'Agent', value: 'agent' },
  { label: 'Kubernetes', value: 'kubernetes' },
  { label: 'External', value: 'external' },
]

export const identityOutpostStatusOptions: Array<{
  label: string
  value: IdentityOutpostStatus
}> = [
  { label: 'Online', value: 'online' },
  { label: 'Offline', value: 'offline' },
  { label: 'Degraded', value: 'degraded' },
]
