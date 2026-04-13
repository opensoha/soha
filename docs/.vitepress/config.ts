import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'kubecrux',
  description: 'Modern multi-cluster Kubernetes platform console',
  lang: 'zh-CN',
  cleanUrls: true,
  appearance: true,
  lastUpdated: true,
  locales: {
    root: {
      label: '简体中文',
      lang: 'zh-CN',
      title: 'kubecrux',
      description: '现代化多集群 Kubernetes 平台控制台',
      themeConfig: {
        nav: [
          { text: '首页', link: '/' },
          { text: '架构', link: '/architecture/' },
          { text: '开发', link: '/development/local-development' },
          { text: 'API', link: '/api/overview' },
          { text: '运维', link: '/operations/configuration' },
          { text: '路线图', link: '/roadmap/' }
        ],
        sidebar: {
          '/architecture/': [
            {
              text: '架构',
              items: [
                { text: '架构入口', link: '/architecture/' },
                { text: '监控与告警', link: '/architecture/monitoring-and-alerting' },
                { text: '应用交付', link: '/architecture/application-delivery' },
                { text: 'AI Copilot', link: '/architecture/ai-copilot' },
                { text: 'Agent 协议', link: '/architecture/agent-protocol' },
                { text: '权限模型', link: '/architecture/authorization' },
                { text: '多集群模型', link: '/architecture/multi-cluster-model' },
                { text: '事件模型', link: '/architecture/event-model' },
                { text: '审计模型', link: '/architecture/audit-model' },
                { text: 'MCP 集成', link: '/architecture/mcp-integration' }
              ]
            }
          ],
          '/development/': [
            {
              text: '开发',
              items: [
                { text: '本地开发', link: '/development/local-development' },
                { text: '前端结构', link: '/development/frontend-structure' },
                { text: '后端结构', link: '/development/backend-structure' },
                { text: '配置说明', link: '/development/configuration' },
                { text: '数据库迁移', link: '/development/database-migrations' },
                { text: '新增资源模块', link: '/development/add-resource-module' },
                { text: '新增页面', link: '/development/add-page' },
                { text: '新增 MCP 集成', link: '/development/add-mcp-integration' }
              ]
            }
          ],
          '/api/': [
            {
              text: 'API',
              items: [
                { text: '总览', link: '/api/overview' },
                { text: '核心接口', link: '/api/core-endpoints' },
                { text: '认证与错误', link: '/api/auth-and-errors' }
              ]
            }
          ],
          '/operations/': [
            {
              text: '运维',
              items: [
                { text: '配置', link: '/operations/configuration' },
                { text: 'Agent 运行时', link: '/operations/agent-runtime' },
                { text: '部署说明', link: '/operations/deployment' },
                { text: '环境变量', link: '/operations/environment-variables' },
                { text: '依赖项', link: '/operations/dependencies' },
                { text: '文档发布', link: '/operations/docs-publishing' },
                { text: 'MCP', link: '/operations/mcp' }
              ]
            }
          ],
          '/roadmap/': [
            {
              text: '路线图',
              items: [{ text: '路线图', link: '/roadmap/' }]
            }
          ],
          '/reference/': [
            {
              text: '参考',
              items: [
                { text: '产品信息架构', link: '/reference/product-information-architecture' },
                { text: '数据库模型', link: '/reference/database-schema' }
              ]
            }
          ]
        },
        outline: {
          level: [2, 3],
          label: '本页导航'
        },
        docFooter: {
          prev: '上一页',
          next: '下一页'
        },
        darkModeSwitchLabel: '主题',
        lightModeSwitchTitle: '切换到浅色模式',
        darkModeSwitchTitle: '切换到深色模式',
        sidebarMenuLabel: '目录',
        returnToTopLabel: '回到顶部',
        socialLinks: [{ icon: 'github', link: 'https://github.com/example/kubecrux' }],
        footer: {
          message: '由 VitePress 构建，并与 kubecrux 代码库保持同步演进。',
          copyright: 'Copyright © 2026 kubecrux contributors'
        }
      }
    },
    en: {
      label: 'English',
      lang: 'en-US',
      link: '/en/',
      title: 'kubecrux',
      description: 'Modern multi-cluster Kubernetes platform console',
      themeConfig: {
        nav: [
          { text: 'Home', link: '/en/' },
          { text: 'Architecture', link: '/en/architecture/' },
          { text: 'Development', link: '/en/development/local-development' },
          { text: 'API', link: '/en/api/overview' },
          { text: 'Operations', link: '/en/operations/configuration' },
          { text: 'Roadmap', link: '/en/roadmap/' }
        ],
        sidebar: {
          '/en/architecture/': [
            {
              text: 'Architecture',
              items: [
                { text: 'Architecture Entry', link: '/en/architecture/' },
                { text: 'Monitoring And Alerting', link: '/en/architecture/monitoring-and-alerting' },
                { text: 'Application Delivery', link: '/en/architecture/application-delivery' },
                { text: 'AI Copilot', link: '/en/architecture/ai-copilot' },
                { text: 'Agent Protocol', link: '/en/architecture/agent-protocol' },
                { text: 'Authorization', link: '/en/architecture/authorization' },
                { text: 'Multi-Cluster Model', link: '/en/architecture/multi-cluster-model' },
                { text: 'Event Model', link: '/en/architecture/event-model' },
                { text: 'Audit Model', link: '/en/architecture/audit-model' },
                { text: 'MCP Integration', link: '/en/architecture/mcp-integration' }
              ]
            }
          ],
          '/en/development/': [
            {
              text: 'Development',
              items: [
                { text: 'Local Development', link: '/en/development/local-development' },
                { text: 'Frontend Structure', link: '/en/development/frontend-structure' },
                { text: 'Backend Structure', link: '/en/development/backend-structure' },
                { text: 'Configuration', link: '/en/development/configuration' },
                { text: 'Database Migrations', link: '/en/development/database-migrations' },
                { text: 'Add Resource Module', link: '/en/development/add-resource-module' },
                { text: 'Add Page', link: '/en/development/add-page' },
                { text: 'Add MCP Integration', link: '/en/development/add-mcp-integration' }
              ]
            }
          ],
          '/en/api/': [
            {
              text: 'API',
              items: [
                { text: 'Overview', link: '/en/api/overview' },
                { text: 'Core Endpoints', link: '/en/api/core-endpoints' },
                { text: 'Auth And Errors', link: '/en/api/auth-and-errors' }
              ]
            }
          ],
          '/en/operations/': [
            {
              text: 'Operations',
              items: [
                { text: 'Configuration', link: '/en/operations/configuration' },
                { text: 'Agent Runtime', link: '/en/operations/agent-runtime' },
                { text: 'Deployment', link: '/en/operations/deployment' },
                { text: 'Environment Variables', link: '/en/operations/environment-variables' },
                { text: 'Dependencies', link: '/en/operations/dependencies' },
                { text: 'Docs Publishing', link: '/en/operations/docs-publishing' },
                { text: 'MCP', link: '/en/operations/mcp' }
              ]
            }
          ],
          '/en/roadmap/': [
            {
              text: 'Roadmap',
              items: [{ text: 'Roadmap', link: '/en/roadmap/' }]
            }
          ],
          '/en/reference/': [
            {
              text: 'Reference',
              items: [
                { text: 'Product Information Architecture', link: '/en/reference/product-information-architecture' },
                { text: 'Database Schema', link: '/en/reference/database-schema' }
              ]
            }
          ]
        },
        outline: {
          level: [2, 3],
          label: 'On this page'
        },
        socialLinks: [{ icon: 'github', link: 'https://github.com/example/kubecrux' }],
        footer: {
          message: 'Built with VitePress and kept in lockstep with the kubecrux codebase.',
          copyright: 'Copyright © 2026 kubecrux contributors'
        }
      }
    }
  },
  themeConfig: {
    logo: '/logo.svg',
    search: {
      provider: 'local'
    },
    outline: {
      level: [2, 3],
      label: 'On this page'
    },
    nav: [
      { text: 'Home', link: '/' },
      { text: 'Architecture', link: '/architecture/' },
      { text: 'Development', link: '/development/local-development' },
      { text: 'API', link: '/api/overview' },
      { text: 'Operations', link: '/operations/configuration' },
      { text: 'Roadmap', link: '/roadmap/' }
    ],
    sidebar: {
      '/architecture/': [
        {
          text: 'Architecture',
          items: [
            { text: 'Architecture Entry', link: '/architecture/' },
            { text: 'Monitoring And Alerting', link: '/architecture/monitoring-and-alerting' },
            { text: 'Application Delivery', link: '/architecture/application-delivery' },
            { text: 'AI Copilot', link: '/architecture/ai-copilot' },
            { text: 'Agent Protocol', link: '/architecture/agent-protocol' },
            { text: 'Authorization', link: '/architecture/authorization' },
            { text: 'Multi-Cluster Model', link: '/architecture/multi-cluster-model' },
            { text: 'Event Model', link: '/architecture/event-model' },
            { text: 'Audit Model', link: '/architecture/audit-model' },
            { text: 'MCP Integration', link: '/architecture/mcp-integration' }
          ]
        }
      ],
      '/development/': [
        {
          text: 'Development',
          items: [
            { text: 'Local Development', link: '/development/local-development' },
            { text: 'Frontend Structure', link: '/development/frontend-structure' },
            { text: 'Backend Structure', link: '/development/backend-structure' },
            { text: 'Configuration', link: '/development/configuration' },
            { text: 'Database Migrations', link: '/development/database-migrations' },
            { text: 'Add Resource Module', link: '/development/add-resource-module' },
            { text: 'Add Page', link: '/development/add-page' },
            { text: 'Add MCP Integration', link: '/development/add-mcp-integration' }
          ]
        }
      ],
      '/api/': [
        {
          text: 'API',
          items: [
            { text: 'Overview', link: '/api/overview' },
            { text: 'Core Endpoints', link: '/api/core-endpoints' },
            { text: 'Auth And Errors', link: '/api/auth-and-errors' }
          ]
        }
      ],
      '/operations/': [
        {
          text: 'Operations',
          items: [
            { text: 'Configuration', link: '/operations/configuration' },
            { text: 'Agent Runtime', link: '/operations/agent-runtime' },
            { text: 'Deployment', link: '/operations/deployment' },
            { text: 'Environment Variables', link: '/operations/environment-variables' },
            { text: 'Dependencies', link: '/operations/dependencies' },
            { text: 'Docs Publishing', link: '/operations/docs-publishing' },
            { text: 'MCP', link: '/operations/mcp' }
          ]
        }
      ],
      '/roadmap/': [
        {
          text: 'Roadmap',
          items: [{ text: 'Roadmap', link: '/roadmap/' }]
        }
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'Product Information Architecture', link: '/reference/product-information-architecture' },
            { text: 'Database Schema', link: '/reference/database-schema' }
          ]
        }
      ]
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/example/kubecrux' }],
    footer: {
      message: 'Built with VitePress and kept in lockstep with the kubecrux codebase.',
      copyright: 'Copyright © 2026 kubecrux contributors'
    }
  }
})
