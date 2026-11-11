import type {
  DirectoryEvent as ContractDirectoryEvent,
  DirectoryRuntimeStatus as ContractDirectoryRuntimeStatus,
} from '@opensoha/contracts/gen/ts/sohaapi'

export type DirectoryProviderType = 'feishu' | 'wecom' | 'dingtalk' | 'ldap' | 'scim' | 'custom'
export type DirectorySyncMode = 'manual' | 'scheduled' | 'scheduled_and_realtime'
export type DirectoryConnectionStatus = 'pending' | 'healthy' | 'degraded' | 'disabled'
export type DirectoryRunStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'partial'
  | 'failed'
  | 'canceled'

export interface DirectorySyncPolicy {
  syncOrganizations: true
  syncPeople: boolean
  mode: DirectorySyncMode
  schedule?: string
  fullReconcileSchedule?: string
  provisionMode: 'create_and_link' | 'review_before_link'
  userDisablePolicy: 'managed_only' | 'never'
  missingObjectPolicy: 'archive'
}

export interface DirectoryConnection {
  id: string
  name: string
  providerType: DirectoryProviderType
  loginProviderId?: string
  credentialRef?: string
  enabled: boolean
  capabilities: string[]
  status: DirectoryConnectionStatus
  lastValidatedAt?: string
  lastRunAt?: string
  policy: DirectorySyncPolicy
  metadata?: Record<string, unknown>
}

export interface DirectoryConnectionInput {
  name: string
  providerType: DirectoryProviderType
  loginProviderId?: string
  credentialRef?: string
  enabled: boolean
  policy: DirectorySyncPolicy
  webhookVerificationToken?: string
  webhookEncryptKey?: string
  scimBearerToken?: string
  ldapBindDn?: string
  ldapBindPassword?: string
  metadata?: {
    endpoint?: string
    baseDN?: string
    organizationBaseDN?: string
    peopleBaseDN?: string
    organizationFilter?: string
    peopleFilter?: string
    startTLS?: boolean
  }
}

export interface DirectoryChangeCounts {
  create?: number
  update?: number
  move?: number
  archive?: number
  skip?: number
  conflict?: number
}

export interface DirectorySyncPreview {
  connectionId: string
  organizations: DirectoryChangeCounts
  people?: DirectoryChangeCounts
  generatedAt?: string
  warnings?: string[]
}

export interface DirectorySyncRun {
  id: string
  connectionId: string
  trigger: 'manual' | 'scheduled' | 'realtime' | 'preview'
  status: DirectoryRunStatus
  startedAt?: string
  finishedAt?: string
  organizations?: DirectoryChangeCounts
  people?: DirectoryChangeCounts
  error?: string
}

export interface DirectoryConflict {
  id: string
  connectionId: string
  objectType: 'organization' | 'person' | 'membership'
  externalId?: string
  reason: string
  status: 'open' | 'resolved' | 'ignored'
  createdAt: string
}

export type DirectoryRuntimeStatus = ContractDirectoryRuntimeStatus
export type DirectoryEvent = ContractDirectoryEvent
