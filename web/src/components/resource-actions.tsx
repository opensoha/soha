import { useState } from 'react'
import { DeleteOutlined } from '@ant-design/icons'
import { Button, message, Popconfirm, Tooltip } from 'antd'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import type { TableColumnsType } from 'antd'

type ColumnProps<T> = TableColumnsType<T>[number]
export const TABLE_ACTIONS_COLUMN_CLASS_NAME = 'soha-table-actions-column'

export function useResourceActions<T extends Record<string, any>>(options: {
  resourcePath: string
  resourceKind: string
  resourceLabel?: string
  getName: (record: T) => string
  getNamespace?: (record: T) => string | undefined
  canDelete?: (record: T) => boolean
  width?: number
  listInvalidationKey?: unknown[]
}): { column: ColumnProps<T>; modalNode: React.ReactNode } {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [deleting, setDeleting] = useState<string | null>(null)

  const invalidate = () => {
    if (options.listInvalidationKey) {
      queryClient.invalidateQueries({ queryKey: options.listInvalidationKey })
    } else {
      queryClient.invalidateQueries({ queryKey: ['platform-resource', options.resourcePath] })
    }
  }

  const deleteMutation = useMutation({
    mutationFn: async ({ name, namespace }: { name: string; namespace?: string }) => {
      const encoded = encodeURIComponent(name)
      const base = buildClusterScopedPath(clusterId!, `${options.resourcePath}/${encoded}`)
      const path = namespace ? `${base}${base.includes('?') ? '&' : '?'}namespace=${encodeURIComponent(namespace)}` : base
      return api.delete(path)
    },
    onMutate: ({ name, namespace }) => setDeleting(`${namespace || ''}/${name}`),
    onSettled: () => setDeleting(null),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? '已删除' : 'Deleted')
      invalidate()
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const column: ColumnProps<T> = {
    title: '',
    dataIndex: '__actions',
    fixed: 'right',
    align: 'center',
    width: options.width ?? 52,
    onHeaderCell: () => ({ className: TABLE_ACTIONS_COLUMN_CLASS_NAME }),
    onCell: () => ({ className: TABLE_ACTIONS_COLUMN_CLASS_NAME }),
    render: (_: unknown, record: T) => {
      if (options.canDelete && !options.canDelete(record)) {
        return null
      }
      const name = options.getName(record)
      const namespace = options.getNamespace?.(record)
      const key = `${namespace || ''}/${name}`
      return (
        <Popconfirm
          title={localeCode === 'zh_CN' ? `确认删除 ${name}？` : `Delete ${name}?`}
          description={localeCode === 'zh_CN' ? '此操作不可恢复，删除后集群资源立即消失。' : 'This deletes the resource immediately and cannot be undone.'}
          okText={localeCode === 'zh_CN' ? '删除' : 'Delete'}
          cancelText={localeCode === 'zh_CN' ? '取消' : 'Cancel'}
          okButtonProps={{ danger: true, loading: deleting === key }}
          placement="topRight"
          onConfirm={() => deleteMutation.mutate({ name, namespace })}
        >
          <Tooltip title={localeCode === 'zh_CN' ? '删除' : 'Delete'}>
            <Button
              size="small"
              type="text"
              danger
              icon={<DeleteOutlined />}
              aria-label={localeCode === 'zh_CN' ? '删除' : 'Delete'}
              loading={deleting === key}
            />
          </Tooltip>
        </Popconfirm>
      )
    },
  }

  return { column, modalNode: null }
}
