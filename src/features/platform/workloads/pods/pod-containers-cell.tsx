import { Popover } from 'antd'
import { useEffect, useRef, useState } from 'react'
import type { PodContainerStatus } from './types'
import { containerLabel, containerTone } from './container-status'

export function PodContainersCell({
  statuses,
  readyContainers,
  zh,
}: {
  statuses?: PodContainerStatus[]
  readyContainers: string
  zh: boolean
}) {
  const [open, setOpen] = useState(false)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const detailRef = useRef<HTMLDivElement>(null)
  const focusFrame = useRef(0)
  useEffect(() => () => cancelAnimationFrame(focusFrame.current), [])
  const closeDetails = () => {
    cancelAnimationFrame(focusFrame.current)
    setOpen(false)
  }
  const label = zh ? '容器状态' : 'Container states'
  const summary = `${zh ? '常规容器就绪' : 'App containers ready'} ${readyContainers || '—'}`
  const details = (
    <div
      ref={detailRef}
      className="soha-pod-container-details"
      role="region"
      aria-label={label}
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key !== 'Escape') return
        event.preventDefault()
        event.stopPropagation()
        closeDetails()
        buttonRef.current?.focus()
      }}
    >
      <div>{summary}</div>
      {statuses?.length ? (
        statuses.map((status) => (
          <div key={status.name} className="soha-pod-container-detail">
            <span className={`soha-pod-container-square is-${containerTone(status)}`} aria-hidden />
            <span>{containerLabel(status, zh)}</span>
          </div>
        ))
      ) : (
        <div>{zh ? '当前后端未提供逐容器状态' : 'Per-container states were not provided'}</div>
      )}
    </div>
  )
  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) cancelAnimationFrame(focusFrame.current)
        setOpen(nextOpen)
      }}
      trigger={['hover', 'click']}
      content={details}
    >
      <button
        ref={buttonRef}
        type="button"
        className="soha-pod-container-cell"
        aria-label={`${label}：${summary}；${statuses?.length ? statuses.map((status) => containerLabel(status, zh)).join('；') : zh ? '逐容器状态未知' : 'Per-container states unknown'}`}
        aria-expanded={open}
        onKeyDown={(event) => {
          if (event.key === 'Escape') {
            event.preventDefault()
            closeDetails()
            return
          }
          if (!['Enter', ' '].includes(event.key)) return
          event.preventDefault()
          setOpen(true)
          cancelAnimationFrame(focusFrame.current)
          focusFrame.current = requestAnimationFrame(() => {
            if (document.activeElement === buttonRef.current) detailRef.current?.focus()
          })
        }}
      >
        {statuses?.length ? (
          statuses
            .slice(0, 6)
            .map((status) => (
              <span
                key={status.name}
                className={`soha-pod-container-square is-${containerTone(status)}`}
                aria-hidden
              />
            ))
        ) : (
          <span className="soha-pod-container-square is-unknown" aria-hidden />
        )}
        {(statuses?.length ?? 0) > 6 ? (
          <span className="soha-pod-container-overflow" aria-hidden>
            …
          </span>
        ) : null}
      </button>
    </Popover>
  )
}
