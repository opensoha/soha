import type { WorkbenchMessage } from './types'

export type WorkbenchBubbleStatus = 'local' | 'loading' | 'updating' | 'success' | 'error' | 'abort'

export type ConversationMessage = WorkbenchMessage & {
  deliveryStatus?: WorkbenchBubbleStatus
}

export function assistantReactionForMessage(
  messages: ConversationMessage[],
  target: ConversationMessage,
) {
  if (target.role !== 'user') return undefined
  const reply = [...messages]
    .reverse()
    .find(
      (item) =>
        item.role === 'assistant' &&
        item.sessionId === target.sessionId &&
        item.metadata?.requestMessageId === target.id &&
        !['loading', 'updating', 'error', 'abort'].includes(item.deliveryStatus || ''),
    )
  const emoji = reply?.metadata?.assistantReaction
  return typeof emoji === 'string' &&
    ['👋', '👍', '❤️', '😊', '🎉', '💡', '👀', '🤔', '✅', '🙏'].includes(emoji)
    ? emoji
    : undefined
}

export function bubbleItems(messages: ConversationMessage[]) {
  return messages.map((item) => ({
    key: item.id,
    role: item.role === 'assistant' ? 'ai' : item.role === 'system' ? 'system' : 'user',
    content: item.content,
    status: item.deliveryStatus ?? ('success' as const),
    extraInfo: { createdAt: item.createdAt, source: item.metadata?.source },
  }))
}

function messageMetadataSource(item: WorkbenchMessage | ConversationMessage) {
  return typeof item.metadata?.source === 'string' ? item.metadata.source : ''
}

export function isLegacyPlatformContextMessage(item: WorkbenchMessage | ConversationMessage) {
  const content = item.content.trim()
  if (
    Array.isArray(item.metadata?.analysisArtifacts) &&
    item.metadata.analysisArtifacts.length > 0
  ) {
    return false
  }
  return (
    item.role === 'assistant' &&
    (messageMetadataSource(item) === 'platform-context' ||
      messageMetadataSource(item) === 'legacy-platform-context' ||
      item.metadata?.legacyFallback === true ||
      content.startsWith('当前平台上下文：') ||
      content.startsWith('当前集群上下文：') ||
      content.startsWith('当前构建上下文：') ||
      content.startsWith('当前告警上下文：') ||
      content.startsWith('当前审计上下文：') ||
      content.startsWith('Current platform context:') ||
      content.startsWith('Current clusters context:') ||
      content.startsWith('Current builds context:') ||
      content.startsWith('Current alerts context:') ||
      content.startsWith('Current audit context:'))
  )
}

export function modelStatusValue(message: ConversationMessage | undefined, pending: boolean) {
  if (pending) return '调用中'
  const source = message ? messageMetadataSource(message) : ''
  switch (source) {
    case 'model-provider':
      return '已响应'
    case 'model-unconfigured':
      return '未配置'
    case 'model-error':
      return '失败'
    case 'model-empty':
      return '空返回'
    default:
      return message ? '已记录' : '待开始'
  }
}

export function modelStatusDetail(message: ConversationMessage | undefined, pending: boolean) {
  if (pending) return '正在等待后端模型调用返回。'
  if (!message) return '发送第一条消息后，这里会显示最近一次模型调用状态。'
  const source = messageMetadataSource(message)
  if (source === 'model-provider') {
    const model = typeof message.metadata?.model === 'string' ? message.metadata.model : ''
    return model ? `最近回复来自模型 ${model}` : '最近回复来自已配置的大模型提供方。'
  }
  if (source === 'model-unconfigured') return '后端没有可用的大模型提供方，请到 AI 设置完成配置。'
  if (source === 'model-error') {
    return typeof message.metadata?.error === 'string'
      ? message.metadata.error
      : '大模型调用失败，请检查 AI 设置。'
  }
  if (source === 'model-empty') return '模型接口返回成功，但没有可显示内容。'
  return '最近回复来自历史消息或兼容数据。'
}

function sortConversationMessages(items: ConversationMessage[]) {
  return [...items].sort((left, right) => {
    const leftTime = new Date(left.createdAt).getTime()
    const rightTime = new Date(right.createdAt).getTime()
    if (Number.isNaN(leftTime) || Number.isNaN(rightTime) || leftTime === rightTime) {
      return left.id.localeCompare(right.id)
    }
    return leftTime - rightTime
  })
}

export function mergeConversationMessages(
  serverMessages: WorkbenchMessage[],
  localMessages: ConversationMessage[],
  sessionId?: string,
) {
  const serverIds = new Set(serverMessages.map((item) => item.id))
  return sortConversationMessages([
    ...serverMessages
      .filter((item) => !isLegacyPlatformContextMessage(item))
      .map((item) => ({ ...item, deliveryStatus: 'success' as WorkbenchBubbleStatus })),
    ...localMessages.filter((item) => item.sessionId === sessionId && !serverIds.has(item.id)),
  ])
}

export function pendingConversationMessages(
  sessionId: string,
  content: string,
): { user: ConversationMessage; assistant: ConversationMessage } {
  const now = Date.now()
  const createdAt = new Date(now).toISOString()
  return {
    user: {
      id: `local:user:${sessionId}:${now}`,
      sessionId,
      role: 'user',
      content,
      metadata: { source: 'local-pending' },
      createdAt,
      deliveryStatus: 'loading',
    },
    assistant: {
      id: `local:assistant:${sessionId}:${now}`,
      sessionId,
      role: 'assistant',
      content: '',
      metadata: { source: 'model-thinking' },
      createdAt: new Date(now + 1).toISOString(),
      deliveryStatus: 'loading',
    },
  }
}
