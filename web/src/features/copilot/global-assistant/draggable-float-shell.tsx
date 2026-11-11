import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from 'react'

export interface FloatPosition {
  x: number
  y: number
  edge?: 'left' | 'right'
}

interface ViewportRect {
  height: number
  left?: number
  width: number
}

interface ShellSize {
  height: number
  width: number
}

interface DragState {
  pointerId: number
  startClientX: number
  startClientY: number
  startX: number
  startY: number
  dragging: boolean
}

interface DraggableFloatShellProps {
  children: ReactNode
  className?: string
  disabled?: boolean
  onDraggingChange?: (dragging: boolean) => void
  shellSize?: ShellSize
  snapToEdge?: boolean
  storageKey: string
  style?: CSSProperties
}

const DEFAULT_SHELL_SIZE: ShellSize = { width: 48, height: 48 }
const DEFAULT_MARGIN = 24
const DRAG_THRESHOLD = 5
const SNAP_THRESHOLD = 36

function browserViewport(): ViewportRect {
  if (typeof window === 'undefined') {
    return { width: 1280, height: 720 }
  }
  const sidebarRight = document.querySelector('.soha-sider')?.getBoundingClientRect().right ?? 0
  return { width: window.innerWidth, height: window.innerHeight, left: Math.max(0, sidebarRight) }
}

function availableViewport(): ViewportRect {
  const viewport = browserViewport()
  const dockTop = document
    .querySelector<HTMLElement>('.soha-realtime-session-dock:not([hidden]):not(.is-maximized)')
    ?.getBoundingClientRect().top
  if (!dockTop || dockTop >= viewport.height) return viewport
  return { ...viewport, height: dockTop }
}

export function defaultFloatPosition(
  viewport = browserViewport(),
  shellSize = DEFAULT_SHELL_SIZE,
): FloatPosition {
  return {
    x: viewport.width - shellSize.width - DEFAULT_MARGIN,
    y: viewport.height - shellSize.height - 96,
    edge: 'right',
  }
}

export function clampFloatPosition(
  position: FloatPosition,
  viewport = browserViewport(),
  shellSize = DEFAULT_SHELL_SIZE,
  margin = DEFAULT_MARGIN,
): FloatPosition {
  const minX = Math.max(margin, (viewport.left ?? 0) + margin)
  const minY = margin
  const maxX = Math.max(minX, viewport.width - shellSize.width - margin)
  const maxY = Math.max(minY, viewport.height - shellSize.height - margin)
  return {
    ...position,
    x: Math.min(Math.max(position.x, minX), maxX),
    y: Math.min(Math.max(position.y, minY), maxY),
  }
}

export function snapFloatPosition(
  position: FloatPosition,
  viewport = browserViewport(),
  shellSize = DEFAULT_SHELL_SIZE,
  threshold = SNAP_THRESHOLD,
): FloatPosition {
  const clamped = clampFloatPosition(position, viewport, shellSize)
  const leftX = Math.max(DEFAULT_MARGIN, (viewport.left ?? 0) + DEFAULT_MARGIN)
  const leftDistance = clamped.x - leftX
  const rightX = viewport.width - shellSize.width - DEFAULT_MARGIN
  const rightDistance = Math.abs(clamped.x - rightX)

  if (leftDistance <= threshold) {
    return { ...clamped, x: leftX, edge: 'left' }
  }
  if (rightDistance <= threshold) {
    return { ...clamped, x: rightX, edge: 'right' }
  }
  return clamped
}

function readStoredPosition(storageKey: string, shellSize: ShellSize) {
  if (typeof window === 'undefined') return defaultFloatPosition(undefined, shellSize)
  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return defaultFloatPosition(undefined, shellSize)
    const parsed = JSON.parse(raw) as Partial<FloatPosition>
    if (typeof parsed.x !== 'number' || typeof parsed.y !== 'number') {
      return defaultFloatPosition(undefined, shellSize)
    }
    return clampFloatPosition(
      { x: parsed.x, y: parsed.y, edge: parsed.edge },
      browserViewport(),
      shellSize,
    )
  } catch {
    return defaultFloatPosition(undefined, shellSize)
  }
}

function persistPosition(storageKey: string, position: FloatPosition) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(position))
  } catch {
    // User storage can be unavailable in private mode; drag should still work.
  }
}

export function DraggableFloatShell({
  children,
  className,
  disabled = false,
  onDraggingChange,
  shellSize = DEFAULT_SHELL_SIZE,
  snapToEdge = true,
  storageKey,
  style,
}: DraggableFloatShellProps) {
  const [position, setPosition] = useState<FloatPosition>(() =>
    readStoredPosition(storageKey, shellSize),
  )
  const [isDragging, setIsDragging] = useState(false)
  const dragRef = useRef<DragState | null>(null)
  const suppressClickRef = useRef(false)
  const shellSizeKey = `${shellSize.width}:${shellSize.height}`

  const commitPosition = useCallback(
    (nextPosition: FloatPosition) => {
      const viewport = browserViewport()
      const next = snapToEdge
        ? snapFloatPosition(nextPosition, viewport, shellSize)
        : clampFloatPosition(nextPosition, viewport, shellSize)
      setPosition(next)
      persistPosition(storageKey, next)
    },
    [shellSize, snapToEdge, storageKey],
  )

  const setDragging = useCallback(
    (next: boolean) => {
      setIsDragging(next)
      onDraggingChange?.(next)
    },
    [onDraggingChange],
  )

  useEffect(() => {
    const onResize = () => {
      setPosition((current) => {
        const next = clampFloatPosition(current, browserViewport(), shellSize)
        persistPosition(storageKey, next)
        return next
      })
    }
    window.addEventListener('resize', onResize)
    const refreshDockLayout = () => setPosition((current) => ({ ...current }))
    window.addEventListener('soha:session-dock-layout', refreshDockLayout)
    const sidebar = document.querySelector('.soha-sider')
    const observer =
      sidebar && typeof ResizeObserver !== 'undefined' ? new ResizeObserver(onResize) : null
    if (sidebar && observer) observer.observe(sidebar)
    return () => {
      window.removeEventListener('resize', onResize)
      window.removeEventListener('soha:session-dock-layout', refreshDockLayout)
      observer?.disconnect()
    }
  }, [shellSize, shellSizeKey, storageKey])

  useEffect(() => {
    if (!isDragging) return undefined

    const onPointerMove = (event: PointerEvent) => {
      const drag = dragRef.current
      if (!drag || event.pointerId !== drag.pointerId) return
      const deltaX = event.clientX - drag.startClientX
      const deltaY = event.clientY - drag.startClientY
      const nextDragging = drag.dragging || Math.hypot(deltaX, deltaY) > DRAG_THRESHOLD

      if (nextDragging && !drag.dragging) {
        suppressClickRef.current = true
        dragRef.current = { ...drag, dragging: true }
      }
      if (!nextDragging) return

      event.preventDefault()
      setPosition(
        clampFloatPosition(
          {
            x: drag.startX + deltaX,
            y: drag.startY + deltaY,
          },
          browserViewport(),
          shellSize,
        ),
      )
    }

    const onPointerUp = (event: PointerEvent) => {
      const drag = dragRef.current
      if (!drag || event.pointerId !== drag.pointerId) return
      const wasDragging = drag.dragging
      const deltaX = event.clientX - drag.startClientX
      const deltaY = event.clientY - drag.startClientY
      dragRef.current = null
      setDragging(false)
      if (wasDragging) {
        commitPosition({
          x: drag.startX + deltaX,
          y: drag.startY + deltaY,
        })
        window.setTimeout(() => {
          suppressClickRef.current = false
        }, 0)
      }
    }

    window.addEventListener('pointermove', onPointerMove, { passive: false })
    window.addEventListener('pointerup', onPointerUp)
    window.addEventListener('pointercancel', onPointerUp)
    return () => {
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', onPointerUp)
      window.removeEventListener('pointercancel', onPointerUp)
    }
  }, [commitPosition, isDragging, setDragging, shellSize])

  const shellStyle = useMemo<CSSProperties>(() => {
    const visiblePosition = clampFloatPosition(position, availableViewport(), shellSize)
    return {
      ...style,
      height: shellSize.height,
      left: 0,
      position: 'fixed',
      top: 0,
      transform: `translate3d(${Math.round(visiblePosition.x)}px, ${Math.round(visiblePosition.y)}px, 0)`,
      touchAction: disabled ? undefined : 'none',
      width: shellSize.width,
      zIndex: 950,
    }
  }, [disabled, position, shellSize, style])

  return (
    <div
      aria-live="polite"
      className={['soha-ai-global-float-shell', isDragging ? 'is-dragging' : '', className ?? '']
        .filter(Boolean)
        .join(' ')}
      data-testid="soha-ai-global-float-shell"
      onClickCapture={(event) => {
        if (!suppressClickRef.current) return
        event.preventDefault()
        event.stopPropagation()
        suppressClickRef.current = false
      }}
      onPointerDown={(event) => {
        if (disabled || event.button !== 0) return
        dragRef.current = {
          pointerId: event.pointerId,
          startClientX: event.clientX,
          startClientY: event.clientY,
          startX: position.x,
          startY: position.y,
          dragging: false,
        }
        setDragging(true)
      }}
      style={shellStyle}
    >
      {children}
    </div>
  )
}
