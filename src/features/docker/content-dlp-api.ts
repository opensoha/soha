import { queryOptions } from '@tanstack/react-query'
import type {
  DLPShadowInput,
  DLPShadowEnvelope,
  DockerVolumeDLPPolicy,
  DockerVolumeDLPPolicyEnvelope,
  DockerVolumeDLPEventListEnvelope,
  DockerVolumeDLPSaveInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

const path = (id: string) => `/docker/projects/${encodeURIComponent(id)}/content-dlp`
export const contentDLPKeys = {
  project: (id: string) => ['docker', 'content-dlp', id] as const,
  policy: (id: string, viewer: string) =>
    [...contentDLPKeys.project(id), 'policy', viewer] as const,
  events: (id: string, viewer: string) =>
    [...contentDLPKeys.project(id), 'events', viewer] as const,
}
export const contentDLPQueries = {
  policy: (id: string, viewer: string, enabled: boolean) =>
    queryOptions({
      queryKey: contentDLPKeys.policy(id, viewer),
      queryFn: async () => (await api.getEnvelope<DockerVolumeDLPPolicyEnvelope>(path(id))).data,
      enabled: enabled && Boolean(id && viewer),
      gcTime: 0,
    }),
  events: (id: string, viewer: string, enabled: boolean) =>
    queryOptions({
      queryKey: contentDLPKeys.events(id, viewer),
      queryFn: async () =>
        (await api.getEnvelope<DockerVolumeDLPEventListEnvelope>(`${path(id)}/events`)).items,
      enabled: enabled && Boolean(id && viewer),
      gcTime: 0,
    }),
}
export async function saveContentDLP(
  id: string,
  input: DockerVolumeDLPSaveInput,
): Promise<DockerVolumeDLPPolicy> {
  return (await api.put<DockerVolumeDLPPolicyEnvelope>(path(id), input)).data
}
export async function publishContentDLP(
  id: string,
  revision: number,
): Promise<DockerVolumeDLPPolicy> {
  return (
    await api.post<DockerVolumeDLPPolicyEnvelope>(`${path(id)}/publish`, {
      expectedRevision: revision,
    })
  ).data
}

export async function previewContentDLP(id: string, viewer: string, input: DLPShadowInput) {
  return (
    await api.scoped<DLPShadowEnvelope>(`${path(id)}/preview`, viewer, {
      method: 'POST',
      body: input,
    })
  ).data
}
