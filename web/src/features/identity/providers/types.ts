import type {
  SAMLCertificateRotation,
  SAMLCertificateRotateRequest,
  SAMLMetadataInput,
  SAMLMetadataImportRequest,
  SAMLMetadataValidation,
  SAMLLoginSource,
  SAMLServiceProviderInput,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type IdentityRuntimeProviderType = 'oidc' | 'proxy' | 'saml'
export type IdentityRuntimeProviderStatus = 'enabled' | 'disabled'
export type IdentityOIDCClientStatus = 'enabled' | 'disabled'
export type IdentityOIDCClientType = 'public' | 'confidential'
export type IdentityOIDCRedirectMatchMode = 'strict' | 'regex'
export type {
  IdentityProviderSetup,
  IdentityProviderUserMetadata,
} from '@opensoha/contracts/gen/ts/sohaapi'

export interface IdentityProvider {
  id: string
  applicationId: string
  name: string
  type: IdentityRuntimeProviderType
  enabled: boolean
  config?: Record<string, unknown>
  configuredSecretAliases?: string[]
  status: IdentityRuntimeProviderStatus
  createdBy?: string
  updatedBy?: string
  createdAt: string
  updatedAt: string
}

export interface IdentityProviderInput {
  applicationId: string
  name: string
  type: IdentityRuntimeProviderType
  enabled: boolean
  config: Record<string, unknown>
  secretRefs?: Record<string, string>
  status: IdentityRuntimeProviderStatus
}

export interface IdentityProviderFilters {
  applicationId?: string
  type?: IdentityRuntimeProviderType | ''
  status?: IdentityRuntimeProviderStatus | ''
  limit?: number
  offset?: number
}

export interface IdentityOIDCClient {
  id: string
  providerId: string
  clientId: string
  clientType: IdentityOIDCClientType
  clientSecretAvailable?: boolean
  redirectUris: string[]
  redirectUriRegexes?: string[]
  postLogoutRedirectUris: string[]
  allowedScopes: string[]
  allowedGrantTypes: string[]
  requirePkce: boolean
  accessTokenTtlSeconds: number
  idTokenTtlSeconds: number
  refreshTokenTtlSeconds: number
  status: IdentityOIDCClientStatus
  createdAt: string
  updatedAt: string
}

export interface IdentityOIDCClientInput {
  providerId?: string
  clientId?: string
  clientType: IdentityOIDCClientType
  clientSecret?: string
  redirectUris: string[]
  redirectUriRegexes?: string[]
  postLogoutRedirectUris: string[]
  allowedScopes: string[]
  allowedGrantTypes: Array<'authorization_code' | 'refresh_token'>
  requirePkce: boolean
  accessTokenTtlSeconds: number
  idTokenTtlSeconds: number
  refreshTokenTtlSeconds: number
  status: IdentityOIDCClientStatus
}

export interface IdentityOIDCClientCreated {
  client: IdentityOIDCClient
  clientSecret?: string
}

export interface IdentityOIDCClientSecretReveal {
  clientId: string
  clientSecret: string
  revealedAt: string
}

export interface UpdateIdentityProviderVariables {
  providerId: string
  input: IdentityProviderInput
}

export interface CreateIdentityOIDCClientVariables {
  providerId: string
  input: IdentityOIDCClientInput
}

export interface UpdateIdentityOIDCClientVariables extends CreateIdentityOIDCClientVariables {
  clientId: string
}

export interface DeleteIdentityOIDCClientVariables {
  providerId: string
  clientId: string
}

export interface RotateIdentityProviderSAMLCertificateVariables {
  providerId: string
  input: SAMLCertificateRotateRequest
}

export interface IdentitySigningKey {
  id: string
  providerId: string
  kid: string
  alg: string
  active: boolean
  createdAt: string
}

export type {
  SAMLCertificateRotation,
  SAMLCertificateRotateRequest,
  SAMLMetadataInput,
  SAMLMetadataImportRequest,
  SAMLMetadataValidation,
  SAMLLoginSource,
  SAMLServiceProviderInput,
}
