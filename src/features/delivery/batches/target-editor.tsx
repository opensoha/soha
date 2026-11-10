import { Alert, Button, Form, Input, Modal, Radio, Select, Space } from 'antd'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { kubernetesHelmQueries } from '@/features/platform'
import { deliveryQueries } from '../queries'
import type { DeliveryTargetInput } from '../types'
import { deliveryActionLabels } from './model'

type TargetForm = DeliveryTargetInput & {
  arguments?: { key: string; value: string }[]
  helmOperation?: 'configuration' | 'rollback'
}

export function DeliveryTargetEditor({
  target,
  fixed,
  embedded = false,
  pending = false,
  blockObserveOnly = false,
  onChange,
  onSave,
  onClose,
}: {
  target: DeliveryTargetInput
  fixed?: boolean
  embedded?: boolean
  pending?: boolean
  blockObserveOnly?: boolean
  onChange?: () => void
  onSave: (target: DeliveryTargetInput) => void
  onClose: () => void
}) {
  const [form] = Form.useForm<TargetForm>()
  const applicationId = Form.useWatch('applicationId', form) ?? ''
  const serviceId = Form.useWatch('serviceId', form) ?? ''
  const environmentId = Form.useWatch('applicationEnvironmentId', form)
  const action = Form.useWatch('action', form)
  const releaseTargetId = Form.useWatch('releaseTargetId', form)
  const helmOperation = Form.useWatch('helmOperation', form)
  const building = action === 'build' || action === 'build_deploy'
  const applications = useQuery(deliveryQueries.applications.list(!fixed))
  const detail = useQuery(deliveryQueries.applications.detail(applicationId))
  const services = useQuery(deliveryQueries.applications.services(applicationId))
  const bundles = useQuery(
    deliveryQueries.releaseBundles.list({
      enabled: !!applicationId && (action === 'deploy' || action === 'config_update'),
    }),
  )
  const repositories = useQuery(
    deliveryQueries.repositories.list({ applicationId }, !!applicationId && building),
  )
  const service = readableQueryData(services)?.find((item) => item.id === serviceId)
  const source = readableQueryData(detail)?.application.buildSources?.find(
    (item) => item.id === service?.buildSourceId,
  )
  const repositoryIds =
    source?.config?.repositoryBindings?.map((item) => item.repositoryId) ??
    [source?.config?.repositoryId || service?.repositoryId].filter(Boolean)
  const binding = readableQueryData(detail)?.bindings?.find(
    (item) => item.applicationEnvironmentId === environmentId,
  )
  const targets = (binding?.targets ?? []).filter(
    (item) => item.enabled && item.metadata?.serviceId === serviceId,
  )
  const selectedTarget =
    targets.length === 1 ? targets[0] : targets.find((item) => item.id === releaseTargetId)
  const observeOnlyTarget = (target?: (typeof targets)[number]) =>
    target?.executorKind === 'observe_only' || target?.metadata?.ownershipMode === 'observe_only'
  const observeOnly = blockObserveOnly && action !== 'build' && observeOnlyTarget(selectedTarget)
  const docker =
    selectedTarget?.executorKind === 'docker_compose' ? selectedTarget.docker : undefined
  const helm = selectedTarget?.executorKind === 'helm_sdk' ? selectedTarget.helm : undefined
  const rollback = !!helm && action === 'config_update' && helmOperation === 'rollback'
  const history = useQuery(
    kubernetesHelmQueries.releaseHistory(
      rollback && selectedTarget
        ? {
            clusterId: selectedTarget.clusterId,
            namespace: selectedTarget.namespace,
            name: helm!.releaseName,
          }
        : null,
    ),
  )
  const latestRevision = Math.max(
    0,
    ...(readableQueryData(history) ?? []).map((item) => Number(item.revision)),
  )
  const blocked = Boolean(
    (!fixed && (applications.isLoading || applications.isError)) ||
    (applicationId &&
      (detail.isError || services.isError || detail.isLoading || services.isLoading)) ||
    (building && (repositories.isLoading || repositories.isError)) ||
    ((action === 'deploy' || (docker && action === 'config_update')) &&
      (bundles.isError || bundles.isLoading)) ||
    (rollback && (history.isError || history.isLoading)),
  )
  const content = (
    <>
      <ReadFeedback query={applications} label="应用选项" />
      <ReadFeedback query={detail} label="应用环境" />
      <ReadFeedback query={services} label="服务选项" />
      <ReadFeedback query={bundles} label="已验证产物" />
      <ReadFeedback query={repositories} label="源码仓库" />
      <ReadFeedback query={history} label="Helm 历史版本" />
      {observeOnly ? (
        <Alert showIcon type="warning" title="此部署目标仅用于观察，请选择可交付的目标。" />
      ) : null}
      <Form
        form={form}
        layout="vertical"
        disabled={pending || blocked}
        onValuesChange={onChange}
        initialValues={{
          ...target,
          helmOperation: target.helmRevision ? 'rollback' : 'configuration',
          arguments: Object.entries(target.buildArgs ?? {}).map(([key, value]) => ({ key, value })),
        }}
        onFinish={(values) => {
          if (blocked || pending || observeOnly) return
          const { arguments: args, helmOperation: _operation, ...input } = values
          onSave({
            ...target,
            ...input,
            releaseTargetId: selectedTarget?.id || input.releaseTargetId,
            helmRevision: rollback ? input.helmRevision : undefined,
            releaseBundleId:
              action === 'deploy' || (docker && action === 'config_update')
                ? input.releaseBundleId
                : undefined,
            repositoryRefs: building ? input.repositoryRefs : undefined,
            buildArgs:
              building && args?.length
                ? Object.fromEntries(args.map((item) => [item.key, item.value]))
                : undefined,
          })
        }}
      >
        <Form.Item
          name="applicationId"
          label="应用"
          rules={[{ required: true }]}
          hidden={fixed && embedded}
        >
          <Select
            disabled={fixed}
            showSearch={{ optionFilterProp: 'label' }}
            loading={applications.isLoading}
            options={(fixed && readableQueryData(detail)
              ? [readableQueryData(detail)!.application]
              : readableQueryData(applications)
            )
              ?.filter((item) => item.enabled)
              .map((item) => ({ value: item.id, label: item.name }))}
            onChange={() =>
              form.setFieldsValue({
                serviceId: undefined,
                applicationEnvironmentId: undefined,
                releaseTargetId: undefined,
                releaseBundleId: undefined,
                helmRevision: undefined,
                repositoryRefs: [],
                arguments: [],
              })
            }
          />
        </Form.Item>
        <Form.Item
          name="serviceId"
          label="服务"
          rules={[{ required: true }]}
          hidden={fixed && embedded}
        >
          <Select
            disabled={fixed || !applicationId}
            showSearch={{ optionFilterProp: 'label' }}
            loading={services.isLoading}
            options={readableQueryData(services)
              ?.filter((item) => item.enabled)
              .map((item) => ({ value: item.id, label: item.name }))}
            onChange={() =>
              form.setFieldsValue({
                releaseTargetId: undefined,
                helmRevision: undefined,
                repositoryRefs: [],
                arguments: [],
              })
            }
          />
        </Form.Item>
        <Form.Item name="action" label="交付方式" rules={[{ required: true }]}>
          <Select
            onChange={() =>
              form.setFieldsValue({ helmRevision: undefined, helmOperation: 'configuration' })
            }
            options={Object.entries(deliveryActionLabels).map(([value, label]) => ({
              value,
              label,
            }))}
          />
        </Form.Item>
        <Form.Item
          name="applicationEnvironmentId"
          label="应用环境"
          rules={[{ required: action !== 'build' }]}
        >
          <Select
            disabled={fixed && !embedded}
            allowClear={action === 'build'}
            loading={detail.isLoading}
            options={readableQueryData(detail)?.bindings?.map((item) => ({
              value: item.applicationEnvironmentId,
              label: item.environmentName || item.environmentKey || item.environmentId,
            }))}
            onChange={() =>
              form.setFieldsValue({ releaseTargetId: undefined, helmRevision: undefined })
            }
          />
        </Form.Item>
        {action !== 'build' && targets.length > 1 ? (
          <Form.Item name="releaseTargetId" label="部署目标" rules={[{ required: true }]}>
            <Select
              onChange={() => form.setFieldValue('helmRevision', undefined)}
              options={targets.map((item) => ({
                value: item.id,
                disabled: blockObserveOnly && observeOnlyTarget(item),
                label: item.docker
                  ? `Docker · ${item.docker.hostId} / ${item.docker.projectId}`
                  : `${item.clusterId} / ${item.namespace} / ${item.workloadName || item.id}${blockObserveOnly && observeOnlyTarget(item) ? '（仅观察）' : ''}`,
              }))}
            />
          </Form.Item>
        ) : null}
        {action === 'deploy' || (docker && action === 'config_update') ? (
          <Form.Item name="releaseBundleId" label="已验证产物" rules={[{ required: true }]}>
            <Select
              showSearch={{ optionFilterProp: 'label' }}
              loading={bundles.isLoading}
              options={readableQueryData(bundles)
                ?.filter((item) => item.applicationId === applicationId && !!item.artifactDigest)
                .map((item) => ({
                  value: item.id,
                  label: `${item.version} · ${item.artifactDigest}`,
                }))}
            />
          </Form.Item>
        ) : null}
        {action === 'config_update' ? (
          <>
            {helm ? (
              <Form.Item name="helmOperation" label="Helm 操作">
                <Radio.Group
                  options={[
                    { value: 'configuration', label: '安装或更新配置' },
                    { value: 'rollback', label: '恢复历史版本' },
                  ]}
                />
              </Form.Item>
            ) : null}
            {rollback ? (
              <Form.Item
                name="helmRevision"
                label="恢复至 Release 版本"
                rules={[{ required: true }]}
              >
                <Select
                  loading={history.isLoading}
                  options={(readableQueryData(history) ?? [])
                    .filter(
                      (item) =>
                        Number(item.revision) > 0 &&
                        Number(item.revision) < latestRevision &&
                        ['deployed', 'superseded'].includes(item.status || ''),
                    )
                    .map((item) => ({
                      value: Number(item.revision),
                      label: `Revision ${item.revision} · ${item.chart || helm?.source.chart} ${item.chartVersion || ''}`,
                    }))}
                />
              </Form.Item>
            ) : null}
            <Alert
              showIcon
              type="info"
              title={
                rollback
                  ? '恢复所选版本的 Chart 和 Values；重新预检并遵循环境审批策略。'
                  : docker
                    ? '使用所选已验证产物，重新预检并应用 Docker 项目配置。'
                    : '复用当前部署产物，重新检查并应用服务配置。'
              }
            />
          </>
        ) : null}
        {building ? (
          <details>
            <summary>构建版本与参数（默认使用服务和环境配置）</summary>
            <Form.List name="repositoryRefs">
              {(fields, { add, remove }) => (
                <Space orientation="vertical" style={{ width: '100%' }}>
                  {fields.map((field) => (
                    <Space key={field.key} align="start" wrap>
                      <Form.Item name={[field.name, 'repositoryId']} rules={[{ required: true }]}>
                        <Select
                          aria-label="源码仓库"
                          placeholder="源码仓库"
                          style={{ width: 200 }}
                          options={readableQueryData(repositories)
                            ?.filter((item) => repositoryIds.includes(item.id))
                            .map((item) => ({ value: item.id, label: item.name }))}
                        />
                      </Form.Item>
                      <Form.Item name={[field.name, 'refType']} rules={[{ required: true }]}>
                        <Select
                          aria-label="版本类型"
                          style={{ width: 95 }}
                          options={[
                            { value: 'branch', label: '分支' },
                            { value: 'tag', label: '标签' },
                            { value: 'commit', label: 'Commit' },
                          ]}
                        />
                      </Form.Item>
                      <Form.Item name={[field.name, 'refName']} rules={[{ required: true }]}>
                        <Input aria-label="版本值" placeholder="分支、标签或 Commit" />
                      </Form.Item>
                      <Button onClick={() => remove(field.name)} aria-label="移除版本覆盖">
                        移除
                      </Button>
                    </Space>
                  ))}
                  <Button onClick={() => add({ refType: 'branch' })}>指定仓库版本</Button>
                </Space>
              )}
            </Form.List>
            <Form.List
              name="arguments"
              rules={[
                {
                  validator: async (_, values: { key: string }[] = []) => {
                    if (new Set(values.map((item) => item.key)).size !== values.length)
                      throw new Error('构建参数名不能重复')
                  },
                },
              ]}
            >
              {(fields, { add, remove }, { errors }) => (
                <Space orientation="vertical" style={{ width: '100%', marginTop: 16 }}>
                  {fields.map((field) => (
                    <Space key={field.key} align="start">
                      <Form.Item name={[field.name, 'key']} rules={[{ required: true }]}>
                        <Input aria-label="构建参数名" placeholder="参数名" />
                      </Form.Item>
                      <Form.Item name={[field.name, 'value']} rules={[{ required: true }]}>
                        <Input aria-label="构建参数值" placeholder="参数值" />
                      </Form.Item>
                      <Button onClick={() => remove(field.name)}>移除</Button>
                    </Space>
                  ))}
                  <Form.ErrorList errors={errors} />
                  <Button onClick={() => add({ key: '', value: '' })}>覆盖构建参数</Button>
                </Space>
              )}
            </Form.List>
          </details>
        ) : null}
        {embedded ? (
          <Space style={{ marginTop: 16 }}>
            <Button onClick={onClose}>取消</Button>
            <Button
              type="primary"
              htmlType="submit"
              loading={pending}
              disabled={blocked || pending || observeOnly}
            >
              检查交付参数
            </Button>
          </Space>
        ) : null}
      </Form>
    </>
  )
  return embedded ? (
    content
  ) : (
    <Modal
      open
      title="交付目标"
      width={720}
      onCancel={onClose}
      okText="保存目标"
      okButtonProps={{ disabled: blocked || pending || observeOnly }}
      onOk={() => form.submit()}
    >
      {content}
    </Modal>
  )
}
