import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  CheckOutlined,
  PlusOutlined,
  CloudSyncOutlined,
  ImportOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  RollbackOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { StatusTag } from '@/components/status-tag'
import { AdminTable } from '@/components/admin-table'
import { ManifestResourceInventoryTable } from './resource-inventory'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { deliveryQueries } from '../queries'
import { formatDateTime } from '@/utils/time'
import { manifestApi } from './api'
import { manifestMutations } from './mutations'
import { manifestQueries } from './queries'
import type {
  ManifestDeliveryIntent,
  ManifestDeployment,
  ManifestExecutionTask,
  ManifestPackage,
  ManifestKustomizeOptions,
  ManifestRenderResult,
  ManifestSource,
  ManifestSyncRun,
} from './types'

const { Text } = Typography

interface SourceFormValue {
  mode: ManifestSource['mode']
  repositoryId?: string
  refType?: ManifestSource['refType']
  refValue?: string
  path?: string
  includePatterns?: string
  excludePatterns?: string
  syncPolicy: ManifestSource['syncPolicy']
  pollIntervalSeconds?: number
  autoPublish: boolean
  autoDeploy: boolean
}

interface BindingFormValue {
  enabled: boolean
  overlay: string
  kustomize?: ManifestKustomizeOptions
}

export function parseBindingOverlay(value?: string): Record<string, string> {
  if (!value?.trim()) return {}
  let parsed: unknown
  try {
    parsed = JSON.parse(value)
  } catch {
    throw new Error('请输入有效的 JSON 对象')
  }
  if (
    !parsed ||
    Array.isArray(parsed) ||
    typeof parsed !== 'object' ||
    Object.values(parsed).some((item) => typeof item !== 'string')
  ) {
    throw new Error('覆盖值必须是键值均为字符串的 JSON 对象')
  }
  return parsed as Record<string, string>
}

export function ManifestOperationsPanel({ item }: { item: ManifestPackage }) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [sourceForm] = Form.useForm<SourceFormValue>()
  const [bindingForm] = Form.useForm<BindingFormValue>()
  const [bindingId, setBindingId] = useState('')
  const [revision, setRevision] = useState(item.currentRevision || 1)
  const [rendered, setRendered] = useState<ManifestRenderResult | null>(null)
  const snapshot = usePermissionSnapshot().data?.data
  const canUpdateSource = hasPermission(snapshot, 'delivery.manifest-sources.update')
  const canSyncSource = hasPermission(snapshot, 'delivery.manifest-sources.sync')
  const canTriggerDeployment = hasPermission(snapshot, 'delivery.manifest-deployments.trigger')
  const canPreflightDeployment = hasPermission(snapshot, 'delivery.manifest-deployments.preflight')
  const canUpdateDeployment = hasPermission(snapshot, 'delivery.manifest-deployments.update')
  const canRepair = hasPermission(snapshot, 'delivery.manifest-drift.repair')
  const canAdopt = hasPermission(snapshot, 'delivery.manifest-drift.adopt')
  const canUseAI = hasPermission(snapshot, 'observe.ai.chat')
  const canUpdateApplication = hasPermission(snapshot, 'delivery.application.update')
  const canDecideIntent = canUseAI && canUpdateApplication
  const sourceQuery = useQuery(manifestQueries.source(item.id))
  const bindingsQuery = useQuery(manifestQueries.bindings(item.id))
  const syncRunsQuery = useQuery(manifestQueries.syncRuns(item.id))
  const deploymentsQuery = useQuery(manifestQueries.deployments(item.id))
  const intentsQuery = useQuery(manifestQueries.intents(item.id, canUseAI))
  const repositoriesQuery = useQuery(
    deliveryQueries.repositories.list({ applicationId: item.applicationId }),
  )
  const sourceMutation = useMutation(manifestMutations.updateSource(queryClient))
  const bindingMutation = useMutation(manifestMutations.updateBinding(queryClient))
  const syncMutation = useMutation(manifestMutations.sync(queryClient))
  const preflightMutation = useMutation(manifestMutations.preflight())
  const desiredMutation = useMutation(manifestMutations.setDesiredRevision(queryClient))
  const deploymentMutation = useMutation(manifestMutations.deploymentAction(queryClient))
  const intentMutation = useMutation(manifestMutations.decideIntent(queryClient))
  const sourceMode = Form.useWatch('mode', sourceForm)
  const syncPolicy = Form.useWatch('syncPolicy', sourceForm)
  const autoPublish = Form.useWatch('autoPublish', sourceForm)

  useEffect(() => {
    const source = sourceQuery.data
    if (!source) return
    sourceForm.setFieldsValue({
      mode: source.mode,
      repositoryId: source.repositoryId,
      refType: source.refType || 'branch',
      refValue: source.refValue,
      path: source.path,
      includePatterns: source.includePatterns?.join(', '),
      excludePatterns: source.excludePatterns?.join(', '),
      syncPolicy: source.syncPolicy,
      pollIntervalSeconds: source.pollIntervalSeconds,
      autoPublish: source.autoPublish,
      autoDeploy: source.autoDeploy,
    })
  }, [sourceForm, sourceQuery.data])

  useEffect(() => {
    if (!bindingId && bindingsQuery.data?.[0]?.id) setBindingId(bindingsQuery.data[0].id)
  }, [bindingId, bindingsQuery.data])

  const selectedBinding = useMemo(
    () => bindingsQuery.data?.find((binding) => binding.id === bindingId),
    [bindingId, bindingsQuery.data],
  )

  useEffect(() => {
    if (!selectedBinding) return
    bindingForm.setFieldsValue({
      enabled: selectedBinding.enabled,
      overlay: JSON.stringify(selectedBinding.overlay ?? {}, null, 2),
      kustomize: selectedBinding.kustomize ?? { entryPath: '', images: [] },
    })
  }, [bindingForm, selectedBinding])

  const deploymentByBinding = useMemo(
    () =>
      new Map(
        (deploymentsQuery.data ?? []).map((deployment) => [deployment.bindingId, deployment]),
      ),
    [deploymentsQuery.data],
  )

  const saveSource = async () => {
    const source = sourceQuery.data
    if (!source) return
    const values = await sourceForm.validateFields()
    await sourceMutation.mutateAsync({
      id: item.id,
      input: {
        mode: values.mode,
        repositoryId: values.repositoryId,
        refType: values.refType,
        refValue: values.refValue,
        path: values.path,
        includePatterns: splitPatterns(values.includePatterns),
        excludePatterns: splitPatterns(values.excludePatterns),
        syncPolicy: values.syncPolicy,
        pollIntervalSeconds: values.syncPolicy === 'poll' ? values.pollIntervalSeconds : undefined,
        autoPublish: values.autoPublish,
        autoDeploy: values.autoDeploy,
        expectedGeneration: source.generation,
      },
    })
    message.success('来源配置已更新')
  }

  const runRender = async () => {
    if (!bindingId) return
    setRendered(await manifestApi.render(item.id, bindingId, revision))
  }

  const saveBinding = async () => {
    if (!selectedBinding) return
    const values = await bindingForm.validateFields().catch(() => undefined)
    if (!values) return
    await bindingMutation.mutateAsync({
      id: selectedBinding.id,
      packageId: item.id,
      input: {
        applicationEnvironmentId: selectedBinding.applicationEnvironmentId,
        clusterId: selectedBinding.clusterId,
        namespace: selectedBinding.namespace,
        overlay: parseBindingOverlay(values.overlay),
        kustomize: item.renderer === 'kustomize' ? values.kustomize : undefined,
        rolloutStrategyId: selectedBinding.rolloutStrategyId,
        verificationPolicyId: selectedBinding.verificationPolicyId,
        driftPolicy: selectedBinding.driftPolicy,
        deletionPolicy: selectedBinding.deletionPolicy,
        enabled: values.enabled,
        expectedVersion: selectedBinding.version,
      },
    })
    message.success('环境配置已更新')
  }

  const runPreflight = async () => {
    if (!bindingId) return
    const task = await preflightMutation.mutateAsync({ id: item.id, bindingId, revision })
    message.success(`预检任务已创建：${task.id}`)
  }

  const setDesired = async () => {
    if (!bindingId) return
    const deployment = deploymentByBinding.get(bindingId)
    const result = await desiredMutation.mutateAsync({
      bindingId,
      revision,
      generation: deployment?.generation ?? 0,
    })
    message.success(`部署任务已创建：${result.task.id}`)
  }

  const sourceTab = (
    <Form form={sourceForm} layout="vertical" requiredMark="optional">
      {sourceQuery.data?.lastErrorMessage ? (
        <Alert showIcon type="error" title={sourceQuery.data.lastErrorMessage} />
      ) : null}
      <div className="soha-manifest-form-grid">
        <Form.Item label="来源模式" name="mode" rules={[{ required: true }]}>
          <Select
            disabled={!canUpdateSource || item.currentRevision > 0}
            options={[
              { value: 'soha_managed', label: 'Soha 托管' },
              { value: 'git_synced', label: 'Git 同步' },
            ]}
          />
        </Form.Item>
        {sourceMode === 'git_synced' ? (
          <>
            <Form.Item label="代码仓库" name="repositoryId" rules={[{ required: true }]}>
              <Select
                showSearch={{ optionFilterProp: 'label' }}
                options={(repositoriesQuery.data ?? []).map((repository) => ({
                  value: repository.id,
                  label: repository.name,
                }))}
              />
            </Form.Item>
            <Form.Item label="引用类型" name="refType" rules={[{ required: true }]}>
              <Select
                options={[
                  { value: 'branch', label: '分支' },
                  { value: 'tag', label: '标签' },
                  { value: 'commit', label: 'Commit' },
                ]}
              />
            </Form.Item>
            <Form.Item label="引用" name="refValue" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item
              label="同步包根"
              name="path"
              rules={[{ required: true }]}
              tooltip="相对仓库根的目录，必须包含 base、overlays 和依赖文件；环境入口在环境配置中选择。"
            >
              <Input placeholder="deploy/manifests" />
            </Form.Item>
            <Form.Item label="同步策略" name="syncPolicy" rules={[{ required: true }]}>
              <Select
                options={[
                  { value: 'manual', label: '手动' },
                  { value: 'webhook', label: 'Webhook' },
                  { value: 'poll', label: '轮询' },
                ]}
              />
            </Form.Item>
            {syncPolicy === 'poll' ? (
              <Form.Item label="轮询间隔（秒）" name="pollIntervalSeconds">
                <InputNumber min={30} max={86400} style={{ width: '100%' }} />
              </Form.Item>
            ) : null}
            <Form.Item className="soha-manifest-form-wide" label="包含规则" name="includePatterns">
              <Input
                placeholder={
                  item.renderer === 'kustomize'
                    ? '留空同步包根内全部文本文件'
                    : '**/*.yaml, **/*.yml'
                }
              />
            </Form.Item>
            <Form.Item className="soha-manifest-form-wide" label="排除规则" name="excludePatterns">
              <Input placeholder="**/examples/**" />
            </Form.Item>
            <Form.Item label="同步后自动发布" name="autoPublish" valuePropName="checked">
              <Switch
                onChange={(checked) => {
                  if (!checked) sourceForm.setFieldValue('autoDeploy', false)
                }}
              />
            </Form.Item>
            <Form.Item label="发布后自动部署" name="autoDeploy" valuePropName="checked">
              <Switch disabled={!autoPublish} />
            </Form.Item>
          </>
        ) : null}
      </div>
      <Space wrap>
        {canUpdateSource ? (
          <Button type="primary" loading={sourceMutation.isPending} onClick={saveSource}>
            保存来源
          </Button>
        ) : null}
        {canSyncSource && sourceQuery.data?.mode === 'git_synced' ? (
          <Button
            icon={<CloudSyncOutlined />}
            loading={syncMutation.isPending}
            onClick={async () => {
              await syncMutation.mutateAsync({
                id: item.id,
                generation: sourceQuery.data!.generation,
              })
              message.success('同步任务已创建')
            }}
          >
            立即同步
          </Button>
        ) : null}
      </Space>
      {sourceQuery.data ? (
        <Descriptions
          className="soha-manifest-runtime-summary"
          size="small"
          column={2}
          items={[
            { key: 'generation', label: '配置代次', children: sourceQuery.data.generation },
            {
              key: 'commit',
              label: '已解析 Commit',
              children: sourceQuery.data.lastResolvedCommit || '-',
            },
            {
              key: 'digest',
              label: '内容摘要',
              children: sourceQuery.data.lastCanonicalDigest || '-',
            },
            {
              key: 'syncedAt',
              label: '最近成功',
              children: sourceQuery.data.lastSuccessfulSyncAt
                ? formatDateTime(sourceQuery.data.lastSuccessfulSyncAt)
                : '-',
            },
          ]}
        />
      ) : null}
    </Form>
  )

  const deploymentColumns: TableColumnsType<ManifestDeployment> = [
    { title: '环境绑定', dataIndex: 'bindingId', ellipsis: true },
    { title: '期望版本', render: (_, value) => `v${value.spec.desiredRevision}` },
    { title: '代次', dataIndex: 'generation', width: 80 },
    { title: '状态', render: (_, value) => <StatusTag value={value.status.phase} />, width: 120 },
    {
      title: '漂移',
      render: (_, value) =>
        value.status.drift?.drifted ? `${value.status.drift.resources.length} 个资源` : '-',
      width: 110,
    },
    {
      title: '操作',
      width: 240,
      render: (_, value) => (
        <Space size={4} wrap>
          {canTriggerDeployment && !value.spec.deliverySnapshot ? (
            <Button
              size="small"
              icon={<ReloadOutlined />}
              onClick={() =>
                runDeploymentAction(value, 'reconcile', deploymentMutation, message.success)
              }
            >
              协调
            </Button>
          ) : null}
          {canRepair && !value.spec.deliverySnapshot ? (
            <Button
              size="small"
              icon={<ToolOutlined />}
              onClick={() =>
                runDeploymentAction(value, 'repair', deploymentMutation, message.success)
              }
            >
              修复
            </Button>
          ) : null}
          {canAdopt && !value.spec.deliverySnapshot ? (
            <Button
              size="small"
              icon={<ImportOutlined />}
              onClick={() =>
                runDeploymentAction(value, 'adopt', deploymentMutation, message.success)
              }
            >
              采纳
            </Button>
          ) : null}
          {canTriggerDeployment &&
          value.status.lastKnownGoodRevision &&
          !value.spec.deliverySnapshot ? (
            <Button
              size="small"
              icon={<RollbackOutlined />}
              onClick={() =>
                runDeploymentAction(value, 'rollback', deploymentMutation, message.success)
              }
            >
              回滚
            </Button>
          ) : null}
          {value.spec.deliverySnapshot ? <Text type="secondary">通过应用交付计划更新</Text> : null}
        </Space>
      ),
    },
  ]

  const environmentTab = (
    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
      <Select
        value={bindingId || undefined}
        onChange={setBindingId}
        placeholder="选择环境绑定"
        loading={bindingsQuery.isLoading}
        style={{ minWidth: 240, alignSelf: 'flex-start' }}
        options={(bindingsQuery.data ?? []).map((binding) => ({
          value: binding.id,
          label: `${binding.environmentKey} / ${binding.namespace}${binding.enabled ? '' : '（已停用）'}`,
        }))}
      />
      {selectedBinding ? (
        <Form form={bindingForm} layout="vertical" requiredMark={false}>
          <Descriptions
            className="soha-manifest-runtime-summary"
            size="small"
            column={3}
            items={[
              { key: 'environment', label: '环境', children: selectedBinding.environmentKey },
              { key: 'cluster', label: '集群', children: selectedBinding.clusterId },
              { key: 'namespace', label: '命名空间', children: selectedBinding.namespace },
            ]}
          />
          <div className="soha-manifest-form-grid">
            <Form.Item label="部署到此环境" name="enabled" valuePropName="checked">
              <Switch
                disabled={!canUpdateDeployment}
                checkedChildren="启用"
                unCheckedChildren="停用"
              />
            </Form.Item>
            {item.renderer === 'kustomize' ? (
              <div className="soha-manifest-form-wide">
                <Form.Item
                  label="环境入口"
                  name={['kustomize', 'entryPath']}
                  tooltip="相对同步包根的目录，内含 kustomization.yaml、kustomization.yml 或 Kustomization；留空使用包根。"
                  rules={[
                    {
                      pattern:
                        /^(\.|\.?[A-Za-z0-9_-][A-Za-z0-9_.-]*(\/\.?[A-Za-z0-9_-][A-Za-z0-9_.-]*)*)?$/,
                      message: '请输入包根内的相对目录，如 overlays/prod',
                    },
                  ]}
                >
                  <Input placeholder="overlays/prod" readOnly={!canUpdateDeployment} />
                </Form.Item>
                <Form.List name={['kustomize', 'images']}>
                  {(fields, { add, remove }) => (
                    <>
                      {fields.map(({ key, name, ...field }) => (
                        <div className="soha-manifest-form-grid" key={key}>
                          <Form.Item
                            {...field}
                            label="原镜像名称"
                            name={[name, 'name']}
                            rules={[
                              {
                                required: true,
                                whitespace: true,
                                message: '请输入要替换的镜像名称',
                              },
                            ]}
                          >
                            <Input placeholder="nginx" readOnly={!canUpdateDeployment} />
                          </Form.Item>
                          <Form.Item
                            {...field}
                            label="目标镜像仓库"
                            name={[name, 'newName']}
                            tooltip="留空保留原仓库；不含 Tag 或 digest。"
                          >
                            <Input
                              placeholder="registry.example.com/team/api"
                              readOnly={!canUpdateDeployment}
                            />
                          </Form.Item>
                          <Form.Item
                            {...field}
                            className="soha-manifest-form-wide"
                            label="镜像 digest"
                            name={[name, 'digest']}
                            rules={[
                              {
                                required: true,
                                pattern: /^sha256:[a-f0-9]{64}$/,
                                message: '请输入 sha256: 加 64 位小写十六进制摘要',
                              },
                            ]}
                          >
                            <Input placeholder="sha256:…" readOnly={!canUpdateDeployment} />
                          </Form.Item>
                          {canUpdateDeployment ? (
                            <Button onClick={() => remove(name)}>移除镜像映射 {name + 1}</Button>
                          ) : null}
                        </div>
                      ))}
                      {canUpdateDeployment ? (
                        <Button
                          icon={<PlusOutlined />}
                          disabled={fields.length >= 50}
                          onClick={() => add()}
                        >
                          添加镜像映射
                        </Button>
                      ) : null}
                    </>
                  )}
                </Form.List>
              </div>
            ) : null}
            <Form.Item
              className="soha-manifest-form-wide"
              label="环境覆盖参数"
              name="overlay"
              extra="填写键值均为字符串的 JSON；留空表示不覆盖。"
              rules={[
                {
                  validator: (_rule, value) => {
                    try {
                      parseBindingOverlay(value)
                      return Promise.resolve()
                    } catch (error) {
                      return Promise.reject(error)
                    }
                  },
                },
              ]}
            >
              <Input.TextArea
                autoSize={{ minRows: 4, maxRows: 12 }}
                readOnly={!canUpdateDeployment}
                placeholder={'{\n  "replicas": "3"\n}'}
              />
            </Form.Item>
          </div>
          {canUpdateDeployment ? (
            <Button type="primary" loading={bindingMutation.isPending} onClick={saveBinding}>
              保存环境配置
            </Button>
          ) : null}
        </Form>
      ) : (
        <Alert showIcon type="info" title="当前资源包尚未配置环境绑定" />
      )}
    </Space>
  )

  const syncColumns: TableColumnsType<ManifestSyncRun> = [
    { title: '触发', dataIndex: 'trigger', width: 90 },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (value) => <StatusTag value={value} />,
    },
    {
      title: 'Commit',
      dataIndex: 'resolvedCommit',
      ellipsis: true,
      render: (value) => value || '-',
    },
    {
      title: '版本',
      dataIndex: 'revision',
      width: 80,
      render: (value) => (value ? `v${value}` : '-'),
    },
    { title: '时间', dataIndex: 'updatedAt', width: 170, render: formatDateTime },
  ]

  const intentColumns: TableColumnsType<ManifestDeliveryIntent> = [
    { title: '模型', render: (_, value) => `${value.provider} / ${value.model}` },
    { title: '风险', dataIndex: 'risk', ellipsis: true },
    { title: '资源', render: (_, value) => value.validation.resourceCount, width: 80 },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (value) => <StatusTag value={value} />,
    },
    {
      title: '审核',
      width: 150,
      render: (_, value) =>
        value.status === 'draft' && canDecideIntent ? (
          <Space size={4}>
            <Button
              size="small"
              type="primary"
              icon={<CheckOutlined />}
              onClick={() =>
                void decideIntent(value, 'accept', item, intentMutation, message.success)
              }
            >
              接受
            </Button>
            <Popconfirm
              title="确认拒绝该交付意图？"
              description="拒绝后该意图不能继续用于交付。"
              okText="拒绝"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              onConfirm={() =>
                void decideIntent(value, 'reject', item, intentMutation, message.success)
              }
            >
              <Button size="small" danger icon={<StopOutlined />}>
                拒绝
              </Button>
            </Popconfirm>
          </Space>
        ) : (
          '-'
        ),
    },
  ]

  return (
    <>
      <Tabs
        size="small"
        items={[
          { key: 'source', label: '来源', children: sourceTab },
          { key: 'environment', label: '环境配置', children: environmentTab },
          {
            key: 'delivery',
            label: '交付',
            children: (
              <>
                <Space className="soha-manifest-operation-bar" wrap>
                  <Select
                    value={bindingId || undefined}
                    onChange={setBindingId}
                    placeholder="选择环境绑定"
                    style={{ minWidth: 220 }}
                    options={(bindingsQuery.data ?? []).map((binding) => ({
                      value: binding.id,
                      label: `${binding.environmentKey} / ${binding.namespace}${binding.enabled ? '' : '（已停用）'}`,
                    }))}
                  />
                  <InputNumber
                    min={1}
                    value={revision}
                    onChange={(value) => setRevision(value ?? 1)}
                    prefix="v"
                  />
                  <Button icon={<PlayCircleOutlined />} disabled={!bindingId} onClick={runRender}>
                    渲染
                  </Button>
                  {canPreflightDeployment ? (
                    <Button
                      icon={<SafetyCertificateOutlined />}
                      disabled={!bindingId}
                      loading={preflightMutation.isPending}
                      onClick={runPreflight}
                    >
                      预检
                    </Button>
                  ) : null}
                  {canUpdateDeployment ? (
                    <Button
                      type="primary"
                      icon={<CloudSyncOutlined />}
                      disabled={
                        !bindingId ||
                        !selectedBinding?.enabled ||
                        item.currentRevision < 1 ||
                        Boolean(deploymentByBinding.get(bindingId)?.spec.deliverySnapshot)
                      }
                      loading={desiredMutation.isPending}
                      onClick={setDesired}
                    >
                      设为期望版本
                    </Button>
                  ) : null}
                </Space>
                <AdminTable
                  rowKey="id"
                  tableSize="small"
                  pagination={false}
                  loading={deploymentsQuery.isLoading}
                  columns={deploymentColumns}
                  dataSource={deploymentsQuery.data ?? []}
                  expandedRowRender={(deployment: ManifestDeployment) => (
                    <ManifestResourceInventoryTable items={deployment.status.inventory ?? []} />
                  )}
                />
              </>
            ),
          },
          {
            key: 'sync',
            label: '同步记录',
            children: (
              <Table
                rowKey="id"
                size="small"
                pagination={false}
                loading={syncRunsQuery.isLoading}
                columns={syncColumns}
                dataSource={syncRunsQuery.data ?? []}
              />
            ),
          },
          {
            key: 'ai',
            label: 'AI 提案',
            children: (
              <Table
                rowKey="id"
                size="small"
                pagination={false}
                loading={intentsQuery.isLoading}
                columns={intentColumns}
                dataSource={intentsQuery.data ?? []}
              />
            ),
          },
        ]}
      />
      <Modal
        open={Boolean(rendered)}
        width={860}
        footer={null}
        title="渲染结果"
        onCancel={() => setRendered(null)}
      >
        {rendered ? (
          <>
            <Descriptions
              size="small"
              column={2}
              items={[
                {
                  key: 'digest',
                  label: '摘要',
                  children: (
                    <Text code copyable>
                      {rendered.renderedDigest}
                    </Text>
                  ),
                },
                { key: 'count', label: '资源数', children: rendered.documents.length },
              ]}
            />
            <div className="soha-manifest-render-output">
              {rendered.documents.map((document) => (
                <pre key={`${document.path}:${document.index}`}>{document.content}</pre>
              ))}
            </div>
          </>
        ) : null}
      </Modal>
    </>
  )
}

function splitPatterns(value?: string) {
  return (value ?? '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

async function runDeploymentAction(
  deployment: ManifestDeployment,
  action: 'reconcile' | 'repair' | 'adopt' | 'rollback',
  mutation: {
    mutateAsync: (variables: {
      id: string
      action: 'reconcile' | 'repair' | 'adopt' | 'rollback'
      input: Record<string, unknown>
    }) => Promise<{ task: ManifestExecutionTask }>
  },
  notify: (content: string) => unknown,
) {
  const input =
    action === 'rollback'
      ? { expectedGeneration: deployment.generation, useLastKnownGood: true }
      : { expectedGeneration: deployment.generation }
  const result = await mutation.mutateAsync({ id: deployment.id, action, input })
  notify(`任务已创建：${result.task.id}`)
}

async function decideIntent(
  intent: ManifestDeliveryIntent,
  decision: 'accept' | 'reject',
  item: ManifestPackage,
  mutation: {
    mutateAsync: (variables: {
      id: string
      decision: 'accept' | 'reject'
      currentRevision: number
      updatedAt: string
    }) => Promise<ManifestDeliveryIntent>
  },
  notify: (content: string) => unknown,
) {
  await mutation.mutateAsync({
    id: intent.id,
    decision,
    currentRevision: item.currentRevision,
    updatedAt: item.updatedAt,
  })
  notify(decision === 'accept' ? '提案已写入草稿' : '提案已拒绝')
}
