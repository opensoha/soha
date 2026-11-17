import { memo, useCallback } from 'react'
import { StatusTag } from '@/components/status-tag'
import { TableCellLink } from '@/components/table-cell-content'
import { NamespaceScopeTag } from './namespace-scope-tag'

export const ResourceNameLink = memo(function ResourceNameLink({
  name,
  to,
  onNavigate,
}: {
  name: string
  to: string
  onNavigate: (path: string) => void
}) {
  const open = useCallback(() => onNavigate(to), [onNavigate, to])
  return <TableCellLink label={name} onClick={open} />
})

export const ResourceStatusCell = memo(StatusTag)
export const ResourceNamespaceCell = memo(NamespaceScopeTag)
