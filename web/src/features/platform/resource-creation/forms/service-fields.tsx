import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import { Button, Form, Input, InputNumber, Select, Space } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { ReadFeedback } from '@/components/request-feedback'
import { podQueries } from '@/features/platform/workloads/pods/queries'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { KeyValueFields } from './field-sections'
import type { ServiceFormValues } from './types'

function ServicePortFields({ showNodePort, editor }: { showNodePort: boolean; editor: boolean }) {
  return (
    <Form.List name="ports">
      {(fields, { add, remove }) => (
        <Form.Item label="端口">
          <Space orientation="vertical" size={8} style={{ display: 'flex' }}>
            {fields.map((field) => {
              const controls = {
                name: {
                  label: '名称',
                  control: (
                    <Input
                      aria-label="端口名称"
                      placeholder="名称"
                      style={{ width: editor ? '100%' : 136 }}
                    />
                  ),
                },
                protocol: {
                  label: '协议',
                  control: (
                    <Select
                      aria-label="端口协议"
                      options={[{ value: 'TCP' }, { value: 'UDP' }, { value: 'SCTP' }]}
                      style={{ width: editor ? '100%' : 84 }}
                    />
                  ),
                },
                port: {
                  label: '服务端口',
                  control: (
                    <InputNumber
                      aria-label="服务端口"
                      min={1}
                      max={65535}
                      placeholder="服务端口"
                      style={{ width: editor ? '100%' : 92 }}
                    />
                  ),
                },
                targetPort: {
                  label: 'TargetPort',
                  control: (
                    <Input
                      aria-label="容器端口"
                      placeholder="端口 / 名称"
                      style={{ width: editor ? '100%' : 124 }}
                    />
                  ),
                },
                nodePort: {
                  label: 'NodePort',
                  control: (
                    <InputNumber
                      aria-label="NodePort"
                      min={1}
                      max={65535}
                      placeholder="NodePort"
                      style={{ width: editor ? '100%' : 124 }}
                    />
                  ),
                },
              }
              const order: Array<keyof typeof controls> = editor
                ? ['name', 'protocol', 'port', 'targetPort', 'nodePort']
                : ['name', 'port', 'targetPort', 'nodePort', 'protocol']
              const items = order
                .filter((key) => key !== 'nodePort' || showNodePort)
                .map((key) => (
                  <Form.Item
                    key={key}
                    name={[field.name, key]}
                    noStyle={!editor}
                    label={editor ? controls[key].label : undefined}
                    rules={
                      key === 'port' || key === 'targetPort' ? [{ required: true }] : undefined
                    }
                  >
                    {controls[key].control}
                  </Form.Item>
                ))
              const removeButton = (
                <Button
                  aria-label="删除端口"
                  icon={<MinusCircleOutlined />}
                  onClick={() => remove(field.name)}
                />
              )
              return editor ? (
                <div key={field.key} className="soha-service-editor-port-row">
                  {items}
                  {removeButton}
                </div>
              ) : (
                <Space key={field.key} wrap>
                  {items}
                  {removeButton}
                </Space>
              )
            })}
            <Button
              icon={<PlusOutlined />}
              onClick={() => add({ port: 80, targetPort: 8080, protocol: 'TCP' })}
            >
              添加端口
            </Button>
          </Space>
        </Form.Item>
      )}
    </Form.List>
  )
}

export function ServiceFields({
  clusterId = '',
  editor = false,
}: {
  clusterId?: string
  editor?: boolean
}) {
  const form = Form.useFormInstance<ServiceFormValues>()
  const type = Form.useWatch('type', form) ?? 'ClusterIP'
  const namespace = Form.useWatch('namespace', form)
  const externalName = type === 'ExternalName'
  const showNodePort = type === 'NodePort' || type === 'LoadBalancer'
  const podListOptions = podQueries.list(toScopeKey(clusterId, namespace))
  const podsQuery = useQuery({
    ...podListOptions,
    enabled: !externalName && Boolean(clusterId.trim() && namespace?.trim()),
  })
  const visiblePodsQueryData =
    !externalName && Boolean(clusterId.trim() && namespace?.trim())
      ? readableQueryData(podsQuery)
      : undefined
  const podsWithLabels = (visiblePodsQueryData ?? []).filter(
    (pod) => Object.keys(pod.labels ?? {}).length > 0,
  )

  const selectPod = (podName?: string) => {
    if (!podName) return
    const pod = podsWithLabels.find((item) => item.name === podName)
    if (!pod) return
    form.setFieldValue(
      'selector',
      Object.entries(pod.labels ?? {})
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, value]) => ({ key, value })),
    )
  }

  return (
    <>
      <ReadFeedback canRead={podsQuery.isEnabled} query={podsQuery} label="Pods" />

      <Form.Item label="类型" name="type" rules={[{ required: true }]}>
        <Select
          options={['ClusterIP', 'NodePort', 'LoadBalancer', 'ExternalName'].map((value) => ({
            value,
          }))}
        />
      </Form.Item>
      {externalName ? (
        <Form.Item
          label="外部 DNS 名称"
          name="externalName"
          rules={[{ required: true, message: '请输入外部 DNS 名称' }]}
        >
          <Input placeholder="database.example.com" />
        </Form.Item>
      ) : (
        <>
          <Form.Item label="从 Pod 填充选择器">
            <Select
              allowClear
              key={`${clusterId}:${namespace ?? ''}`}
              loading={podsQuery.isLoading}
              notFoundContent={podsQuery.isError ? 'Pod 加载失败' : '没有包含标签的 Pod'}
              onChange={selectPod}
              options={podsWithLabels.map((pod) => ({
                label: `${pod.name} · ${Object.keys(pod.labels ?? {}).length} 个标签`,
                value: pod.name,
              }))}
              placeholder="选择 Pod 自动填入标签"
              showSearch={{ optionFilterProp: 'label' }}
              status={podsQuery.isError ? 'error' : undefined}
            />
          </Form.Item>
          <KeyValueFields label="Pod 选择器" name="selector" />
          <ServicePortFields showNodePort={showNodePort} editor={editor} />
        </>
      )}
    </>
  )
}
