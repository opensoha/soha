import type { components } from '@opensoha/contracts/gen/ts/sohaapi'

export type AppHostingSettings = components['schemas']['AppHostingSettings']
export type AppHostingSettingsUpdateRequest =
  components['schemas']['AppHostingSettingsUpdateRequest']
export type AppHostingOrganizationVisibilityPolicy =
  components['schemas']['AppHostingOrganizationVisibilityPolicy']
export type AppHostingGatewayTokenRotateRequest =
  components['schemas']['AppHostingGatewayTokenRotateRequest']
export type AppHostingGatewayTokenSecret = components['schemas']['AppHostingGatewayTokenSecret']
export type AppHostingAudienceUser = components['schemas']['AppHostingAudienceUser']
export type AppHostingAudienceTeam = components['schemas']['AppHostingAudienceTeam']
export type AppHostingAudienceCandidates = components['schemas']['AppHostingAudienceCandidates']
export type HostedApp = components['schemas']['HostedApp']
export type HostedAppList = components['schemas']['HostedAppList']
export type HostedAppKind = components['schemas']['HostedAppKind']
export type HostedAppStatus = components['schemas']['HostedAppStatus']
export type HostedAppVisibilityMode = components['schemas']['HostedAppVisibilityMode']
export type HostedAppVisibilityScope = components['schemas']['HostedAppVisibilityScope']
export type HostedAppUserSummary = components['schemas']['HostedAppUserSummary']
export type HostedAppRevision = components['schemas']['HostedAppRevision']
export type HostedAppRevisionList = components['schemas']['HostedAppRevisionList']
export type HostedAppRevisionStatus = components['schemas']['HostedAppRevisionStatus']
export type HostedAppVisibilityApproval = components['schemas']['HostedAppVisibilityApproval']
export type HostedAppVisibilityApprovalList =
  components['schemas']['HostedAppVisibilityApprovalList']
export type HostedAppVisibilityApprovalStatus =
  components['schemas']['HostedAppVisibilityApprovalStatus']
export type HostedAppVisibilityApprovalDecisionResult =
  components['schemas']['HostedAppVisibilityApprovalDecisionResult']
export type DeleteHostedAppResult = components['schemas']['DeleteHostedAppResult']

export interface AdminHostedAppListParams {
  query?: string
  status?: HostedAppStatus
  ownerUserId?: string
  teamId?: string
  limit?: number
  offset?: number
}

export interface AdminVisibilityApprovalListParams {
  status?: HostedAppVisibilityApprovalStatus
  appId?: string
  limit?: number
  offset?: number
}

export interface AdminHostedAppRevisionListParams {
  limit?: number
  offset?: number
}

export interface SuspendHostedAppInput {
  appId: string
  appName?: string
  reason: string
}

export interface UnsuspendHostedAppInput {
  appId: string
  appName?: string
  reason?: string
}

export interface TransferHostedAppInput {
  appId: string
  appName?: string
  newOwnerUserId: string
  reason?: string
}

export interface DeleteHostedAppInput {
  appId: string
  appName?: string
  confirmationName: string
  reason?: string
}

export interface ApproveVisibilityApprovalInput {
  approvalId: string
  appName?: string
  comment?: string
}

export interface RejectVisibilityApprovalInput {
  approvalId: string
  appName?: string
  comment: string
}
