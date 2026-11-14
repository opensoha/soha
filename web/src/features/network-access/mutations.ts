import { mutationOptions, type QueryClient } from '@tanstack/react-query'
import {
  putNetworkWorkloadBinding,
  analyzeNetworkAccessConflicts,
  compileNetworkAccessPolicySnapshot,
  createNetworkAccessGrant,
  createNetworkAccessPolicy,
  createNetworkGateway,
  createNetworkMihomoProfile,
  createNetworkNASBinding,
  createNetworkRuntimeEnrollment,
  createNetworkResource,
  createNetworkSite,
  createNetworkSiteProfileBinding,
  createNetworkSpace,
  deleteNetworkAccessPolicy,
  deleteNetworkMihomoProfile,
  deleteNetworkNASBinding,
  deleteNetworkResource,
  deleteNetworkSite,
  deleteNetworkSiteProfileBinding,
  deleteNetworkSpace,
  previewNetworkAccessPolicy,
  previewNetworkAdmission,
  executeNetworkSessionAction,
  planNetworkSessionAction,
  revokeNetworkRuntimeEnrollment,
  revokeNetworkAccessGrant,
  updateNetworkAccessPolicy,
  updateNetworkGateway,
  updateNetworkMihomoProfile,
  updateNetworkNASBinding,
  updateEndpointDevice,
  updateNetworkResource,
  updateNetworkSite,
  updateNetworkSiteProfileBinding,
  updateNetworkSpace,
} from './api'
import { networkAccessKeys, networkAccessMutationKeys } from './keys'

const invalidate = (queryClient: QueryClient) =>
  queryClient.invalidateQueries({ queryKey: networkAccessKeys.all })

export const networkAccessMutations = {
 workloadBindings:{put:(queryClient:QueryClient)=>mutationOptions({mutationKey:['network-access','workload-bindings','put'],mutationFn:putNetworkWorkloadBinding,onSuccess:()=>invalidate(queryClient)})},
  devices: {
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.deviceUpdate,
        mutationFn: updateEndpointDevice,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  sites: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.siteCreate,
        mutationFn: createNetworkSite,
        onSuccess: () => invalidate(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.siteUpdate,
        mutationFn: updateNetworkSite,
        onSuccess: () => invalidate(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.siteDelete,
        mutationFn: deleteNetworkSite,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  spaces: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.spaceCreate,
        mutationFn: createNetworkSpace,
        onSuccess: () => invalidate(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.spaceUpdate,
        mutationFn: updateNetworkSpace,
        onSuccess: () => invalidate(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.spaceDelete,
        mutationFn: deleteNetworkSpace,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  resources: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.resourceCreate,
        mutationFn: createNetworkResource,
        onSuccess: () => invalidate(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.resourceUpdate,
        mutationFn: updateNetworkResource,
        onSuccess: () => invalidate(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.resourceDelete,
        mutationFn: deleteNetworkResource,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  gateways: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.gatewayCreate,
        mutationFn: createNetworkGateway,
        onSuccess: () => invalidate(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.gatewayUpdate,
        mutationFn: updateNetworkGateway,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  mihomoProfiles: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.mihomoProfileCreate,
        mutationFn: createNetworkMihomoProfile,
        onSuccess: () => invalidate(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.mihomoProfileUpdate,
        mutationFn: updateNetworkMihomoProfile,
        onSuccess: () => invalidate(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.mihomoProfileDelete,
        mutationFn: deleteNetworkMihomoProfile,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  enrollments: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.enrollmentCreate,
        mutationFn: createNetworkRuntimeEnrollment,
        gcTime: 0,
        onSuccess: () => invalidate(queryClient),
      }),
    revoke: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.enrollmentRevoke,
        mutationFn: revokeNetworkRuntimeEnrollment,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  accessGrants: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.accessGrantCreate,
        mutationFn: createNetworkAccessGrant,
        gcTime: 0,
        onSuccess: () => invalidate(queryClient),
      }),
    revoke: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.accessGrantRevoke,
        mutationFn: revokeNetworkAccessGrant,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  nasBindings: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.nasBindingCreate,
        mutationFn: createNetworkNASBinding,
        onSuccess: () => invalidate(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.nasBindingUpdate,
        mutationFn: updateNetworkNASBinding,
        onSuccess: () => invalidate(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.nasBindingDelete,
        mutationFn: deleteNetworkNASBinding,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  siteProfileBindings: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.siteProfileBindingCreate,
        mutationFn: createNetworkSiteProfileBinding,
        onSuccess: () => invalidate(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.siteProfileBindingUpdate,
        mutationFn: updateNetworkSiteProfileBinding,
        onSuccess: () => invalidate(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.siteProfileBindingDelete,
        mutationFn: deleteNetworkSiteProfileBinding,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  sessions: {
    plan: () =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.sessionActionPlan,
        mutationFn: planNetworkSessionAction,
      }),
    execute: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.sessionActionExecute,
        mutationFn: executeNetworkSessionAction,
        onSuccess: () => invalidate(queryClient),
      }),
  },
  policy: {
    create: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.policyCreate,
        mutationFn: createNetworkAccessPolicy,
        onSuccess: () => invalidate(queryClient),
      }),
    update: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.policyUpdate,
        mutationFn: updateNetworkAccessPolicy,
        onSuccess: () => invalidate(queryClient),
      }),
    remove: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.policyDelete,
        mutationFn: deleteNetworkAccessPolicy,
        onSuccess: () => invalidate(queryClient),
      }),
    compile: (queryClient: QueryClient) =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.policyCompile,
        mutationFn: compileNetworkAccessPolicySnapshot,
        onSuccess: () => invalidate(queryClient),
      }),
    preview: () =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.policyPreview,
        mutationFn: previewNetworkAccessPolicy,
      }),
    admissionPreview: () =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.admissionPreview,
        mutationFn: previewNetworkAdmission,
      }),
    analyzeConflicts: () =>
      mutationOptions({
        mutationKey: networkAccessMutationKeys.policyConflictAnalysis,
        mutationFn: analyzeNetworkAccessConflicts,
      }),
  },
}
