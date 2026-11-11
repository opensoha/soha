import { queryOptions } from '@tanstack/react-query'
import type {
  DockerVolumeDLPAuthorizationEnvelope,
  DockerVolumeDLPAppealEnvelope,
  DockerVolumeDLPAppealInput,
  DockerVolumeDLPAppealReviewInput,
  DockerVolumeDLPCaseEnvelope,
  DockerVolumeDLPCaseListEnvelope,
  DockerVolumeFileReadInput,
  DockerVolumeTransferResultEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

const base = (id: string) => `/docker/projects/${encodeURIComponent(id)}/content-dlp`
const eventPath = (id: string, event: string) => `${base(id)}/events/${encodeURIComponent(event)}`
export const dlpWorkflowKeys = {
  project: (id: string) => ['docker', 'content-dlp-response', id] as const,
  activity: (id: string, viewer: string, scope: string) =>
    [...dlpWorkflowKeys.project(id), 'activity', viewer, scope] as const,
  case: (id: string, event: string, viewer: string, scope: string) =>
    [...dlpWorkflowKeys.project(id), 'case', event, viewer, scope] as const,
  file: (id: string, viewer: string, input: DockerVolumeFileReadInput) =>
    [
      'docker',
      'projects',
      id,
      'runtime',
      'dlp-file',
      viewer,
      input.serviceName,
      input.target,
      input.path,
    ] as const,
}
export const dlpWorkflowQueries = {
  activity: (id: string, viewer: string, scope: string, enabled: boolean) =>
    queryOptions({
      queryKey: dlpWorkflowKeys.activity(id, viewer, scope),
      queryFn: async () =>
        (await api.getEnvelope<DockerVolumeDLPCaseListEnvelope>(`${base(id)}/activity`)).items,
      enabled: enabled && Boolean(id && viewer),
      gcTime: 0,
    }),
  case: (id: string, event: string, viewer: string, scope: string, enabled: boolean) =>
    queryOptions({
      queryKey: dlpWorkflowKeys.case(id, event, viewer, scope),
      queryFn: async () =>
        (await api.getEnvelope<DockerVolumeDLPCaseEnvelope>(eventPath(id, event))).data,
      enabled: enabled && Boolean(id && event && viewer),
      gcTime: 0,
    }),
  file: (id: string, viewer: string, input: DockerVolumeFileReadInput, enabled: boolean) =>
    queryOptions({
      queryKey: dlpWorkflowKeys.file(id, viewer, input),
      queryFn: () => readVolumeTransfer(id, input),
      enabled: enabled && Boolean(viewer),
      gcTime: 0,
      staleTime: 0,
      retry: false,
      refetchOnMount: 'always' as const,
    }),
}
export async function readVolumeTransfer(id: string, input: DockerVolumeFileReadInput) {
  const result = (
    await api.post<DockerVolumeTransferResultEnvelope>(
      `/docker/projects/${encodeURIComponent(id)}/runtime/volume-file/read`,
      input,
    )
  ).data
  return {
    file: 'file' in result ? result.file : undefined,
    decision: 'decision' in result ? result.decision : undefined,
  }
}
export async function authorizeDLP(
  id: string,
  event: string,
  action: 'acknowledge' | 'approved_appeal',
) {
  return (
    await api.post<DockerVolumeDLPAuthorizationEnvelope>(`${eventPath(id, event)}/authorizations`, {
      action,
    })
  ).data
}
export async function submitDLPAppeal(
  id: string,
  event: string,
  input: DockerVolumeDLPAppealInput,
) {
  return (await api.post<DockerVolumeDLPAppealEnvelope>(`${eventPath(id, event)}/appeal`, input))
    .data
}
export async function reviewDLPAppeal(
  id: string,
  event: string,
  input: DockerVolumeDLPAppealReviewInput,
) {
  return (
    await api.post<DockerVolumeDLPAppealEnvelope>(`${eventPath(id, event)}/appeal/review`, input)
  ).data
}
