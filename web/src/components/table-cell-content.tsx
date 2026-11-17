import { memo, useEffect, type MouseEventHandler } from 'react'
import { useDeferredOverlay } from './use-deferred-overlay'
import { Button, Tooltip } from 'antd'
import type { TooltipProps } from 'antd'

const tooltipStyles = {
  root: { maxWidth: 'min(640px, 78vw)' },
  container: { overflowWrap: 'anywhere', whiteSpace: 'normal' },
} satisfies TooltipProps['styles']

function displayValue(value?: null | number | string) {
  return value === null || value === undefined || value === '' ? '-' : String(value)
}

export const TableCellText = memo(function TableCellText({
  className,
  value,
}: {
  className?: string
  value?: null | number | string
}) {
  const display = displayValue(value)
  const {
    triggerRef: textRef,
    activated,
    open,
    setOpen,
    activate,
  } = useDeferredOverlay<HTMLSpanElement>()
  const hasOverflow = () => {
    const text = textRef.current
    return display !== '-' && Boolean(text && text.scrollWidth > text.clientWidth)
  }
  useEffect(() => {
    if (!open) return
    const text = textRef.current
    if (!hasOverflow()) {
      setOpen(false)
      return
    }
    // Closed cells need no resize observers; recheck only an already visible popup.
    const observer =
      typeof ResizeObserver === 'undefined'
        ? null
        : new ResizeObserver(() => {
            if (!hasOverflow()) setOpen(false)
          })
    if (text) observer?.observe(text)
    return () => observer?.disconnect()
  }, [display, open])
  const text = (
    <span
      ref={textRef}
      onMouseEnter={
        !activated
          ? () => {
              if (hasOverflow()) activate()
            }
          : undefined
      }
      className={['soha-table-cell-text', className].filter(Boolean).join(' ')}
      style={{
        display: 'inline-block',
        maxWidth: '100%',
        overflow: 'hidden',
        textOverflow: 'ellipsis',
        whiteSpace: 'nowrap',
        verticalAlign: 'bottom',
        color: 'inherit',
        fontWeight: 'inherit',
        lineHeight: 'inherit',
      }}
    >
      {display}
    </span>
  )
  if (display === '-' || !activated) return text
  return (
    <Tooltip
      title={display}
      placement="topLeft"
      styles={tooltipStyles}
      open={open}
      onOpenChange={(next) => setOpen(next && hasOverflow())}
    >
      {text}
    </Tooltip>
  )
})

export function TableCellLink({
  label,
  onClick,
}: {
  label: string
  onClick: MouseEventHandler<HTMLElement>
}) {
  return (
    <Button
      type="text"
      style={{ maxWidth: '100%' }}
      styles={{ content: { minWidth: 0 } }}
      onClick={onClick}
    >
      <TableCellText value={label} />
    </Button>
  )
}
