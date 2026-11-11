import { Button, Form, Input, Select, Space, Typography, type FormInstance } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { sourceControlQueries } from '@/features/settings'
import { deliveryQueries } from '../queries'

const { Text } = Typography
export function RepositoryFields({
  form,
  prefix = [],
  active = true,
}: {
  form: FormInstance
  prefix?: (string | number)[]
  active?: boolean
}) {
  const permissionQuery = usePermissionSnapshot()
  const canMapConnection = hasPermission(
    permissionQuery.data?.data,
    'settings.system-integrations.update',
  )
  const fieldName = (...path: (string | number)[]) => [...prefix, ...path]
  const selectedRepositoryProvider = Form.useWatch(fieldName('provider'), form)
  const sourceConnectionId = Form.useWatch(fieldName('sourceConnectionId'), form) as
    | string
    | undefined
  const selectedGitLabProjectId = Form.useWatch(fieldName('gitlabProjectId'), form)
  const gitProjectsQuery = useQuery(
    deliveryQueries.repositories.gitProjects(
      {},
      active && selectedRepositoryProvider === 'gitlab' && !sourceConnectionId,
    ),
  )
  const gitBranchesQuery = useQuery(
    deliveryQueries.repositories.gitBranches(
      { projectId: selectedGitLabProjectId ?? '' },
      active &&
        selectedRepositoryProvider === 'gitlab' &&
        Boolean(selectedGitLabProjectId) &&
        !sourceConnectionId,
    ),
  )
  const connections = useQuery(sourceControlQueries.connections(active && canMapConnection))
  const sourceProjects = useQuery(
    sourceControlQueries.repositories(sourceConnectionId ?? '', active && canMapConnection),
  )
  const sourceBranches = useQuery(
    sourceControlQueries.branches(
      sourceConnectionId ?? '',
      selectedGitLabProjectId ?? '',
      active && canMapConnection,
    ),
  )
  const projects = sourceConnectionId
    ? (sourceProjects.data ?? []).map((item) => ({ ...item, pathWithNamespace: item.fullName }))
    : (gitProjectsQuery.data ?? [])
  const branches = sourceConnectionId ? sourceBranches.data : gitBranchesQuery.data
  const setRepositoryFields = (values: Record<string, unknown>) => {
    for (const [key, value] of Object.entries(values)) form.setFieldValue(fieldName(key), value)
  }
  return (
    <div className="soha-application-service-form-grid">
      <Form.Item name={fieldName('provider')} label="提供方" rules={[{ required: true }]}>
        <Select
          disabled={Boolean(sourceConnectionId) && !canMapConnection}
          onChange={() =>
            setRepositoryFields({
              sourceConnectionId: undefined,
              providerRepositoryId: undefined,
              gitlabProjectId: undefined,
            })
          }
          options={[
            { value: 'gitlab', label: 'GitLab' },
            { value: 'git', label: 'Git URL' },
          ]}
        />
      </Form.Item>
      <Form.Item name={fieldName('protocol')} label="协议" rules={[{ required: true }]}>
        <Select
          options={[
            { value: 'https', label: 'HTTPS' },
            { value: 'ssh', label: 'SSH' },
          ]}
        />
      </Form.Item>
      {selectedRepositoryProvider === 'gitlab' && canMapConnection ? (
        <Form.Item
          name={fieldName('sourceConnectionId')}
          label="代码源连接"
          extra="关联后，当前应用的授权成员可以分析该仓库。"
        >
          <Select
            allowClear
            loading={connections.isFetching}
            placeholder="选择明确授权的连接"
            options={(connections.data ?? [])
              .filter((item) => item.providerType === 'gitlab')
              .map((item) => ({ value: item.id, label: item.name }))}
            onChange={() =>
              setRepositoryFields({ gitlabProjectId: undefined, providerRepositoryId: undefined })
            }
          />
        </Form.Item>
      ) : (
        <Form.Item name={fieldName('sourceConnectionId')} hidden>
          <Input />
        </Form.Item>
      )}
      <Form.Item name={fieldName('providerRepositoryId')} hidden>
        <Input />
      </Form.Item>
      {selectedRepositoryProvider === 'gitlab' ? (
        <Form.Item
          name={fieldName('gitlabProjectId')}
          label="代码源仓库"
          rules={[{ required: true, message: '请选择代码源仓库' }]}
        >
          <Select
            showSearch={{ optionFilterProp: 'label' }}
            loading={sourceConnectionId ? sourceProjects.isFetching : gitProjectsQuery.isFetching}
            placeholder="选择设置中心已配置的仓库"
            notFoundContent={
              gitProjectsQuery.isFetching ? (
                '正在读取代码源…'
              ) : (
                <Space orientation="vertical" size={2} align="center">
                  <Text type="secondary">没有可用仓库，请确认代码源已启用并完成授权</Text>
                  <Button
                    type="link"
                    size="small"
                    onClick={() =>
                      window.open('/settings/source-control', '_blank', 'noopener,noreferrer')
                    }
                  >
                    检查代码源设置
                  </Button>
                </Space>
              )
            }
            options={projects.map((item) => ({
              value: item.id,
              label: item.pathWithNamespace,
            }))}
            onChange={(id) => {
              const project = projects.find((item) => item.id === id)
              if (project)
                setRepositoryFields({
                  providerRepositoryId: sourceConnectionId ? id : undefined,
                  name: project.name,
                  path: project.pathWithNamespace,
                  url: project.webUrl,
                  defaultBranch: project.defaultBranch || 'main',
                })
            }}
          />
        </Form.Item>
      ) : null}
      <Form.Item name={fieldName('name')} label="仓库名称" rules={[{ required: true }]}>
        <Input />
      </Form.Item>
      <Form.Item name={fieldName('path')} label="仓库路径" rules={[{ required: true }]}>
        <Input placeholder="group/project" />
      </Form.Item>
      <Form.Item name={fieldName('url')} label="Git URL" rules={[{ required: true }]}>
        <Input placeholder="https://git.example.com/group/project.git" />
      </Form.Item>
      <Form.Item name={fieldName('defaultBranch')} label="默认分支" rules={[{ required: true }]}>
        {selectedRepositoryProvider === 'gitlab' ? (
          <Select
            showSearch={{ optionFilterProp: 'label' }}
            loading={sourceConnectionId ? sourceBranches.isFetching : gitBranchesQuery.isFetching}
            options={(branches ?? []).map((item) => ({
              value: item.name,
              label: item.name,
            }))}
          />
        ) : (
          <Input placeholder="main" />
        )}
      </Form.Item>
      <Form.Item name={fieldName('credentialRef')} label="凭据引用">
        <Input placeholder="server-side credential ref" />
      </Form.Item>
    </div>
  )
}
