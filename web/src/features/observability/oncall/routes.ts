import { defineRoutes } from '@/routes/definitions'

export const observabilityOncallRoutes = defineRoutes([
  {
    meta: {
      id: 'monitoring-workbench-oncall',
      path: '/monitoring-workbench/oncall',
      title: '值班协同',
      description: '值班轮换与升级联动',
      icon: 'IconUserCircle',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-oncall',
      permissionKey: 'observe.oncall.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./board-page')
      return { default: module.OnCallBoardPage }
    },
  },
  {
    meta: {
      id: 'monitoring-workbench-oncall-settings',
      path: '/monitoring-workbench/oncall/settings',
      title: '值班设置',
      description: '排班、轮值、升级链与 IRM 路由',
      icon: 'IconSettings',
      group: 'observe',
      workbenchId: 'monitoring',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'monitoring-workbench',
      menuId: 'monitoring-workbench-oncall-settings',
      permissionKey: 'observe.oncall.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./settings-page')
      return { default: module.OnCallSettingsPage }
    },
  },
] as const)
