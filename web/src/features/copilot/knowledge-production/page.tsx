import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { PlusOutlined, SafetyCertificateOutlined, SyncOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Form, Input, Modal, Popconfirm, Select, Space } from 'antd'
import { useEffect, useState } from 'react'
import { AIWorkbenchFeatureGate } from '../production/feature-gate'
import { ProductionOperationsPage } from '../production/operations-page'
import { knowledgeProductionMutations } from './mutations'
import { knowledgeProductionQueries } from './queries'
import type { ConnectorFormValues, CreateConnectorInput, StartSyncInput } from './types'

export function KnowledgeProductionPage() {
  return (
    <AIWorkbenchFeatureGate
      features={['knowledge.external_connectors', 'knowledge.async_ingestion']}
    >
      <KnowledgeProductionPageContent />
    </AIWorkbenchFeatureGate>
  )
}

function KnowledgeProductionPageContent() {
  const client = useQueryClient()
  const [connectorOpen, setConnectorOpen] = useState(false)
  const [syncOpen, setSyncOpen] = useState(false)
  const [connectorForm] = Form.useForm<ConnectorFormValues>()
  const [syncForm] = Form.useForm<StartSyncInput>()
  const connectors = useQuery(knowledgeProductionQueries.connectors())
  const jobs = useQuery(knowledgeProductionQueries.jobs())
  const createConnector = useOperationMutation(
    knowledgeProductionMutations.createConnector(client),
    {
      label: ['创建知识 Connector', 'Create knowledge connector'],
      target: (v) => v.name,
      receipt: (r) => ({ message: '创建知识 Connector请求已返回', evidence: r.data.id }),
    },
  )
  const validateConnector = useOperationMutation(
    knowledgeProductionMutations.validateConnector(client),
    {
      label: ['验证知识 Connector', 'Validate knowledge connector'],
      target: (id) => id,
      readOnly: true,
      receipt: (r) => ({ message: '验证知识 Connector请求已返回', evidence: r.data.id }),
    },
  )
  const startSync = useOperationMutation(knowledgeProductionMutations.startSync(client), {
    label: ['启动知识同步', 'Start knowledge sync'],
    target: (v) => v.knowledgeBaseId,
    receipt: (r) => ({ message: '启动知识同步请求已返回', evidence: r.data.id }),
  })
  const jobAction = useOperationMutation(knowledgeProductionMutations.jobAction(client), {
    label: ['变更知识同步任务', 'Change ingestion job'],
    target: (v) => v.id,
    receipt: (r) => ({ message: '变更知识同步任务请求已返回', evidence: r.data.id }),
  })
  const writeBlocked =
    createConnector.blocked || validateConnector.blocked || startSync.blocked || jobAction.blocked
  const blocked = writeBlocked || queryAccessDenied(connectors) || queryAccessDenied(jobs)
  const connectorOpenDenied = queryAccessDenied(createConnector) || queryAccessDenied(connectors)
  useEffect(() => {
    if (connectorOpenDenied) {
      setConnectorOpen(false)
      connectorForm.resetFields()
    }
  }, [connectorOpenDenied, connectorForm])
  const syncOpenDenied = queryAccessDenied(startSync) || queryAccessDenied(jobs)
  useEffect(() => {
    if (syncOpenDenied) {
      setSyncOpen(false)
      syncForm.resetFields()
    }
  }, [syncOpenDenied, syncForm])
  const submitConnector = async () => {
    if (blocked) return
    const values = await connectorForm.validateFields()
    const input: CreateConnectorInput = {
      knowledgeBaseId: values.knowledgeBaseId,
      name: values.name,
      kind: values.kind,
      version: values.version,
      secretRef: values.secretRef,
      config: JSON.parse(values.configJson) as Record<string, unknown>,
      syncPolicy: { mode: 'manual' },
    }
    await createConnector.mutateAsync(input)
    connectorForm.resetFields()
    setConnectorOpen(false)
  }
  const submitSync = async () => {
    if (blocked) return
    await startSync.mutateAsync(await syncForm.validateFields())
    syncForm.resetFields()
    setSyncOpen(false)
  }
  return (
    <ProductionOperationsPage
      feedback={
        <>
          {' '}
          {!connectorOpen ? createConnector.feedback : null}
          {validateConnector.feedback}
          {!syncOpen ? startSync.feedback : null}
          {jobAction.feedback}{' '}
        </>
      }
      refreshing={connectors.isFetching || jobs.isFetching}
      onRefresh={() => void Promise.all([connectors.refetch(), jobs.refetch()])}
      actions={
        <>
          <Button
            disabled={blocked}
            icon={<PlusOutlined />}
            onClick={() => {
              if (!blocked) {
                connectorForm.resetFields()
                createConnector.reset()
                setConnectorOpen(true)
              }
            }}
          >
            新建 Connector
          </Button>
          <Button
            disabled={blocked}
            type="primary"
            icon={<SyncOutlined />}
            onClick={() => {
              if (!blocked) {
                syncForm.resetFields()
                startSync.reset()
                setSyncOpen(true)
              }
            }}
          >
            启动同步
          </Button>
        </>
      }
      tabs={[
        {
          key: 'connectors',
          label: 'Connectors',
          records: readableQueryData(connectors)?.data ?? [],
          loading: connectors.isLoading,
          error: connectors.error,
          hasReadData: connectors.data !== undefined,
          emptyDescription: '配置 HTTP、Git 或 Object Store Connector。',
          actions: (row) => (
            <Button
              disabled={blocked}
              type="link"
              icon={<SafetyCertificateOutlined />}
              onClick={() => validateConnector.mutate(row.id)}
            >
              验证
            </Button>
          ),
        },
        {
          key: 'pipeline',
          label: 'Pipeline Jobs',
          records: readableQueryData(jobs)?.data ?? [],
          loading: jobs.isLoading,
          error: jobs.error,
          hasReadData: jobs.data !== undefined,
          emptyDescription: '同步或重建后会显示分阶段任务。',
          actions: (row) => (
            <Space>
              <Button
                disabled={blocked}
                type="link"
                onClick={() => jobAction.mutate({ id: row.id, action: 'retry' })}
              >
                重试
              </Button>
              <Popconfirm
                disabled={blocked}
                title="确认取消同步任务？"
                onConfirm={() => jobAction.mutate({ id: row.id, action: 'cancel' })}
              >
                <Button disabled={blocked} danger type="link">
                  取消
                </Button>
              </Popconfirm>
            </Space>
          ),
        },
      ]}
    >
      <Modal
        title="新建 Connector"
        open={connectorOpen && !connectorOpenDenied}
        closable={!writeBlocked}
        keyboard={!writeBlocked}
        mask={{ closable: !writeBlocked }}
        okButtonProps={{ disabled: blocked }}
        cancelButtonProps={{ disabled: writeBlocked }}
        onCancel={() => {
          if (!writeBlocked) {
            setConnectorOpen(false)
            connectorForm.resetFields()
          }
        }}
        onOk={() => void submitConnector().catch(() => undefined)}
        confirmLoading={createConnector.isPending}
        destroyOnHidden
      >
        <> {createConnector.feedback} </>
        <Form
          disabled={blocked}
          form={connectorForm}
          layout="vertical"
          preserve={false}
          initialValues={{
            kind: 'http',
            version: 'v1',
            configJson: JSON.stringify(
              {
                url: 'https://docs.example.com/',
                allowedHosts: ['docs.example.com'],
                maxBytes: 8388608,
              },
              null,
              2,
            ),
          }}
        >
          <Form.Item name="knowledgeBaseId" label="Knowledge Base ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="kind" label="类型" rules={[{ required: true }]}>
            <Select
              options={[
                { value: 'http', label: 'HTTP' },
                { value: 'git', label: 'Git' },
                { value: 'object', label: 'Object Store' },
              ]}
            />
          </Form.Item>
          <Form.Item name="version" label="版本" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="secretRef" label="Secret Ref" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="configJson"
            label="Connector Config"
            rules={[
              { required: true },
              {
                validator: async (_, value: string) => {
                  try {
                    const parsed = JSON.parse(value) as unknown
                    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
                      throw new Error('config must be an object')
                    }
                  } catch {
                    throw new Error('请输入有效的 JSON 对象')
                  }
                },
              },
            ]}
          >
            <Input.TextArea autoSize={{ minRows: 7, maxRows: 14 }} />
          </Form.Item>
        </Form>
      </Modal>
      <Modal
        title="启动同步"
        open={syncOpen && !syncOpenDenied}
        closable={!writeBlocked}
        keyboard={!writeBlocked}
        mask={{ closable: !writeBlocked }}
        okButtonProps={{ disabled: blocked }}
        cancelButtonProps={{ disabled: writeBlocked }}
        onCancel={() => {
          if (!writeBlocked) {
            setSyncOpen(false)
            syncForm.resetFields()
          }
        }}
        onOk={() => void submitSync().catch(() => undefined)}
        confirmLoading={startSync.isPending}
        destroyOnHidden
      >
        <> {startSync.feedback} </>
        <Form disabled={blocked} form={syncForm} layout="vertical" preserve={false}>
          <Form.Item name="knowledgeBaseId" label="Knowledge Base ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="sourceId" label="Source ID" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </ProductionOperationsPage>
  )
}
