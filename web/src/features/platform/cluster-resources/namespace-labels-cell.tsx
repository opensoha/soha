import { Button, Popover, Typography } from 'antd'
import { memo, useMemo, type KeyboardEvent } from 'react'
import { useDeferredOverlay } from '@/components/use-deferred-overlay'
import { MetadataTag } from '@/components/status-tag'
import { namespaceLabels } from './namespace-list-model'

export const NamespaceLabelsCell = memo(function NamespaceLabelsCell({
  labels,
  zh,
}: {
  labels: Record<string, string>
  zh: boolean
}) {
  const {
    activated,
    open,
    setOpen,
    activate,
    triggerRef: trigger,
  } = useDeferredOverlay<HTMLButtonElement>()
  const closeOnEscape = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== 'Escape') return
    event.stopPropagation()
    setOpen(false)
    trigger.current?.focus()
  }
  const entries = useMemo(() => namespaceLabels(labels), [labels])
  if (!entries.length) return <span>—</span>
  const [key, value] = entries[0]
  const button = (
    <Button
      ref={trigger}
      type="text"
      size="small"
      className="soha-namespace-label-preview"
      aria-label={zh ? `查看全部 ${entries.length} 个标签` : `View all ${entries.length} labels`}
      aria-expanded={open}
      onClick={activated ? undefined : activate}
      onKeyDown={(event) => {
        closeOnEscape(event)
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          if (activated) setOpen((current) => !current)
          else activate()
        }
      }}
    >
      <MetadataTag label={`${key}=${value}`} />
      {entries.length > 1 ? (
        <span className="soha-namespace-label-more">+{entries.length - 1}</span>
      ) : null}
    </Button>
  )
  if (!activated) return button
  return (
    <Popover
      title={zh ? `标签 · ${entries.length}` : `Labels · ${entries.length}`}
      trigger="click"
      open={open}
      onOpenChange={setOpen}
      placement="bottomLeft"
      content={
        <div className="soha-namespace-label-details" onKeyDown={closeOnEscape}>
          {entries.map(([key, value]) => (
            <div key={key} className="soha-namespace-label-entry">
              <div>
                <div className="soha-namespace-label-key">{key}</div>
                <div>{value}</div>
              </div>
              <Typography.Text copyable={{ text: `${key}=${value}` }} />
            </div>
          ))}
        </div>
      }
    >
      {button}
    </Popover>
  )
})
