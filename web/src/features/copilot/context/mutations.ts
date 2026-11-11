import { mutationOptions } from '@tanstack/react-query'
import { contextApi } from './api'
import { contextMutationKeys } from './keys'

export const contextMutations = {
  inspect: () =>
    mutationOptions({
      mutationKey: contextMutationKeys.inspect,
      mutationFn: contextApi.inspect,
    }),
}
