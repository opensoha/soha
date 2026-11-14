import { Button, Space } from 'antd'
import {
  BulbOutlined,
  FileSearchOutlined,
  OrderedListOutlined,
  ProfileOutlined,
} from '@ant-design/icons'
import type { AIGlobalAssistantAction } from './ai-context'

export interface AISelectionToolbarState {
  left: number
  top: number
  text: string
}

interface AISelectionToolbarProps {
  disabled?: boolean
  onAction: (action: AIGlobalAssistantAction) => void
  onClose: () => void
  state: AISelectionToolbarState | null
}

const TOOLBAR_ACTIONS: Array<{ action: AIGlobalAssistantAction; icon: JSX.Element; label: string }> = [
  { action: 'explain-selection', icon: <BulbOutlined />, label: '解释' },
  { action: 'troubleshoot-selection', icon: <FileSearchOutlined />, label: '排查' },
  { action: 'summarize-selection', icon: <ProfileOutlined />, label: '总结' },
  { action: 'next-steps-selection', icon: <OrderedListOutlined />, label: '下一步' },
]

export function AISelectionToolbar({ disabled = false, onAction, onClose, state }: AISelectionToolbarProps) {
  if (!state) return null

  return (
    <div
      className="soha-ai-selection-toolbar"
      data-testid="soha-ai-selection-toolbar"
      role="toolbar"
      style={{ left: state.left, top: state.top }}
      onMouseDown={(event) => event.preventDefault()}
    >
      <Space size={4}>
        {TOOLBAR_ACTIONS.map((item) => (
          <Button
            key={item.action}
            disabled={disabled}
            icon={item.icon}
            size="small"
            type="text"
            onClick={() => {
              onAction(item.action)
              onClose()
            }}
          >
            {item.label}
          </Button>
        ))}
      </Space>
    </div>
  )
}
