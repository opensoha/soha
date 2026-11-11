import { queryOptions } from '@tanstack/react-query'
import type {
  EndpointChannelViewEnvelope,
  EndpointChannelPolicyEnvelope,
  EndpointChannelSaveInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import { endpointControlKeys } from './keys'

const path = (id: string) =>
  `/network-access/endpoint-devices/${encodeURIComponent(id)}/channel-controls`
export const endpointControlQuery = (id: string, viewer: string, scope: string, enabled: boolean) =>
  queryOptions({
    queryKey: endpointControlKeys.detail(id, viewer, scope),
    queryFn: async () => (await api.getEnvelope<EndpointChannelViewEnvelope>(path(id))).data,
    enabled: enabled && Boolean(id && viewer),
    gcTime: 0,
    refetchInterval: 20000,
  })
export async function saveEndpointControls(id: string, input: EndpointChannelSaveInput) {
  return (await api.put<EndpointChannelPolicyEnvelope>(path(id), input)).data
}
export async function publishEndpointControls(id: string, expectedRevision: number) {
  return (
    await api.post<EndpointChannelPolicyEnvelope>(`${path(id)}/publish`, { expectedRevision })
  ).data
}
