import type { IdentityRuntimeCapabilityEnvelope } from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

export async function getIdentityRuntimeCapabilities() {
  return (await api.get<IdentityRuntimeCapabilityEnvelope>('/identity/capabilities')).data
}
