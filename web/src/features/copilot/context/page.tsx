import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { BugOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Form, Input, InputNumber, Select, Space, Typography } from 'antd'
import { useEffect } from 'react'
import { knowledgeQueries } from '../knowledge/queries'
import { contextMutations } from './mutations'
import type { ContextInspectInput, ContextInspectorFormValues } from './types'

const { Text } = Typography

export function ContextInspectorPage() {
  const [form] = Form.useForm<ContextInspectorFormValues>()
  const basesQuery = useQuery(knowledgeQueries.bases())
  const inspectMutation = useOperationMutation(contextMutations.inspect(), {
    label: ['上下文预览', 'Inspect context'],
    target: () => '当前检查输入',
    readOnly: true,
    receipt: () => ({ message: '上下文预览已返回', evidence: '未提交执行任务。' }),
  })
  const bases = readableQueryData(basesQuery)?.data ?? []

  useEffect(() => {
    if (queryAccessDenied(inspectMutation)) form.resetFields()
  }, [inspectMutation.error, form])
  const inspect = (values: ContextInspectorFormValues) => {
    const knowledgeBaseIds = values.knowledgeBaseIds ?? []
    const input: ContextInspectInput = {
      task: { mode: values.mode, goal: values.goal },
      knowledge: {
        enabled: knowledgeBaseIds.length > 0,
        knowledgeBaseIds,
        query: values.query?.trim() || values.goal,
        topK: values.topK,
      },
      budgets: {
        maxInputTokens: values.maxInputTokens,
        maxEvidenceTokens: values.maxEvidenceTokens,
        maxSteps: values.maxSteps,
      },
    }
    inspectMutation.mutate(input)
  }

  return (
    <div className="soha-page soha-ai-context-inspector">
      <ReadFeedback label="知识库选项" query={basesQuery} />
      <div className="grid gap-3 xl:grid-cols-[minmax(320px,0.8fr)_minmax(0,1.2fr)]">
        <Card size="small" variant="outlined" title="检查输入">
          <Form
            form={form}
            disabled={inspectMutation.isPending}
            onValuesChange={() => inspectMutation.reset()}
            layout="vertical"
            initialValues={{
              mode: 'analysis',
              topK: 5,
              maxInputTokens: 16000,
              maxEvidenceTokens: 6000,
              maxSteps: 8,
            }}
            onFinish={inspect}
          >
            <Form.Item
              name="goal"
              label="任务目标"
              rules={[{ required: true, message: '请输入任务目标' }]}
            >
              <Input.TextArea rows={4} placeholder="描述希望模型或 Agent 完成的任务" />
            </Form.Item>
            <Form.Item name="mode" label="模式">
              <Select
                options={[
                  { value: 'analysis', label: '通用分析' },
                  { value: 'root_cause', label: '根因分析' },
                  { value: 'performance', label: '性能分析' },
                  { value: 'trace', label: '链路分析' },
                  { value: 'inspection_review', label: '巡检复盘' },
                ]}
              />
            </Form.Item>
            <Form.Item name="knowledgeBaseIds" label="知识库">
              <Select
                mode="multiple"
                loading={basesQuery.isLoading}
                options={bases.map((base) => ({ label: base.name, value: base.id }))}
                placeholder="不选择时不加载 RAG 证据"
              />
            </Form.Item>
            <Form.Item name="query" label="检索问题">
              <Input placeholder="留空时使用任务目标" />
            </Form.Item>
            <Space wrap align="start">
              <Form.Item name="topK" label="Top K">
                <InputNumber min={1} max={50} />
              </Form.Item>
              <Form.Item name="maxInputTokens" label="输入 Token">
                <InputNumber min={256} max={200000} />
              </Form.Item>
              <Form.Item name="maxEvidenceTokens" label="证据 Token">
                <InputNumber min={0} max={100000} />
              </Form.Item>
              <Form.Item name="maxSteps" label="最大步骤">
                <InputNumber min={1} max={100} />
              </Form.Item>
            </Space>
            <Button
              type="primary"
              htmlType="submit"
              icon={<BugOutlined />}
              loading={inspectMutation.isPending}
            >
              生成上下文预览
            </Button>
          </Form>
        </Card>
        <Card size="small" variant="outlined" title="Context Envelope">
          {inspectMutation.feedback}
          {inspectMutation.isError ? null : readableQueryData(inspectMutation)?.data ? (
            <Space orientation="vertical" size={12} style={{ width: '100%' }}>
              <Text type="secondary">
                requestId:{' '}
                {String(readableQueryData(inspectMutation)?.data.envelope.requestId || '-')}
              </Text>
              <pre className="soha-code-block">
                {JSON.stringify(readableQueryData(inspectMutation)?.data, null, 2)}
              </pre>
            </Space>
          ) : (
            <ManagementState
              bordered={false}
              title="尚未生成上下文"
              description="提交检查输入后，这里会展示服务端生成的 Envelope、证据引用和预算。"
            />
          )}
        </Card>
      </div>
    </div>
  )
}
