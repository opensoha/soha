import { queryOptions } from '@tanstack/react-query'
import { memoryApi } from './api'
import { memoryKeys } from './keys'
export const memoryQueries = {
  records: () =>
    queryOptions({ queryKey: memoryKeys.records(), queryFn: () => memoryApi.records.list(true) }),
  policies: () =>
    queryOptions({ queryKey: memoryKeys.policies(), queryFn: memoryApi.policies.list }),
}
