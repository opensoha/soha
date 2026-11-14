import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementIconButton,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  DeleteOutlined,
  EyeOutlined,
  PlusOutlined,
  ReloadOutlined,
  SyncOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Button,
  Card,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Tabs,
  Typography,
} from 'antd'
import type { ReactNode } from 'react'
import { Children, useEffect, useState } from 'react'
import { knowledgeMutations } from './mutations'
import { knowledgeQueries } from './queries'
import type {
  CreateKnowledgeBaseInput,
  CreateKnowledgeSourceInput,
  KnowledgeBase,
  KnowledgeSearchInput,
} from './types'

const { Text } = Typography

function KnowledgeList({
  children,
  emptyText,
  loading,
}: {
  children: ReactNode
  emptyText: string
  loading?: boolean
}) {
  if (loading) return <Spin size="small" />
  if (Children.count(children) === 0) {
    return <ManagementState bordered={false} compact description={emptyText} />
  }
  return <ul className="soha-list-panel">{children}</ul>
}

export function KnowledgeCenterPage() {
  const queryClient = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const [sourceOpen, setSourceOpen] = useState(false)
  const [selectedBase, setSelectedBase] = useState<KnowledgeBase | null>(null)
  const [createForm] = Form.useForm<CreateKnowledgeBaseInput>()
  const [searchForm] = Form.useForm<KnowledgeSearchInput>()
  const [sourceForm] = Form.useForm<CreateKnowledgeSourceInput>()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const basesQuery = useQuery(knowledgeQueries.bases())
  const sourcesQuery = useQuery(knowledgeQueries.sources(selectedBase?.id))
  const documentsQuery = useQuery(knowledgeQueries.documents(selectedBase?.id))
  const syncRunsQuery = useQuery(knowledgeQueries.syncRuns(selectedBase?.id))
  const revisionsQuery = useQuery(knowledgeQueries.indexRevisions(selectedBase?.id))
  const createMutation = useOperationMutation(knowledgeMutations.createBase(queryClient), {
    label: ['创建知识库', 'Create knowledge base'],
    target: (v) => v.name,
    receipt: (r) => ({ message: '知识库请求已返回', evidence: r.data.id }),
  })
  const deleteMutation = useOperationMutation(knowledgeMutations.deleteBase(queryClient), {
    label: ['删除知识库', 'Delete knowledge base'],
    target: (id) => id,
  })
  const searchMutation = useOperationMutation(knowledgeMutations.search(), {
    label: ['检索知识', 'Search knowledge'],
    target: () => '当前检索输入',
    readOnly: true,
  })
  const createSourceMutation = useOperationMutation(
    knowledgeMutations.createSource(queryClient, selectedBase?.id ?? ''),
    {
      label: ['添加知识来源', 'Add knowledge source'],
      target: (v) => v.name,
      receipt: (r) => ({ message: '来源请求已返回', evidence: r.data.id }),
    },
  )
  const syncSourceMutation = useOperationMutation(
    knowledgeMutations.syncSource(queryClient, selectedBase?.id ?? ''),
    {
      label: ['同步知识来源', 'Sync knowledge source'],
      target: (id) => id,
      receipt: (r) => ({ message: '同步请求已返回，请查看同步运行', evidence: r.data.id }),
    },
  )
  const bases = readableQueryData(basesQuery)?.data ?? []
  const permissionSnapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canCreateBase = hasPermission(permissionSnapshot, 'ai.knowledge.create')
  const canUpdateBase = hasPermission(permissionSnapshot, 'ai.knowledge.update')
  const canDeleteBase = hasPermission(permissionSnapshot, 'ai.knowledge.delete')
  const canOperateIngestion = hasPermission(permissionSnapshot, 'ai.knowledge.ingestion.operate')

  const blocked =
    [createMutation, deleteMutation, createSourceMutation, syncSourceMutation].some(
      (m) => m.blocked,
    ) ||
    queryAccessDenied(basesQuery) ||
    queryAccessDenied(sourcesQuery)
  const searchResult = readableQueryData(searchMutation)?.data
  const baseDenied = queryAccessDenied(basesQuery) || queryAccessDenied(permissionSnapshotQuery)
  const sourceDenied = queryAccessDenied(sourcesQuery) || queryAccessDenied(createSourceMutation)
  const createDenied = queryAccessDenied(createMutation)
  useEffect(() => {
    if (baseDenied || createDenied || !canCreateBase) {
      setCreateOpen(false)
      createForm.resetFields()
    }
  }, [baseDenied, createDenied, canCreateBase, createForm])
  useEffect(() => {
    if (baseDenied || sourceDenied || !canUpdateBase) {
      setSourceOpen(false)
      sourceForm.resetFields()
    }
  }, [baseDenied, sourceDenied, canUpdateBase, sourceForm])
  useEffect(() => {
    if (baseDenied) {
      setSelectedBase(null)
      searchForm.resetFields()
      searchMutation.reset()
    }
  }, [baseDenied, searchForm])

  const columns: TableColumnsType<KnowledgeBase> = [
    { title: '名称', dataIndex: 'name', key: 'name' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 120,
      render: (value) => <StatusTag value={String(value || 'unknown')} />,
    },
    {
      title: '来源',
      dataIndex: 'sourceCount',
      key: 'sourceCount',
      width: 90,
      render: (value) => value ?? '-',
    },
    {
      title: '文档',
      dataIndex: 'documentCount',
      key: 'documentCount',
      width: 90,
      render: (value) => value ?? '-',
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      width: 190,
      render: (value) => value || '-',
    },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      render: (_, record) => (
        <Space size={4}>
          <Button
            type="text"
            icon={<EyeOutlined />}
            aria-label={`查看知识库 ${record.name}`}
            disabled={blocked}
            onClick={() => setSelectedBase(record)}
          />
          <Popconfirm
            cancelText="取消"
            description="知识库及其来源、文档和索引将不可恢复。"
            disabled={blocked || !canDeleteBase}
            okButtonProps={{ danger: true }}
            okText="删除"
            title={`删除知识库 ${record.name}？`}
            onConfirm={() => deleteMutation.mutateAsync(record.id)}
          >
            <Button
              danger
              type="text"
              icon={<DeleteOutlined />}
              aria-label={`删除知识库 ${record.name}`}
              loading={deleteMutation.isPending && deleteMutation.variables === record.id}
              disabled={blocked || !canDeleteBase}
            />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const submitCreate = async () => {
    if (blocked || !canCreateBase) return
    const values = await createForm.validateFields()
    await createMutation.mutateAsync(values)
    createForm.resetFields()
    setCreateOpen(false)
  }

  const submitSource = async () => {
    if (blocked || !canUpdateBase || !selectedBase) return
    const values = await sourceForm.validateFields()
    await createSourceMutation.mutateAsync(values)
    sourceForm.resetFields()
    setSourceOpen(false)
  }

  return (
    <ManagementDataPage
      className="soha-ai-knowledge-center"
      query={{
        form: searchForm,
        initialValues: { topK: 8, filters: {} },
        collapsible: false,
        onFinish: (values) => searchMutation.mutate(values as KnowledgeSearchInput),
        actions: (
          <ManagementQueryActions
            loading={searchMutation.isPending}
            resetLabel="清空"
            submitLabel="检索"
            onReset={() => {
              searchForm.resetFields()
              searchMutation.reset()
            }}
          />
        ),
        children: (
          <>
            <ManagementQueryField
              grow
              label="检索问题"
              name="query"
              rules={[{ required: true, message: '请输入检索问题' }]}
            >
              <Input placeholder="输入要从知识库检索的问题" allowClear />
            </ManagementQueryField>
            <ManagementQueryField
              label="知识库"
              name="knowledgeBaseIds"
              width={320}
              minWidth={240}
              rules={[{ required: true, message: '请选择知识库' }]}
            >
              <Select
                mode="multiple"
                placeholder="选择知识库"
                options={bases.map((base) => ({ label: base.name, value: base.id }))}
              />
            </ManagementQueryField>
          </>
        ),
      }}
      beforeQuery={
        <>
          {!createOpen && createMutation.feedback}
          {deleteMutation.feedback}
          {!sourceOpen && createSourceMutation.feedback}
          {syncSourceMutation.feedback}
          {searchMutation.feedback}
        </>
      }
      table={{
        columns,
        dataSource: bases,
        loading: basesQuery.isLoading,
        error: basesQuery.error,
        hasReadData: readableQueryData(basesQuery) !== undefined,
        onRetry: () => void basesQuery.refetch(),
        rowKey: 'id',
        toolbarExtra: (
          <ManagementTableToolbar>
            <ManagementIconButton
              tooltip="刷新"
              aria-label="刷新知识库"
              icon={<ReloadOutlined />}
              loading={basesQuery.isFetching}
              onClick={() => void basesQuery.refetch()}
            />
            {canCreateBase ? (
              <Button
                type="primary"
                icon={<PlusOutlined />}
                disabled={blocked}
                onClick={() => {
                  createMutation.reset()
                  createForm.resetFields()
                  setCreateOpen(true)
                }}
              >
                新建知识库
              </Button>
            ) : null}
          </ManagementTableToolbar>
        ),
        empty: (
          <ManagementState title="暂无知识库" description="创建知识库后可添加来源并构建索引。" />
        ),
      }}
    >
      {searchResult ? (
        <div className="soha-list-panel">
          <Space wrap>
            <Text>命中 {searchResult.hits?.length ?? 0} 条</Text>
            <Text type="secondary">{searchResult.timingMs ?? '-'} ms</Text>
            {searchResult.noAnswer ? <StatusTag value="warning" label="证据不足" /> : null}
          </Space>
          <KnowledgeList emptyText="暂无检索结果">
            {(searchResult.hits ?? []).map((hit, index) => (
              <li key={`${hit.documentId}:${index}`} className="soha-list-row">
                <div className="soha-list-row-meta">
                  <div>
                    <Text strong>{hit.title || hit.documentId || '检索片段'}</Text>
                    <div>
                      <Text type="secondary">{hit.content || hit.source || '-'}</Text>
                    </div>
                  </div>
                </div>
                {typeof hit.score === 'number' ? (
                  <div className="soha-list-row-extra">
                    <Text type="secondary">{hit.score.toFixed(3)}</Text>
                  </div>
                ) : null}
              </li>
            ))}
          </KnowledgeList>
        </div>
      ) : null}
      <Modal
        open={createOpen}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: !blocked }}
        cancelButtonProps={{ disabled: blocked }}
        okButtonProps={{ disabled: blocked, 'aria-label': '创建' }}
        title="新建知识库"
        okText="创建"
        cancelText="取消"
        confirmLoading={createMutation.isPending}
        destroyOnHidden
        onCancel={() => {
          if (!blocked) {
            setCreateOpen(false)
            createForm.resetFields()
          }
        }}
        onOk={() => void submitCreate().catch(() => undefined)}
      >
        {createMutation.feedback}
        <Form
          form={createForm}
          disabled={blocked}
          layout="vertical"
          preserve={false}
          initialValues={{
            scope: { visibility: 'private' },
            retrievalPolicy: {
              defaultTopK: 5,
              maxTopK: 20,
              lexicalWeight: 0.45,
              vectorWeight: 0.55,
              minScore: 0.05,
            },
          }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input autoFocus maxLength={120} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={3} maxLength={500} />
          </Form.Item>
          <Form.Item name={['scope', 'visibility']} label="可见范围" rules={[{ required: true }]}>
            <Select
              options={[
                { label: '仅本人', value: 'private' },
                { label: '受限成员', value: 'restricted' },
                { label: '当前平台公开', value: 'public' },
              ]}
            />
          </Form.Item>
        </Form>
      </Modal>
      <Drawer
        open={Boolean(selectedBase)}
        title={selectedBase?.name || '知识库详情'}
        size="large"
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: !blocked }}
        destroyOnHidden
        extra={
          canUpdateBase ? (
            <Button
              icon={<PlusOutlined />}
              disabled={blocked}
              onClick={() => {
                createSourceMutation.reset()
                sourceForm.resetFields()
                setSourceOpen(true)
              }}
            >
              添加来源
            </Button>
          ) : null
        }
        onClose={() => {
          if (!blocked) {
            setSelectedBase(null)
            setSourceOpen(false)
            sourceForm.resetFields()
          }
        }}
      >
        {syncSourceMutation.feedback}
        <Tabs
          items={[
            {
              key: 'sources',
              label: `来源 (${readableQueryData(sourcesQuery)?.data.length ?? '—'})`,
              children: (
                <>
                  <ReadFeedback query={sourcesQuery} label="知识来源" />
                  <KnowledgeList
                    loading={sourcesQuery.isLoading}
                    emptyText={sourcesQuery.isError ? '来源加载失败' : '暂无来源'}
                  >
                    {(readableQueryData(sourcesQuery)?.data ?? []).map((item) => (
                      <li key={item.id} className="soha-list-row">
                        <div className="soha-list-row-meta">
                          <div>
                            <Text strong>{item.name}</Text>
                            <div>
                              <Text type="secondary">{`${item.kind}${item.lastError ? ` · ${item.lastError}` : ''}`}</Text>
                            </div>
                          </div>
                        </div>
                        <div className="soha-list-row-extra">
                          <StatusTag value={item.status} />
                          {canOperateIngestion ? (
                            <Button
                              type="text"
                              icon={<SyncOutlined />}
                              aria-label={`同步来源 ${item.name}`}
                              loading={
                                syncSourceMutation.isPending &&
                                syncSourceMutation.variables === item.id
                              }
                              disabled={blocked}
                              onClick={() => syncSourceMutation.mutate(item.id)}
                            />
                          ) : null}
                        </div>
                      </li>
                    ))}
                  </KnowledgeList>
                </>
              ),
            },
            {
              key: 'documents',
              label: `文档 (${readableQueryData(documentsQuery)?.data.length ?? '—'})`,
              children: (
                <>
                  <ReadFeedback query={documentsQuery} label="知识文档" />
                  <KnowledgeList
                    loading={documentsQuery.isLoading}
                    emptyText={documentsQuery.isError ? '文档加载失败' : '暂无文档'}
                  >
                    {(readableQueryData(documentsQuery)?.data ?? []).map((item) => (
                      <li key={item.id} className="soha-list-row">
                        <div className="soha-list-row-meta">
                          <div>
                            <Text strong>{item.title}</Text>
                            <div>
                              <Text type="secondary">{item.uri || item.version || '-'}</Text>
                            </div>
                          </div>
                        </div>
                        <div className="soha-list-row-extra">
                          <MetadataTag label={`${item.chunkCount ?? 0} chunks`} />
                        </div>
                      </li>
                    ))}
                  </KnowledgeList>
                </>
              ),
            },
            {
              key: 'sync-runs',
              label: `同步运行 (${readableQueryData(syncRunsQuery)?.data.length ?? '—'})`,
              children: (
                <>
                  <ReadFeedback query={syncRunsQuery} label="知识同步运行" />
                  <KnowledgeList
                    loading={syncRunsQuery.isLoading}
                    emptyText={syncRunsQuery.isError ? '同步记录加载失败' : '暂无同步记录'}
                  >
                    {(readableQueryData(syncRunsQuery)?.data ?? []).map((item) => (
                      <li key={item.id} className="soha-list-row">
                        <div className="soha-list-row-meta">
                          <div>
                            <Text strong>{item.id}</Text>
                            <div>
                              <Text type="secondary">{`${item.documentsStored ?? 0} documents · ${item.chunksStored ?? 0} chunks`}</Text>
                            </div>
                          </div>
                        </div>
                        <div className="soha-list-row-extra">
                          <StatusTag value={item.status} />
                        </div>
                      </li>
                    ))}
                  </KnowledgeList>
                </>
              ),
            },
            {
              key: 'revisions',
              label: `索引版本 (${readableQueryData(revisionsQuery)?.data.length ?? '—'})`,
              children: (
                <>
                  <ReadFeedback query={revisionsQuery} label="索引版本" />
                  <KnowledgeList
                    loading={revisionsQuery.isLoading}
                    emptyText={revisionsQuery.isError ? '索引版本加载失败' : '暂无索引版本'}
                  >
                    {(readableQueryData(revisionsQuery)?.data ?? []).map((item) => (
                      <li key={item.id} className="soha-list-row">
                        <div className="soha-list-row-meta">
                          <div>
                            <Text strong>{`Revision ${item.revision}`}</Text>
                            <div>
                              <Text type="secondary">{`${item.documentCount ?? 0} documents · ${item.chunkCount ?? 0} chunks`}</Text>
                            </div>
                          </div>
                        </div>
                        <div className="soha-list-row-extra">
                          <StatusTag value={item.status} />
                        </div>
                      </li>
                    ))}
                  </KnowledgeList>
                </>
              ),
            },
          ]}
        />
      </Drawer>
      <Modal
        open={sourceOpen}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: !blocked }}
        cancelButtonProps={{ disabled: blocked }}
        okButtonProps={{ disabled: blocked }}
        title="添加 Inline 来源"
        okText="添加"
        cancelText="取消"
        width={720}
        confirmLoading={createSourceMutation.isPending}
        destroyOnHidden
        onCancel={() => {
          if (!blocked) {
            setSourceOpen(false)
            sourceForm.resetFields()
          }
        }}
        onOk={() => void submitSource().catch(() => undefined)}
      >
        {createSourceMutation.feedback}
        <Form
          form={sourceForm}
          disabled={blocked}
          layout="vertical"
          preserve={false}
          initialValues={{
            kind: 'inline',
            syncPolicy: { mode: 'manual' },
            config: { documents: [{ externalId: '', title: '', content: '' }] },
          }}
        >
          <Form.Item name="name" label="来源名称" rules={[{ required: true }]}>
            <Input maxLength={120} />
          </Form.Item>
          <Form.List name={['config', 'documents']}>
            {(fields, { add, remove }) => (
              <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                {fields.map((field, index) => (
                  <Card
                    key={field.key}
                    size="small"
                    title={`文档 ${index + 1}`}
                    extra={
                      fields.length > 1 ? (
                        <Button
                          danger
                          type="text"
                          icon={<DeleteOutlined />}
                          aria-label={`移除文档 ${index + 1}`}
                          disabled={blocked}
                          onClick={() => remove(field.name)}
                        />
                      ) : null
                    }
                  >
                    <Form.Item
                      name={[field.name, 'externalId']}
                      label="External ID"
                      rules={[{ required: true }]}
                    >
                      <Input maxLength={200} />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, 'title']}
                      label="标题"
                      rules={[{ required: true }]}
                    >
                      <Input maxLength={300} />
                    </Form.Item>
                    <Form.Item
                      name={[field.name, 'content']}
                      label="内容"
                      rules={[{ required: true }]}
                    >
                      <Input.TextArea rows={6} />
                    </Form.Item>
                    <Form.Item name={[field.name, 'uri']} label="来源 URI">
                      <Input placeholder="https://docs.example.com/page" />
                    </Form.Item>
                  </Card>
                ))}
                <Button icon={<PlusOutlined />} disabled={blocked} onClick={() => add()}>
                  添加文档
                </Button>
              </Space>
            )}
          </Form.List>
          <Form.Item name="kind" hidden>
            <Input />
          </Form.Item>
          <Form.Item name={['syncPolicy', 'mode']} hidden>
            <Input />
          </Form.Item>
        </Form>
      </Modal>
    </ManagementDataPage>
  )
}
