import { api } from '@/services/api-client'
import type {
  NetworkProxySource,
  NetworkProxySourceInput,
  NetworkProxySourceList,
  NetworkProxyConversionPreview,
  NetworkProxyBundle,
  NetworkProxyBundleList,
  NetworkProxyBundleInput,
  NetworkProxyBundleActivateInput,
  NetworkProxyRules,
  NetworkProxyRulesInput,
  NetworkProxyRuntimeCommandInput,
  NetworkProxyRuntimeCommandReceipt,
  NetworkProxyDeployment,
  NetworkProxyDeploymentInput,
  NetworkProxyDeploymentControlInput,
  NetworkProxyDeploymentOperation,
} from '@opensoha/contracts/gen/ts/sohaapi'
const path = (id: string) => `/network-access/proxy-instances/${encodeURIComponent(id)}`
const unwrap = async <T>(promise: Promise<{ data: T }>) => (await promise).data
export const workspaceApi = {
  sources: (id: string) => unwrap(api.get<{ data: NetworkProxySourceList }>(`${path(id)}/sources`)),
  saveSource: (id: string, input: NetworkProxySourceInput, sourceId?: string) =>
    sourceId
      ? unwrap(
          api.put<{ data: NetworkProxySource }>(
            `${path(id)}/sources/${encodeURIComponent(sourceId)}`,
            input,
          ),
        )
      : unwrap(api.post<{ data: NetworkProxySource }>(`${path(id)}/sources`, input)),
  preview: (id: string, input: NetworkProxySourceInput) =>
    unwrap(api.post<{ data: NetworkProxyConversionPreview }>(`${path(id)}/sources/preview`, input)),
  deleteSource: (id: string, source: NetworkProxySource, requestId: string) =>
    unwrap(
      api.delete<{ data: NetworkProxySource }>(
        `${path(id)}/sources/${encodeURIComponent(source.sourceId)}?expectedRevision=${source.revision}&requestId=${encodeURIComponent(requestId)}`,
      ),
    ),
  bundles: (id: string) => unwrap(api.get<{ data: NetworkProxyBundleList }>(`${path(id)}/bundles`)),
  prepare: (id: string, input: NetworkProxyBundleInput) =>
    unwrap(api.post<{ data: NetworkProxyBundle }>(`${path(id)}/bundles`, input)),
  activate: (id: string, bundleId: string, input: NetworkProxyBundleActivateInput) =>
    unwrap(
      api.post<{ data: NetworkProxyBundle }>(
        `${path(id)}/bundles/${encodeURIComponent(bundleId)}/activate`,
        input,
      ),
    ),
  rules: (id: string, personal = false) =>
    unwrap(
      api.get<{ data: NetworkProxyRules }>(`${path(id)}/${personal ? 'personal-rules' : 'rules'}`),
    ),
  saveRules: (id: string, input: NetworkProxyRulesInput, personal: boolean) =>
    unwrap(
      api.put<{ data: NetworkProxyRules }>(
        `${path(id)}/${personal ? 'personal-rules' : 'rules'}`,
        input,
      ),
    ),
  queue: (id: string, input: NetworkProxyRuntimeCommandInput) =>
    unwrap(api.post<{ data: NetworkProxyRuntimeCommandReceipt }>(`${path(id)}/commands`, input)),
  command: (id: string, commandId: string) =>
    unwrap(
      api.get<{ data: NetworkProxyRuntimeCommandReceipt }>(
        `${path(id)}/commands/${encodeURIComponent(commandId)}`,
      ),
    ),
  cancel: (id: string, commandId: string) =>
    api.post<void>(`${path(id)}/commands/${encodeURIComponent(commandId)}/cancel`),
  deployment: (id: string) =>
    unwrap(api.get<{ data: NetworkProxyDeployment }>(`${path(id)}/deployment`)),
  bind: (id: string, input: NetworkProxyDeploymentInput) =>
    unwrap(api.put<{ data: NetworkProxyDeployment }>(`${path(id)}/deployment`, input)),
  control: (id: string, input: NetworkProxyDeploymentControlInput) =>
    unwrap(
      api.post<{ data: NetworkProxyDeploymentOperation }>(`${path(id)}/deployment/control`, input),
    ),
}
