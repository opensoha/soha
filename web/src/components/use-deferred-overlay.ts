import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type SetStateAction,
} from 'react'

export function useDeferredOverlay<T extends HTMLElement>() {
  const triggerRef = useRef<T>(null)
  const restoreFocus = useRef(false)
  const hoverTimer = useRef<ReturnType<typeof setTimeout>>()
  const [activated, setActivated] = useState(false)
  const [open, updateOpen] = useState(false)
  const cancelHover = useCallback(() => {
    clearTimeout(hoverTimer.current)
    hoverTimer.current = undefined
  }, [])
  const setOpen = useCallback(
    (next: SetStateAction<boolean>) => {
      cancelHover()
      updateOpen(next)
    },
    [cancelHover],
  )
  const activate = useCallback(() => {
    cancelHover()
    if (!activated) restoreFocus.current = document.activeElement === triggerRef.current
    setActivated(true)
    setOpen(true)
  }, [activated, cancelHover, setOpen])
  const activateOnHover = useCallback(() => {
    if (activated) return
    cancelHover()
    // Preserve Antd's delay: a quick hover followed by click must open, not toggle closed.
    hoverTimer.current = setTimeout(activate, 100)
  }, [activated, activate, cancelHover])
  useEffect(() => cancelHover, [cancelHover])
  useLayoutEffect(() => {
    if (activated && restoreFocus.current && document.activeElement === document.body)
      triggerRef.current?.focus()
    restoreFocus.current = false
  }, [activated])
  return { triggerRef, activated, open, setOpen, activate, activateOnHover, cancelHover }
}
