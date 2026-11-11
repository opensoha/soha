import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { PlusOutlined, RocketOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Form, Input, Modal, Segmented } from 'antd'
import { useEffect, useState } from 'react'
import { AIWorkbenchFeatureGate } from '../production/feature-gate'
import { ProductionOperationsPage } from '../production/operations-page'
import { evaluationLifecycleMutations } from './mutations'
import { evaluationLifecycleQueries } from './queries'
import type { CreateFeedbackInput, CreateGatePolicyInput, CreateReplayInput } from './types'
type Mode = 'replay' | 'policy' | 'feedback' | 'gate'
export function EvaluationLifecyclePage() {
  return (
    <AIWorkbenchFeatureGate
      features={[
        'evaluation.candidate_executor',
        'evaluation.isolated_replay',
        'evaluation.release_gate',
        'evaluation.feedback_sampling',
      ]}
    >
      <EvaluationLifecyclePageContent />
    </AIWorkbenchFeatureGate>
  )
}

function EvaluationLifecyclePageContent() {
  const c = useQueryClient()
  const [open, setOpen] = useState(false)
  const [mode, setMode] = useState<Mode>('replay')
  const [form] = Form.useForm<Record<string, string | number>>()
  const replays = useQuery(evaluationLifecycleQueries.replays())
  const policies = useQuery(evaluationLifecycleQueries.policies())
  const feedback = useQuery(evaluationLifecycleQueries.feedback())
  const replay = useOperationMutation(evaluationLifecycleMutations.replay(c), {
    label: ['创建隔离回放', 'Create isolated replay'],
    target: (v) => v.id,
    receipt: (r) => ({ message: '创建隔离回放请求已返回', evidence: r.data.id }),
  })
  const policy = useOperationMutation(evaluationLifecycleMutations.policy(c), {
    label: ['创建评测门禁策略', 'Create evaluation gate policy'],
    target: (v) => v.id,
    receipt: (r) => ({ message: '创建评测门禁策略请求已返回', evidence: r.data.id }),
  })
  const curate = useOperationMutation(evaluationLifecycleMutations.feedback(c), {
    label: ['整理评测反馈', 'Curate evaluation feedback'],
    target: (v) => v.id,
    receipt: (r) => ({ message: '整理评测反馈请求已返回', evidence: r.data.id }),
  })
  const gate = useOperationMutation(evaluationLifecycleMutations.gate(), {
    label: ['提交评测门禁', 'Submit evaluation gate'],
    target: (v) => v.policyId,
    receipt: (r) => ({ message: '提交评测门禁请求已返回', evidence: r.data.id }),
  })
  const writeBlocked = replay.blocked || policy.blocked || curate.blocked || gate.blocked
  const blocked =
    writeBlocked ||
    queryAccessDenied(replays) ||
    queryAccessDenied(policies) ||
    queryAccessDenied(feedback)
  const openDenied =
    queryAccessDenied(replay) ||
    queryAccessDenied(policy) ||
    queryAccessDenied(curate) ||
    queryAccessDenied(gate) ||
    queryAccessDenied(replays) ||
    queryAccessDenied(policies) ||
    queryAccessDenied(feedback)
  useEffect(() => {
    if (openDenied) {
      setOpen(false)
      form.resetFields()
    }
  }, [openDenied, form])
  const submit = async () => {
    if (blocked) return
    const v = await form.validateFields()
    if (mode === 'replay') await replay.mutateAsync(v as unknown as CreateReplayInput)
    else if (mode === 'policy') await policy.mutateAsync(v as unknown as CreateGatePolicyInput)
    else if (mode === 'feedback') await curate.mutateAsync(v as unknown as CreateFeedbackInput)
    else
      await gate.mutateAsync(
        v as { policyId: string; baselineRunId: string; candidateRunId: string },
      )
    form.resetFields()
    setOpen(false)
  }
  return (
    <ProductionOperationsPage
      feedback={
        <>
          {' '}
          {!open ? replay.feedback : null}
          {!open ? policy.feedback : null}
          {!open ? curate.feedback : null}
          {!open ? gate.feedback : null}{' '}
        </>
      }
      notice={
        <Alert
          showIcon
          type="warning"
          title="Gate error 永不等价于 pass"
          description="发布流程只消费版本化 GateDecision；执行错误、证据缺失或回放不一致必须阻断自动放量。"
        />
      }
      refreshing={replays.isFetching || policies.isFetching || feedback.isFetching}
      onRefresh={() =>
        void Promise.all([replays.refetch(), policies.refetch(), feedback.refetch()])
      }
      actions={
        <Button
          disabled={blocked}
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => {
            if (!blocked) {
              form.resetFields()
              replay.reset()
              policy.reset()
              curate.reset()
              gate.reset()
              setOpen(true)
            }
          }}
        >
          新建任务
        </Button>
      }
      tabs={[
        {
          key: 'replays',
          label: 'Replays',
          records: readableQueryData(replays)?.data ?? [],
          loading: replays.isLoading,
          error: replays.error,
          hasReadData: replays.data !== undefined,
          emptyDescription: '创建 baseline 与 candidate 的隔离回放。',
        },
        {
          key: 'feedback',
          label: 'Feedback',
          records: readableQueryData(feedback)?.data ?? [],
          loading: feedback.isLoading,
          error: feedback.error,
          hasReadData: feedback.data !== undefined,
          emptyDescription: '从 trace 筛选并整理可追溯反馈样本。',
        },
        {
          key: 'gates',
          label: 'Release Gates',
          records: readableQueryData(policies)?.data ?? [],
          loading: policies.isLoading,
          error: policies.error,
          hasReadData: policies.data !== undefined,
          emptyDescription: '创建版本化门禁策略。',
        },
      ]}
    >
      <Modal
        title="新建 Evaluation 操作"
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
        confirmLoading={replay.isPending || policy.isPending || curate.isPending || gate.isPending}
        destroyOnHidden
      >
        <Segmented
          disabled={blocked}
          block
          value={mode}
          onChange={(v) => {
            setMode(v as Mode)
            form.resetFields()
          }}
          options={[
            { value: 'replay', label: 'Replay' },
            { value: 'policy', label: 'Gate Policy' },
            { value: 'feedback', label: 'Feedback' },
            {
              value: 'gate',
              label: (
                <>
                  <RocketOutlined /> Gate
                </>
              ),
            },
          ]}
        />
        <>
          {' '}
          {replay.feedback}
          {policy.feedback}
          {curate.feedback}
          {gate.feedback}{' '}
        </>
        <Form
          disabled={blocked}
          form={form}
          layout="vertical"
          preserve={false}
          style={{ marginTop: 16 }}
        >
          <Form.Item name="id" label="ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          {mode === 'replay' ? (
            <>
              <Form.Item name="baselineRunId" label="Baseline Run" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="candidateRunId" label="Candidate Run" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item
                name="executorProfileId"
                label="Executor Profile"
                rules={[{ required: true }]}
              >
                <Input />
              </Form.Item>
            </>
          ) : mode === 'policy' ? (
            <>
              <Form.Item name="name" label="名称" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="version" label="版本" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="metric" label="Metric" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="threshold" label="Threshold" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
            </>
          ) : mode === 'feedback' ? (
            <>
              <Form.Item name="traceRef" label="Trace Ref" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="disposition" label="Disposition" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
            </>
          ) : (
            <>
              <Form.Item name="policyId" label="Gate Policy" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="baselineRunId" label="Baseline Run" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <Form.Item name="candidateRunId" label="Candidate Run" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
            </>
          )}
        </Form>
      </Modal>
    </ProductionOperationsPage>
  )
}
