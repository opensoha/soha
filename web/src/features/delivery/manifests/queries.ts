import { queryOptions } from '@tanstack/react-query'
import { manifestApi } from './api'
import { manifestKeys, normalizeManifestFilter } from './keys'
import type { ManifestFilter } from './types'

export const manifestQueries = {
  applicationPackages: (applicationId: string, enabled = true) =>
    queryOptions({
      queryKey: manifestKeys.applicationPackages(applicationId),
      queryFn: () => manifestApi.applicationPackages(applicationId),
      enabled: enabled && Boolean(applicationId),
    }),
  list: (filter: ManifestFilter = {}, enabled = true) => {
    const normalized = normalizeManifestFilter(filter)
    return queryOptions({
      queryKey: manifestKeys.list(normalized),
      queryFn: () => manifestApi.list(normalized),
      enabled,
    })
  },
  detail: (id: string, enabled = true) =>
    queryOptions({
      queryKey: manifestKeys.detail(id),
      queryFn: () => manifestApi.get(id),
      enabled: enabled && Boolean(id.trim()),
    }),
  revisions: (id: string, enabled = true) =>
    queryOptions({
      queryKey: manifestKeys.revisions(id),
      queryFn: () => manifestApi.revisions(id),
      enabled: enabled && Boolean(id.trim()),
    }),
  source: (id: string, enabled = true) =>
    queryOptions({
      queryKey: manifestKeys.source(id),
      queryFn: () => manifestApi.source(id),
      enabled: enabled && Boolean(id.trim()),
    }),
  bindings: (id: string, enabled = true) =>
    queryOptions({
      queryKey: manifestKeys.bindings(id),
      queryFn: () => manifestApi.bindings(id),
      enabled: enabled && Boolean(id.trim()),
    }),
  syncRuns: (id: string, enabled = true) =>
    queryOptions({
      queryKey: manifestKeys.syncRuns(id),
      queryFn: () => manifestApi.syncRuns(id),
      enabled: enabled && Boolean(id.trim()),
      refetchInterval: 5000,
    }),
  deployments: (id: string, enabled = true) =>
    queryOptions({
      queryKey: manifestKeys.deployments(id),
      queryFn: async () => (await manifestApi.deployments(id)).items,
      enabled: enabled && Boolean(id.trim()),
      refetchInterval: 5000,
    }),
  intents: (id: string, enabled = true) =>
    queryOptions({
      queryKey: manifestKeys.intents(id),
      queryFn: () => manifestApi.intents(id),
      enabled: enabled && Boolean(id.trim()),
    }),
}
