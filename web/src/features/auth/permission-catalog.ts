import permissionCatalogArtifact from '@opensoha/contracts/auth/permission-catalog.json'
import type {
  PermissionCatalog,
  PermissionDefinition,
} from '@opensoha/contracts/gen/ts/sohaapi'

export interface PermissionCatalogOption {
  value: string
  label: string
  description?: string
}

export interface PermissionCatalogGroup {
  key: string
  label: string
  description: string
  options: PermissionCatalogOption[]
}

const GROUP_LABELS: Record<string, string> = {
  access: '访问控制',
  ai: 'AI 运维',
  delivery: '持续交付',
  docker: '容器运行时',
  identity: '身份门户',
  observe: '运维智能',
  overview: '概览',
  platform: '平台管理',
  plugin: '插件管理',
  secret: 'Secret Store',
  settings: '设置中心',
  software: '软件库',
  system: '系统管理',
  virtualization: '虚拟化管理',
  workspace: '工作台',
}

export const consolePermissionCatalog = permissionCatalogArtifact as PermissionCatalog
const assignablePermissions = consolePermissionCatalog.permissions.filter(
  (permission: PermissionDefinition) => permission.assignable && permission.status === 'active',
)

export const consolePermissionGroups: PermissionCatalogGroup[] = Array.from(
  new Set(assignablePermissions.map((permission) => permission.domain)),
  (domain) => ({
    key: domain,
    label: GROUP_LABELS[domain] || domain,
    description: '',
    options: assignablePermissions
      .filter((permission) => permission.domain === domain)
      .map((permission) => ({
        value: permission.key,
        label: permission.displayName,
      })),
  }),
)

export const consolePermissionOptions = consolePermissionGroups.flatMap((group) => group.options)

export const consolePermissionLabelMap = Object.fromEntries(
  consolePermissionCatalog.permissions.map((permission) => [
    permission.key,
    permission.displayName,
  ]),
) as Record<string, string>
