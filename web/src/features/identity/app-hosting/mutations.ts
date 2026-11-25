import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  approveAdminVisibilityApproval,
  deleteAdminHostedApp,
  rejectAdminVisibilityApproval,
  rotateAppHostingGatewayToken,
  suspendAdminHostedApp,
  transferAdminHostedAppOwnership,
  unsuspendAdminHostedApp,
  updateAppHostingSettings,
} from './api'
import { appHostingAdminKeys, appHostingAdminMutationKeys } from './keys'

export const appHostingAdminMutations = {
  updateSettings: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: appHostingAdminMutationKeys.updateSettings,
      mutationFn: updateAppHostingSettings,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.settings }),
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.approvals }),
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.apps }),
        ])
      },
    }),
  rotateGatewayToken: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: appHostingAdminMutationKeys.rotateGatewayToken,
      mutationFn: rotateAppHostingGatewayToken,
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.settings })
      },
    }),
  suspendApp: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: appHostingAdminMutationKeys.suspendApp,
      mutationFn: suspendAdminHostedApp,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.apps }),
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.approvals }),
        ])
      },
    }),
  unsuspendApp: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: appHostingAdminMutationKeys.unsuspendApp,
      mutationFn: unsuspendAdminHostedApp,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.apps }),
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.approvals }),
        ])
      },
    }),
  transferApp: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: appHostingAdminMutationKeys.transferApp,
      mutationFn: transferAdminHostedAppOwnership,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.apps }),
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.approvals }),
        ])
      },
    }),
  deleteApp: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: appHostingAdminMutationKeys.deleteApp,
      mutationFn: deleteAdminHostedApp,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.apps }),
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.approvals }),
        ])
      },
    }),
  approveVisibility: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: appHostingAdminMutationKeys.approveVisibility,
      mutationFn: approveAdminVisibilityApproval,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.approvals }),
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.apps }),
        ])
      },
    }),
  rejectVisibility: (queryClient: QueryClient) =>
    mutationOptions({
      mutationKey: appHostingAdminMutationKeys.rejectVisibility,
      mutationFn: rejectAdminVisibilityApproval,
      onSuccess: async () => {
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.approvals }),
          queryClient.invalidateQueries({ queryKey: appHostingAdminKeys.apps }),
        ])
      },
    }),
}
