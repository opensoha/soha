import { queryOptions } from '@tanstack/react-query'
import type {
  DLPShadowInput,
  DLPShadowEnvelope,
  ManagedContentNetworkEnvelope,
  ManagedContentAdminDestinationsEnvelope,
  ManagedContentDestinationsEnvelope,
  ManagedContentDestinationEnvelope,
  ManagedContentDestinationConfig,
  ManagedContentRecordEnvelope,
  ManagedContentRecordPageEnvelope,
  ManagedContentResultEnvelope,
  ManagedContentAuthorization,
  ManagedContentAuthorizationEnvelope,
  ManagedContentUploadInput,
  ManagedContentPasteInput,
  ManagedContentReviewInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'
import { managedContentKeys } from './keys'

export const base = '/network-access/content-transfers'
const id = (value: string) => encodeURIComponent(value)
export const destinationQuery = (viewer: string, scope: string, enabled: boolean) =>
  queryOptions({
    queryKey: managedContentKeys.destinations(viewer, scope, false),
    queryFn: async () =>
      (await api.scoped<ManagedContentDestinationsEnvelope>(`${base}/destinations`, viewer)).data,
    enabled: enabled && Boolean(viewer),
    gcTime: 0,
    retry: false,
  })
export const adminDestinationQuery = (viewer: string, scope: string, enabled: boolean) =>
  queryOptions({
    queryKey: managedContentKeys.destinations(viewer, scope, true),
    queryFn: async () =>
      (
        await api.scoped<ManagedContentAdminDestinationsEnvelope>(
          `${base}/destinations/admin`,
          viewer,
        )
      ).data,
    enabled: enabled && Boolean(viewer),
    gcTime: 0,
    retry: false,
  })
export const recordQuery = (viewer: string, scope: string, recordID: string, enabled: boolean) =>
  queryOptions({
    queryKey: managedContentKeys.detail(viewer, scope, recordID),
    queryFn: async () =>
      (await api.scoped<ManagedContentRecordEnvelope>(`${base}/records/${id(recordID)}`, viewer))
        .data,
    enabled: enabled && Boolean(viewer && recordID),
    gcTime: 0,
    retry: false,
  })
export const recordsQuery = (
  viewer: string,
  scope: string,
  kind: 'files' | 'records' | 'cases',
  destination: string,
  offset: number,
  limit: number,
  enabled: boolean,
) =>
  queryOptions({
    queryKey: managedContentKeys.records(viewer, scope, kind, destination, offset, limit),
    queryFn: async () =>
      (
        await api.scoped<ManagedContentRecordPageEnvelope>(
          kind === 'cases'
            ? `${base}/destinations/${id(destination)}/cases?offset=${offset}&limit=${limit}`
            : `${base}/${kind}?offset=${offset}&limit=${limit}`,
          viewer,
        )
      ).data,
    enabled: enabled && Boolean(viewer) && (kind !== 'cases' || Boolean(destination)),
    gcTime: 0,
    retry: false,
  })
export async function saveDestination(
  viewer: string,
  destinationID: string,
  expectedRevision: number,
  draft: ManagedContentDestinationConfig,
) {
  return (
    await api.scoped<ManagedContentDestinationEnvelope>(
      `${base}/destinations/${id(destinationID)}`,
      viewer,
      { method: 'PUT', body: { expectedRevision, draft } },
    )
  ).data
}
export async function publishDestination(
  viewer: string,
  destinationID: string,
  expectedRevision: number,
) {
  return (
    await api.scoped<ManagedContentDestinationEnvelope>(
      `${base}/destinations/${id(destinationID)}/publish`,
      viewer,
      { method: 'POST', body: { expectedRevision } },
    )
  ).data
}
export async function upload(viewer: string, input: ManagedContentUploadInput) {
  return (
    await api.scoped<ManagedContentResultEnvelope>(`${base}/uploads`, viewer, {
      method: 'POST',
      body: input,
    })
  ).data
}
export async function paste(viewer: string, input: ManagedContentPasteInput) {
  return (
    await api.scoped<ManagedContentResultEnvelope>(`${base}/pastes`, viewer, {
      method: 'POST',
      body: input,
    })
  ).data
}
export async function fileAction(
  viewer: string,
  fileID: string,
  action: 'read' | 'copy' | 'delete',
  requestId: string,
  destinationId?: string,
  authorization?: ManagedContentAuthorization,
) {
  return (
    await api.scoped<ManagedContentResultEnvelope>(
      `${base}/files/${id(fileID)}/${action}`,
      viewer,
      {
        method: 'POST',
        body: {
          requestId,
          ...(destinationId ? { destinationId } : {}),
          ...(authorization ? { authorization } : {}),
        },
      },
    )
  ).data
}
export async function authorize(
  viewer: string,
  recordID: string,
  action: 'acknowledge' | 'use_approval',
) {
  return (
    await api.scoped<ManagedContentAuthorizationEnvelope>(
      `${base}/records/${id(recordID)}/authorize`,
      viewer,
      { method: 'POST', body: { action } },
    )
  ).data
}
export async function appeal(
  viewer: string,
  recordID: string,
  reasonCode: 'false_positive' | 'business_exception',
) {
  return (
    await api.scoped<ManagedContentRecordEnvelope>(
      `${base}/records/${id(recordID)}/appeal`,
      viewer,
      { method: 'POST', body: { reasonCode } },
    )
  ).data
}
export async function review(viewer: string, recordID: string, input: ManagedContentReviewInput) {
  return (
    await api.scoped<ManagedContentRecordEnvelope>(
      `${base}/records/${id(recordID)}/review`,
      viewer,
      { method: 'POST', body: input },
    )
  ).data
}
export function encodeContent(bytes: Uint8Array) {
  let raw = ''
  for (let offset = 0; offset < bytes.length; offset += 8192)
    raw += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
  return btoa(raw)
}
export function saveDownload(content: string, fileName: string) {
  const bytes = Uint8Array.from(atob(content), (value) => value.charCodeAt(0))
  const url = URL.createObjectURL(new Blob([bytes], { type: 'application/octet-stream' }))
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
  bytes.fill(0)
}

export async function previewDestination(
  viewer: string,
  destinationID: string,
  input: DLPShadowInput,
) {
  return (
    await api.scoped<DLPShadowEnvelope>(
      `${base}/destinations/${id(destinationID)}/preview`,
      viewer,
      { method: 'POST', body: input },
    )
  ).data
}
export const networkStateQuery = (
  viewer: string,
  scope: string,
  recordID: string,
  enabled: boolean,
) =>
  queryOptions({
    queryKey: managedContentKeys.network(viewer, scope, recordID),
    queryFn: async () =>
      (
        await api.scoped<ManagedContentNetworkEnvelope>(
          `${base}/records/${id(recordID)}/network`,
          viewer,
        )
      ).data,
    enabled: enabled && Boolean(viewer && recordID),
    gcTime: 0,
    retry: false,
  })

export const reviewDestinationQuery = (viewer: string, scope: string, enabled: boolean) =>
  queryOptions({
    queryKey: managedContentKeys.reviewDestinations(viewer, scope),
    queryFn: async () =>
      (await api.scoped<ManagedContentDestinationsEnvelope>(`${base}/destinations/review`, viewer))
        .data,
    enabled: enabled && Boolean(viewer),
    gcTime: 0,
    retry: false,
  })
