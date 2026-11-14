import { Button, Divider } from 'antd'
import {
  ExpandAltOutlined,
  FileSearchOutlined,
  HighlightOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import type { AIGlobalAssistantAction, AIPageContext } from './ai-context'

export interface AIContextMenuState {
  contextOverride?: Partial<AIPageContext>
  left: number
  top: number
  hasSelection: boolean
}

interface AIContextMenuProps {
  disabled?: boolean
  onAction: (action: AIGlobalAssistantAction) => void
  onClose: () => void
  onOpenWorkbench: () => void
  state: AIContextMenuState | null
}

export function AIContextMenu({
  disabled = false,
  onAction,
  onClose,
  onOpenWorkbench,
  state,
}: AIContextMenuProps) {
  if (!state) return null

  return (
    <div
      className="soha-ai-context-menu"
      data-testid="soha-ai-context-menu"
      role="menu"
      style={{ left: state.left, top: state.top }}
      onMouseDown={(event) => event.preventDefault()}
    >
      <Button
        block
        disabled={disabled}
        icon={<SearchOutlined />}
        role="menuitem"
        size="small"
        type="text"
        onClick={() => {
          onAction('analyze-page')
          onClose()
        }}
      >
        AI 分析当前页面
      </Button>
      <Button
        block
        disabled={disabled}
        icon={<FileSearchOutlined />}
        role="menuitem"
        size="small"
        type="text"
        onClick={() => {
          onAction('troubleshoot-resource')
          onClose()
        }}
      >
        AI 排查这个资源
      </Button>
      <Button
        block
        disabled={disabled || !state.hasSelection}
        icon={<HighlightOutlined />}
        role="menuitem"
        size="small"
        type="text"
        onClick={() => {
          onAction('troubleshoot-selection')
          onClose()
        }}
      >
        AI 分析选中内容
      </Button>
      <Divider className="soha-ai-context-menu__divider" />
      <Button
        block
        icon={<ExpandAltOutlined />}
        role="menuitem"
        size="small"
        type="text"
        onClick={() => {
          onOpenWorkbench()
          onClose()
        }}
      >
        打开完整 AI Workbench
      </Button>
    </div>
  )
}
