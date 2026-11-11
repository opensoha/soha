import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { PlayCircleOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Input, Modal, Select } from 'antd'
import { useEffect, useState } from 'react'
import { AIWorkbenchFeatureGate } from '../production/feature-gate'
import { ProductionOperationsPage } from '../production/operations-page'
import { aiProductionOperationsMutations } from './mutations'
import { aiProductionOperationsQueries } from './queries'
import type { StartAIOperationInput } from './types'
export function AIProductionOperationsPage() {
  return (
    <AIWorkbenchFeatureGate features={['ai.production_operations']}>
      <AIProductionOperationsPageContent />
    </AIWorkbenchFeatureGate>
  )
}

function AIProductionOperationsPageContent() {
  const c = useQueryClient()
  const [open, setOpen] = useState(false)
  const [form] = Form.useForm<StartAIOperationInput>()
  const snapshots = useQuery(aiProductionOperationsQueries.snapshots())
  const evidence = useQuery(aiProductionOperationsQueries.evidence())
  const start = useOperationMutation(aiProductionOperationsMutations.start(c), {
    label: ['提交 AI 生产操作', 'Submit AI production operation'],
    target: (v) => v.targetRef,
    receipt: (r) => ({ message: '提交 AI 生产操作请求已返回', evidence: r.data.id }),
  })
  const writeBlocked = start.blocked
  const blocked = writeBlocked || queryAccessDenied(snapshots) || queryAccessDenied(evidence)
  const openDenied = queryAccessDenied(start) || queryAccessDenied(snapshots)
  useEffect(() => {
    if (openDenied) {
      setOpen(false)
      form.resetFields()
    }
  }, [openDenied, form])
  const submit = async () => {
    if (blocked) return
    await start.mutateAsync(await form.validateFields())
    form.resetFields()
    setOpen(false)
  }
  return (
    <ProductionOperationsPage
      feedback={<> {!open ? start.feedback : null} </>}
      notice={
        <Alert
          showIcon
          type="info"
          title="生产操作需要审计证据"
          description="恢复、重建和演练必须保留 operation ID、耗时、结果与 runbook 版本。"
        />
      }
      refreshing={snapshots.isFetching || evidence.isFetching}
      onRefresh={() => void Promise.all([snapshots.refetch(), evidence.refetch()])}
      actions={
        <Button
          disabled={blocked}
          type="primary"
          icon={<PlayCircleOutlined />}
          onClick={() => {
            if (!blocked) {
              form.resetFields()
              start.reset()
              setOpen(true)
            }
          }}
        >
          启动操作
        </Button>
      }
      tabs={[
        {
          key: 'capacity',
          label: 'Capacity & SLO',
          records: readableQueryData(snapshots)?.data ?? [],
          loading: snapshots.isLoading,
          error: snapshots.error,
          hasReadData: snapshots.data !== undefined,
          emptyDescription: '采集 queue age、吞吐、成本与 deletion lag 后显示。',
        },
        {
          key: 'evidence',
          label: 'Runbook Evidence',
          records: readableQueryData(evidence)?.data ?? [],
          loading: evidence.isLoading,
          error: evidence.error,
          hasReadData: evidence.data !== undefined,
          emptyDescription: '完成备份恢复或演练后显示证据。',
        },
      ]}
    >
      <Modal
        title="启动 AI 生产操作"
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
        confirmLoading={start.isPending}
        destroyOnHidden
      >
        <> {start.feedback} </>
        <Form
          disabled={blocked}
          form={form}
          layout="vertical"
          preserve={false}
          initialValues={{ kind: 'drill' }}
        >
          <Form.Item name="kind" label="操作" rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'backup', label: 'Backup' },
                { value: 'restore', label: 'Restore' },
                { value: 'index_rebuild', label: 'Index Rebuild' },
                { value: 'drill', label: 'Drill' },
              ]}
            />
          </Form.Item>
          <Form.Item name="targetRef" label="Target Ref" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="runbookId" label="Runbook ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </ProductionOperationsPage>
  )
}
