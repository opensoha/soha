import { ManagementIconButton } from '@/components/management-list'
import { requestErrorMessage, useInlineApiError } from '@/components/request-feedback'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import {
  DownOutlined,
  FileTextOutlined,
  FormOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Dropdown, Space, Tooltip } from 'antd'
import { useState } from 'react'
import { resolveCreateEntryAvailability } from '../model'
import { resourceCreationQueries } from '../queries'
import { hasResourceCreateForm } from '../registry'
import type { ResourceCreateContext, ResourceCreateResult } from '../types'
import { CreateShell, type ResourceCreateFormAdapter } from './create-shell'

export function CreateEntry({
  context,
  defaultTemplate,
  form,
  label,
  onCreated,
}: {
  readonly context: ResourceCreateContext
  readonly defaultTemplate: string
  readonly form?: ResourceCreateFormAdapter
  readonly label: string
  readonly onCreated?: (result: ResourceCreateResult) => void
}) {
  const { localeCode } = useI18n()
  const isChinese = localeCode === 'zh_CN'
  const formSupported = Boolean(form || hasResourceCreateForm(context.expectedKind))
  const [open, setOpen] = useState(false)
  const [initialMode, setInitialMode] = useState<'form' | 'yaml'>(formSupported ? 'form' : 'yaml')
  const decision = useQuery(
    resourceCreationQueries.scopeDecision(context.clusterId, {
      ...(context.defaultNamespace ? { namespace: context.defaultNamespace } : {}),
      resourceGroup: context.resourceGroup || 'platform',
      ...(context.expectedApiVersion ? { apiVersion: context.expectedApiVersion } : {}),
      kind: context.expectedKind || '',
      action: 'create',
    }),
  )
  const visibleDecisionData = readableQueryData(decision)
  useInlineApiError(decision.error)
  const { disabled, reason } = resolveCreateEntryAvailability({
    clusterId: context.clusterId,
    decision: visibleDecisionData,
    error: decision.isError ? decision.error : null,
    isLoading: decision.isLoading,
    localeCode,
  })

  function show(mode: 'form' | 'yaml') {
    setInitialMode(mode)
    setOpen(true)
  }

  return (
    <>
      {decision.isError ? (
        <ManagementIconButton
          aria-label={isChinese ? '重试读取创建权限' : 'Retry create permission lookup'}
          icon={<ReloadOutlined />}
          loading={decision.isFetching}
          tooltip={requestErrorMessage(decision.error, localeCode)}
          onClick={() => void decision.refetch()}
        />
      ) : null}

      <Tooltip title={disabled ? reason : ''}>
        <span>
          <Space.Compact>
            <Button
              autoInsertSpace={false}
              disabled={disabled}
              icon={<PlusOutlined />}
              onClick={() => show(formSupported ? 'form' : 'yaml')}
              size="small"
              type="primary"
            >
              {isChinese ? '新增' : 'Create'}
            </Button>
            <Dropdown
              disabled={disabled}
              menu={{
                items: [
                  ...(formSupported
                    ? [
                        {
                          key: 'form',
                          icon: <FormOutlined />,
                          label: isChinese ? '表单创建' : 'Create with form',
                        },
                      ]
                    : []),
                  {
                    key: 'yaml',
                    icon: <FileTextOutlined />,
                    label: isChinese ? '使用 YAML 创建' : 'Create from YAML',
                  },
                ],
                onClick: ({ key }) => show(key as 'form' | 'yaml'),
              }}
              trigger={['click']}
            >
              <Button
                aria-label={isChinese ? '选择创建方式' : 'Choose creation mode'}
                disabled={disabled}
                icon={<DownOutlined />}
                size="small"
                type="primary"
              />
            </Dropdown>
          </Space.Compact>
        </span>
      </Tooltip>
      <CreateShell
        context={context}
        defaultTemplate={defaultTemplate}
        form={form}
        formSupported={formSupported}
        initialMode={initialMode}
        label={label}
        onClose={() => setOpen(false)}
        onCreated={onCreated}
        open={open}
      />
    </>
  )
}
