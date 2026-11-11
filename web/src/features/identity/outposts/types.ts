import type {
  IdentityOutpost as ContractOutpost,
  IdentityOutpostInput as ContractOutpostInput,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type IdentityOutpost = ContractOutpost
export type IdentityOutpostInput = ContractOutpostInput
export type IdentityOutpostMode = IdentityOutpost['mode']
export type IdentityOutpostStatus = 'online' | 'offline' | 'degraded'

export interface IdentityOutpostFilters {
  mode?: IdentityOutpostMode | ''
  status?: IdentityOutpostStatus | ''
  limit?: number
  offset?: number
}

export interface UpdateIdentityOutpostVariables {
  outpostId: string
  input: IdentityOutpostInput
}
