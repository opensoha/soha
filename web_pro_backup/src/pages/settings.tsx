import { useMemo } from 'react'
import { useLocation } from '@umijs/max'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import {
  AISettingsPage,
  BrandingSettingsPage,
  IdentitySettingsPage,
  MonitoringSettingsPage,
  SettingsCenterPage,
} from '@/features/settings/settings-pages'
import { NonPlatformWorkspace } from '@/pages/nonplatform-workspace'

const settingsItems = [
  { key: 'identity', path: '/settings', label: '身份设置', description: '配置 OIDC 与登录身份基线。', permissionKey: 'settings.identity.view' },
  { key: 'branding', path: '/settings/branding', label: '品牌设置', description: '维护品牌 Logo、Favicon 与控制台标题。', permissionKey: 'settings.branding.view' },
  { key: 'monitoring', path: '/settings/monitoring', label: '监控设置', description: '管理 Prometheus 和监控集成参数。', permissionKey: 'settings.monitoring.view' },
  { key: 'ai', path: '/settings/ai', label: 'AI 设置', description: '维护 AI 提供商、模型与密钥配置。', permissionKey: 'settings.ai.view' },
] as const

function renderSettingsContent(pathname: string) {
  if (pathname.startsWith('/settings/branding')) return <BrandingSettingsPage />
  if (pathname.startsWith('/settings/monitoring')) return <MonitoringSettingsPage embedded />
  if (pathname.startsWith('/settings/ai')) return <AISettingsPage embedded />
  if (pathname === '/settings' || pathname.startsWith('/settings/identity')) return <IdentitySettingsPage />
  return <SettingsCenterPage embedded />
}

export default function SettingsPage() {
  const { pathname } = useLocation()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data

  const stats = useMemo(() => [
    { label: '可见设置', value: String(settingsItems.filter((item) => !item.permissionKey || snapshot?.permissionKeys.includes(item.permissionKey)).length), hint: '当前账号在设置中心下可见的设置分区。' },
    { label: '身份权限', value: snapshot?.permissionKeys.includes('settings.identity.manage') ? '可管理' : '只读', hint: '决定是否允许保存身份配置。' },
    { label: '品牌权限', value: snapshot?.permissionKeys.includes('settings.branding.manage') ? '可管理' : '只读', hint: '控制品牌资源上传与保存。' },
    { label: 'AI 权限', value: snapshot?.permissionKeys.includes('settings.ai.manage') ? '可管理' : '只读', hint: '决定 AI provider 配置是否可修改。' },
  ], [snapshot?.permissionKeys])

  return (
    <NonPlatformWorkspace
      rootPath="/settings"
      title="设置中心"
      description="身份、品牌、监控与 AI 的集中配置入口。"
      workspaceLabel="Settings"
      workspaceSummary="设置中心入口页现在主动呈现设置分区和权限摘要，不再只是把 `/settings` 直接导向 tabs 组件。各设置页面的具体表单、上传和保存逻辑仍由原有 feature 模块负责，入口层只负责 Pro-native 的工作区组织。"
      items={[...settingsItems]}
      stats={stats}
      highlights={[
        { title: '配置中心与具体表单分开', description: '入口页负责展示当前账号可见的设置域和管理权限，具体保存逻辑仍留在身份、品牌、监控和 AI 模块内部。' },
        { title: '避免外层与内层重复标题', description: '设置分区现在以内嵌模式渲染，工作区保持单一页面框架，不再叠加第二层设置中心头部。' },
      ]}
      actions={[
        { label: '先确认权限边界', description: '通过身份、品牌和 AI 权限摘要判断当前账号是只读还是可管理，再进入具体设置表单。' },
        { label: '按域完成配置', description: '监控和 AI 仍保持独立配置面，品牌与身份则继续承载控制台全局基线。' },
      ]}
    >
      {renderSettingsContent(pathname)}
    </NonPlatformWorkspace>
  )
}
