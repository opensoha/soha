import type { AIGlobalAssistantAction, AIPageContext, AISelectionContext } from './ai-context'
import { contextDisplayName, sanitizeSelectionText } from './ai-context'

const ACTION_TITLES: Record<AIGlobalAssistantAction, string> = {
  open: '打开全局 AI 助手',
  'analyze-page': '分析当前页面上下文',
  'troubleshoot-resource': '排查当前资源',
  'explain-selection': '解释选中内容',
  'troubleshoot-selection': '排查选中内容',
  'summarize-selection': '总结选中内容',
  'next-steps-selection': '生成下一步排查建议',
  freeform: '继续追问',
}

function contextLines(context: AIPageContext) {
  return [
    ['sourceWorkbench', context.sourceWorkbench],
    ['sourceRoute', context.sourceRoute],
    ['sourceTitle', context.sourceTitle],
    ['entityKind', context.entityKind],
    ['entityName', context.entityName],
    ['clusterId', context.clusterId],
    ['namespace', context.namespace],
    ['workload', context.workload],
    ['service', context.service],
    ['pod', context.pod],
    ['node', context.node],
    ['alertId', context.alertId],
    ['applicationId', context.applicationId],
    ['releaseBundleId', context.releaseBundleId],
    ['dockerHostId', context.dockerHostId],
    ['dockerServiceId', context.dockerServiceId],
    ['virtualizationConnectionId', context.virtualizationConnectionId],
    ['vmId', context.vmId],
    ['timeRangeMinutes', context.timeRangeMinutes],
  ]
    .filter(([, value]) => value !== undefined && value !== null && value !== '')
    .map(([key, value]) => `- ${key}: ${String(value)}`)
}

function actionInstruction(action: AIGlobalAssistantAction) {
  switch (action) {
    case 'analyze-page':
      return '请基于当前页面上下文判断需要关注的异常信号、可查询证据和下一步排查路径。'
    case 'troubleshoot-resource':
      return '请围绕当前资源做快速故障排查，优先检查健康状态、事件、日志、指标、依赖关系和最近变更。'
    case 'explain-selection':
      return '请解释选中内容的含义、关键信号和可能影响。'
    case 'troubleshoot-selection':
      return '请结合选中内容和页面上下文判断可能原因，并给出验证步骤。'
    case 'summarize-selection':
      return '请总结选中内容，提炼异常、影响范围和需要继续确认的信息。'
    case 'next-steps-selection':
      return '请基于选中内容生成下一步排查清单，按优先级排序并说明每一步要验证什么。'
    case 'freeform':
      return '请回答用户追问，并继续结合当前页面上下文和已有会话证据。'
    case 'open':
    default:
      return '请等待用户输入，必要时先说明你已经读取到的页面上下文范围。'
  }
}

export function buildGlobalAssistantTitle(action: AIGlobalAssistantAction, context: AIPageContext) {
  const target = contextDisplayName(context)
  if (action === 'troubleshoot-resource') return `${target} 排查`
  if (action.includes('selection')) return `${target} 文本分析`
  if (action === 'analyze-page') return `${target} 页面分析`
  return `${target} AI 助手`
}

export function buildGlobalAssistantPrompt(
  action: AIGlobalAssistantAction,
  context: AIPageContext,
  selection?: AISelectionContext,
  prompt?: string,
) {
  if (action === 'freeform' && prompt?.trim()) {
    return prompt.trim()
  }

  const lines = [
    `任务: ${ACTION_TITLES[action]}`,
    '',
    actionInstruction(action),
    '',
    '页面上下文:',
    ...contextLines(context),
  ]

  if (context.promptHint) {
    lines.push('', `页面提示: ${context.promptHint}`)
  }

  if (selection?.text) {
    lines.push(
      '',
      `选中内容类型: ${selection.kind}`,
      '选中内容:',
      '```text',
      sanitizeSelectionText(selection.text),
      '```',
    )
  }

  lines.push('', '请用简洁中文输出：关键判断、证据缺口、下一步操作。高风险操作只给建议，不要直接执行。')
  return lines.join('\n')
}

export function defaultPromptSuggestions(context: AIPageContext) {
  const target = contextDisplayName(context)
  return [
    `排查 ${target} 当前异常`,
    `总结 ${target} 最近 60 分钟风险`,
    '列出下一步需要检查的日志、事件和指标',
  ]
}
