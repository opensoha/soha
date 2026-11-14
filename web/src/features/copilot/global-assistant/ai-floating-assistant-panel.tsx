import {
  CloseOutlined,
  ExpandAltOutlined,
  FileSearchOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import type { ThoughtChainItemType } from '@ant-design/x'
import { Bubble, Sender, Sources, Suggestion, ThoughtChain, XProvider } from '@ant-design/x'
import type { WorkbenchSource } from '@opensoha/contracts/gen/ts/sohaapi'
import { Button, Empty, Space, Tag, Typography } from 'antd'
import { useMemo, type ReactNode } from 'react'
import type { WorkbenchStreamToolCall } from '../workbench/stream'
import type { AIGlobalAssistantMessage, AIPageContext } from './ai-context'
import { contextDisplayName } from './ai-context'
import { defaultPromptSuggestions } from './ai-prompts'

const { Text } = Typography

interface AIFloatingAssistantPanelProps {
  context: AIPageContext
  disabled?: boolean
  feedback?: ReactNode
  inputValue: string
  messages: AIGlobalAssistantMessage[]
  onCancel: () => void
  onClose: () => void
  onInputChange: (value: string) => void
  onOpenWorkbench: () => void
  onQuickPrompt: (prompt: string) => void
  onSubmit: (message: string) => void
  open: boolean
  running?: boolean
  sources: WorkbenchSource[]
  thinkingSummary?: string
  toolCalls: WorkbenchStreamToolCall[]
}

function toolStatus(status?: string): ThoughtChainItemType['status'] {
  switch ((status || '').toLowerCase()) {
    case 'success':
    case 'succeeded':
    case 'completed':
      return 'success'
    case 'error':
    case 'failed':
      return 'error'
    case 'skipped':
    case 'cancelled':
    case 'canceled':
      return 'abort'
    default:
      return 'loading'
  }
}

function outputPreview(item: WorkbenchStreamToolCall) {
  const preview = (item as { outputPreview?: unknown }).outputPreview
  const output = (item as { output?: unknown }).output
  if (output) return JSON.stringify(output, null, 2)
  if (typeof preview === 'string') return preview
  if (preview !== undefined) return JSON.stringify(preview, null, 2)
  if (item.outputLog) return item.outputLog
  return ''
}

function contextTags(context: AIPageContext) {
  return [
    context.sourceWorkbench,
    context.clusterId,
    context.namespace,
    context.service || context.workload || context.pod || context.node,
    context.alertId,
  ].filter(Boolean)
}

export function AIFloatingAssistantPanel({
  context,
  disabled = false,
  feedback,
  inputValue,
  messages,
  onCancel,
  onClose,
  onInputChange,
  onOpenWorkbench,
  onQuickPrompt,
  onSubmit,
  open,
  running = false,
  sources,
  thinkingSummary,
  toolCalls,
}: AIFloatingAssistantPanelProps) {
  const promptSuggestions = useMemo(() => defaultPromptSuggestions(context), [context])
  const sourceItems = useMemo(
    () =>
      sources.map((item) => ({
        key: item.id,
        title: item.title,
        url: item.url,
        description: item.summary || item.kind,
      })),
    [sources],
  )
  const chainItems = useMemo<ThoughtChainItemType[]>(() => {
    if (!toolCalls.length && !thinkingSummary) {
      return []
    }
    const thinkingItem: ThoughtChainItemType[] = thinkingSummary
      ? [
          {
            key: 'thinking',
            title: '分析思路',
            description: thinkingSummary,
            status: running ? 'loading' : 'success',
          },
        ]
      : []
    return thinkingItem.concat(
      toolCalls.map((item) => {
        const preview = outputPreview(item)
        return {
          key: item.id,
          title: item.toolName,
          description: item.summary || item.adapterId,
          status: toolStatus(item.status),
          blink: item.status === 'running',
          content: preview ? (
            <pre className="soha-ai-panel__tool-output">{preview}</pre>
          ) : undefined,
          collapsible: Boolean(preview),
        }
      }),
    )
  }, [running, thinkingSummary, toolCalls])

  if (!open) return null

  return (
    <XProvider>
      <section className="soha-ai-panel" data-testid="soha-ai-panel" aria-label="Soha AI 助手">
        <header className="soha-ai-panel__header">
          <div className="soha-ai-panel__title">
            <RobotOutlined />
            <div>
              <Text strong>全局 AI 助手</Text>
              <div className="soha-ai-panel__subtitle">{contextDisplayName(context)}</div>
            </div>
          </div>
          <Space size={4}>
            <Button
              aria-label="打开完整 AI Workbench"
              icon={<ExpandAltOutlined />}
              size="small"
              type="text"
              onClick={onOpenWorkbench}
            />
            <Button
              aria-label="关闭 AI 助手"
              icon={<CloseOutlined />}
              size="small"
              type="text"
              onClick={onClose}
            />
          </Space>
        </header>

        <div className="soha-ai-panel__context">
          <div className="soha-ai-panel__tags">
            {contextTags(context).map((item) => (
              <Tag key={String(item)}>{String(item)}</Tag>
            ))}
          </div>
          {context.promptHint ? <Text type="secondary">{context.promptHint}</Text> : null}
        </div>

        <div className="soha-ai-panel__body">
          {messages.length === 0 ? (
            <div className="soha-ai-panel__empty">
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="选择一个问题开始排查" />
              <div className="soha-ai-panel__quick-prompts">
                {promptSuggestions.map((prompt) => (
                  <Button
                    key={prompt}
                    block
                    disabled={disabled}
                    icon={<FileSearchOutlined />}
                    size="small"
                    onClick={() => onQuickPrompt(prompt)}
                  >
                    {prompt}
                  </Button>
                ))}
              </div>
            </div>
          ) : (
            <Bubble.List
              autoScroll
              items={messages.map((item) => ({
                key: item.id,
                role: item.role === 'assistant' ? 'ai' : item.role,
                content: item.content || (item.status === 'loading' ? '正在分析...' : ''),
                loading: item.status === 'loading' && !item.content,
              }))}
              role={{
                ai: { placement: 'start', avatar: <RobotOutlined />, variant: 'borderless' },
                user: { placement: 'end', variant: 'filled' },
                system: { placement: 'start', variant: 'outlined' },
              }}
              style={{ height: '100%', overflow: 'auto', paddingRight: 8 }}
            />
          )}
        </div>

        {chainItems.length > 0 ? (
          <div className="soha-ai-panel__chain">
            <ThoughtChain items={chainItems} />
          </div>
        ) : null}

        {sourceItems.length > 0 ? (
          <div className="soha-ai-panel__sources">
            <Sources
              title="证据来源"
              items={sourceItems}
              onClick={(item) => {
                if (item.url) window.open(item.url, '_blank', 'noopener,noreferrer')
              }}
            />
          </div>
        ) : null}

        {feedback}
        <div className="soha-ai-panel__sender">
          <Suggestion
            items={promptSuggestions.map((prompt) => ({ label: prompt, value: prompt }))}
            onSelect={(value) => onInputChange(value)}
          >
            {({ onKeyDown, onTrigger }) => (
              <Sender
                autoSize={{ minRows: 1, maxRows: 4 }}
                disabled={disabled}
                loading={running}
                placeholder={
                  disabled ? '当前暂不能发送，请查看状态提示' : '输入问题或使用 / 选择建议'
                }
                value={inputValue}
                onCancel={onCancel}
                onChange={(value) => {
                  onInputChange(value)
                  onTrigger(value.trim() === '/' ? {} : false)
                }}
                onKeyDown={onKeyDown}
                onSubmit={onSubmit}
              />
            )}
          </Suggestion>
        </div>
      </section>
    </XProvider>
  )
}
