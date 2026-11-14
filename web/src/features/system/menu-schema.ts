export const MENU_SECTION_ORDER = [
  'account',
  'software',
  'provider',
  'integrations',
  'endpoint',
  'network',
  'vpn',
  'proxy',
  'users',
  'operations',
  'extensions',
  'platform',
  'ops',
  'observe-signals',
  'observe-data',
  'dashboards',
  'alerting',
  'ai-interaction',
  'ai-engineering',
  'ai-model-access',
  'ai-governance',
  'delivery',
  'delivery-records',
  'delivery-platform',
  'catalog',
  'admin',
] as const

export type MenuSectionKey = (typeof MENU_SECTION_ORDER)[number]

const MENU_SECTION_LABELS: Record<MenuSectionKey, { zh: string; en: string }> = {
  account: { zh: '基础', en: 'Basics' },
  software: { zh: '软件', en: 'Software' },
  provider: { zh: '应用接入', en: 'Application Access' },
  integrations: { zh: '系统集成', en: 'System Integrations' },
  endpoint: { zh: '终端', en: 'Endpoints' },
  network: { zh: '网络准入', en: 'Network Admission' },
  vpn: { zh: '零信任网络与 VPN', en: 'Zero Trust Network & VPN' },
  proxy: { zh: '代理', en: 'Proxy' },
  users: { zh: '用户管理', en: 'User Management' },
  operations: { zh: '系统运维', en: 'System Operations' },
  extensions: { zh: '扩展', en: 'Extensions' },
  platform: { zh: 'Dashboard', en: 'Dashboard' },
  ops: { zh: 'Observe', en: 'Observe' },
  'observe-signals': { zh: '探索', en: 'Explore' },
  'observe-data': { zh: '数据与集成', en: 'Data & Integrations' },
  dashboards: { zh: '仪表盘', en: 'Dashboards' },
  alerting: { zh: '告警与响应', en: 'Alerting & Response' },
  'ai-interaction': { zh: '交互', en: 'Interaction' },
  'ai-engineering': { zh: 'AI 工程', en: 'AI Engineering' },
  'ai-model-access': { zh: '模型与接入', en: 'Models & Access' },
  'ai-governance': { zh: '治理与可观测', en: 'Governance & Observability' },
  delivery: { zh: '持续交付', en: 'Delivery' },
  'delivery-records': { zh: '交付记录', en: 'Delivery Records' },
  'delivery-platform': { zh: '平台配置', en: 'Platform Configuration' },
  catalog: { zh: 'Catalog', en: 'Catalog' },
  admin: { zh: '管理', en: 'Admin' },
}

const MENU_SECTION_ALIASES: Record<string, MenuSectionKey> = {
  account: 'account',
  basic: 'account',
  basics: 'account',
  software: 'software',
  provider: 'provider',
  providers: 'provider',
  integration: 'integrations',
  integrations: 'integrations',
  'system-integration': 'integrations',
  'system-integrations': 'integrations',
  endpoint: 'endpoint',
  endpoints: 'endpoint',
  terminal: 'endpoint',
  terminals: 'endpoint',
  network: 'network',
  vpn: 'vpn',
  proxy: 'proxy',
  users: 'users',
  user: 'users',
  access: 'users',
  operations: 'operations',
  operation: 'operations',
  system: 'operations',
  extension: 'extensions',
  extensions: 'extensions',
  platform: 'platform',
  dashboard: 'platform',
  ops: 'ops',
  observe: 'ops',
  logging: 'observe-signals',
  logs: 'observe-signals',
  signals: 'observe-signals',
  explore: 'observe-signals',
  'observe-signals': 'observe-signals',
  'observe-data': 'observe-data',
  'observability-data': 'observe-data',
  dashboards: 'dashboards',
  'observability-dashboards': 'dashboards',
  alerting: 'alerting',
  alerts: 'alerting',
  interaction: 'ai-interaction',
  'ai-interaction': 'ai-interaction',
  'ai-engineering': 'ai-engineering',
  'model-access': 'ai-model-access',
  'ai-model-access': 'ai-model-access',
  governance: 'ai-governance',
  'ai-governance': 'ai-governance',
  'ai-operations': 'ai-governance',
  'production-operations': 'ai-governance',
  deliver: 'delivery',
  delivery: 'delivery',
  'delivery-record': 'delivery-records',
  'delivery-records': 'delivery-records',
  records: 'delivery-records',
  'delivery-platform': 'delivery-platform',
  'platform-config': 'delivery-platform',
  catalog: 'catalog',
  admin: 'admin',
}

export function normalizeMenuSection(section: string) {
  const trimmed = String(section || '').trim()
  if (!trimmed) return ''
  return MENU_SECTION_ALIASES[trimmed.toLowerCase()] ?? trimmed
}

export function getMenuSectionOrder(section: string) {
  const normalized = normalizeMenuSection(section) as MenuSectionKey
  const index = MENU_SECTION_ORDER.indexOf(normalized)
  return index >= 0 ? index : Number.MAX_SAFE_INTEGER
}

export function resolveMenuSectionLabel(section: string, localeCode: 'zh_CN' | 'en_US' = 'zh_CN') {
  const normalized = normalizeMenuSection(section) as MenuSectionKey
  const labels = MENU_SECTION_LABELS[normalized]
  if (!labels) return section || 'unknown'
  return localeCode === 'en_US' ? labels.en : labels.zh
}

export function buildMenuSectionOptions(
  sections: string[],
  localeCode: 'zh_CN' | 'en_US' = 'zh_CN',
) {
  return Array.from(new Set(sections.map((item) => normalizeMenuSection(item)).filter(Boolean)))
    .sort((left, right) => {
      const sectionOrder = getMenuSectionOrder(left) - getMenuSectionOrder(right)
      if (sectionOrder !== 0) return sectionOrder
      return resolveMenuSectionLabel(left, localeCode).localeCompare(
        resolveMenuSectionLabel(right, localeCode),
      )
    })
    .map((value) => ({
      value,
      label: resolveMenuSectionLabel(value, localeCode),
    }))
}
