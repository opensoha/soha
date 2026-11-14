import type { CSSProperties, ReactNode } from 'react'
import './workflow-canvas-surface.css'

interface WorkflowCanvasSurfaceProps {
  children: ReactNode
  className?: string
  height?: number | string
  role?: string
  style?: CSSProperties
}

function classNames(...items: Array<string | false | null | undefined>) {
  return items.filter(Boolean).join(' ')
}

function formatSize(value?: number | string) {
  if (typeof value === 'number') return `${value}px`
  return value
}

export function WorkflowCanvasSurface({
  children,
  className,
  height,
  role,
  style,
}: WorkflowCanvasSurfaceProps) {
  const surfaceStyle = {
    ...style,
    ...(height ? { '--soha-workflow-canvas-height': formatSize(height) } : {}),
  } as CSSProperties

  return (
    <div
      className={classNames('soha-workflow-canvas-surface', className)}
      role={role}
      style={surfaceStyle}
    >
      {children}
    </div>
  )
}
