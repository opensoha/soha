import { useEffect, useRef } from 'react'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Select,
  Space,
  Switch,
  Typography,
  type FormInstance,
} from 'antd'
import { LinkOutlined, MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { ManagementIconButton } from '@/components/management-list'
import { deliveryQueries } from '../queries'
import { RepositoryAnalysis } from './repository-analysis'
import { ExternalPipelineFields } from '../external-pipeline-fields'
import { BuildpacksFields } from './buildpacks-fields'
import { ParameterField } from './deployment-template-fields'
import type { BuildRepositoryBinding, DeliveryRepository, TemplateParameterSchema } from '../types'

const { Text } = Typography
export function BuildSourceFields({
  applicationId,
  form,
  prefix = [],
  repositories,
  repositoriesLoading = false,
  onConnectRepository,
  section = 'all',
}: {
  applicationId?: string
  form: FormInstance
  prefix?: (string | number)[]
  repositories: DeliveryRepository[]
  repositoriesLoading?: boolean
  onConnectRepository?: (index: number) => void
  section?: 'all' | 'repositories' | 'build'
}) {
  const fieldName = (...path: (string | number)[]) => [...prefix, ...path]
  const buildSourceType =
    Form.useWatch(fieldName('type'), { form, preserve: true }) ??
    form.getFieldValue(fieldName('type')) ??
    'repo_dockerfile'
  const buildSourceRepositoryBindings = Form.useWatch(fieldName('config', 'repositoryBindings'), {
    form,
    preserve: true,
  }) as BuildRepositoryBinding[] | undefined
  const templateId = Form.useWatch(fieldName('config', 'buildTemplateId'), {
    form,
    preserve: true,
  }) as string | undefined
  const buildTemplatesQuery = useQuery(
    deliveryQueries.buildTemplates.list(buildSourceType === 'platform_build_template'),
  )
  const templateVersion = Form.useWatch(fieldName('config', 'buildTemplateVersion'), {
    form,
    preserve: true,
  }) as number | undefined
  const versionsQuery = useQuery(
    deliveryQueries.buildTemplates.versions(
      templateId ?? '',
      buildSourceType === 'platform_build_template',
    ),
  )
  const selectedBuildTemplate = versionsQuery.data?.find(
    (item) => item.publishedVersion === templateVersion,
  )
  const variablesPath = JSON.stringify([...prefix, 'config', 'variables'])
  const initializedTemplate = useRef('')
  useEffect(() => {
    if (!selectedBuildTemplate) return
    const selection = `${variablesPath}:${selectedBuildTemplate.id}:${selectedBuildTemplate.publishedVersion}`
    if (initializedTemplate.current === selection) return
    const path = JSON.parse(variablesPath) as (string | number)[]
    const current = form.getFieldValue(path) ?? {}
    form.setFieldValue(path, {
      ...selectedBuildTemplate.defaultVariables,
      ...Object.fromEntries(
        Object.entries(current).filter(
          ([key]) => key in (selectedBuildTemplate.variableSchema ?? {}),
        ),
      ),
    })
    initializedTemplate.current = selection
  }, [form, selectedBuildTemplate, variablesPath])
  return (
    <div className="soha-application-service-editor">
      {applicationId && section !== 'repositories' && buildSourceType !== 'external_pipeline' ? (
        <RepositoryAnalysis
          key={applicationId}
          applicationId={applicationId}
          repositories={repositories.filter((repository) =>
            repository.applicationIds?.includes(applicationId),
          )}
        />
      ) : null}
      {section !== 'repositories' && buildSourceType === 'external_pipeline' ? (
        <Alert
          type="info"
          showIcon
          title="使用已接入的 GitLab 仓库。运行时固定源码提交和受保护标签；产物核验、运行跟踪与取消确认由 Soha 完成。"
        />
      ) : null}
      {section !== 'repositories' ? (
        <section className="soha-application-service-editor-section">
          <div className="soha-application-service-editor-section__head">
            <span className="soha-application-service-editor-section__identity">
              <Text strong>基础信息</Text>
              <Text type="secondary">定义构建方式与默认产物</Text>
            </span>
          </div>
          <div className="soha-application-service-form-grid">
            <Form.Item name={fieldName('name')} label="名称" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name={fieldName('type')} label="构建方式" rules={[{ required: true }]}>
              <Select
                onChange={(type) => {
                  const config = form.getFieldValue(fieldName('config')) ?? {}
                  if (type === 'external_pipeline') {
                    form.setFieldValue(fieldName('config'), {
                      repositoryBindings: (config.repositoryBindings ?? []).map(
                        (binding: BuildRepositoryBinding) => ({ ...binding, submodules: false }),
                      ),
                      variables: config.variables,
                      buildArgs: config.buildArgs,
                      externalPipeline: config.externalPipeline ?? { provider: 'gitlab' },
                    })
                  } else if (config.externalPipeline) {
                    form.setFieldValue(fieldName('config', 'externalPipeline'), undefined)
                  }
                }}
                options={[
                  { value: 'repo_dockerfile', label: '仓库 Dockerfile' },
                  { value: 'platform_build_template', label: '平台构建模板' },
                  { value: 'repo_buildpacks', label: 'Buildpacks 自动构建' },
                  {
                    value: 'external_pipeline',
                    label: 'GitLab CI',
                  },
                ]}
              />
            </Form.Item>
            <Form.Item
              name={fieldName('buildImage')}
              label="产物镜像"
              extra="构建完成后自动推送产物镜像"
            >
              <Input />
            </Form.Item>
            <Form.Item name={fieldName('defaultTag')} label="默认 Tag">
              <Input />
            </Form.Item>
            <Form.Item name={fieldName('isDefault')} label="默认构建源" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name={fieldName('enabled')} label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </div>
        </section>
      ) : null}

      {section !== 'build' ? (
        <section className="soha-application-service-editor-section">
          <Form.List name={fieldName('config', 'repositoryBindings')}>
            {(fields, { add, remove }) => (
              <div className="soha-application-build-repository-editor">
                <div className="soha-application-service-editor-section__head">
                  <span className="soha-application-service-editor-section__identity">
                    <Text strong>源码仓库</Text>
                    <Text type="secondary">
                      {buildSourceType === 'external_pipeline'
                        ? '绑定一个 GitLab 源码仓库；其他检出由受保护的 CI 定义管理'
                        : '多仓库独立检出；运行工作流时逐仓选择版本'}
                    </Text>
                  </span>
                  <Space>
                    <Button
                      size="small"
                      icon={<LinkOutlined />}
                      onClick={() => onConnectRepository?.(fields[0]?.name ?? 0)}
                    >
                      从代码源接入
                    </Button>
                    <Button
                      size="small"
                      icon={<PlusOutlined />}
                      disabled={buildSourceType === 'external_pipeline' && fields.length >= 1}
                      onClick={() =>
                        add({
                          checkoutPath:
                            fields.length === 0 ? '.' : `repository-${fields.length + 1}`,
                          allowCommitSelection: false,
                          submodules: false,
                        })
                      }
                    >
                      添加检出项
                    </Button>
                  </Space>
                </div>
                {fields.map((field, index) => {
                  const binding = buildSourceRepositoryBindings?.[field.name]
                  const repository = repositories.find((item) => item.id === binding?.repositoryId)
                  return (
                    <Card
                      key={field.key}
                      size="small"
                      className="soha-application-build-repository-editor__item"
                      title={`检出项 ${index + 1}`}
                      extra={
                        fields.length > 1 ? (
                          <ManagementIconButton
                            aria-label={`移除检出项 ${index + 1}`}
                            danger
                            icon={<MinusCircleOutlined />}
                            size="small"
                            tooltip="移除检出项"
                            onClick={() => remove(field.name)}
                          />
                        ) : null
                      }
                    >
                      <div className="soha-application-service-form-grid">
                        <Form.Item
                          name={[field.name, 'repositoryId']}
                          label="已接入仓库"
                          rules={[{ required: true, message: '请选择已接入仓库' }]}
                        >
                          <Select
                            showSearch={{ optionFilterProp: 'label' }}
                            loading={repositoriesLoading}
                            placeholder="选择已接入当前应用的仓库"
                            notFoundContent={
                              repositoriesLoading ? (
                                '正在加载仓库…'
                              ) : (
                                <Button
                                  type="link"
                                  size="small"
                                  onMouseDown={(event) => event.preventDefault()}
                                  onClick={() => onConnectRepository?.(field.name)}
                                >
                                  未找到仓库，从代码源接入
                                </Button>
                              )
                            }
                            options={repositories.map((item) => ({
                              value: item.id,
                              label: `${item.name} · ${item.path}`,
                              disabled: buildSourceRepositoryBindings?.some(
                                (current, currentIndex) =>
                                  currentIndex !== index && current?.repositoryId === item.id,
                              ),
                            }))}
                            onChange={(repositoryId) => {
                              const nextRepository = repositories.find(
                                (item) => item.id === repositoryId,
                              )
                              form.setFieldValue(
                                fieldName(
                                  'config',
                                  'repositoryBindings',
                                  field.name,
                                  'defaultBranch',
                                ),
                                nextRepository?.defaultBranch,
                              )
                            }}
                          />
                        </Form.Item>
                        <Form.Item name={[field.name, 'checkoutPath']} label="检出目录">
                          <Input placeholder={index === 0 ? '.' : `repository-${index + 1}`} />
                        </Form.Item>
                        <Form.Item name={[field.name, 'defaultBranch']} label="默认分支">
                          <Input placeholder={repository?.defaultBranch || 'main'} />
                        </Form.Item>
                        <Form.Item
                          name={[field.name, 'allowCommitSelection']}
                          label="运行时允许选择 Commit"
                          valuePropName="checked"
                        >
                          <Switch />
                        </Form.Item>
                        <Form.Item
                          name={[field.name, 'submodules']}
                          label="拉取 Git Submodule"
                          valuePropName="checked"
                        >
                          <Switch disabled={buildSourceType === 'external_pipeline'} />
                        </Form.Item>
                      </div>
                      {repository ? (
                        <Text type="secondary">{`${repository.url} · ${repository.protocol.toUpperCase()}`}</Text>
                      ) : null}
                    </Card>
                  )
                })}
              </div>
            )}
          </Form.List>
        </section>
      ) : null}

      {section !== 'repositories' ? (
        <section className="soha-application-service-editor-section">
          <div className="soha-application-service-editor-section__head">
            <span className="soha-application-service-editor-section__identity">
              <Text strong>构建执行</Text>
              <Text type="secondary">配置所选方式的构建参数</Text>
            </span>
          </div>
          <div className="soha-application-service-form-grid">
            {buildSourceType === 'repo_buildpacks' ? (
              <BuildpacksFields applicationId={applicationId} form={form} prefix={prefix} />
            ) : null}
            {buildSourceType === 'repo_dockerfile' ? (
              <>
                <Form.Item name={fieldName('config', 'dockerfilePath')} label="Dockerfile">
                  <Input placeholder="Dockerfile" />
                </Form.Item>
                <Form.Item name={fieldName('config', 'contextDir')} label="构建上下文">
                  <Input placeholder="." />
                </Form.Item>
                <Form.Item name={fieldName('config', 'builderKind')} label="构建执行器">
                  <Select
                    options={[
                      { value: 'docker', label: 'Docker' },
                      { value: 'buildx', label: 'Docker Buildx' },
                      { value: 'kaniko', label: 'Kaniko' },
                    ]}
                  />
                </Form.Item>
              </>
            ) : null}
            {buildSourceType === 'platform_build_template' ? (
              <>
                <Form.Item
                  name={fieldName('config', 'buildTemplateId')}
                  label="构建模板"
                  rules={[{ required: true, message: '请选择构建模板' }]}
                >
                  <Select
                    loading={buildTemplatesQuery.isFetching}
                    options={(buildTemplatesQuery.data ?? [])
                      .filter(
                        (template) =>
                          template.id === templateId ||
                          (template.enabled &&
                            (template.publishedVersion ?? 0) > 0 &&
                            template.publicationState !== 'deprecated'),
                      )
                      .map((template) => ({
                        value: template.id,
                        label: template.name,
                        disabled: template.publicationState === 'deprecated' || !template.enabled,
                      }))}
                    onChange={(id) =>
                      form.setFieldValue(
                        fieldName('config', 'buildTemplateVersion'),
                        buildTemplatesQuery.data?.find((item) => item.id === id)?.publishedVersion,
                      )
                    }
                  />
                </Form.Item>
                {Object.entries(selectedBuildTemplate?.variableSchema ?? {}).map(([key, value]) => {
                  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
                  const { required, title, label, ...field } = value as Record<string, unknown>
                  return (
                    <ParameterField
                      key={`${templateId}:${templateVersion}:${key}`}
                      name={fieldName('config', 'variables', key)}
                      field={field as TemplateParameterSchema}
                      label={String(title || label || key)}
                      required={Boolean(required)}
                    />
                  )
                })}
                <Form.Item
                  name={fieldName('config', 'buildTemplateVersion')}
                  hidden
                  rules={[{ required: true, message: '请选择已发布版本' }]}
                >
                  <Input />
                </Form.Item>
                <Form.Item label="固定版本">
                  <Select
                    aria-label="构建模板固定版本"
                    loading={versionsQuery.isFetching}
                    value={templateVersion}
                    options={versionsQuery.data?.map((item) => ({
                      value: item.publishedVersion,
                      label: `v${item.publishedVersion}`,
                    }))}
                    onChange={(version) => {
                      const next = versionsQuery.data?.find(
                        (item) => item.publishedVersion === version,
                      )
                      if (
                        selectedBuildTemplate &&
                        next &&
                        !window.confirm(
                          `确认将构建模板从 v${templateVersion} 改为 v${version}？\n当前版本：\n${JSON.stringify({ commands: selectedBuildTemplate.buildCommands, dockerfile: selectedBuildTemplate.dockerfileTemplate, variables: selectedBuildTemplate.variableSchema, defaults: selectedBuildTemplate.defaultVariables }, null, 2)}\n新版本：\n${JSON.stringify({ commands: next.buildCommands, dockerfile: next.dockerfileTemplate, variables: next.variableSchema, defaults: next.defaultVariables }, null, 2)}`,
                        )
                      )
                        return
                      form.setFieldValue(fieldName('config', 'buildTemplateVersion'), version)
                    }}
                  />
                  {versionsQuery.isError ? (
                    <Text type="danger">{versionsQuery.error.message}</Text>
                  ) : null}
                </Form.Item>
              </>
            ) : null}
            {buildSourceType === 'external_pipeline' ? (
              <ExternalPipelineFields prefix={fieldName('config', 'externalPipeline')} />
            ) : null}
          </div>
          {buildSourceType === 'platform_build_template' ? (
            <div className="soha-application-service-build-steps">
              <Text strong>构建步骤</Text>
              {selectedBuildTemplate?.buildCommands?.length ? (
                <ol>
                  {selectedBuildTemplate.buildCommands.map((command) => (
                    <li key={command}>
                      <code>{command}</code>
                    </li>
                  ))}
                </ol>
              ) : (
                <Text type="secondary">
                  {selectedBuildTemplate?.dockerfileTemplate
                    ? '使用模板 Dockerfile 构建并推送镜像。'
                    : '选择模板后显示执行命令。'}
                </Text>
              )}
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  )
}
