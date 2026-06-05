export function displayWorkbenchSessionTitle(title?: string) {
  const trimmed = String(title || '').trim()
  if (!trimmed) return '新的会话'
  if (trimmed === '新的调查会话') return '新的会话'
  if (trimmed === 'New Investigation') return 'New Chat'
  return trimmed
}
