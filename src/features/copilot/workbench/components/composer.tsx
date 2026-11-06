import type { WorkbenchAgentProvider, WorkbenchMode } from '../types'
import type { AssistantChatTask } from '../types'
import { prepareAssistantAvatar, prepareChatImage } from '../avatar-image'
import {
  DownOutlined,
  ExperimentOutlined,
  PictureOutlined,
  PlusOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import { agentUnavailableReason } from '../agent-selection'
import { useMemo, useRef, useState, type RefObject, type ReactNode } from 'react'
import { App, Button, Popover, Progress, Select, Segmented, Tabs } from 'antd'
import type {
  WorkbenchModelPreferences,
  WorkbenchModelOption,
  WorkbenchImageAttachment,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { Attachments, Sender, Suggestion } from '@ant-design/x'
import type { AttachmentsRef } from '@ant-design/x/es/attachments'
import type { SenderRef } from '@ant-design/x/es/sender'
import { ContextRows, ContextUsage } from './context-usage'
import { modelContextSummary, formatTokens } from '../context-selection'
import { CHAT_COMMANDS, matchingCommands, replaceSlashToken } from '../composer-commands'

export function WorkbenchComposer({
  heldValue,
  onRestoreDraft,
  assistantTask,
  onAssistantTaskChange,
  images = [],
  onImagesChange,
  agentProviders,
  agentProviderId,
  agentMode,
  onAgentChange,
  preferences,
  onPreferencesChange,
  modelOptions,
  defaultPublicModel,
  modelError,
  modelSelectionEnabled,
  contextSummary,
  usedContextSummary = [],
  contextSource,
  contextUsage,
  contextControls,
  selectedContext,
  value,
  onChange,
  onSubmit,
  onCancel,
  disabled: unavailable,
  contextParsing = false,
  readOnly = false,
  loading,
  senderRef,
  resetVersion,
  onTools,
  onSettings,
  onAnalysis,
  onSession,
}: {
  heldValue?: string
  onRestoreDraft?: () => void
  assistantTask?: AssistantChatTask
  onAssistantTaskChange?: (task?: AssistantChatTask) => void
  images?: WorkbenchImageAttachment[]
  onImagesChange?: (images: WorkbenchImageAttachment[]) => void
  agentProviders: WorkbenchAgentProvider[]
  agentProviderId: string
  agentMode: WorkbenchMode
  onAgentChange: (id: string) => void
  preferences: WorkbenchModelPreferences
  onPreferencesChange: (value: WorkbenchModelPreferences) => void
  modelOptions: WorkbenchModelOption[]
  defaultPublicModel?: string
  modelError?: string
  modelSelectionEnabled: boolean
  contextSummary: { label: string; value: string }[]
  usedContextSummary?: { label: string; value: string }[]
  contextSource?: ReactNode
  contextUsage?: unknown
  contextControls?: ReactNode
  selectedContext?: ReactNode
  value: string
  onChange: (value: string) => void
  onSubmit: (value: string) => void
  onCancel: () => void
  disabled: boolean
  contextParsing?: boolean
  readOnly?: boolean
  loading: boolean
  senderRef: RefObject<SenderRef>
  resetVersion: number
  onTools: () => void
  onSettings: () => void
  onAnalysis: () => void
  onSession: (id?: string) => void
}) {
  const { message } = App.useApp()
  const disabled = unavailable || contextParsing
  const attachmentsRef = useRef<AttachmentsRef>(null)
  const [addOpen, setAddOpen] = useState(false)
  const [modelOpen, setModelOpen] = useState(false)
  const [contextOpen, setContextOpen] = useState(false)
  const [preparing, setPreparing] = useState(false)
  const attach = async (file: File) => {
    if (disabled || loading || preparing) return
    if (agentProviderId !== 'internal') {
      void message.warning('当前助手不支持图片，请选择内置助手')
      return
    }
    setPreparing(true)
    try {
      if (assistantTask?.kind === 'avatar') {
        const image = await prepareAssistantAvatar(file)
        onAssistantTaskChange?.({ ...assistantTask, image })
        if (!value.trim() || value.trim() === '把虚拟形象改成') onChange('用这张图片作为你的头像')
      } else onImagesChange?.([await prepareChatImage(file)])
      senderRef.current?.focus()
    } catch (cause) {
      void message.error(cause instanceof Error ? cause.message : '图片处理失败')
    } finally {
      setPreparing(false)
    }
  }
  const effectiveModel = preferences.publicModel || defaultPublicModel
  const efforts =
    modelOptions.find((item) => item.publicModel === effectiveModel)?.reasoningEfforts ?? []
  const configuredCapacity = modelContextSummary(
    modelOptions.find((item) => item.publicModel === effectiveModel),
  ).capacity
  const effortLabels = { auto: '自动', low: '轻度', medium: '标准', high: '深度' }
  const provider = agentProviders.find((item) => item.id === agentProviderId)
  const modelLabel = modelSelectionEnabled
    ? effectiveModel || '默认模型'
    : provider?.name || '助手管理'
  const effortLabel = effortLabels[preferences.reasoningEffort || 'auto']
  const context = modelContextSummary(contextUsage)
  const controlsDisabled = disabled || loading || !modelSelectionEnabled
  const [open, setOpen] = useState(false)
  const matches = useMemo(() => matchingCommands(value), [value])
  const items = useMemo(
    () =>
      matches.map((item) => ({
        value: item.value,
        label: item.label,
        icon: item.icon,
        extra: `/${item.value}`,
      })),
    [matches],
  )
  const menuOpen = open && matches.length > 0 && !disabled && !loading

  return (
    <div className="soha-ai-workbench__composer">
      <Suggestion
        block
        open={menuOpen}
        onOpenChange={setOpen}
        items={items}
        onSelect={(key) => {
          const command = CHAT_COMMANDS.find((item) => item.value === key)
          if (!command) return
          onChange(replaceSlashToken(value, 'prompt' in command ? command.prompt : ''))
          setOpen(false)
          if ('action' in command) {
            if (command.action === 'session') onSession()
            else if (command.action === 'tools') onTools()
            else if (command.action === 'context') {
              setContextOpen(true)
              setModelOpen(false)
              setAddOpen(false)
            } else if (command.action === 'analysis') onAnalysis()
            else onSettings()
          } else senderRef.current?.focus()
        }}
      >
        {({ onKeyDown }) => (
          <Sender
            key={resetVersion}
            ref={senderRef}
            autoSize={{ minRows: 1, maxRows: 12 }}
            placeholder="消息"
            value={value}
            disabled={disabled || preparing}
            readOnly={readOnly}
            loading={loading}
            onPasteFile={(files) => {
              if (files[0]) void attach(files[0])
            }}
            prefix={
              <div onKeyDown={(event) => event.stopPropagation()}>
                <Attachments
                  ref={attachmentsRef}
                  disabled={disabled || loading || preparing}
                  accept="image/png,image/jpeg,image/gif,image/webp"
                  maxCount={1}
                  items={[]}
                  getDropContainer={() => senderRef.current?.nativeElement}
                  beforeUpload={(file) => {
                    void attach(file)
                    return false
                  }}
                >
                  <span hidden />
                </Attachments>
                <Popover
                  fresh
                  trigger="click"
                  placement="topLeft"
                  arrow={false}
                  open={addOpen && !unavailable && !loading}
                  onOpenChange={setAddOpen}
                  content={
                    <div
                      className="soha-ai-workbench__add-menu"
                      onKeyDown={(event) => {
                        if (event.key === 'Escape') {
                          setAddOpen(false)
                          senderRef.current?.focus()
                        }
                      }}
                    >
                      <Button
                        type="text"
                        icon={<PictureOutlined />}
                        aria-label="添加图片"
                        disabled={disabled || loading || preparing || !onImagesChange}
                        onClick={() => {
                          attachmentsRef.current?.select()
                          setAddOpen(false)
                        }}
                      >
                        添加图片
                      </Button>
                      {contextControls}
                      <Button
                        type="text"
                        icon={<ExperimentOutlined aria-hidden />}
                        disabled={disabled || loading}
                        onClick={() => {
                          setAddOpen(false)
                          onAnalysis()
                        }}
                      >
                        专业分析任务
                      </Button>
                      <Button
                        type="text"
                        icon={<ToolOutlined aria-hidden />}
                        disabled={disabled || loading}
                        onClick={() => {
                          setAddOpen(false)
                          onTools()
                        }}
                      >
                        工具与技能
                      </Button>
                    </div>
                  }
                >
                  <Button
                    type="text"
                    shape="circle"
                    icon={<PlusOutlined />}
                    aria-label="添加内容"
                    aria-expanded={addOpen}
                    loading={preparing}
                    disabled={disabled || loading}
                  />
                </Popover>
              </div>
            }
            header={
              <>
                {images.map((image) => (
                  <div key={image.id} className="soha-ai-assistant-chat-task is-chat-image">
                    <img src={image.dataUrl} alt={image.name} />
                    <span>{image.name}</span>
                    <Button
                      type="text"
                      size="small"
                      disabled={loading}
                      aria-label={`移除图片 ${image.name}`}
                      onClick={() => onImagesChange?.([])}
                    >
                      ×
                    </Button>
                  </div>
                ))}
                {Boolean(heldValue) && (
                  <div className="soha-ai-assistant-chat-task">
                    <span>{heldValue ? '原草稿已保留' : '在聊天中定制助手'}</span>
                    <Button type="text" size="small" disabled={loading} onClick={onRestoreDraft}>
                      {heldValue ? '返回原草稿' : '退出定制'}
                    </Button>
                  </div>
                )}
                {assistantTask?.kind === 'avatar' && (
                  <div className="soha-ai-assistant-chat-task">
                    {assistantTask.image && (
                      <img
                        src={assistantTask.image.avatarPosterUrl || assistantTask.image.avatarUrl}
                        alt="待发送的形象图片"
                      />
                    )}
                    <span>
                      {assistantTask.image
                        ? '形象参考图'
                        : assistantTask.useCurrentAvatar
                          ? '继续调整当前形象'
                          : '调整助手形象'}
                    </span>
                    <Button
                      type="text"
                      size="small"
                      disabled={loading}
                      aria-label="退出形象调整"
                      onClick={() => onAssistantTaskChange?.(undefined)}
                    >
                      ×
                    </Button>
                  </div>
                )}
                {selectedContext && (
                  <div className="soha-ai-workbench__composer-context">{selectedContext}</div>
                )}
              </>
            }
            onChange={(next) => {
              onChange(next)
              setOpen(true)
            }}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) {
                event.stopPropagation()
                return false
              }
              onKeyDown(event)
              if (menuOpen && event.key === 'Enter') return false
            }}
            onSubmit={(text) => {
              if (menuOpen) return
              const reference = text.trim().match(/^\/session\s+(\S+)(?:\s+([\s\S]*))?$/)
              if (reference) {
                onChange(reference[2] || '')
                onSession(reference[1])
              } else onSubmit(text)
            }}
            onCancel={onCancel}
            suffix={(_, { components: { SendButton, LoadingButton } }) => (
              <div
                className="soha-ai-workbench__composer-actions"
                onKeyDown={(event) => event.stopPropagation()}
              >
                <Popover
                  trigger="click"
                  placement="top"
                  arrow={false}
                  fresh
                  open={contextOpen}
                  onOpenChange={(next) => {
                    setContextOpen(next)
                    if (next) {
                      setModelOpen(false)
                      setAddOpen(false)
                    }
                  }}
                  title="上下文用量"
                  content={
                    <div
                      className="soha-ai-workbench__context-popover"
                      onKeyDown={(event) => {
                        if (event.key === 'Escape') {
                          setContextOpen(false)
                          senderRef.current?.focus()
                        }
                      }}
                    >
                      <ContextUsage value={contextUsage} />
                      <strong>背景与来源</strong>
                      <Tabs
                        items={[
                          {
                            key: 'next',
                            label: '下次发送',
                            children: <ContextRows items={contextSummary} />,
                          },
                          {
                            key: 'used',
                            label: '最近实际使用',
                            children: usedContextSummary.length ? (
                              <ContextRows items={usedContextSummary} />
                            ) : (
                              <p>尚无背景快照。没有额外资料不代表没有系统提示与历史消息。</p>
                            ),
                          },
                        ]}
                      />
                      {contextSource}
                    </div>
                  }
                >
                  <Button
                    type="text"
                    shape="circle"
                    className={
                      'soha-ai-workbench__context-trigger' +
                      (context.percent === undefined ? ' is-unknown' : '')
                    }
                    title={context.label}
                    aria-label="上下文用量与背景来源"
                    aria-expanded={contextOpen}
                    onKeyDown={(event) => {
                      if (event.key === 'Escape') {
                        setContextOpen(false)
                        senderRef.current?.focus()
                      }
                    }}
                  >
                    <Progress
                      aria-hidden="true"
                      type="circle"
                      size={14}
                      strokeWidth={12}
                      status="normal"
                      showInfo={false}
                      percent={Math.min(100, context.percent ?? 0)}
                      steps={context.percent === undefined ? { count: 8, gap: 8 } : undefined}
                      strokeColor="currentColor"
                      railColor={
                        context.percent === undefined ? 'currentColor' : 'var(--soha-border-color)'
                      }
                    />
                  </Button>
                </Popover>
                <Popover
                  fresh
                  trigger="click"
                  placement="topRight"
                  arrow={false}
                  open={modelOpen && !disabled && !loading}
                  onOpenChange={setModelOpen}
                  content={
                    <div
                      className="soha-ai-workbench__model-menu"
                      onKeyDown={(event) => {
                        if (event.key === 'Escape') {
                          setModelOpen(false)
                          senderRef.current?.focus()
                        }
                      }}
                    >
                      <label className="soha-ai-workbench__menu-label">助手</label>
                      <Select
                        aria-label="选择助手"
                        value={agentProviderId}
                        disabled={disabled || loading}
                        style={{ width: '100%' }}
                        onChange={onAgentChange}
                        options={agentProviders.map((item) => ({
                          value: item.id,
                          label:
                            item.id === 'internal'
                              ? '模型直连（兼容）'
                              : item.name +
                                (agentUnavailableReason(item, agentMode)
                                  ? ' · ' + agentUnavailableReason(item, agentMode)
                                  : ''),
                          disabled: !!agentUnavailableReason(item, agentMode),
                        }))}
                      />
                      {modelSelectionEnabled ? (
                        <>
                          <label className="soha-ai-workbench__menu-label">模型</label>
                          <Select
                            aria-label="选择模型"
                            showSearch={{ optionFilterProp: 'label' }}
                            value={preferences.publicModel || ''}
                            disabled={controlsDisabled}
                            style={{ width: '100%' }}
                            onChange={(publicModel) =>
                              onPreferencesChange({
                                publicModel: publicModel || undefined,
                                reasoningEffort: 'auto',
                              })
                            }
                            options={[
                              {
                                value: '',
                                label: defaultPublicModel
                                  ? `默认 · ${defaultPublicModel}`
                                  : '默认模型',
                              },
                              ...modelOptions.map((item) => ({
                                value: item.publicModel,
                                label: item.publicModel,
                              })),
                            ]}
                          />
                          <label className="soha-ai-workbench__menu-label">思考强度</label>
                          <Segmented
                            block
                            aria-label="思考强度"
                            value={preferences.reasoningEffort || 'auto'}
                            disabled={controlsDisabled || !efforts.length}
                            options={['auto' as const, ...efforts].map((effort) => ({
                              value: effort,
                              label: effortLabels[effort],
                            }))}
                            onChange={(reasoningEffort) =>
                              onPreferencesChange({
                                ...preferences,
                                reasoningEffort:
                                  reasoningEffort as WorkbenchModelPreferences['reasoningEffort'],
                              })
                            }
                          />
                          {!efforts.length && (
                            <span className="soha-ai-workbench__menu-label">
                              当前模型自动管理思考强度
                            </span>
                          )}
                          <span className="soha-ai-workbench__menu-label">
                            配置窗口：
                            {configuredCapacity
                              ? formatTokens(configuredCapacity) + ' tokens'
                              : '未配置'}
                          </span>
                          {modelError && <span role="alert">{modelError}</span>}
                        </>
                      ) : (
                        <span className="soha-ai-workbench__menu-label">模型由助手管理</span>
                      )}
                    </div>
                  }
                >
                  <Button
                    type="text"
                    className="soha-ai-workbench__model-trigger"
                    aria-label="模型与思考强度"
                    aria-expanded={modelOpen}
                    onKeyDown={(event) => {
                      if (event.key === 'Escape') {
                        setModelOpen(false)
                        senderRef.current?.focus()
                      }
                    }}
                    disabled={disabled || loading}
                  >
                    <span className="soha-ai-workbench__model-name" title={modelLabel}>
                      {modelLabel}
                    </span>
                    {modelSelectionEnabled && (
                      <span className="soha-ai-workbench__model-effort">{effortLabel}</span>
                    )}
                    <DownOutlined />
                  </Button>
                </Popover>
                {loading ? (
                  <LoadingButton aria-label="停止回复" />
                ) : (
                  <SendButton aria-label="发送消息" />
                )}
              </div>
            )}
          />
        )}
      </Suggestion>
    </div>
  )
}
