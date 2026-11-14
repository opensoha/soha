import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { configurationMutations } from '../shared/mutations'
import type { ConfigurationTarget } from '../shared/types'
import { configMapKind, updateConfigMapData } from './api'
import type { ConfigMapDetail, UpdateConfigMapDataPayload } from './types'

export const configMapMutations = {
  create: (queryClient: QueryClient) => configurationMutations.create(configMapKind, queryClient),
  remove: (queryClient: QueryClient) => configurationMutations.remove(configMapKind, queryClient),
  updateData: (queryClient: QueryClient) =>
    mutationOptions<
      ConfigMapDetail,
      Error,
      { target: ConfigurationTarget; payload: UpdateConfigMapDataPayload }
    >({
      ...configurationMutations.updateData<ConfigMapDetail, UpdateConfigMapDataPayload>(
        configMapKind,
        queryClient,
      ),
      mutationFn: ({ target, payload }) => updateConfigMapData(target, payload),
    }),
  updateYAML: (queryClient: QueryClient) =>
    configurationMutations.updateYAML(configMapKind, queryClient),
}
