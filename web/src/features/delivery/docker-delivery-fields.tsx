import { ContextFeedback } from '@/components/context-feedback'
import { useState } from 'react'
import { Button, Form, Input, Select, Space } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { dockerQueries } from '@/features/docker'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import type { DockerDeliveryConfiguration } from '@opensoha/contracts/gen/ts/sohaapi'

export type DockerDeliveryForm = {
  hostId?: string
  projectId?: string
  imageMappings?: { service: string; container: string }[]
}

export function dockerDeliveryForm(value: DockerDeliveryConfiguration): DockerDeliveryForm {
  return {
    ...value,
    imageMappings: Object.entries(value.imageMappings).map(([service, container]) => ({
      service,
      container,
    })),
  }
}

export function parseDockerDelivery(value?: DockerDeliveryForm): DockerDeliveryConfiguration {
  const mappings = value?.imageMappings ?? []
  if (
    !value?.hostId ||
    !value.projectId ||
    !mappings.length ||
    mappings.some((item) => !item.service?.trim() || !item.container?.trim())
  )
    throw new Error('请选择 Docker 主机、项目并填写镜像映射')
  if (new Set(mappings.map((item) => item.service.trim())).size !== mappings.length)
    throw new Error('Compose 服务不能重复映射')
  return {
    hostId: value.hostId,
    projectId: value.projectId,
    imageMappings: Object.fromEntries(
      mappings.map((item) => [item.service.trim(), item.container.trim()]),
    ),
  }
}

export function DockerDeliveryFields({ name }: { name: (string | number)[] }) {
  const form = Form.useFormInstance()
  const hostId = Form.useWatch([...name, 'hostId'], form)
  const permissions = usePermissionSnapshot()
  const canReadHosts = hasPermission(permissions.data?.data, 'docker.hosts.view')
  const canReadProjects = hasPermission(permissions.data?.data, 'docker.projects.view')
  const [hostSearch, setHostSearch] = useState('')
  const [projectSearch, setProjectSearch] = useState('')
  const hosts = useQuery(
    dockerQueries.hosts({ search: hostSearch, page: 1, pageSize: 200 }, canReadHosts),
  )
  const projects = useQuery(
    dockerQueries.projects(
      { hostId, search: projectSearch, page: 1, pageSize: 200 },
      canReadProjects && !!hostId,
    ),
  )
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <ReadFeedback query={hosts} label="Docker 主机" canRead={canReadHosts} />
      <ReadFeedback query={projects} label="Docker 项目" canRead={canReadProjects && !!hostId} />
      {!canReadHosts || !canReadProjects ? (
        <ContextFeedback type="warning" showIcon title="需要 Docker 主机和项目的查看权限" />
      ) : null}
      <Form.Item name={[...name, 'hostId']} label="Docker 主机" rules={[{ required: true }]}>
        <Select
          showSearch={{ filterOption: false, onSearch: setHostSearch }}
          loading={hosts.isLoading}
          options={readableQueryData(hosts)?.items.map((item) => ({
            value: item.id,
            label: item.name || item.id,
          }))}
          onChange={() => form.setFieldValue([...name, 'projectId'], undefined)}
        />
      </Form.Item>
      <Form.Item
        name={[...name, 'projectId']}
        label="Docker / Compose 项目"
        rules={[{ required: true }]}
      >
        <Select
          disabled={!hostId}
          showSearch={{ filterOption: false, onSearch: setProjectSearch }}
          loading={projects.isLoading}
          options={readableQueryData(projects)?.items.map((item) => ({
            value: item.id,
            label: item.name || item.id,
          }))}
        />
      </Form.Item>
      <Form.List name={[...name, 'imageMappings']}>
        {(fields, { add, remove }) => (
          <Space orientation="vertical" style={{ width: '100%' }}>
            {fields.map((field) => (
              <Space key={field.key} align="start">
                <Form.Item name={[field.name, 'service']} rules={[{ required: true }]}>
                  <Input aria-label="Compose 服务名" placeholder="Compose 服务名" />
                </Form.Item>
                <Form.Item name={[field.name, 'container']} rules={[{ required: true }]}>
                  <Input aria-label="产物容器名" placeholder="产物容器名" />
                </Form.Item>
                <Button onClick={() => remove(field.name)}>移除映射</Button>
              </Space>
            ))}
            <Button onClick={() => add({ service: '', container: '' })}>添加镜像映射</Button>
          </Space>
        )}
      </Form.List>
      <ContextFeedback
        type="info"
        showIcon
        title="交付时使用版本包中的镜像摘要，先预检再审批。项目配置应包含完整 Compose 内容，其余镜像也需要固定摘要。"
      />
    </Space>
  )
}
