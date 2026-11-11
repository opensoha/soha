import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import type {
  AlertIntegration,
  AlertIntegrationTestPayload,
  AlertIntegrationTestResult,
  AlertIntegrationUpsertPayload,
  UpdateAlertIntegrationInput,
} from './types'

export async function listAlertIntegrations(): Promise<AlertIntegration[]> {
  const response = await api.get<ApiResponse<AlertIntegration[]>>('/alert-integrations')
  return response.data ?? []
}

export async function createAlertIntegration(
  payload: AlertIntegrationUpsertPayload,
): Promise<AlertIntegration> {
  const response = await api.post<ApiResponse<AlertIntegration>>('/alert-integrations', payload)
  return response.data
}

export async function updateAlertIntegration({
  id,
  payload,
}: UpdateAlertIntegrationInput): Promise<AlertIntegration> {
  const response = await api.put<ApiResponse<AlertIntegration>>(
    `/alert-integrations/${id}`,
    payload,
  )
  return response.data
}

export async function testAlertIntegration(
  payload: AlertIntegrationTestPayload,
): Promise<AlertIntegrationTestResult> {
  const response = await api.post<ApiResponse<AlertIntegrationTestResult>>(
    '/alert-integrations/test',
    payload,
  )
  return response.data
}
