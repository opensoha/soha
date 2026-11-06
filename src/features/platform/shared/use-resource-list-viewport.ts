import { useCallback, useLayoutEffect, useRef } from 'react'
import type { GetRef, Table, TableProps } from 'antd'

type Viewport = { key?: string; offset: number; top: number }

export function useResourceListViewport<T>(
  rows: T[],
  rowKey: string | ((record: T) => string),
  resetKey: string,
) {
  const tableRef = useRef<GetRef<typeof Table>>(null)
  const keyRef = useRef(rowKey)
  keyRef.current = rowKey
  const viewport = useRef<Viewport>()
  const captureFrame = useRef(0)
  const previousResetKey = useRef(resetKey)
  const resetPosition = useCallback(() => {
    cancelAnimationFrame(captureFrame.current)
    viewport.current = undefined
    tableRef.current?.scrollTo({ top: 0 })
  }, [])
  const onScroll: NonNullable<TableProps['onScroll']> = useCallback((event) => {
    const holder = event.currentTarget
    cancelAnimationFrame(captureFrame.current)
    // Virtual rows commit after the scroll callback; capture the completed layout.
    captureFrame.current = requestAnimationFrame(() => {
      if (!holder.isConnected) return
      const bounds = holder.getBoundingClientRect()
      const anchor = Array.from(holder.querySelectorAll<HTMLElement>('[data-row-key]')).find(
        (row) => row.getBoundingClientRect().bottom > bounds.top + 1,
      )
      viewport.current = {
        key: anchor?.dataset.rowKey,
        offset: anchor ? anchor.getBoundingClientRect().top - bounds.top : 0,
        top: holder.scrollTop,
      }
    })
  }, [])
  useLayoutEffect(() => () => cancelAnimationFrame(captureFrame.current), [])
  useLayoutEffect(() => {
    if (previousResetKey.current !== resetKey || rows.length === 0) {
      previousResetKey.current = resetKey
      resetPosition()
      return
    }
    const saved = viewport.current
    if (!saved) return
    const key = keyRef.current
    const exists = rows.some(
      (row) =>
        String(typeof key === 'function' ? key(row) : (row as Record<string, unknown>)[key]) ===
        saved.key,
    )
    if (exists) tableRef.current?.scrollTo({ key: saved.key, align: 'start', offset: saved.offset })
    else tableRef.current?.scrollTo({ top: saved.top })
  }, [rows, resetKey, resetPosition])
  return { tableRef, onScroll, resetPosition }
}
