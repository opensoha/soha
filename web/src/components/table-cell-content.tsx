import type { MouseEventHandler } from 'react'
import { Button, Typography } from 'antd'
import type { TooltipProps } from 'antd'

const { Text } = Typography
const tooltipStyles = {
  root: { maxWidth: 'min(640px, 78vw)' },
  container: { overflowWrap: 'anywhere', whiteSpace: 'normal' },
} satisfies TooltipProps['styles']

function displayValue(value?: null | number | string) {
  return value === null || value === undefined || value === '' ? '-' : String(value)
}

export function TableCellText({
  className,
  value,
}: {
  className?: string
  value?: null | number | string
}) {
  const display = displayValue(value)
  return (
    <Text
      className={className}
      style={{ maxWidth: '100%', color: 'inherit', fontWeight: 'inherit', lineHeight: 'inherit' }}
      ellipsis={{ tooltip: { placement: 'topLeft', styles: tooltipStyles, title: display } }}
    >
      {display}
    </Text>
  )
}

export function TableCellLink({
  label,
  onClick,
}: {
  label: string
  onClick: MouseEventHandler<HTMLElement>
}) {
  return (
    <Button
      type="text"
      style={{ maxWidth: '100%' }}
      styles={{ content: { minWidth: 0 } }}
      onClick={onClick}
    >
      <TableCellText value={label} />
    </Button>
  )
}
