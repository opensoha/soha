import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Form, Input, Modal, Popconfirm, Select } from 'antd'
import { useEffect, useState } from 'react'
import { AIWorkbenchFeatureGate } from '../production/feature-gate'
import { ProductionOperationsPage } from '../production/operations-page'
import { environmentMutations } from './mutations'
import { environmentQueries } from './queries'
import type { CreateEnvironmentTemplateInput } from './types'
export function EnvironmentsPage() {
  return (
    <AIWorkbenchFeatureGate features={['agent.environment_management']}>
      <EnvironmentsPageContent />
    </AIWorkbenchFeatureGate>
  )
}

function EnvironmentsPageContent() {
  const c = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CreateEnvironmentTemplateInput>()
  const templates = useQuery(environmentQueries.templates())
  const leases = useQuery(environmentQueries.leases())
  const create = useOperationMutation(environmentMutations.create(c), {
    label: ['创建环境模板', 'Create environment template'],
    target: (v) => v.id,
    receipt: (r) => ({ message: '创建环境模板请求已返回', evidence: r.data.id }),
  })
  const release = useOperationMutation(environmentMutations.release(c), {
    label: ['释放环境租约', 'Release environment lease'],
    target: (id) => id,
    receipt: (r) => ({ message: '释放环境租约请求已返回', evidence: r.data.id }),
  })
  const gc = useOperationMutation(environmentMutations.gc(c), {
    label: ['环境清理', 'Collect environments'],
    target: () => '环境租约',
    receipt: (r) => ({ message: '环境清理请求已返回', evidence: r.data.id }),
  })
  const writeBlocked = create.blocked || release.blocked || gc.blocked
  const blocked = writeBlocked || queryAccessDenied(templates) || queryAccessDenied(leases)
  const openDenied = queryAccessDenied(create) || queryAccessDenied(templates)
  useEffect(() => {
    if (openDenied) {
      setOpen(false)
      form.resetFields()
    }
  }, [openDenied, form])
  const submit = async () => {
    if (blocked) return
    await create.mutateAsync(await form.validateFields())
    form.resetFields()
    setOpen(false)
  }
  return (
    <ProductionOperationsPage
      feedback={
        <>
          {' '}
          {!open ? create.feedback : null}
          {release.feedback}
          {gc.feedback}{' '}
        </>
      }
      refreshing={templates.isFetching || leases.isFetching}
      onRefresh={() => void Promise.all([templates.refetch(), leases.refetch()])}
      actions={
        <>
          <Popconfirm disabled={blocked} title="确认运行环境 GC？" onConfirm={() => gc.mutate()}>
            <Button disabled={blocked} icon={<DeleteOutlined />}>
              运行 GC
            </Button>
          </Popconfirm>
          <Button
            disabled={blocked}
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => {
              if (!blocked) {
                form.resetFields()
                create.reset()
                setOpen(true)
              }
            }}
          >
            新建模板
          </Button>
        </>
      }
      tabs={[
        {
          key: 'templates',
          label: 'Templates',
          records: readableQueryData(templates)?.data ?? [],
          loading: templates.isLoading,
          error: templates.error,
          hasReadData: templates.data !== undefined,
          emptyDescription: '创建 container 或 Kubernetes 隔离模板。',
        },
        {
          key: 'leases',
          label: 'Leases',
          records: readableQueryData(leases)?.data ?? [],
          loading: leases.isLoading,
          error: leases.error,
          hasReadData: leases.data !== undefined,
          emptyDescription: 'Agent 或 Evaluation 获取环境后会显示租约。',
          actions: (row) => (
            <Popconfirm
              disabled={blocked}
              title="确认释放环境租约？"
              onConfirm={() => release.mutate(row.id)}
            >
              <Button disabled={blocked} danger type="link">
                释放
              </Button>
            </Popconfirm>
          ),
        },
      ]}
    >
      <Modal
        title="新建 Environment Template"
        open={open && !openDenied}
        closable={!writeBlocked}
        keyboard={!writeBlocked}
        mask={{ closable: !writeBlocked }}
        okButtonProps={{ disabled: blocked }}
        cancelButtonProps={{ disabled: writeBlocked }}
        onCancel={() => {
          if (!writeBlocked) {
            setOpen(false)
            form.resetFields()
          }
        }}
        onOk={() => void submit().catch(() => undefined)}
        confirmLoading={create.isPending}
        destroyOnHidden
      >
        <> {create.feedback} </>
        <Form
          disabled={blocked}
          form={form}
          layout="vertical"
          preserve={false}
          initialValues={{ backend: 'container', isolationMode: 'read-only' }}
        >
          <Form.Item name="id" label="ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="backend" label="Backend" rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'container', label: 'Container' },
                { value: 'kubernetes', label: 'Kubernetes' },
              ]}
            />
          </Form.Item>
          <Form.Item name="isolationMode" label="隔离模式" rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'read-only', label: 'Read only' },
                { value: 'disposable-write', label: 'Disposable write' },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>
    </ProductionOperationsPage>
  )
}
