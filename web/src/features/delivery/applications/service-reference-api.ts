import { queryOptions } from '@tanstack/react-query'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  EnvironmentServiceReference,
  EnvironmentServiceReferenceCandidate,
  EnvironmentServiceReferenceCandidateDeleteInput,
  EnvironmentServiceReferenceCandidateInput,
  EnvironmentServiceReferenceCandidatePage,
  EnvironmentServiceReferenceCandidateSummary,
  EnvironmentServiceReferenceDeleteInput,
  EnvironmentServiceReferenceInput,
  EnvironmentServiceReferencePage,
  EnvironmentServiceReferencePreview,
  EnvironmentServiceReferencePreviewInput,
  EnvironmentServiceReferenceSummary,
} from '@opensoha/contracts/gen/ts/sohaapi'
import type { ApplicationEnvironment } from '../types'

const segment = (value: string) => encodeURIComponent(value)

function withQuery(path: string, params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') search.set(key, String(value))
  })
  const query = search.toString()
  return query ? `${path}?${query}` : path
}

async function unwrap<T>(request: Promise<ApiResponse<T>>): Promise<T> {
  const response = await request
  return response.data
}

async function discard(request: Promise<unknown>): Promise<void> {
  await request
}

function candidatePath(bindingId: string, serviceId?: string) {
  const base = `/application-environments/${segment(bindingId)}/service-reference-candidates`
  return serviceId === undefined ? base : `${base}/${segment(serviceId)}`
}

function activePath(bindingId: string, serviceId?: string) {
  const base = `/application-environments/${segment(bindingId)}/service-references`
  return serviceId === undefined ? base : `${base}/${segment(serviceId)}`
}

function previewPath(bindingId: string) {
  return `/application-environments/${segment(bindingId)}/service-reference-previews`
}

async function findByServiceId<T extends { serviceId: string }>(
  serviceId: string,
  buildPath: (cursor?: string) => string,
  load: (path: string) => Promise<{ items: T[]; nextCursor?: string }>,
): Promise<T | null> {
  let cursor: string | undefined
  const seenCursors = new Set<string>()
  while (true) {
    const page = await load(buildPath(cursor))
    const match = page.items.find((item) => item.serviceId === serviceId)
    if (match) return match
    const nextCursor = page.nextCursor
    if (!nextCursor) return null
    if (seenCursors.has(nextCursor)) {
      throw new Error('Environment service reference list repeated its pagination cursor.')
    }
    seenCursors.add(nextCursor)
    cursor = nextCursor
  }
}

async function listActive(bindingId: string) {
  const items: EnvironmentServiceReferenceSummary[] = []
  const seen = new Set<string>()
  let cursor: string | undefined
  do {
    const page = await api.getEnvelope<EnvironmentServiceReferencePage>(
      withQuery(activePath(bindingId), { limit: 100, cursor }),
    )
    items.push(...page.items)
    cursor = page.nextCursor
    if (cursor && seen.has(cursor)) throw new Error('Repeated service reference cursor')
    if (cursor) seen.add(cursor)
  } while (cursor)
  return items
}

export const environmentReferenceKey = (bindingId: string) =>
  ['delivery', 'environment-reference-summaries', bindingId] as const

export const environmentReferenceQuery = (bindingId: string, enabled: boolean) =>
  queryOptions({
    queryKey: environmentReferenceKey(bindingId),
    queryFn: () => listActive(bindingId),
    enabled: Boolean(bindingId && enabled),
    gcTime: 0,
    retry: false,
  })

export const serviceReferenceApi = {
  bindings: (applicationId: string) =>
    unwrap(
      api.get<ApiResponse<ApplicationEnvironment[]>>(
        withQuery('/application-environments', { applicationId }),
      ),
    ),
  findCandidate: (bindingId: string, serviceId: string) =>
    findByServiceId<EnvironmentServiceReferenceCandidateSummary>(
      serviceId,
      (cursor) => withQuery(candidatePath(bindingId), { limit: 100, cursor }),
      (path) => api.getEnvelope<EnvironmentServiceReferenceCandidatePage>(path),
    ),
  findActive: (bindingId: string, serviceId: string) =>
    findByServiceId<EnvironmentServiceReferenceSummary>(
      serviceId,
      (cursor) => withQuery(activePath(bindingId), { limit: 100, cursor }),
      (path) => api.getEnvelope<EnvironmentServiceReferencePage>(path),
    ),
  candidate: (bindingId: string, serviceId: string) =>
    unwrap(
      api.get<ApiResponse<EnvironmentServiceReferenceCandidate>>(
        candidatePath(bindingId, serviceId),
      ),
    ),
  active: (bindingId: string, serviceId: string) =>
    unwrap(api.get<ApiResponse<EnvironmentServiceReference>>(activePath(bindingId, serviceId))),
  preview: (
    bindingId: string,
    input: EnvironmentServiceReferencePreviewInput,
    signal: AbortSignal,
  ) =>
    unwrap(
      api.postWithSignal<ApiResponse<EnvironmentServiceReferencePreview>>(
        previewPath(bindingId),
        input,
        signal,
      ),
    ),
  saveCandidate: (
    bindingId: string,
    serviceId: string,
    input: EnvironmentServiceReferenceCandidateInput,
  ) =>
    unwrap(
      api.put<ApiResponse<EnvironmentServiceReferenceCandidate>>(
        candidatePath(bindingId, serviceId),
        input,
      ),
    ),
  activate: (bindingId: string, serviceId: string, input: EnvironmentServiceReferenceInput) =>
    unwrap(
      api.put<ApiResponse<EnvironmentServiceReference>>(activePath(bindingId, serviceId), input),
    ),
  clearCandidate: (
    bindingId: string,
    serviceId: string,
    input: EnvironmentServiceReferenceCandidateDeleteInput,
  ) => discard(api.delete(candidatePath(bindingId, serviceId), input)),
  detach: (bindingId: string, serviceId: string, input: EnvironmentServiceReferenceDeleteInput) =>
    discard(api.delete(activePath(bindingId, serviceId), input)),
}
