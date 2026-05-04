export const MENU_SECTION_ORDER = ['observe', 'deliver', 'catalog', 'control'] as const

export type MenuSectionKey = (typeof MENU_SECTION_ORDER)[number]

const MENU_SECTION_LABELS: Record<MenuSectionKey, { zh: string; en: string }> = {
  observe: { zh: '平台资源 / 运维智能', en: 'Platform / Operations' },
  deliver: { zh: '应用交付', en: 'Delivery' },
  catalog: { zh: '基础目录', en: 'Catalog' },
  control: { zh: '访问控制 / 系统 / 设置', en: 'Control / System / Settings' },
}

export function resolveMenuSectionLabel(section: string, localeCode: 'zh_CN' | 'en_US' = 'zh_CN') {
  const normalized = String(section || '').trim() as MenuSectionKey
  const labels = MENU_SECTION_LABELS[normalized]
  if (!labels) return section || 'unknown'
  return localeCode === 'en_US' ? labels.en : labels.zh
}

export const MENU_SECTION_OPTIONS = MENU_SECTION_ORDER.map((value) => ({
  value,
  label: MENU_SECTION_LABELS[value].zh,
}))

export function buildMenuSectionOptions(
  sections: string[],
  localeCode: 'zh_CN' | 'en_US' = 'zh_CN',
) {
  const knownSections = new Set(MENU_SECTION_ORDER)
  const customSections = Array.from(
    new Set(
      sections
        .map((item) => String(item || '').trim())
        .filter(Boolean)
        .filter((item) => !knownSections.has(item as MenuSectionKey)),
    ),
  ).sort((left, right) => left.localeCompare(right))

  return [
    ...MENU_SECTION_OPTIONS.map((item) => ({
      value: item.value,
      label: resolveMenuSectionLabel(item.value, localeCode),
    })),
    ...customSections.map((item) => ({
      value: item,
      label: resolveMenuSectionLabel(item, localeCode),
    })),
  ]
}
