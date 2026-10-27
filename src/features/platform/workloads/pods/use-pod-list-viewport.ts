import { useCallback, useLayoutEffect, useRef } from 'react'
import type { GetRef, Table, TableProps } from 'antd'
import type { Pod } from './types'

type Viewport = { key?: string; offset: number; top: number }

export function usePodListViewport(pods: Pod[], resetKey: string) {
  const tableRef = useRef<GetRef<typeof Table>>(null)
  const viewport = useRef<Viewport>()
  const previousResetKey = useRef(resetKey)

  const resetPosition = useCallback(() => {
    viewport.current = undefined
    tableRef.current?.scrollTo({ top: 0 })
  }, [])

  const onScroll: NonNullable<TableProps['onScroll']> = useCallback((event) => {
    const holder = event.currentTarget
    const bounds = holder.getBoundingClientRect()
    const anchor = Array.from(holder.querySelectorAll<HTMLElement>('[data-row-key]')).find(
      (row) => row.getBoundingClientRect().bottom > bounds.top + 1,
    )
    viewport.current = {
      key: anchor?.dataset.rowKey,
      offset: anchor ? anchor.getBoundingClientRect().top - bounds.top : 0,
      top: holder.scrollTop,
    }
  }, [])

  useLayoutEffect(() => {
    if (previousResetKey.current !== resetKey || pods.length === 0) {
      previousResetKey.current = resetKey
      resetPosition()
      return
    }
    const saved = viewport.current
    if (!saved) return
    // Preserve the resource, not its array index: new Pods can sort before the visible row.
    if (pods.some((pod) => `${pod.namespace}/${pod.name}` === saved.key)) {
      tableRef.current?.scrollTo({ key: saved.key, align: 'start', offset: saved.offset })
    } else {
      tableRef.current?.scrollTo({ top: saved.top })
    }
  }, [pods, resetKey, resetPosition])

  return { tableRef, onScroll, resetPosition }
}
