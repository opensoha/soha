import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import { updateConfigurationYAML } from '../shared/api'
import { configurationKeys } from '../shared/keys'
import { configurationMutations, invalidateConfigurationCaches } from '../shared/mutations'
import type {
  ConfigurationTarget,
  UpdateConfigurationDataVariables,
  UpdateConfigurationYAMLVariables,
} from '../shared/types'
import { secretKind, updateSecretData } from './api'
import type { UpdateSecretDataPayload } from './types'

type SecretContentUpdate =
  | UpdateConfigurationDataVariables<UpdateSecretDataPayload>
  | UpdateConfigurationYAMLVariables

export const secretMutations = {
  create: (queryClient: QueryClient) => configurationMutations.create(secretKind, queryClient),
  remove: (queryClient: QueryClient) => configurationMutations.remove(secretKind, queryClient),
  contentKey: ({ scope, name }: ConfigurationTarget) =>
    [...configurationKeys.detail(secretKind, scope, name), 'update-content'] as const,
  updateContent: (
    queryClient: QueryClient,
    target: ConfigurationTarget,
    canRetain: () => boolean,
  ) => {
    const dataOptions = configurationMutations.updateData<
      Awaited<ReturnType<typeof updateSecretData>>,
      UpdateSecretDataPayload
    >(secretKind, queryClient)
    const yamlOptions = configurationMutations.updateYAML(secretKind, queryClient)
    return mutationOptions({
      mutationKey: secretMutations.contentKey(target),
      gcTime: 0,
      mutationFn: async (variables: SecretContentUpdate) =>
        'payload' in variables
          ? { detail: await updateSecretData(variables.target, variables.payload) }
          : { yaml: await updateConfigurationYAML(secretKind, variables) },
      onSuccess: (result, variables, _onMutateResult, context) => {
        if (!canRetain()) return invalidateConfigurationCaches(queryClient, secretKind, target)
        if ('payload' in variables) {
          if (result.detail) return dataOptions.onSuccess?.(result.detail, variables, undefined, context)
        } else if (result.yaml) {
          return yamlOptions.onSuccess?.(result.yaml, variables, undefined, context)
        }
      },
    })
  },
}
