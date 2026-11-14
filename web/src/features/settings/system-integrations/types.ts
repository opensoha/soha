import type {
  SystemIntegrationCategory,
  SystemIntegrationUpdateRequest,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type {
  SystemIntegration,
  SystemIntegrationCategory,
  SystemIntegrationCreateRequest,
  SystemIntegrationTestResult,
  SystemIntegrationUpdateRequest,
  SystemIntegrationOAuthAuthorization,
} from '@opensoha/contracts/gen/ts/sohaapi'

export interface SystemIntegrationFilters {
  category?: SystemIntegrationCategory
  providerType?: string
  enabled?: boolean
}

export interface UpdateSystemIntegrationInput {
  id: string
  values: SystemIntegrationUpdateRequest
}
