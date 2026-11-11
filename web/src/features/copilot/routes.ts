import { defineRoutes } from '@/routes/definitions'
import { copilotObserveRouteManifests } from './observe/routes'

export const copilotGatewayRoutes = defineRoutes([
  {
    meta: {
      id: 'ai-gateway-relay',
      path: '/ai-gateway/relay',
      title: '模型中转',
      description: '管理 AI Gateway 模型中转、上游、模型路由与模型调用日志',
      icon: 'IconLink',
      group: 'ai-gateway',
      workbenchId: 'ai',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'ai-workbench',
      menuId: 'ai-gateway-relay',
      permissionKey: 'ai.gateway.relay.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway/pages/relay-page')
      return { default: module.AIGatewayRelayPage }
    },
  },
  {
    meta: {
      id: 'ai-gateway-manifest',
      path: '/ai-gateway/manifest',
      title: '能力清单',
      description: '查看当前身份可见的 MCP tools、resources、prompts 和 skills',
      icon: 'IconShield',
      group: 'ai-gateway',
      workbenchId: 'ai',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'ai-workbench',
      menuId: 'ai-gateway-manifest',
      permissionKey: 'ai.gateway.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway/pages/manifest-page')
      return { default: module.AIGatewayManifestPage }
    },
  },
  {
    meta: {
      id: 'ai-gateway-clients',
      path: '/ai-gateway/clients',
      title: 'AI Clients',
      description: '管理外部 AI 客户端注册入口',
      icon: 'IconShield',
      group: 'ai-gateway',
      workbenchId: 'ai',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'ai-workbench',
      menuId: 'ai-gateway-clients',
      permissionKey: 'ai.gateway.clients.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway/pages/clients-page')
      return { default: module.AIGatewayClientsPage }
    },
  },
  {
    meta: {
      id: 'ai-gateway-tokens',
      path: '/ai-gateway/tokens',
      title: 'Tokens',
      description: '管理 personal access tokens 与服务账号 token',
      icon: 'IconShield',
      group: 'ai-gateway',
      workbenchId: 'ai',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'ai-workbench',
      menuId: 'ai-gateway-tokens',
      permissionKey: 'ai.gateway.tokens.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway/pages/tokens-page')
      return { default: module.AIGatewayTokensPage }
    },
  },
  {
    meta: {
      id: 'ai-gateway-governance',
      path: '/ai-gateway/governance',
      title: 'Governance',
      description: '管理 Gateway 授权、策略与审批治理',
      icon: 'IconShield',
      group: 'ai-gateway',
      workbenchId: 'ai',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'ai-workbench',
      menuId: 'ai-gateway-governance',
      permissionKeysAny: [
        'ai.gateway.approvals.view',
        'ai.gateway.grants.view',
        'ai.gateway.policies.view',
      ],
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway/pages/governance-page')
      return { default: module.AIGatewayGovernancePage }
    },
  },
  {
    meta: {
      id: 'ai-gateway-call-logs',
      path: '/ai-gateway/call-logs',
      title: '调用日志',
      description: '查看 AI Gateway 调用者、调用内容与结果',
      icon: 'IconShield',
      group: 'ai-gateway',
      workbenchId: 'ai',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'ai-workbench',
      menuId: 'ai-gateway-call-logs',
      permissionKey: 'ai.gateway.view',
      scopeMode: 'passive',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./gateway/pages/call-logs-page')
      return { default: module.AIGatewayCallLogsPage }
    },
  },
] as const)

export const copilotRouteManifests = [
  ...copilotObserveRouteManifests,
  copilotGatewayRoutes,
] as const
