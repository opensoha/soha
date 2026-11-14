import { createUUID } from '@/utils/uuid'
import type { LoginProviderSettings } from '../types'

export const LOGIN_PROVIDER_TYPE_OPTIONS = [
  { value: 'oidc', label: 'OIDC' },
  { value: 'feishu', label: '飞书 OAuth2' },
  { value: 'dingtalk', label: '钉钉 OAuth2' },
  { value: 'wecom', label: '企业微信 OAuth2' },
  { value: 'oauth2', label: '通用 OAuth2' },
  { value: 'saml', label: 'SAML' },
]

export const LOGIN_SYNC_MODE_OPTIONS = [
  { value: 'append', label: '补充绑定' },
  { value: 'replace_external', label: '替换该登录源绑定' },
]

export function normalizeLoginProvider(
  item?: Partial<LoginProviderSettings> | null,
): LoginProviderSettings {
  return {
    id: String(item?.id || ''),
    name: String(item?.name || ''),
    type: String(item?.type || 'oidc'),
    iconUrl: String(item?.iconUrl || ''),
    enabled: Boolean(item?.enabled),
    clientId: String(item?.clientId || ''),
    clientSecret: String(item?.clientSecret || ''),
    issuer: String(item?.issuer || ''),
    authorizeUrl: String(item?.authorizeUrl || ''),
    tokenUrl: String(item?.tokenUrl || ''),
    userInfoUrl: String(item?.userInfoUrl || ''),
    profileUrl: String(item?.profileUrl || ''),
    redirectUrl: String(item?.redirectUrl || ''),
    frontendRedirectUrl: String(item?.frontendRedirectUrl || ''),
    scopes: Array.isArray(item?.scopes) ? item.scopes.map(String) : [],
    defaultRoles: Array.isArray(item?.defaultRoles) ? item.defaultRoles.map(String) : [],
    userIdField: String(item?.userIdField || ''),
    userNameField: String(item?.userNameField || ''),
    emailField: String(item?.emailField || ''),
    phoneField: String(item?.phoneField || ''),
    avatarField: String(item?.avatarField || ''),
    roleField: String(item?.roleField || ''),
    organizationField: String(item?.organizationField || ''),
    syncRolesOnLogin: Boolean(item?.syncRolesOnLogin),
    syncOrgsOnLogin: Boolean(item?.syncOrgsOnLogin),
    roleSyncMode: String(item?.roleSyncMode || 'append'),
    orgSyncMode: String(item?.orgSyncMode || 'append'),
    metadataUrl: String(item?.metadataUrl || ''),
    entityId: String(item?.entityId || ''),
    certificate: String(item?.certificate || ''),
  }
}

export function defaultRedirectPath(providerId: string) {
  return `${window.location.origin}/api/v1/auth/login/${providerId || 'provider'}/callback`
}

export function defaultFrontendRedirectPath() {
  return `${window.location.origin}/login/callback`
}

export function newLoginProviderID() {
  return createUUID()
}

export function applyProviderPreset(
  type: string,
  current?: Partial<LoginProviderSettings> | null,
): LoginProviderSettings {
  const provider = normalizeLoginProvider(current)
  switch (type) {
    case 'feishu':
      return {
        ...provider,
        type,
        authorizeUrl:
          provider.authorizeUrl || 'https://accounts.feishu.cn/open-apis/authen/v1/authorize',
        tokenUrl: provider.tokenUrl || 'https://accounts.feishu.cn/oauth/v3/token',
        userInfoUrl: provider.userInfoUrl || 'https://open.feishu.cn/open-apis/authen/v1/user_info',
        scopes: provider.scopes.length > 0 ? provider.scopes : ['contact:user.base:readonly'],
        userIdField: provider.userIdField || 'open_id',
        userNameField: provider.userNameField || 'name',
        emailField: provider.emailField || 'enterprise_email',
        phoneField: provider.phoneField || 'mobile',
        avatarField: provider.avatarField || 'avatar_url',
        roleField: provider.roleField || 'role_ids',
        organizationField: provider.organizationField || 'department_ids',
      }
    case 'dingtalk':
      return {
        ...provider,
        type,
        authorizeUrl: provider.authorizeUrl || 'https://login.dingtalk.com/oauth2/auth',
        tokenUrl: provider.tokenUrl || 'https://api.dingtalk.com/v1.0/oauth2/userAccessToken',
        userInfoUrl: provider.userInfoUrl || 'https://api.dingtalk.com/v1.0/contact/users/me',
        scopes: provider.scopes.length > 0 ? provider.scopes : ['openid'],
        userIdField: provider.userIdField || 'unionId',
        userNameField: provider.userNameField || 'nick',
        emailField: provider.emailField || 'email',
        phoneField: provider.phoneField || 'mobile',
        avatarField: provider.avatarField || 'avatarUrl',
        roleField: provider.roleField || 'roleList',
        organizationField: provider.organizationField || 'dept_id_list',
      }
    case 'wecom':
      return {
        ...provider,
        type,
        authorizeUrl:
          provider.authorizeUrl || 'https://open.weixin.qq.com/connect/oauth2/authorize',
        tokenUrl: provider.tokenUrl || 'https://qyapi.weixin.qq.com/cgi-bin/gettoken',
        userInfoUrl: provider.userInfoUrl || 'https://qyapi.weixin.qq.com/cgi-bin/user/getuserinfo',
        profileUrl: provider.profileUrl || 'https://qyapi.weixin.qq.com/cgi-bin/user/get',
        scopes: provider.scopes.length > 0 ? provider.scopes : ['snsapi_base'],
        userIdField: provider.userIdField || 'UserId',
        userNameField: provider.userNameField || 'UserId',
        emailField: provider.emailField || 'email',
        phoneField: provider.phoneField || 'mobile',
        avatarField: provider.avatarField || 'avatar',
        roleField: provider.roleField || 'roles',
        organizationField: provider.organizationField || 'department',
      }
    case 'saml':
      return { ...provider, type, scopes: [], authorizeUrl: '', tokenUrl: '', userInfoUrl: '' }
    default:
      return {
        ...provider,
        type,
        scopes: provider.scopes.length > 0 ? provider.scopes : ['openid', 'profile', 'email'],
        userIdField: provider.userIdField || 'sub',
        userNameField: provider.userNameField || 'name',
        emailField: provider.emailField || 'email',
        phoneField: provider.phoneField || 'phone_number',
        avatarField: provider.avatarField || 'picture',
        roleField: provider.roleField || 'roles',
        organizationField: provider.organizationField || 'groups',
      }
  }
}
