import { lazy, Suspense, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Checkbox,
  Descriptions,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Switch,
  Tabs,
  Typography,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { deliveryApi } from '../api'
import { deliveryKeys } from '../keys'
import { deliveryQueries } from '../queries'
import { documentKindLabels } from '../documents/model'
import { useUnsavedDocument } from '../documents/use-unsaved-document'
import type {
  DeliveryTemplateSource,
  DeliveryTemplateSourceInput,
  DeliveryTemplateSourceAssociation,
  DeliveryTemplateSourceRemoveInput,
} from '../types'
import { TemplateSourceSync } from './sync-run'

const TriggerManager = lazy(() =>
  import('../triggers/manager').then((module) => ({ default: module.DeliveryTriggerManager })),
)

const kinds = Object.entries(documentKindLabels).map(([value, label]) => ({ value, label }))

export function TemplateSourceManager({
  initialId,
  onClose,
}: {
  initialId?: string
  onClose: () => void
}) {
  const permission = usePermissionSnapshot()
  const allowed = (action: string) =>
    hasPermission(permission.data?.data, `delivery.template-sources.${action}`)
  const [offset, setOffset] = useState(0)
  const [id, setId] = useState(initialId ?? '')
  const [editing, setEditing] = useState<{ source?: DeliveryTemplateSource }>()
  const sources = useQuery(deliveryQueries.templateSources.list(offset, allowed('view')))
  const selectedId = id || sources.data?.[0]?.id || ''
  const detail = useQuery(deliveryQueries.templateSources.detail(selectedId, allowed('view')))
  const source = detail.data
  return (
    <Modal open title="Git 来源" width={1060} footer={null} onCancel={onClose}>
      <Space orientation="vertical" style={{ width: '100%' }}>
        <Space wrap>
          <Select
            aria-label="Git 来源"
            placeholder="选择来源"
            style={{ minWidth: 320 }}
            value={selectedId || undefined}
            onChange={setId}
            options={[
              ...(sources.data ?? []),
              ...(source && !sources.data?.some((item) => item.id === source.id) ? [source] : []),
            ].map((item) => ({ value: item.id, label: item.name }))}
          />
          <Button type="primary" disabled={!allowed('create')} onClick={() => setEditing({})}>
            添加来源
          </Button>
          <Button disabled={!source || !allowed('update')} onClick={() => setEditing({ source })}>
            编辑配置
          </Button>
          <Button
            onClick={() => {
              void sources.refetch()
              if (selectedId) void detail.refetch()
            }}
          >
            刷新
          </Button>
        </Space>
        {offset > 0 || sources.data?.length === 50 ? (
          <Space>
            <Button
              disabled={offset === 0 || sources.isFetching}
              onClick={() => setOffset(offset - 50)}
            >
              上一页
            </Button>
            <Button
              disabled={(sources.data?.length ?? 0) < 50 || sources.isFetching}
              onClick={() => setOffset(offset + 50)}
            >
              下一页
            </Button>
          </Space>
        ) : null}
        {sources.isError || detail.isError ? (
          <Alert
            type="error"
            showIcon
            title="来源读取失败"
            description={(sources.error || detail.error)?.message}
          />
        ) : sources.isLoading || detail.isLoading ? (
          <ManagementState compact kind="loading" />
        ) : source ? (
          <Tabs
            key={source.id}
            items={[
              {
                key: 'sync',
                label: '同步与差异',
                children: <TemplateSourceSync source={source} canSync={allowed('sync')} />,
              },
              {
                key: 'triggers',
                label: '自动同步',
                children: (
                  <Suspense fallback={<ManagementState compact kind="loading" />}>
                    <TriggerManager
                      targetKind="template_source"
                      targetId={source.id}
                      webhookRefs={
                        source.refType === 'commit'
                          ? []
                          : [
                              {
                                provider: 'gitlab_standard',
                                repositoryId: source.repositoryId,
                                refType: source.refType,
                                refValue: source.refValue,
                              },
                            ]
                      }
                    />
                  </Suspense>
                ),
              },
              {
                key: 'config',
                label: '配置',
                children: (
                  <Descriptions
                    column={1}
                    items={[
                      { key: 'repo', label: '仓库', children: source.repositoryId },
                      {
                        key: 'ref',
                        label: '引用',
                        children: `${{ branch: '分支', tag: '标签', commit: '提交' }[source.refType]} · ${source.refValue}`,
                      },
                      { key: 'path', label: '目录', children: source.path },
                      {
                        key: 'kinds',
                        label: '文档类型',
                        children: source.kinds.map((kind) => documentKindLabels[kind]).join('、'),
                      },
                      {
                        key: 'include',
                        label: '包含文件',
                        children:
                          source.includePatterns?.join('、') ||
                          '*.soha.yaml / *.soha.yml / *.soha.json（包含子目录）',
                      },
                      {
                        key: 'exclude',
                        label: '排除文件',
                        children: source.excludePatterns?.join('、') || '无',
                      },
                      { key: 'enabled', label: '启用同步', children: source.enabled ? '是' : '否' },
                    ]}
                  />
                ),
              },
              {
                key: 'objects',
                label: '关联对象',
                children: (
                  <SourceObjects
                    source={source}
                    canUpdate={allowed('update')}
                    canRemove={allowed('delete')}
                    onRemoved={() => {
                      setId('')
                      void sources.refetch()
                    }}
                  />
                ),
              },
            ]}
          />
        ) : (
          <ManagementState
            compact
            kind="empty"
            title="暂无 Git 来源"
            description="选择已登记的代码仓库、引用和目录，导入其中的 Soha 交付文档。"
          />
        )}
      </Space>
      {editing ? (
        <SourceEditor
          source={editing.source}
          onClose={() => setEditing(undefined)}
          onSaved={(saved) => {
            setId(saved.id)
            setEditing(undefined)
          }}
        />
      ) : null}
    </Modal>
  )
}

function SourceEditor({
  source,
  onClose,
  onSaved,
}: {
  source?: DeliveryTemplateSource
  onClose: () => void
  onSaved: (source: DeliveryTemplateSource) => void
}) {
  const [form] = Form.useForm<DeliveryTemplateSourceInput>()
  const [search, setSearch] = useState('')
  const [dirty, setDirty] = useState(false)
  useUnsavedDocument(dirty)
  const client = useQueryClient()
  const repositories = useQuery(deliveryQueries.repositories.list({ search, limit: 200 }))
  const save = useMutation({
    mutationFn: (input: DeliveryTemplateSourceInput) =>
      source
        ? deliveryApi.templateSources.update(source.id, input)
        : deliveryApi.templateSources.create(input),
    onSuccess: async (saved) => {
      await client.invalidateQueries({ queryKey: deliveryKeys.all })
      onSaved(saved)
    },
  })
  return (
    <Modal
      open
      title={source ? '编辑 Git 来源' : '添加 Git 来源'}
      width={700}
      okText="保存配置"
      confirmLoading={save.isPending}
      onOk={() => {
        void form
          .validateFields()
          .then((values) => save.mutate({ ...values, expectedGeneration: source?.generation ?? 0 }))
          .catch(() => {})
      }}
      onCancel={() => {
        if (!save.isPending && (!dirty || window.confirm('放弃未保存的来源配置？'))) onClose()
      }}
    >
      {save.error ? (
        <Alert type="error" title="配置保存失败" description={save.error.message} />
      ) : null}
      {repositories.isError ? (
        <Alert
          type="error"
          title="仓库读取失败"
          action={<Button onClick={() => void repositories.refetch()}>重试</Button>}
        />
      ) : null}
      <Form
        form={form}
        layout="vertical"
        disabled={save.isPending}
        onValuesChange={() => setDirty(true)}
        initialValues={
          source ?? {
            name: '',
            repositoryId: '',
            refType: 'branch',
            refValue: 'main',
            path: '.',
            kinds: ['BuildTemplate', 'DeploymentTemplate', 'WorkflowTemplate'],
            enabled: true,
          }
        }
      >
        <Form.Item
          name="name"
          label="名称"
          rules={[{ required: true, whitespace: true, max: 200 }]}
        >
          <Input />
        </Form.Item>
        <Form.Item
          name="repositoryId"
          label="代码仓库"
          rules={[{ required: true }]}
          extra="仓库需关联已授权的代码源连接。凭据在系统集成中维护。"
        >
          <Select
            showSearch={{ onSearch: setSearch, filterOption: false }}
            loading={repositories.isFetching}
            options={(repositories.data ?? [])
              .filter((item) => item.sourceConnectionId && item.providerRepositoryId)
              .map((item) => ({ value: item.id, label: item.name }))}
          />
        </Form.Item>
        <Form.Item name="refType" label="引用类型">
          <Select
            options={[
              { value: 'branch', label: '分支' },
              { value: 'tag', label: '标签' },
              { value: 'commit', label: '完整提交 SHA' },
            ]}
          />
        </Form.Item>
        <Form.Item
          name="refValue"
          label="引用"
          rules={[{ required: true, whitespace: true, max: 512 }]}
        >
          <Input />
        </Form.Item>
        <Form.Item
          name="path"
          label="文档目录"
          rules={[{ required: true, whitespace: true, max: 512 }]}
        >
          <Input placeholder=". 或 templates" />
        </Form.Item>
        <Form.Item
          name="kinds"
          label="文档类型"
          rules={[{ required: true, type: 'array', min: 1 }]}
        >
          <Checkbox.Group options={kinds} />
        </Form.Item>
        <Form.Item
          name="includePatterns"
          label="包含文件"
          extra="留空时读取目录及子目录下的 *.soha.yaml、*.soha.yml、*.soha.json。"
        >
          <Select mode="tags" tokenSeparators={[',']} open={false} />
        </Form.Item>
        <Form.Item name="excludePatterns" label="排除文件">
          <Select mode="tags" tokenSeparators={[',']} open={false} />
        </Form.Item>
        <Form.Item name="enabled" label="启用同步" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  )
}

function SourceObjects({
  source,
  canUpdate,
  canRemove,
  onRemoved,
}: {
  source: DeliveryTemplateSource
  canUpdate: boolean
  canRemove: boolean
  onRemoved: () => void
}) {
  const [offset, setOffset] = useState(0)
  const [removal, setRemoval] = useState<{
    object?: DeliveryTemplateSourceAssociation
    generation: number
  }>()
  const [disposition, setDisposition] =
    useState<DeliveryTemplateSourceRemoveInput['disposition']>('keep')
  const objects = useQuery(deliveryQueries.templateSources.objects(source.id, offset))
  const client = useQueryClient()
  const { message } = App.useApp()
  const remove = useMutation({
    mutationFn: () => {
      if (!removal) throw new Error('请先选择关联对象。')
      const input = { expectedGeneration: removal.generation, disposition }
      return removal.object
        ? deliveryApi.templateSources.detach(
            source.id,
            removal.object.kind,
            removal.object.objectId,
            input,
          )
        : deliveryApi.templateSources.remove(source.id, input)
    },
    onSuccess: async () => {
      const wholeSource = !removal?.object
      setRemoval(undefined)
      await client.invalidateQueries({ queryKey: deliveryKeys.all })
      message.success('已解除 Git 管理')
      if (wholeSource) onRemoved()
    },
  })
  const review = (object?: DeliveryTemplateSourceAssociation) => {
    remove.reset()
    setDisposition('keep')
    setRemoval({ object, generation: source.generation })
  }
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      {objects.isError ? (
        <Alert
          type="error"
          title="关联对象读取失败"
          action={<Button onClick={() => void objects.refetch()}>重试</Button>}
        />
      ) : objects.isLoading ? (
        <ManagementState compact kind="loading" />
      ) : (
        <>
          {objects.data?.map((item) => (
            <Card
              size="small"
              key={`${item.kind}:${item.objectId}`}
              title={`${documentKindLabels[item.kind]} · ${item.key}`}
              extra={
                <Button disabled={!canUpdate} onClick={() => review(item)}>
                  解除关联
                </Button>
              }
            >
              <Typography.Text>
                {item.path}
                {item.removed ? ' · 来源文件已移除' : ''}
              </Typography.Text>
              <Typography.Paragraph type="secondary">{item.resolvedCommit}</Typography.Paragraph>
            </Card>
          ))}
          {!objects.data?.length ? (
            <ManagementState compact kind="empty" title="暂无关联对象" />
          ) : null}
          <Space wrap>
            <Button
              disabled={offset === 0 || objects.isFetching}
              onClick={() => setOffset(offset - 50)}
            >
              上一页
            </Button>
            <Button
              disabled={(objects.data?.length ?? 0) < 50 || objects.isFetching}
              onClick={() => setOffset(offset + 50)}
            >
              下一页
            </Button>
            <Button danger disabled={!canRemove || objects.isFetching} onClick={() => review()}>
              移除整个来源
            </Button>
          </Space>
        </>
      )}
      <Modal
        open={Boolean(removal)}
        title={removal?.object ? `解除 ${removal.object.key} 的 Git 关联` : '移除整个 Git 来源'}
        okText="确认解除"
        confirmLoading={remove.isPending}
        onOk={() => remove.mutate()}
        onCancel={() => {
          if (!remove.isPending) setRemoval(undefined)
        }}
      >
        <Typography.Paragraph>
          {removal?.object
            ? `文件：${removal.object.path}`
            : '这会解除该来源全部对象的 Git 管理，包括其他分页中的对象。'}{' '}
          绑定、运行记录和已发布版本来源会保留。
        </Typography.Paragraph>
        <Select
          aria-label="关联解除后的处理"
          style={{ width: '100%' }}
          value={disposition}
          onChange={setDisposition}
          options={[
            { value: 'keep', label: '保留定义，转为 Soha 管理' },
            ...(removal?.object?.kind === 'Workflow' ||
            (!removal?.object && source.kinds.includes('Workflow'))
              ? []
              : [{ value: 'deprecate', label: '同时废弃关联模板，停止新的选择' }]),
          ]}
        />
        {remove.error ? (
          <Alert
            type="error"
            showIcon
            title="解除失败，请检查权限或刷新后重新查看关联"
            description={remove.error.message}
          />
        ) : null}
      </Modal>
    </Space>
  )
}
