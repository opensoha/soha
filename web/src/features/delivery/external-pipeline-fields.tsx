import { Form, Input, Select } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { deliveryQueries } from './queries'

export function ExternalPipelineFields({ prefix }: { prefix: (string | number)[] }) {
  const fieldName = (name: string) => [...prefix, name]
  const registriesQuery = useQuery(deliveryQueries.registries.list())
  return (
    <>
      <Form.Item
        name={fieldName('provider')}
        label="CI 平台"
        initialValue="gitlab"
        rules={[{ required: true }]}
      >
        <Select options={[{ value: 'gitlab', label: 'GitLab' }]} />
      </Form.Item>
      <Form.Item
        name={fieldName('pipelineTag')}
        label="CI 定义标签"
        extra="标签必须受保护，运行时会固定其实际提交。"
        rules={[{ required: true, whitespace: true, max: 255 }]}
      >
        <Input placeholder="soha-build-v1" />
      </Form.Item>
      <Form.Item
        name={fieldName('artifactJob')}
        label="产物任务"
        extra="根流水线中生成 soha-artifact.json 的 Job 名称。"
        rules={[{ required: true, whitespace: true, max: 255 }]}
      >
        <Input placeholder="publish" />
      </Form.Item>
      <Form.Item
        name={fieldName('registryId')}
        label="产物镜像仓库"
        rules={[{ required: true, message: '请选择用于核验产物的镜像仓库' }]}
        validateStatus={registriesQuery.isError ? 'error' : undefined}
        help={registriesQuery.isError ? registriesQuery.error.message : undefined}
      >
        <Select
          loading={registriesQuery.isFetching}
          options={registriesQuery.data?.map((item) => ({
            value: item.id,
            label: `${item.name} · ${item.endpoint}`,
          }))}
        />
      </Form.Item>
    </>
  )
}
