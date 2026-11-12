import { Link } from 'react-router-dom'
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Checkbox,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Switch,
  Space,
  Typography,
} from 'antd'
import type {
  NetworkProxySource,
  NetworkProxySourceInput,
  NetworkProxyConversionPreview,
  NetworkProxyDeliveryMode,
  NetworkProxyBundle,
  NetworkProxyConversionError,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { workspaceApi } from './workspace-api'
import { workspaceKeys, workspaceQueries } from './workspace-queries'
const modes: { value: NetworkProxyDeliveryMode; label: string }[] = [
  { value: 'mapped_nodes', label: '逐节点内网映射' },
  { value: 'gateway', label: '服务端聚合网关' },
  { value: 'direct_subscription', label: '直接交付上游' },
]
const modeLabel = (mode: NetworkProxyDeliveryMode) => modes.find((x) => x.value === mode)?.label
const tokens = (value?: string) => value?.split(/[，,\s]+/).filter(Boolean) ?? []
export function SourcesPanel({ id, canUpdate }: { id: string; canUpdate: boolean }) {
  const client = useQueryClient()
  const sources = useQuery(workspaceQueries.sources(id, true))
  const bundles = useQuery(workspaceQueries.bundles(id, true))
  const rules = useQuery(workspaceQueries.rules(id, false, true))
  const [form] = Form.useForm<NetworkProxySourceInput>()
  const [editing, setEditing] = useState<NetworkProxySource>()
  const [preview, setPreview] = useState<NetworkProxyConversionPreview>()
  const [disclosed, setDisclosed] = useState(false)
  const [target, setTarget] = useState<NetworkProxySource>()
  const [scopeForm] = Form.useForm<{
    users: string
    devices: string
    nodes: string
    endpoints: string
  }>()
  useEffect(() => {
    form.resetFields()
    setEditing(undefined)
    setPreview(undefined)
    setDisclosed(false)
  }, [id, canUpdate, form])
  const format = Form.useWatch('format', form)
  const mode = Form.useWatch('deliveryMode', form)
  const refresh = () => client.invalidateQueries({ queryKey: workspaceKeys.instance(id) })
  const save = useOperationMutation(
    {
      mutationFn: (input: NetworkProxySourceInput) =>
        workspaceApi.saveSource(id, input, editing?.sourceId),
      onSettled: (_data, _error, input) => {
        input.url = undefined
        input.content = undefined
        input.nodes = undefined
      },
      onSuccess: () => {
        setEditing(undefined)
        form.resetFields()
        setPreview(undefined)
        void refresh()
      },
    },
    { label: ['保存来源', 'Save source'], target: (v) => v.name },
  )
  const convert = useOperationMutation(
    {
      mutationFn: (input: NetworkProxySourceInput) => workspaceApi.preview(id, input),
      onSuccess: setPreview,
      onSettled: (_data, _error, input) => {
        input.url = undefined
        input.content = undefined
        input.nodes = undefined
      },
    },
    { label: ['转换预览', 'Conversion preview'], target: (v) => v.name, readOnly: true },
  )
  const remove = useOperationMutation(
    {
      mutationFn: (source: NetworkProxySource) =>
        workspaceApi.deleteSource(id, source, crypto.randomUUID()),
      onSuccess: () => {
        setTarget(undefined)
        void refresh()
      },
    },
    { label: ['删除来源', 'Delete source'], target: (v) => v.name },
  )
  const prepare = useOperationMutation(
    {
      mutationFn: async (values: {
        users: string
        devices: string
        nodes: string
        endpoints: string
      }) =>
        workspaceApi.prepare(id, {
          requestId: crypto.randomUUID(),
          expectedRevision: 0,
          sourceId: target!.sourceId,
          sourceRevision: target!.revision,
          orgRevision: readableQueryData(rules)?.orgRevision ?? 0,
          deliveryMode: target!.deliveryMode,
          scope: {
            userIds: tokens(values.users),
            deviceIds: tokens(values.devices),
            nodeIds: tokens(values.nodes),
          },
          endpoints: tokens(values.endpoints).map((endpointId) => ({ endpointId })),
        }),
      onSuccess: () => void refresh(),
    },
    {
      label: ['准备发布', 'Prepare publication'],
      target: () => target?.name ?? '',
      receipt: (d) => ({ message: '发布准备请求已返回，请核对发布状态。', evidence: d.bundleId }),
    },
  )
  const activate = useOperationMutation(
    {
      mutationFn: (v: { bundleId: string; revision: number }) =>
        workspaceApi.activate(id, v.bundleId, {
          requestId: crypto.randomUUID(),
          expectedRevision: v.revision,
        }),
      onSuccess: () => void refresh(),
    },
    { label: ['激活发布', 'Activate publication'], target: (v) => v.bundleId },
  )
  const busy =
    save.blocked || convert.blocked || remove.blocked || prepare.blocked || activate.blocked
  function edit(source?: NetworkProxySource) {
    setEditing(source)
    setPreview(undefined)
    setDisclosed(false)
    form.resetFields()
    if (source)
      form.setFieldsValue({
        name: source.name,
        expectedRevision: source.revision,
        format: source.format,
        deliveryMode: source.deliveryMode,
      })
  }
  return (
    <section className="soha-proxy-workspace-section">
      <div className="soha-server-proxy-actions">
        <h2>来源与发布</h2>
        <Button onClick={() => void refresh()}>刷新记录</Button>
      </div>
      <ReadFeedback canRead query={sources} label="来源" />
      <ReadFeedback canRead query={bundles} label="发布" />
      <AdminTable
        rowKey="sourceId"
        dataSource={readableQueryData(sources)?.items ?? []}
        loading={sources.isLoading}
        pagination={false}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '格式', dataIndex: 'format' },
          {
            title: '交付方式',
            render: (_: unknown, s: NetworkProxySource) => modeLabel(s.deliveryMode),
          },
          { title: '节点', dataIndex: 'nodeCount' },
          { title: '版本', dataIndex: 'revision' },
          {
            title: '秘密配置',
            render: (_: unknown, s: NetworkProxySource) => (s.configured ? '已配置' : '未配置'),
          },
          {
            title: '操作',
            render: (_: unknown, s: NetworkProxySource) => (
              <Space wrap>
                <Button onClick={() => setTarget(s)}>发布</Button>
                {canUpdate && (
                  <>
                    <Button disabled={busy} onClick={() => edit(s)}>
                      编辑
                    </Button>
                    <Popconfirm title="删除来源并撤回相关访问？" onConfirm={() => remove.mutate(s)}>
                      <Button danger disabled={busy}>
                        删除
                      </Button>
                    </Popconfirm>
                  </>
                )}
              </Space>
            ),
          },
        ]}
      />
      {remove.feedback}
      {canUpdate && (
        <div className="soha-proxy-workspace-editor">
          <div className="soha-server-proxy-actions">
            <h3>{editing ? '编辑来源' : '新增来源'}</h3>
            {editing && <Button onClick={() => edit()}>改为新增</Button>}
          </div>
          <Form
            form={form}
            layout="vertical"
            initialValues={{ expectedRevision: 0, format: 'manual', deliveryMode: 'mapped_nodes' }}
            onValuesChange={() => setPreview(undefined)}
            onFinish={(input) => save.mutate(structuredClone(input))}
          >
            <Form.Item name="expectedRevision" hidden>
              <InputNumber />
            </Form.Item>
            <div className="soha-proxy-workspace-fields">
              <Form.Item name="name" label="来源名称" rules={[{ required: true }]}>
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item name="format" label="输入格式" rules={[{ required: true }]}>
                <Select
                  options={['manual', 'mihomo_yaml', 'sing_box_json', 'v2ray_json', 'uri_list'].map(
                    (value) => ({ value, label: value }),
                  )}
                />
              </Form.Item>
              <Form.Item name="deliveryMode" label="交付方式">
                <Select options={modes} />
              </Form.Item>
            </div>
            {format === 'manual' ? (
              <Form.List name="nodes">
                {(fields, { add, remove }) => (
                  <>
                    <Typography.Paragraph type="secondary">
                      手动节点仅提交给服务端；预览和记录不回显上游参数。
                    </Typography.Paragraph>
                    {fields.map(({ key, name }) => (
                      <div key={key} className="soha-proxy-workspace-fields">
                        <Form.Item
                          name={[name, 'nodeId']}
                          label="逻辑节点 ID"
                          rules={[{ required: true }]}
                        >
                          <Input />
                        </Form.Item>
                        <Form.Item name={[name, 'label']} label="名称" rules={[{ required: true }]}>
                          <Input />
                        </Form.Item>
                        <Form.Item
                          name={[name, 'protocol']}
                          label="协议"
                          rules={[{ required: true }]}
                        >
                          <Select
                            options={['http', 'socks5', 'ss', 'trojan', 'vmess', 'vless'].map(
                              (value) => ({ value, label: value }),
                            )}
                          />
                        </Form.Item>
                        <Form.Item
                          name={[name, 'server']}
                          label="服务器"
                          rules={[{ required: true }]}
                        >
                          <Input autoComplete="off" />
                        </Form.Item>
                        <Form.Item name={[name, 'port']} label="端口" rules={[{ required: true }]}>
                          <InputNumber min={1} max={65535} />
                        </Form.Item>
                        <Form.Item name={[name, 'username']} label="用户名">
                          <Input autoComplete="off" />
                        </Form.Item>
                        <Form.Item name={[name, 'secret']} label="秘密">
                          <Input.Password autoComplete="new-password" />
                        </Form.Item>
                        <Form.Item name={[name, 'uuid']} label="UUID（适用协议）">
                          <Input autoComplete="off" />
                        </Form.Item>
                        <Form.Item name={[name, 'cipher']} label="加密方式（适用协议）">
                          <Input />
                        </Form.Item>
                        <Form.Item
                          name={[name, 'tls', 'enabled']}
                          label="TLS"
                          valuePropName="checked"
                        >
                          <Switch />
                        </Form.Item>
                        <Form.Item name={[name, 'tls', 'serverName']} label="TLS 服务器名称">
                          <Input />
                        </Form.Item>
                        <Form.Item name={[name, 'transport', 'type']} label="传输">
                          <Select
                            allowClear
                            options={['tcp', 'ws', 'grpc'].map((value) => ({
                              value,
                              label: value,
                            }))}
                          />
                        </Form.Item>
                        <Form.Item name={[name, 'transport', 'host']} label="传输主机">
                          <Input />
                        </Form.Item>
                        <Form.Item name={[name, 'transport', 'path']} label="WebSocket 路径">
                          <Input />
                        </Form.Item>
                        <Form.Item name={[name, 'transport', 'serviceName']} label="gRPC 服务名称">
                          <Input />
                        </Form.Item>
                        <Button onClick={() => remove(name)}>移除节点</Button>
                      </div>
                    ))}
                    <Button onClick={() => add({ protocol: 'socks5' })}>添加节点</Button>
                  </>
                )}
              </Form.List>
            ) : (
              <>
                <Form.Item name="url" label="订阅地址（写入后不回显）">
                  <Input.Password autoComplete="new-password" />
                </Form.Item>
                <Form.Item name="content" label="或粘贴配置（保存成功后清除）">
                  <Input.TextArea rows={6} autoComplete="off" />
                </Form.Item>
              </>
            )}
            <Alert
              type={mode === 'direct_subscription' ? 'warning' : 'info'}
              showIcon
              title={
                mode === 'direct_subscription'
                  ? '将向获准设备交付真实上游参数，不是内网映射'
                  : mode === 'gateway'
                    ? '仅发布一个聚合网关逻辑节点'
                    : '保留各逻辑节点，经获准内网入口访问；失败不回退直连'
              }
            />
            {mode === 'direct_subscription' && (
              <Checkbox checked={disclosed} onChange={(e) => setDisclosed(e.target.checked)}>
                我已了解真实上游披露范围
              </Checkbox>
            )}
            <Space wrap>
              <Button
                disabled={busy}
                onClick={() =>
                  void form.validateFields().then((v) => convert.mutate(structuredClone(v)))
                }
              >
                转换预览（主动读取订阅）
              </Button>
              <Button
                type="primary"
                htmlType="submit"
                disabled={busy || (mode === 'direct_subscription' && !disclosed)}
              >
                保存来源
              </Button>
            </Space>
          </Form>
          {convert.feedback}
          {save.feedback}
          {preview && (
            <>
              <Alert
                type={preview.supported ? 'success' : 'error'}
                title={preview.supported ? '转换通过' : '整版转换失败，未丢弃不支持的配置'}
                description={`${preview.nodes.length} 个节点 · ${preview.groups.length} 个策略组`}
              />
              <AdminTable
                rowKey={(error: NetworkProxyConversionError) => `${error.location}:${error.code}`}
                pagination={false}
                dataSource={preview.errors}
                columns={[
                  { title: '位置', dataIndex: 'location' },
                  { title: '原因', dataIndex: 'code' },
                  { title: '说明', dataIndex: 'message' },
                ]}
              />
              <Typography.Paragraph>
                预览不会保存来源；当前编辑稿保留，保存成功或退出编辑后清除秘密。
              </Typography.Paragraph>
            </>
          )}
        </div>
      )}
      {target && (
        <div className="soha-proxy-workspace-editor">
          <h3>准备发布 · {target.name}</h3>
          <Typography.Paragraph>
            {modeLabel(target.deliveryMode)} · 源版本 {target.revision}
            。填写已授权的用户、设备、逻辑节点及批准入口 ID，逗号分隔。
          </Typography.Paragraph>
          <Form form={scopeForm} layout="vertical" onFinish={(v) => prepare.mutate(v)}>
            <div className="soha-proxy-workspace-fields">
              {(['users', 'devices', 'nodes', 'endpoints'] as const).map((name, i) => (
                <Form.Item
                  key={name}
                  name={name}
                  label={['用户', '设备', '逻辑节点', '批准入口'][i]}
                  rules={[
                    {
                      required:
                        name !== 'endpoints' || target.deliveryMode !== 'direct_subscription',
                    },
                  ]}
                >
                  <Input />
                </Form.Item>
              ))}
            </div>
            {target.deliveryMode === 'direct_subscription' && (
              <Alert type="warning" title="激活后获准设备可获得真实上游参数" />
            )}
            <Popconfirm title="确认发布范围与交付方式？" onConfirm={() => scopeForm.submit()}>
              <Button type="primary" disabled={!canUpdate || busy || !readableQueryData(rules)}>
                校验并准备发布
              </Button>
            </Popconfirm>
            <Button onClick={() => setTarget(undefined)}>收起</Button>
          </Form>
          {prepare.feedback}
        </div>
      )}
      <h3>发布状态</h3>
      <Typography.Paragraph type="secondary">
        准备、服务端就绪、允许设备领取与激活分别确认。直接交付无需中继就绪；任何模式均不以请求受理代替完成。
      </Typography.Paragraph>
      <AdminTable
        rowKey="bundleId"
        dataSource={readableQueryData(bundles)?.items ?? []}
        pagination={false}
        columns={[
          { title: '发布', dataIndex: 'bundleId', ellipsis: true },
          {
            title: '交付方式',
            render: (_: unknown, b: NetworkProxyBundle) => modeLabel(b.deliveryMode),
          },
          { title: '状态', dataIndex: 'state' },
          {
            title: '发布 / 已激活版本',
            render: (_: unknown, b: NetworkProxyBundle) => `${b.revision} / ${b.activeRevision}`,
          },
          { title: '原因', dataIndex: 'reasonCode' },
          {
            title: '操作',
            render: (_: unknown, b: NetworkProxyBundle) =>
              b.state === 'activated' ? (
                <Link to="/network-access/mihomo-profiles">配置员工 Mihomo</Link>
              ) : (
                <Popconfirm
                  title={
                    b.deliveryMode === 'direct_subscription'
                      ? '确认向授权设备披露真实上游参数？'
                      : '确认激活该版本？'
                  }
                  onConfirm={() => activate.mutate(b)}
                >
                  <Button disabled={!canUpdate || busy || b.state !== 'client_eligible'}>
                    激活
                  </Button>
                </Popconfirm>
              ),
          },
        ]}
      />
      {activate.feedback}
    </section>
  )
}
