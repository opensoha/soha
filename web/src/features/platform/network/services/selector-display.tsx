import { MetadataTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { Button, Popover, Typography } from 'antd'
import { useEffect, useId, useRef, useState } from 'react'
import { Link } from 'react-router-dom'

export function ServiceSelectorDisplay({
  selector,
  compact = false,
  backendPath,
  backendLabel,
}: {
  selector?: Record<string, string>
  compact?: boolean
  backendPath?: string
  backendLabel?: string
}) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const entries = Object.entries(selector || {}).sort(([a], [b]) => a.localeCompare(b))
  const identity = JSON.stringify([backendPath, entries])
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const id = useId()
  useEffect(() => setOpen(false), [identity])
  useEffect(() => {
    if (!open) return
    const frame = requestAnimationFrame(() =>
      content.current?.querySelector<HTMLElement>('button')?.focus(),
    )
    return () => cancelAnimationFrame(frame)
  }, [open])
  const close = () => {
    setOpen(false)
    trigger.current?.focus()
  }
  const tags = (items: typeof entries, small = false) => (
    <span className={`soha-service-selector-tags${small ? ' is-compact' : ''}`}>
      {items.map(([key, value]) => (
        <MetadataTag
          key={key}
          label={
            <span className="soha-service-tag-text">
              {key}={value}
            </span>
          }
          title={`${key}=${value}`}
          className="soha-service-selector-tag"
        />
      ))}
    </span>
  )
  if (!entries.length)
    return backendPath ? (
      <Link to={backendPath} aria-label={backendLabel}>
        {zh ? '无选择器' : 'No selector'}
      </Link>
    ) : zh ? (
      '无选择器'
    ) : (
      'No selector'
    )
  if (!compact) return tags(entries)
  return (
    <div className="soha-service-selector-summary">
      {backendPath ? (
        <Link className="soha-service-selector-link" to={backendPath} aria-label={backendLabel}>
          {tags(entries.slice(0, 2), true)}
        </Link>
      ) : (
        tags(entries.slice(0, 2), true)
      )}
      <Popover
        open={open}
        trigger="click"
        placement="leftTop"
        onOpenChange={setOpen}
        destroyOnHidden
        content={
          <div
            ref={content}
            id={id}
            className="soha-service-selector-expanded"
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                event.stopPropagation()
                close()
              }
            }}
          >
            <div className="soha-service-selector-heading">
              <Typography.Text strong>
                {zh ? `选择器 · ${entries.length}` : `Selectors · ${entries.length}`}
              </Typography.Text>
              <Button size="small" type="text" onClick={close}>
                {zh ? '收起' : 'Close'}
              </Button>
            </div>
            {tags(entries)}
            {backendPath && (
              <Link to={backendPath} onClick={() => setOpen(false)}>
                {zh ? '查看匹配后端' : 'View matching backends'}
              </Link>
            )}
          </div>
        }
      >
        <Button
          ref={trigger}
          type="text"
          size="small"
          className="soha-service-selector-more"
          aria-label={zh ? '查看完整选择器' : 'View full selector'}
          aria-expanded={open}
          aria-controls={open ? id : undefined}
          onKeyDown={(event) => {
            if (event.key === 'Escape') close()
          }}
        >
          {entries.length > 2 ? `+${entries.length - 2}` : zh ? '展开' : 'Expand'}
        </Button>
      </Popover>
    </div>
  )
}
