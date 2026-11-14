import { defineRoutes } from '@/routes/definitions'

export const companionRoutes = defineRoutes([
  {
    meta: {
      id: 'ai-workbench-companion',
      path: '/ai-workbench/companion',
      title: 'Companion',
      description: '配置桌面悬浮形态、Live2D 模型与对话反馈',
      icon: 'IconRobot',
      group: 'ai',
      workbenchId: 'ai',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'ai-workbench',
      menuId: 'ai-workbench-companion',
      permissionKey: 'observe.ai.chat',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./settings-page')
      return { default: module.CompanionSettingsPage }
    },
  },
  {
    meta: {
      id: 'companion-window',
      path: '/companion',
      title: '桌面宠物',
      description: '桌面宠物窗口',
      icon: 'IconRobot',
      group: 'ai',
      workbenchId: 'ai',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      permissionKey: 'observe.ai.chat',
      scopeMode: 'hidden',
      workspace: 'system',
    },
    shell: 'portal',
    load: async () => {
      const module = await import('./window-page')
      return { default: module.CompanionWindowPage }
    },
  },
] as const)
