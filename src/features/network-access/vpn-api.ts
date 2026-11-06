import type {
  ApiResponse,
  NetworkVPNRevocation,
  NetworkVPNRevocationStateEnvelope,
  NetworkVPNProfile,
  NetworkVPNProfileConfig,
  NetworkVPNSelectionPolicy,
  NetworkVPNSelectionPolicyConfig,
  NetworkVPNDashboard,
  NetworkVPNDecision,
  NetworkVPNPreviewInput,
  NetworkVPNProfileRevision,
  NetworkVPNSelectionPolicyRevision,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

export { vpnKeys } from './keys'
export type VPNDocumentKind = 'profiles' | 'selection-policies'
export type VPNDocument = NetworkVPNProfile | NetworkVPNSelectionPolicy
export type VPNRevision = NetworkVPNProfileRevision | NetworkVPNSelectionPolicyRevision
const base = '/network-access/vpn'

export async function vpnDocuments(kind: VPNDocumentKind): Promise<VPNDocument[]> {
  return (await api.get<ApiResponse<VPNDocument[]>>(`${base}/${kind}?limit=200`)).data ?? []
}
export async function saveVPNDocument(
  kind: VPNDocumentKind,
  id: string | undefined,
  expectedRevision: number,
  configuration: NetworkVPNProfileConfig | NetworkVPNSelectionPolicyConfig,
) {
  const body = { expectedRevision, configuration }
  return id
    ? api.put(`${base}/${kind}/${encodeURIComponent(id)}`, body)
    : api.post(`${base}/${kind}`, body)
}
export function publishVPNDocument(
  kind: VPNDocumentKind,
  document: VPNDocument,
  targetRevision?: number,
) {
  return api.post(
    `${base}/${kind}/${encodeURIComponent(document.id)}/${targetRevision == null ? 'publish' : 'rollback'}`,
    { expectedRevision: document.revision, ...(targetRevision == null ? {} : { targetRevision }) },
  )
}
export function deleteVPNDocument(kind: VPNDocumentKind, document: VPNDocument) {
  return api.delete(
    `${base}/${kind}/${encodeURIComponent(document.id)}?expectedRevision=${document.revision}`,
  )
}
export async function vpnRevisions(kind: VPNDocumentKind, id: string): Promise<VPNRevision[]> {
  return (
    (
      await api.get<ApiResponse<VPNRevision[]>>(
        `${base}/${kind}/${encodeURIComponent(id)}/revisions`,
      )
    ).data ?? []
  )
}
export async function vpnDashboard(filters: Record<string, string>): Promise<NetworkVPNDashboard> {
  return (
    await api.get<ApiResponse<NetworkVPNDashboard>>(
      `${base}/dashboard?${new URLSearchParams(filters)}`,
    )
  ).data!
}
export async function vpnDecision(id: string): Promise<NetworkVPNDecision> {
  return (
    await api.get<ApiResponse<NetworkVPNDecision>>(`${base}/decisions/${encodeURIComponent(id)}`)
  ).data!
}
export async function previewVPN(input: NetworkVPNPreviewInput): Promise<NetworkVPNDecision> {
  return (await api.post<ApiResponse<NetworkVPNDecision>>(`${base}/selection-preview`, input)).data!
}

const revocationPath = (sessionId: string) =>
  `/network-access/sessions/${encodeURIComponent(sessionId)}/vpn-revocation`
export async function getVPNRevocation(sessionId: string): Promise<NetworkVPNRevocation | null> {
  return (
    (await api.get<NetworkVPNRevocationStateEnvelope>(revocationPath(sessionId))).data.revocation ??
    null
  )
}
export async function revokeVPNSession(sessionId: string): Promise<NetworkVPNRevocation> {
  return (await api.post<ApiResponse<NetworkVPNRevocation>>(revocationPath(sessionId))).data
}
export async function releaseVPNRevocation(
  sessionId: string,
  expectedEpoch: number,
): Promise<NetworkVPNRevocation> {
  return (
    await api.post<ApiResponse<NetworkVPNRevocation>>(`${revocationPath(sessionId)}/release`, {
      expectedEpoch,
    })
  ).data
}
