import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { workbenchKeys } from '@/features/copilot'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { settingsApi } from './api'
import { settingsKeys, settingsMutationKeys } from './keys'

export function invalidateBrandingSettings(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: settingsKeys.branding.all })
}

export function invalidateIdentitySettings(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: settingsKeys.identity.all }),
    queryClient.invalidateQueries({ queryKey: permissionSnapshotQueryKey }),
  ])
}

export function invalidateAISettings(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: settingsKeys.ai.all })
}

export const settingsMutations = {
  branding: {
    save: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.branding('save'),
        mutationFn: settingsApi.branding.save,
        onSuccess: () => invalidateBrandingSettings(queryClient),
      }),
    upload: () =>
      mutationOptions({
        mutationKey: settingsMutationKeys.branding('upload'),
        mutationFn: settingsApi.branding.upload,
      }),
  },
  identity: {
    save: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.identity('save'),
        mutationFn: settingsApi.identity.save,
        onSuccess: () => invalidateIdentitySettings(queryClient),
      }),
  },
  ai: {
    setRuntimeAssetEnabled: (queryClient: QueryClient, owner: string) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.ai('set-runtime-asset-enabled'),
        mutationFn: settingsApi.ai.setRuntimeAssetEnabled,
        retry: false,
        onSettled: () =>
          queryClient.invalidateQueries({ queryKey: settingsKeys.ai.runtimeAssets(owner) }),
      }),
    probeRuntimeMCP: () =>
      mutationOptions({
        mutationKey: settingsMutationKeys.ai('probe-runtime-mcp'),
        mutationFn: settingsApi.ai.probeRuntimeMCP,
        retry: false,
      }),
    publishRuntimeProfile: (queryClient: QueryClient, owner: string) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.ai('publish-runtime-profile'),
        mutationFn: settingsApi.ai.publishRuntimeProfile,
        retry: false,
        onSettled: async (_data, _error, input) => {
          await Promise.all([
            queryClient.invalidateQueries({ queryKey: settingsKeys.ai.runtimeProfileHistory(owner, input.id) }),
            queryClient.invalidateQueries({ queryKey: workbenchKeys.runtimeProfiles() }),
          ])
        },
      }),
    saveWorkbenchModel: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.ai('save-workbench-model'),
        mutationFn: settingsApi.ai.saveWorkbenchModel,
        onSuccess: () => invalidateAISettings(queryClient),
      }),
    saveSkills: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.ai('save-skills'),
        mutationFn: settingsApi.ai.saveSkills,
        onSuccess: () => invalidateAISettings(queryClient),
      }),
    upsertDataSource: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.ai('upsert-data-source'),
        mutationFn: settingsApi.ai.upsertDataSource,
        onSuccess: () => queryClient.invalidateQueries({ queryKey: settingsKeys.ai.dataSources() }),
      }),
    validateDataSource: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.ai('validate-data-source'),
        mutationFn: settingsApi.ai.validateDataSource,
        onSettled: () => queryClient.invalidateQueries({ queryKey: settingsKeys.ai.dataSources() }),
      }),
    upsertAnalysisProfile: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: settingsMutationKeys.ai('upsert-analysis-profile'),
        mutationFn: settingsApi.ai.upsertAnalysisProfile,
        onSuccess: () =>
          queryClient.invalidateQueries({ queryKey: settingsKeys.ai.analysisProfiles() }),
      }),
  },
}
