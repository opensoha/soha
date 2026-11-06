import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  createEndpointMaintenance,
  deleteEndpointResponsePolicy,
  endEndpointMaintenance,
  previewEndpointResponse,
  publishEndpointResponsePolicy,
  restoreEndpointResponse,
  rollbackEndpointResponsePolicy,
  saveEndpointResponsePolicy,
} from './api'
import { endpointResponseKeys } from './queries'
import { networkAccessKeys } from '../keys'

const invalidate = (client: QueryClient) =>
  client.invalidateQueries({ queryKey: endpointResponseKeys.all })
export const endpointResponseMutations = {
  save: (client: QueryClient) =>
    mutationOptions({
      mutationKey: [...endpointResponseKeys.all, 'save'],
      mutationFn: saveEndpointResponsePolicy,
      onSuccess: () => invalidate(client),
    }),
  publish: (client: QueryClient) =>
    mutationOptions({
      mutationKey: [...endpointResponseKeys.all, 'publish'],
      mutationFn: publishEndpointResponsePolicy,
      onSuccess: () => invalidate(client),
    }),
  rollback: (client: QueryClient) =>
    mutationOptions({
      mutationKey: [...endpointResponseKeys.all, 'rollback'],
      mutationFn: rollbackEndpointResponsePolicy,
      onSuccess: () => invalidate(client),
    }),
  remove: (client: QueryClient) =>
    mutationOptions({
      mutationKey: [...endpointResponseKeys.all, 'remove'],
      mutationFn: deleteEndpointResponsePolicy,
      onSuccess: () => invalidate(client),
    }),
  preview: () =>
    mutationOptions({
      mutationKey: [...endpointResponseKeys.all, 'preview'],
      mutationFn: previewEndpointResponse,
    }),
  restore: (client: QueryClient) =>
    mutationOptions({
      mutationKey: [...endpointResponseKeys.all, 'restore'],
      mutationFn: restoreEndpointResponse,
      onSuccess: async () => {
        await Promise.all([
          invalidate(client),
          client.invalidateQueries({ queryKey: networkAccessKeys.sessions.all }),
        ])
      },
    }),
  createMaintenance: (client: QueryClient) =>
    mutationOptions({
      mutationKey: [...endpointResponseKeys.all, 'maintenance-create'],
      mutationFn: createEndpointMaintenance,
      onSuccess: () => invalidate(client),
    }),
  endMaintenance: (client: QueryClient) =>
    mutationOptions({
      mutationKey: [...endpointResponseKeys.all, 'maintenance-end'],
      mutationFn: endEndpointMaintenance,
      onSuccess: () => invalidate(client),
    }),
}
