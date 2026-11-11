interface ConfigurationCopyFeedback {
  error: (content: string) => unknown
  success: (content: string) => unknown
}

export function copyConfigurationValue(
  value: string,
  localeCode: string,
  feedback: ConfigurationCopyFeedback,
) {
  if (!navigator.clipboard) return
  navigator.clipboard.writeText(value).then(
    () => void feedback.success(localeCode === 'zh_CN' ? '已复制' : 'Copied'),
    () => void feedback.error(localeCode === 'zh_CN' ? '复制失败' : 'Copy failed'),
  )
}

export function configurationDataRows(entries?: Record<string, string>) {
  return Object.entries(entries ?? {})
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => ({ key, value }))
}

export function configurationDataSize(value: string) {
  return `${new Blob([value]).size} B`
}
