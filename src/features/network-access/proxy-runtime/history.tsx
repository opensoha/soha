import { useEffect, useState } from 'react'
import { Alert, Button, Modal, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  NetworkProxyInstance,
  NetworkProxyConfigurationRevision,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { operationModalStyles } from '@/components/modal-styles'
import { readableQueryData, queryAccessDenied } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { useI18n } from '@/i18n'
import { restoreConfiguration } from './api'
import { proxyKeys, proxyQueries } from './queries'

export function ProxyConfigurationHistory({
  instance,
  canUpdate,
  disabled,
}: {
  instance: NetworkProxyInstance
  canUpdate: boolean
  disabled: boolean
}) {
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState<NetworkProxyConfigurationRevision>()
  useEffect(() => {
    if (!canUpdate) setSelected(undefined)
  }, [canUpdate])
  const client = useQueryClient()
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const history = useQuery(proxyQueries.history(instance.id, open && !disabled))
  const restore = useOperationMutation(
    {
      mutationFn: (revision: number) =>
        restoreConfiguration(instance.id, revision, instance.desiredRevision),
      onSuccess: () => {
        setSelected(undefined)
        void client.invalidateQueries({ queryKey: proxyKeys.all })
      },
      onError: () => {
        void client.invalidateQueries({ queryKey: proxyKeys.all })
      },
    },
    {
      label: ['恢复代理配置', 'Restore proxy configuration'],
      target: (revision) => `${instance.name} r${revision}`,
      receipt: (data) => ({
        message: text(
          '已保存为新版本，等待运行时应用',
          'Saved as a new revision; awaiting runtime application',
        ),
        evidence: `r${data.desiredRevision}`,
      }),
    },
  )
  const blocked = disabled || !canUpdate || restore.blocked || queryAccessDenied(history)
  return (
    <>
      <Button size="small" disabled={disabled} onClick={() => setOpen(true)}>
        {text('历史', 'History')}
      </Button>
      <Modal
        title={text('配置历史', 'Configuration history')}
        open={open && !disabled}
        footer={null}
        width={760}
        styles={operationModalStyles}
        destroyOnHidden
        onCancel={() => {
          if (!restore.isPending) {
            setOpen(false)
            setSelected(undefined)
          }
        }}
        closable={!restore.isPending}
      >
        {restore.feedback}
        <Typography.Paragraph type="secondary">
          {text(
            '显示最近 100 个版本，不回显配置内容。恢复只取历史内容，保留当前启停和绑定，并应用当前保护策略。',
            'Shows the latest 100 revisions without disclosing source contents. Restore retains current enablement, binding and protection policy.',
          )}
        </Typography.Paragraph>
        <AdminTable
          rowKey="revision"
          dataSource={readableQueryData(history) ?? []}
          loading={history.isLoading}
          refreshing={history.isFetching}
          error={history.error ?? undefined}
          hasReadData={readableQueryData(history) !== undefined}
          onRefresh={() => void history.refetch()}
          pagination={{ pageSize: 10, showSizeChanger: false }}
          columns={[
            { title: text('版本', 'Revision'), dataIndex: 'revision', width: 80 },
            { title: text('内容摘要', 'Content hash'), dataIndex: 'contentHash', ellipsis: true },
            {
              title: text('保存时间', 'Saved at'),
              dataIndex: 'createdAt',
              width: 180,
              render: (value: string) => formatDateTime(value),
            },
            {
              title: text('操作', 'Action'),
              key: 'action',
              width: 100,
              render: (_: unknown, row: NetworkProxyConfigurationRevision) =>
                canUpdate && row.revision !== instance.desiredRevision ? (
                  <Button size="small" disabled={blocked} onClick={() => setSelected(row)}>
                    {text('恢复', 'Restore')}
                  </Button>
                ) : null,
            },
          ]}
        />
        <Modal
          title={text('恢复历史内容', 'Restore source revision')}
          open={Boolean(selected) && canUpdate && !disabled && !queryAccessDenied(history)}
          styles={operationModalStyles}
          confirmLoading={restore.isPending}
          onCancel={() => {
            if (!restore.isPending) setSelected(undefined)
          }}
          onOk={() => {
            if (selected && !blocked) restore.mutate(selected.revision)
          }}
          cancelButtonProps={{ disabled: restore.isPending }}
          okButtonProps={{ disabled: restore.isPending }}
        >
          <Alert
            type="warning"
            showIcon
            title={text(
              `将 r${selected?.revision} 内容保存为新版本。旧权限和旧租约不会恢复。`,
              `Save source r${selected?.revision} as a new revision. Historical permissions and leases are not restored.`,
            )}
          />
        </Modal>
      </Modal>
    </>
  )
}
