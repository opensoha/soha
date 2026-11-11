import { useState } from 'react'
import { FloatButton } from 'antd'
import {
  ExpandAltOutlined,
  HighlightOutlined,
  MessageOutlined,
  RobotOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import type { AIGlobalAssistantAction } from './ai-context'
import { DraggableFloatShell } from './draggable-float-shell'

interface AIFloatButtonProps {
  disabled?: boolean
  hasSelection?: boolean
  onAction: (action: AIGlobalAssistantAction) => void
  onOpenAssistant: () => void
  onOpenWorkbench: () => void
  running?: boolean
}

export function AIFloatButton({
  disabled = false,
  hasSelection = false,
  onAction,
  onOpenAssistant,
  onOpenWorkbench,
  running = false,
}: AIFloatButtonProps) {
  const [open, setOpen] = useState(false)
  const tooltip = disabled ? '没有 AI 助手权限' : 'AI 助手'

  return (
    <DraggableFloatShell storageKey="soha.ai.global.float.position">
      <FloatButton.Group
        badge={running ? { dot: true, color: 'processing' } : undefined}
        className="soha-ai-float-button"
        closeIcon={<RobotOutlined />}
        disabled={disabled}
        icon={<RobotOutlined />}
        open={open}
        placement="top"
        shape="circle"
        tooltip={tooltip}
        trigger="click"
        type="primary"
        onOpenChange={setOpen}
      >
        <FloatButton
          icon={<MessageOutlined />}
          tooltip="打开助手"
          onClick={() => {
            onOpenAssistant()
            setOpen(false)
          }}
        />
        <FloatButton
          icon={<SearchOutlined />}
          tooltip="分析当前页面"
          onClick={() => {
            onAction('analyze-page')
            setOpen(false)
          }}
        />
        <FloatButton
          disabled={!hasSelection}
          icon={<HighlightOutlined />}
          tooltip={hasSelection ? '分析选中内容' : '先选中页面文本'}
          onClick={() => {
            onAction('troubleshoot-selection')
            setOpen(false)
          }}
        />
        <FloatButton
          icon={<ExpandAltOutlined />}
          tooltip="打开完整 AI Workbench"
          onClick={() => {
            onOpenWorkbench()
            setOpen(false)
          }}
        />
      </FloatButton.Group>
    </DraggableFloatShell>
  )
}
