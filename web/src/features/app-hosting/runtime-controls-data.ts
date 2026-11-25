import { api } from '@/services/api-client'
import type {
  DecideHostedAppVisitorAttributesRequest,
  HostedAppRevisionLogsResponse,
  HostedAppRuntimeConfigResponse,
  HostedAppRuntimeUsageResponse,
  HostedAppVisitorAttributesResponse,
  RequestHostedAppVisitorAttributesRequest,
  UpdateHostedAppRuntimeConfigRequest,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { mutationOptions, queryOptions, type QueryClient } from '@tanstack/react-query'
import { appHostingKeys } from './keys'

const appPath = (appId: string) => `/app-hosting/apps/${encodeURIComponent(appId.trim())}`
const configPath = (appId: string) => `${appPath(appId)}/runtime-config`
const attributesPath = (appId: string) => `${appPath(appId)}/visitor-attributes`

const scopeKey = (appId: string) =>
  [...appHostingKeys.appDetail(appId), 'runtime-controls'] as const
const configKeys = (appId: string) => [...scopeKey(appId), 'config'] as const
const configKey = (appId: string, ownerId: string) => [...configKeys(appId), ownerId] as const
const usageKey = (appId: string) => [...scopeKey(appId), 'usage'] as const
const attributesKey = (appId: string) => [...scopeKey(appId), 'visitor-attributes'] as const

export const hostedRuntimeQueries = {
  config: (appId: string, ownerId: string) =>
    queryOptions({
      queryKey: configKey(appId, ownerId),
      queryFn: async ({ signal }) =>
        (await api.get<HostedAppRuntimeConfigResponse>(configPath(appId), { signal })).data,
    }),
  usage: (appId: string) =>
    queryOptions({
      queryKey: usageKey(appId),
      queryFn: async ({ signal }) =>
        (
          await api.get<HostedAppRuntimeUsageResponse>(`${appPath(appId)}/runtime-usage`, {
            signal,
          })
        ).data,
    }),
  attributes: (appId: string) =>
    queryOptions({
      queryKey: attributesKey(appId),
      queryFn: async ({ signal }) =>
        (await api.get<HostedAppVisitorAttributesResponse>(attributesPath(appId), { signal })).data,
    }),
  logs: (appId: string, revisionId: string) =>
    queryOptions({
      queryKey: [...scopeKey(appId), 'revision-logs', revisionId.trim()] as const,
      queryFn: async ({ signal }) =>
        (
          await api.get<HostedAppRevisionLogsResponse>(
            `${appPath(appId)}/revisions/${encodeURIComponent(revisionId.trim())}/logs`,
            { signal },
          )
        ).data,
    }),
}

export const hostedRuntimeMutations = {
  updateConfig: (client: QueryClient, appId: string) =>
    mutationOptions({
      mutationFn: async (input: Omit<UpdateHostedAppRuntimeConfigRequest, 'setEnvironment'>) => {
        await api.put<HostedAppRuntimeConfigResponse>(configPath(appId), input)
      },
      onSuccess: (_, input) =>
        Promise.all([
          client.invalidateQueries({ queryKey: configKeys(appId) }),
          ...(input.dataWarningBytes === undefined
            ? []
            : [client.invalidateQueries({ queryKey: usageKey(appId), exact: true })]),
        ]).then(() => undefined),
    }),
  writeEnvironment: (
    client: QueryClient,
    appId: string,
    ownerId: string,
    takeInput: () => NonNullable<UpdateHostedAppRuntimeConfigRequest['setEnvironment']>[number],
  ) =>
    mutationOptions({
      retry: false,
      networkMode: 'always',
      mutationFn: async () => {
        await api.scoped<HostedAppRuntimeConfigResponse>(configPath(appId), ownerId, {
          method: 'PUT',
          body: { setEnvironment: [takeInput()] } satisfies UpdateHostedAppRuntimeConfigRequest,
        })
      },
      onSuccess: () => {
        void client.invalidateQueries({ queryKey: configKey(appId, ownerId), exact: true })
      },
    }),
  requestAttributes: (client: QueryClient, appId: string) =>
    mutationOptions({
      mutationFn: async (input: RequestHostedAppVisitorAttributesRequest) =>
        (await api.post<HostedAppVisitorAttributesResponse>(attributesPath(appId), input)).data,
      onSuccess: async (data) => {
        const queryKey = attributesKey(appId)
        await client.cancelQueries({ queryKey, exact: true })
        client.setQueryData(queryKey, data)
      },
    }),
  decideAttributes: (client: QueryClient, appId: string) =>
    mutationOptions({
      mutationFn: async (input: DecideHostedAppVisitorAttributesRequest) =>
        (await api.put<HostedAppVisitorAttributesResponse>(attributesPath(appId), input)).data,
      onSuccess: async (data) => {
        const queryKey = attributesKey(appId)
        await client.cancelQueries({ queryKey, exact: true })
        client.setQueryData(queryKey, data)
      },
    }),
}
