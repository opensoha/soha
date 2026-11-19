import { memo, useCallback, useEffect, type MouseEventHandler } from 'react'
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
  const hasOverflow = useCallback(() => {
    const text = textRef.current
    return display !== '-' && Boolean(text && text.scrollWidth > text.clientWidth)
  }, [display, textRef])
  useEffect(() => {
    // Keep the existing link or button as the focus target and measure only on focus.
    const focusTarget = textRef.current?.closest<HTMLElement>('a[href], button')
    if (!focusTarget) return
    const showOnFocus = () => {
      // A pointer click must reach the original text while the tooltip is still deferred.
      if (focusTarget.matches(':focus-visible') && hasOverflow()) activate()
    }
    const hideOnBlur = () => setOpen(false)
    focusTarget.addEventListener('focus', showOnFocus)
    focusTarget.addEventListener('blur', hideOnBlur)
    return () => {
      focusTarget.removeEventListener('focus', showOnFocus)
      focusTarget.removeEventListener('blur', hideOnBlur)
    }
  }, [activate, hasOverflow, setOpen, textRef])
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
    const focusTarget = text?.closest<HTMLElement>('a[href], button')
    const hideOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    focusTarget?.addEventListener('keydown', hideOnEscape)
    if (text) observer?.observe(text)
    return () => {
      focusTarget?.removeEventListener('keydown', hideOnEscape)
      observer?.disconnect()
    }
  }, [hasOverflow, open, setOpen, textRef])
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
