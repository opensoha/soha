import { useDeferredOverlay } from '@/components/use-deferred-overlay'
import { Tag, Tooltip, Typography } from 'antd'
import { memo } from 'react'

const { Text } = Typography

const NetworkTextList = memo(function NetworkTextList({
  value,
  empty,
}: {
  value?: string[]
  empty: string
}) {
  const { triggerRef, activated, open, setOpen, activate, activateOnHover, cancelHover } =
    useDeferredOverlay<HTMLSpanElement>()
  if (!value?.length) return <Text type="secondary">{empty}</Text>
  const overflow =
    value.length > 3 ? (
      <span
        ref={triggerRef}
        tabIndex={0}
        onMouseEnter={activateOnHover}
        onMouseLeave={cancelHover}
        onFocus={activated ? undefined : activate}
        onClick={activated ? undefined : activate}
        onKeyDown={(event) => {
          if (event.key === 'Escape') setOpen(false)
        }}
      >
        <Tag>{`+${value.length - 3}`}</Tag>
      </span>
    ) : null
  return (
    <div className="soha-rbac-subject-list">
      {value.slice(0, 3).map((item) => (
        <Tag key={item}>{item}</Tag>
      ))}
      {activated && overflow ? (
        <Tooltip
          title={value.slice(3).join(', ')}
          trigger={['hover', 'focus', 'click']}
          open={open}
          onOpenChange={setOpen}
        >
          {overflow}
        </Tooltip>
      ) : (
        overflow
      )}
    </div>
  )
})

export function renderNetworkTextList(value?: string[], empty = '-') {
  return <NetworkTextList value={value} empty={empty} />
}
