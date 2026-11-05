import { Button, Popover, Typography } from 'antd'
import { useRef, useState, type KeyboardEvent } from 'react'
import { MetadataTag } from '@/components/status-tag'
import { namespaceLabels } from './namespace-list-model'

export function NamespaceLabelsCell({
  labels,
  zh,
}: {
  labels: Record<string, string>
  zh: boolean
}) {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const closeOnEscape = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== 'Escape') return
    event.stopPropagation()
    setOpen(false)
    trigger.current?.focus()
  }
  const entries = namespaceLabels(labels)
  if (!entries.length) return <span>—</span>
  const [key, value] = entries[0]
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
      <Button
        ref={trigger}
        type="text"
        size="small"
        className="soha-namespace-label-preview"
        aria-label={zh ? `查看全部 ${entries.length} 个标签` : `View all ${entries.length} labels`}
        aria-expanded={open}
        onKeyDown={closeOnEscape}
      >
        <MetadataTag label={`${key}=${value}`} />
        {entries.length > 1 ? (
          <span className="soha-namespace-label-more">+{entries.length - 1}</span>
        ) : null}
      </Button>
    </Popover>
  )
}
