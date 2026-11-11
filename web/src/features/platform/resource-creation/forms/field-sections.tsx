import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import { Button, Checkbox, Col, Form, Input, InputNumber, Row, Select, Space, Switch } from 'antd'
import type { NamePath } from 'antd/es/form/interface'

export function KeyValueFields({ label, name }: { label: string; name: NamePath }) {
  return (
    <Form.List name={name}>
      {(fields, { add, remove }) => (
        <Form.Item label={label}>
          <Space orientation="vertical" size={8} style={{ display: 'flex' }}>
            {fields.map((field) => (
              <Space.Compact block key={field.key}>
                <Form.Item name={[field.name, 'key']} noStyle>
                  <Input aria-label={`${label} key`} placeholder="key" />
                </Form.Item>
                <Form.Item name={[field.name, 'value']} noStyle>
                  <Input aria-label={`${label} value`} placeholder="value" />
                </Form.Item>
                <Button
                  aria-label={`Remove ${label}`}
                  icon={<MinusCircleOutlined />}
                  onClick={() => remove(field.name)}
                />
              </Space.Compact>
            ))}
            <Button icon={<PlusOutlined />} onClick={() => add({ key: '', value: '' })}>
              添加{label}
            </Button>
          </Space>
        </Form.Item>
      )}
    </Form.List>
  )
}

export function MetadataFields({
  identityDisabled = false,
  namespaced = true,
  namespaceLoading = false,
  namespaceOptions = [],
}: {
  identityDisabled?: boolean
  namespaced?: boolean
  namespaceLoading?: boolean
  namespaceOptions?: readonly string[]
}) {
  return (
    <>
      <Row gutter={16}>
        <Col span={namespaced ? 12 : 24}>
          <Form.Item
            label="名称"
            name="name"
            rules={[{ required: true, message: '请输入资源名称' }]}
          >
            <Input disabled={identityDisabled} placeholder="example" />
          </Form.Item>
        </Col>
        {namespaced ? (
          <Col span={12}>
            <Form.Item
              label="命名空间"
              name="namespace"
              rules={[{ required: true, message: '请选择命名空间' }]}
            >
              <Select
                disabled={identityDisabled}
                loading={namespaceLoading}
                options={namespaceOptions.map((value) => ({ label: value, value }))}
                placeholder="请选择命名空间"
                showSearch={{ optionFilterProp: 'label' }}
              />
            </Form.Item>
          </Col>
        ) : null}
      </Row>
      <KeyValueFields label="标签" name="labels" />
      <KeyValueFields label="注解" name="annotations" />
    </>
  )
}

export function ContainerFields() {
  return (
    <>
      <Row gutter={16}>
        <Col span={12}>
          <Form.Item
            label="容器名称"
            name="containerName"
            rules={[{ required: true, message: '请输入容器名称' }]}
          >
            <Input placeholder="app" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item label="镜像" name="image" rules={[{ required: true, message: '请输入镜像' }]}>
            <Input placeholder="nginx:latest" />
          </Form.Item>
        </Col>
      </Row>
      <Form.Item label="容器端口" name="containerPort">
        <InputNumber min={1} max={65535} style={{ width: '100%' }} />
      </Form.Item>
      <KeyValueFields label="环境变量" name="env" />
      <Row gutter={16}>
        <Col span={12}>
          <Form.Item label="CPU 请求" name="cpuRequest">
            <Input placeholder="100m" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item label="CPU 限制" name="cpuLimit">
            <Input placeholder="500m" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item label="内存请求" name="memoryRequest">
            <Input placeholder="128Mi" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item label="内存限制" name="memoryLimit">
            <Input placeholder="512Mi" />
          </Form.Item>
        </Col>
      </Row>
    </>
  )
}

export function PodTemplateFields() {
  return (
    <>
      <ContainerFields />
      <Form.Item label="ServiceAccount" name="serviceAccountName">
        <Input placeholder="default" />
      </Form.Item>
      <KeyValueFields label="节点选择器" name="nodeSelector" />
      <Row gutter={16}>
        <Col span={12}>
          <Form.Item label="PVC 名称" name="volumeClaimName">
            <Input />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item label="挂载路径" name="volumeMountPath">
            <Input placeholder="/data" />
          </Form.Item>
        </Col>
      </Row>
    </>
  )
}

export function WorkloadPolicyFields({
  kind,
}: {
  kind: 'Deployment' | 'StatefulSet' | 'DaemonSet'
}) {
  return (
    <Row gutter={16}>
      {kind !== 'DaemonSet' ? (
        <Col span={12}>
          <Form.Item label="副本数" name="replicas" rules={[{ required: true }]}>
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Col>
      ) : null}
      {kind === 'StatefulSet' ? (
        <Col span={12}>
          <Form.Item label="服务名称" name="serviceName">
            <Input placeholder="默认使用资源名称" />
          </Form.Item>
        </Col>
      ) : null}
    </Row>
  )
}

export function JobPolicyFields({ cron }: { cron?: boolean }) {
  return (
    <>
      {cron ? (
        <Row gutter={16}>
          <Col span={16}>
            <Form.Item label="Cron 表达式" name="schedule" rules={[{ required: true }]}>
              <Input placeholder="0 * * * *" />
            </Form.Item>
          </Col>
          <Col span={8}>
            <Form.Item label="暂停调度" name="suspend" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Col>
        </Row>
      ) : null}
      <Row gutter={16}>
        <Col span={12}>
          <Form.Item label="并行数" name="parallelism">
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item label="完成数" name="completions">
            <InputNumber min={1} style={{ width: '100%' }} />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item label="重试次数" name="backoffLimit">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item label="重启策略" name="restartPolicy">
            <Select options={[{ value: 'Never' }, { value: 'OnFailure' }]} />
          </Form.Item>
        </Col>
      </Row>
    </>
  )
}

export function BooleanField({ label, name }: { label: string; name: string }) {
  return (
    <Form.Item name={name} valuePropName="checked">
      <Checkbox>{label}</Checkbox>
    </Form.Item>
  )
}
