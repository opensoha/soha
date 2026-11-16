import { Alert, type AlertProps } from 'antd'
import './context-feedback.css'

// Persistent explanations and resource/task states stay with their owning region.
export function ContextFeedback({ className, showIcon = true, ...props }: AlertProps) {
  return (
    <Alert
      {...props}
      showIcon={showIcon}
      className={['soha-context-feedback', className].filter(Boolean).join(' ')}
    />
  )
}
