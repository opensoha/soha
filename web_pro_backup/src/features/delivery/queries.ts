import { api } from '@/services/api-client'
import type { ApiResponse, ApplicationEnvironment, BusinessLine, DeliveryEnvironment, WorkflowTemplate } from '@/types'

export interface DeliveryApplication {
  id: string
  name?: string
  businessLineId?: string
  enabled?: boolean
}

export interface DeliveryWorkflowSummary {
  id: string
  applicationId?: string
  clusterId?: string
  namespace?: string
  deploymentName?: string
  status: string
  updatedAt?: string
}

export interface DeliveryRegistry {
  id: string
  enabled: boolean
}

export interface DeliveryBuildSummary {
  id: string
  applicationId: string
  status: string
  createdAt: string
}

export interface DeliveryReleaseSummary {
  id: string
  applicationId: string
  clusterId: string
  namespace: string
  deploymentName: string
  status: string
  createdAt: string
}

export const deliveryApplicationsQueryKey = ['applications'] as const
export const deliveryBusinessLinesQueryKey = ['business-lines'] as const
export const deliveryEnvironmentsQueryKey = ['delivery-environments'] as const
export const deliveryApplicationEnvironmentsQueryKey = ['application-environments'] as const
export const deliveryWorkflowTemplatesQueryKey = ['workflow-templates'] as const
export const deliveryBuildsQueryKey = ['builds'] as const
export const deliveryWorkflowsQueryKey = ['workflows'] as const
export const deliveryReleasesQueryKey = ['releases'] as const
export const deliveryRegistriesQueryKey = ['registries'] as const

export function deliveryApplicationsQueryOptions<T extends DeliveryApplication = DeliveryApplication>() {
  return {
    queryKey: deliveryApplicationsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/applications'),
  }
}

export function deliveryBusinessLinesQueryOptions<T extends BusinessLine = BusinessLine>() {
  return {
    queryKey: deliveryBusinessLinesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/business-lines'),
  }
}

export function deliveryEnvironmentsQueryOptions<T extends DeliveryEnvironment = DeliveryEnvironment>() {
  return {
    queryKey: deliveryEnvironmentsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/delivery-environments'),
  }
}

export function deliveryApplicationEnvironmentsQueryOptions<T extends ApplicationEnvironment = ApplicationEnvironment>() {
  return {
    queryKey: deliveryApplicationEnvironmentsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/application-environments'),
  }
}

export function deliveryWorkflowTemplatesQueryOptions<T extends WorkflowTemplate = WorkflowTemplate>() {
  return {
    queryKey: deliveryWorkflowTemplatesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/workflow-templates'),
  }
}

export function deliveryBuildsQueryOptions<T extends DeliveryBuildSummary = DeliveryBuildSummary>() {
  return {
    queryKey: deliveryBuildsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/builds'),
  }
}

export function deliveryWorkflowsQueryOptions<T extends DeliveryWorkflowSummary = DeliveryWorkflowSummary>() {
  return {
    queryKey: deliveryWorkflowsQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/workflows'),
  }
}

export function deliveryReleasesQueryOptions<T extends DeliveryReleaseSummary = DeliveryReleaseSummary>() {
  return {
    queryKey: deliveryReleasesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/releases'),
  }
}

export function deliveryRegistriesQueryOptions<T extends DeliveryRegistry = DeliveryRegistry>() {
  return {
    queryKey: deliveryRegistriesQueryKey,
    queryFn: () => api.get<ApiResponse<T[]>>('/registries'),
  }
}
