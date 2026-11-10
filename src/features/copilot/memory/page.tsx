import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Checkbox, Form, Input, InputNumber, Modal, Popconfirm, Select } from 'antd'
import { useEffect, useState } from 'react'
import { AIWorkbenchFeatureGate } from '../production/feature-gate'
import { ProductionOperationsPage } from '../production/operations-page'
import { memoryDeleted, memorySyncLabel } from './state'
import type { MemoryRecord } from './types'
import { memoryMutations } from './mutations'
import { memoryQueries } from './queries'
import type { CreateMemoryPolicyInput } from './types'
export function MemoryPoliciesPage() {
  return (
    <AIWorkbenchFeatureGate features={['memory.long_term']}>
      <MemoryPoliciesPageContent />
    </AIWorkbenchFeatureGate>
  )
}

function MemoryPoliciesPageContent() {
  const c = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CreateMemoryPolicyInput>()
  const mode = Form.useWatch('mode', form)
  const records = useQuery(memoryQueries.records())
  const policies = useQuery(memoryQueries.policies())
  const remove = useOperationMutation(memoryMutations.deleteRecord(c), {
    label: ['删除 Memory', 'Delete memory'],
    target: (id) => id,
  })
  const create = useOperationMutation(memoryMutations.createPolicy(c), {
    label: ['创建 Memory 策略', 'Create memory policy'],
    target: (v) => v.id,
    receipt: (r) => ({ message: '创建 Memory 策略请求已返回', evidence: r.data.id }),
  })
  const writeBlocked = remove.blocked || create.blocked
  const blocked = writeBlocked || queryAccessDenied(records) || queryAccessDenied(policies)
  const openDenied = queryAccessDenied(create) || queryAccessDenied(policies)
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
          {remove.feedback}
          {!open ? create.feedback : null}{' '}
        </>
      }
      notice={
        <Alert
          showIcon
          type="info"
          title="长期 Memory 默认关闭"
          description="Chat 仅显式写入；Heya 自动记忆须主动开启。关闭授权保留已有记录；删除后立即停用并显示传播状态。"
        />
      }
      refreshing={records.isFetching || policies.isFetching}
      onRefresh={() => void Promise.all([records.refetch(), policies.refetch()])}
      actions={
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
          新建策略
        </Button>
      }
      tabs={[
        {
          key: 'policies',
          label: 'Policies',
          records: readableQueryData(policies)?.data ?? [],
          columns: [
            { title: '策略', dataIndex: 'id' },
            { title: '入口', render: (_, row) => row.mode || '历史全局策略' },
            {
              title: '读取',
              render: (_, row) =>
                row.enabled && (row.readEnabled ?? row.enabled) ? '启用' : '关闭',
            },
            {
              title: '写入',
              render: (_, row) =>
                !row.enabled || !(row.writeEnabled ?? row.enabled)
                  ? '关闭'
                  : row.explicitWriteOnly
                    ? '显式'
                    : 'Heya 自动',
            },
          ],
          loading: policies.isLoading,
          error: policies.error,
          hasReadData: policies.data !== undefined,
          emptyDescription: '创建显式同意和 TTL 策略。',
        },
        {
          key: 'records',
          label: 'Records',
          records: readableQueryData(records)?.data ?? [],
          columns: [
            {
              title: '条目',
              render: (_, row) =>
                memoryDeleted(row as MemoryRecord) ? '已删除的记忆' : String(row.fact || row.id),
            },
            {
              title: '来源',
              render: (_, row) =>
                memoryDeleted(row as MemoryRecord)
                  ? '已隐藏'
                  : (row as MemoryRecord).sourceRefs?.join('、') || '显式输入',
            },
            { title: '同步状态', render: (_, row) => memorySyncLabel(row as MemoryRecord) },
          ],
          loading: records.isLoading,
          error: records.error,
          hasReadData: records.data !== undefined,
          emptyDescription: '启用策略并产生可信事实后显示 Memory。',
          actions: (row) =>
            memoryDeleted(row as MemoryRecord) ? null : (
              <Popconfirm
                disabled={blocked}
                title="确认删除并传播该 Memory？"
                onConfirm={() => remove.mutate(row.id)}
              >
                <Button disabled={blocked} danger type="link">
                  删除并传播
                </Button>
              </Popconfirm>
            ),
        },
      ]}
    >
      <Modal
        title="新建 Memory Policy"
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
          initialValues={{
            consentMode: 'explicit',
            ttlDays: 30,
            mode: 'chat',
            readEnabled: true,
            writeEnabled: true,
          }}
        >
          <Form.Item name="id" label="ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="mode" label="入口" rules={[{ required: true }]}>
            <Select
              onChange={() => form.setFieldValue('consentMode', 'explicit')}
              options={[
                { value: 'chat', label: 'Chat' },
                { value: 'heya', label: 'Heya' },
              ]}
            />
          </Form.Item>
          <Form.Item name="readEnabled" valuePropName="checked">
            <Checkbox>允许读取记忆</Checkbox>
          </Form.Item>
          <Form.Item name="writeEnabled" valuePropName="checked">
            <Checkbox>允许写入记忆</Checkbox>
          </Form.Item>
          <Form.Item name="consentMode" label="Consent" rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'explicit', label: 'Explicit' },
                { value: 'disabled', label: 'Disabled' },
                ...(mode === 'heya'
                  ? [{ value: 'automatic', label: '主动启用 Heya 自动记忆' }]
                  : []),
              ]}
            />
          </Form.Item>
          <Form.Item name="ttlDays" label="TTL (days)" rules={[{ required: true }]}>
            <InputNumber min={1} max={365} />
          </Form.Item>
        </Form>
      </Modal>
    </ProductionOperationsPage>
  )
}
