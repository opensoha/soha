import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Form, Input, InputNumber, Modal, Popconfirm, Space } from 'antd'
import { useEffect, useState } from 'react'
import { AIWorkbenchFeatureGate } from '../production/feature-gate'
import { ProductionOperationsPage } from '../production/operations-page'
import { providerFleetMutations } from './mutations'
import { providerFleetQueries } from './queries'
import type { CreateRolloutInput } from './types'
export function ProviderFleetPage() {
  return (
    <AIWorkbenchFeatureGate features={['agent.fleet_rollout', 'agent.conformance_suite']}>
      <ProviderFleetPageContent />
    </AIWorkbenchFeatureGate>
  )
}

function ProviderFleetPageContent() {
  const c = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<CreateRolloutInput>()
  const rollouts = useQuery(providerFleetQueries.rollouts())
  const runs = useQuery(providerFleetQueries.conformance())
  const rollout = useOperationMutation(providerFleetMutations.rollout(c), {
    label: ['创建 Provider rollout', 'Create Provider rollout'],
    target: (v) => v.id,
    receipt: (r) => ({ message: '创建 Provider rollout请求已返回', evidence: r.data.id }),
  })
  const action = useOperationMutation(providerFleetMutations.action(c), {
    label: ['变更 Provider rollout', 'Change Provider rollout'],
    target: (v) => v.id,
    receipt: (r) => ({ message: '变更 Provider rollout请求已返回', evidence: r.data.id }),
  })
  const writeBlocked = rollout.blocked || action.blocked
  const blocked = writeBlocked || queryAccessDenied(rollouts) || queryAccessDenied(runs)
  const openDenied = queryAccessDenied(rollout) || queryAccessDenied(rollouts)
  useEffect(() => {
    if (openDenied) {
      setOpen(false)
      form.resetFields()
    }
  }, [openDenied, form])
  const submit = async () => {
    if (blocked) return
    const v = await form.validateFields()
    await rollout.mutateAsync({
      ...v,
      environments: String(v.environments || '')
        .split(',')
        .map((x) => x.trim())
        .filter(Boolean),
    } as CreateRolloutInput)
    form.resetFields()
    setOpen(false)
  }
  return (
    <ProductionOperationsPage
      feedback={
        <>
          {' '}
          {!open ? rollout.feedback : null}
          {action.feedback}{' '}
        </>
      }
      refreshing={rollouts.isFetching || runs.isFetching}
      onRefresh={() => void Promise.all([rollouts.refetch(), runs.refetch()])}
      actions={
        <Button
          disabled={blocked}
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => {
            if (!blocked) {
              form.resetFields()
              rollout.reset()
              setOpen(true)
            }
          }}
        >
          新建 Rollout
        </Button>
      }
      tabs={[
        {
          key: 'rollouts',
          label: 'Rollouts',
          records: readableQueryData(rollouts)?.data ?? [],
          loading: rollouts.isLoading,
          error: rollouts.error,
          hasReadData: rollouts.data !== undefined,
          emptyDescription: '选择目标环境和期望修订后启动 canary。',
          actions: (row) => (
            <Space>
              <Button
                disabled={blocked}
                type="link"
                onClick={() => action.mutate({ id: row.id, action: 'pause' })}
              >
                暂停
              </Button>
              <Button
                disabled={blocked}
                type="link"
                onClick={() => action.mutate({ id: row.id, action: 'resume' })}
              >
                继续
              </Button>
              <Popconfirm
                disabled={blocked}
                title="确认回滚 Provider rollout？"
                onConfirm={() => action.mutate({ id: row.id, action: 'rollback' })}
              >
                <Button disabled={blocked} danger type="link">
                  回滚
                </Button>
              </Popconfirm>
            </Space>
          ),
        },
        {
          key: 'conformance',
          label: 'Conformance',
          records: readableQueryData(runs)?.data ?? [],
          loading: runs.isLoading,
          error: runs.error,
          hasReadData: runs.data !== undefined,
          emptyDescription: 'Provider 与 Environment 组合验证后显示结果。',
        },
      ]}
    >
      <Modal
        title="新建 Provider Rollout"
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
        confirmLoading={rollout.isPending}
        destroyOnHidden
      >
        <> {rollout.feedback} </>
        <Form
          disabled={blocked}
          form={form}
          layout="vertical"
          preserve={false}
          initialValues={{ canaryPercent: 10 }}
        >
          <Form.Item name="id" label="ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="desiredRevision" label="Desired Revision" rules={[{ required: true }]}>
            <InputNumber min={1} />
          </Form.Item>
          <Form.Item name="environments" label="Environment IDs" rules={[{ required: true }]}>
            <Input placeholder="env-a, env-b" />
          </Form.Item>
          <Form.Item name="canaryPercent" label="Canary %" rules={[{ required: true }]}>
            <InputNumber min={1} max={100} />
          </Form.Item>
        </Form>
      </Modal>
    </ProductionOperationsPage>
  )
}
