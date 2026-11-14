import type {
  IdentityApplicationInput,
  IdentityApplicationStatus,
  IdentityProviderType,
} from '../shared/types'

export type {
  IdentityApplication,
  IdentityApplicationInput,
  IdentityApplicationStatus,
} from '../shared/types'

export interface IdentityApplicationFilters {
  query?: string
  status?: IdentityApplicationStatus | ''
}

export interface IdentityProviderCapability {
  type: IdentityProviderType
  status: string
  endpoints: string[]
  description?: string
}

export interface UpdateIdentityApplicationVariables {
  applicationId: string
  input: IdentityApplicationInput
}
