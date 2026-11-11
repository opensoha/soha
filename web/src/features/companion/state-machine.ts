import type { AIGlobalAssistantMessage } from '@/features/copilot'
import type { CompanionVisualState } from './types'

interface CompanionStateInput {
  disabled?: boolean
  dragging?: boolean
  hovered?: boolean
  panelOpen?: boolean
  running?: boolean
  messages?: AIGlobalAssistantMessage[]
}

export function latestAssistantMessage(messages: AIGlobalAssistantMessage[] = []) {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    if (messages[index].role === 'assistant') return messages[index]
  }
  return undefined
}

export function resolveCompanionVisualState({
  disabled = false,
  dragging = false,
  hovered = false,
  panelOpen = false,
  running = false,
  messages = [],
}: CompanionStateInput): CompanionVisualState {
  if (disabled) return 'disabled'
  const latest = latestAssistantMessage(messages)
  if (latest?.status === 'error') return 'error'
  if (dragging) return 'dragging'
  if (running) return latest?.content ? 'speaking' : 'thinking'
  if (panelOpen) return 'listening'
  if (hovered) return 'hover'
  return 'idle'
}

export function companionSpeech(messages: AIGlobalAssistantMessage[] = [], running = false) {
  const latest = latestAssistantMessage(messages)
  if (!latest || latest.status === 'abort') return ''
  const value = latest.content.trim()
  if (!value && running) return '正在思考...'
  return value.length > 180 ? `${value.slice(0, 180)}...` : value
}
