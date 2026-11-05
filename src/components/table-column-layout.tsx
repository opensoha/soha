import { useEffect, useRef, useState } from 'react'
import { Button, InputNumber, Select } from 'antd'

type Preferences = { key?: string; widths: Record<string, number>; visible?: string[] }
export type LayoutColumn = { id: string; label: string; width: number; min: number }

function read(key?: string): Preferences {
  try {
    const value = key
      ? JSON.parse(localStorage.getItem(`soha-table-layout:${key}`) || 'null')
      : null
    if (value?.version === 1) {
      const widths = Object.fromEntries(
        Object.entries(value.widths || {}).filter(
          ([, width]) =>
            typeof width === 'number' && Number.isFinite(width) && width >= 40 && width <= 800,
        ),
      ) as Record<string, number>
      const visible =
        Array.isArray(value.visible) && value.visible.every((id: unknown) => typeof id === 'string')
          ? value.visible
          : undefined
      return { key, widths, visible }
    }
  } catch {
    /* Storage can be unavailable; the table remains usable in memory. */
  }
  return { key, widths: {} }
}

export function useTableColumnLayout(key?: string) {
  const [preferences, setPreferences] = useState(() => read(key))
  const current = useRef(preferences)
  useEffect(() => {
    if (current.current.key === key) return
    const next = read(key)
    current.current = next
    setPreferences(next)
  }, [key])
  const update = (
    patch: Partial<Preferences> | ((previous: Preferences) => Partial<Preferences>),
    persist = true,
  ) => {
    const previous = current.current.key === key ? current.current : read(key)
    const next = { ...previous, ...(typeof patch === 'function' ? patch(previous) : patch), key }
    current.current = next
    setPreferences(next)
    if (key && persist) {
      try {
        localStorage.setItem(
          `soha-table-layout:${key}`,
          JSON.stringify({ ...next, key: undefined, version: 1 }),
        )
      } catch {
        /* Private browsing or quota must not block resizing. */
      }
    }
  }
  const effective: Preferences = preferences.key === key ? preferences : { widths: {} }
  return {
    ...effective,
    setVisible: (visible: string[]) => update({ visible }),
    setWidth: (id: string, width: number, persist = true) =>
      update((previous) => ({ widths: { ...previous.widths, [id]: width } }), persist),
    fit: () => update({ widths: {} }),
    reset: () => update({ widths: {}, visible: undefined }),
  }
}

export function ColumnResizeHandle({
  column,
  width,
  onResize,
  zh,
}: {
  column: LayoutColumn
  width: number
  onResize: (width: number, persist?: boolean) => void
  zh: boolean
}) {
  const drag = useRef<{ id: number; x: number; width: number; latest: number } | null>(null)
  const clamp = (value: number) => Math.max(column.min, Math.min(800, Math.round(value)))
  return (
    <span
      role="separator"
      tabIndex={0}
      aria-orientation="vertical"
      aria-label={zh ? `调整${column.label}列宽` : `Resize ${column.label} column`}
      aria-valuemin={column.min}
      aria-valuemax={800}
      aria-valuenow={width}
      className="soha-table-column-resizer"
      title={
        zh
          ? '拖动调整，双击适应内容；方向键调宽，Home适应内容'
          : 'Drag to resize; double-click or Home to fit; arrow keys adjust width'
      }
      onClick={(event) => event.stopPropagation()}
      onDoubleClick={(event) => {
        event.stopPropagation()
        onResize(column.width)
      }}
      onKeyDown={(event) => {
        if (!['ArrowLeft', 'ArrowRight', 'Home'].includes(event.key)) return
        event.preventDefault()
        event.stopPropagation()
        onResize(
          event.key === 'Home'
            ? column.width
            : clamp(width + (event.key === 'ArrowRight' ? 8 : -8)),
        )
      }}
      onPointerDown={(event) => {
        if (event.button !== 0) return
        event.preventDefault()
        event.stopPropagation()
        drag.current = { id: event.pointerId, x: event.clientX, width, latest: width }
        event.currentTarget.setPointerCapture(event.pointerId)
      }}
      onPointerMove={(event) => {
        const start = drag.current
        if (!start || start.id !== event.pointerId) return
        start.latest = clamp(start.width + event.clientX - start.x)
        onResize(start.latest, false)
      }}
      onPointerUp={(event) => {
        const start = drag.current
        if (!start || start.id !== event.pointerId) return
        drag.current = null
        onResize(start.latest)
        event.currentTarget.releasePointerCapture(event.pointerId)
      }}
      onPointerCancel={() => {
        if (drag.current) onResize(drag.current.width)
        drag.current = null
      }}
    />
  )
}

export function ColumnLayoutControls({
  columns,
  widths,
  setWidth,
  fit,
  reset,
  zh,
}: {
  columns: LayoutColumn[]
  widths: Record<string, number>
  setWidth: (id: string, width: number) => void
  fit: () => void
  reset: () => void
  zh: boolean
}) {
  const [selected, setSelected] = useState<string>()
  const column = columns.find((item) => item.id === selected) ?? columns[0]
  if (!column) return null
  return (
    <div className="soha-table-column-layout-controls">
      <div className="soha-table-column-layout-width">
        <Select
          size="small"
          aria-label={zh ? '调整列宽的列' : 'Column to resize'}
          value={column.id}
          options={columns.map((item) => ({ value: item.id, label: item.label }))}
          onChange={setSelected}
        />
        <InputNumber
          size="small"
          aria-label={zh ? '列宽像素' : 'Column width in pixels'}
          min={column.min}
          max={800}
          step={8}
          precision={0}
          value={Math.max(column.min, widths[column.id] ?? column.width)}
          onChange={(value) => {
            if (typeof value === 'number' && Number.isFinite(value))
              setWidth(column.id, Math.max(column.min, Math.min(800, value)))
          }}
        />
      </div>
      <div className="soha-admin-table-column-actions">
        <Button size="small" onClick={fit}>
          {zh ? '适应内容' : 'Fit content'}
        </Button>
        <Button size="small" onClick={reset}>
          {zh ? '恢复默认' : 'Reset layout'}
        </Button>
      </div>
    </div>
  )
}
