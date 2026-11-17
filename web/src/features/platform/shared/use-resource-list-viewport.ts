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
  const scrollHolder = useRef<HTMLElement>()
  const captureFrame = useRef(0)
  const previousResetKey = useRef(resetKey)
  const previousKeys = useRef<string[]>()
  const resetPosition = useCallback(() => {
    cancelAnimationFrame(captureFrame.current)
    viewport.current = undefined
    tableRef.current?.scrollTo({ top: 0 })
  }, [])
  const onScroll: NonNullable<TableProps['onScroll']> = useCallback((event) => {
    const holder = event.currentTarget
    scrollHolder.current = holder
    const saved = viewport.current
    // A refresh can commit before the capture frame; keep the latest user scroll delta.
    viewport.current = {
      key: saved?.key,
      offset: saved ? saved.offset + saved.top - holder.scrollTop : 0,
      top: holder.scrollTop,
    }
    cancelAnimationFrame(captureFrame.current)
    // Virtual rows commit after the scroll callback; capture the completed layout.
    captureFrame.current = requestAnimationFrame(() => {
      captureFrame.current = 0
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
    const key = keyRef.current
    const keys = rows.map((row) =>
      String(typeof key === 'function' ? key(row) : (row as Record<string, unknown>)[key]),
    )
    const previous = previousKeys.current
    previousKeys.current = keys
    if (previousResetKey.current !== resetKey || rows.length === 0) {
      previousResetKey.current = resetKey
      resetPosition()
      return
    }
    const saved = viewport.current
    if (!saved) return
    if (previous?.length === keys.length && keys.every((key, index) => key === previous[index])) {
      const holder = scrollHolder.current
      if (saved.key === undefined || !holder?.isConnected) return
      // Antd may reorder the visible rows locally while the input key sequence stays unchanged.
      const anchor = Array.from(holder.querySelectorAll<HTMLElement>('[data-row-key]')).find(
        (row) => row.dataset.rowKey === saved.key,
      )
      if (
        anchor &&
        Math.abs(
          anchor.getBoundingClientRect().top - holder.getBoundingClientRect().top - saved.offset,
        ) < 1
      )
        return
    }
    const exists = saved.key !== undefined && keys.includes(saved.key)
    if (exists) tableRef.current?.scrollTo({ key: saved.key, align: 'start', offset: saved.offset })
    else tableRef.current?.scrollTo({ top: saved.top })
  }, [rows, resetKey, resetPosition])
  return { tableRef, onScroll, resetPosition }
}
