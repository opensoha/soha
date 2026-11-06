import { PlusOutlined } from '@ant-design/icons'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Form, Input, Modal, Select, Space, Spin, Typography } from 'antd'
import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { usePlatformMutation, platformResourceTarget } from '../shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { toScopeKey } from '@/types'
import { clusterMutations } from './mutations'
import { clusterQueries } from './queries'
import { clusterTypeOf, clusterTypeOptions } from './presentation'
import { PrometheusCheckResult, useClusterPrometheusCheck } from './prometheus-check'
import type { Cluster, ClusterFormValues, ConnectionMode } from './types'

const { Text } = Typography

export function ClusterEditor({
  cluster,
  onClose,
  writeBlocked = false,
}: {
  cluster: Cluster | null
  onClose: (created?: Cluster) => void
  writeBlocked?: boolean
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const queryClient = useQueryClient()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canCreate = hasPermission(permissions, 'platform.clusters.create')
  const canUpdate = hasPermission(permissions, 'platform.clusters.update')
  const [created, setCreated] = useState<Cluster>()
  const editingCluster = cluster || created || null
  const [savedId, setSavedId] = useState<string>()
  const [kubeconfigExpanded, setKubeconfigExpanded] = useState(false)
  const [tokenExpanded, setTokenExpanded] = useState(false)
  const [savedTokenConfigured, setSavedTokenConfigured] = useState<boolean>()
  const formId = useId()
  const resultRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (savedId) {
      resultRef.current?.focus({ preventScroll: true })
      resultRef.current?.scrollIntoView?.({ block: 'nearest' })
    }
  }, [savedId])
  const [connectionMode, setConnectionMode] = useState<ConnectionMode>(
    cluster?.connectionMode === 'agent' ? 'agent' : 'direct_kubeconfig',
  )
  const [form] = Form.useForm<ClusterFormValues>()
  const hasPrometheusLabel = Boolean(Form.useWatch('prometheusClusterLabel', form)?.trim())
  const clusterDetailQuery = useQuery({
    ...clusterQueries.detail(toScopeKey(cluster?.id, null)),
    enabled: Boolean(cluster?.id && canUpdate),
    refetchOnWindowFocus: false,
  })
  const detail = readableQueryData(clusterDetailQuery)
  const initialized = useRef(false)
  useEffect(() => {
    if (detail && !initialized.current) {
      initialized.current = true
      setConnectionMode(detail.connection.mode === 'agent' ? 'agent' : 'direct_kubeconfig')
    }
  }, [detail])
  const initialValues = useMemo<ClusterFormValues>(
    () =>
      cluster
        ? {
            name: cluster.name,
            provider: clusterTypeOf(cluster) || undefined,
            environment: cluster.environment,
            connectionMode: (detail?.connection.mode || cluster.connectionMode) as ConnectionMode,
            prometheusTransport: detail?.monitoring.prometheus.transport || 'direct',
            agentActionProfile: detail?.connection.actionProfile || 'legacy',
            agentCustomResourceRules: detail?.connection.customResourceRules || [],
            prometheusBaseUrl: detail?.monitoring.prometheus.baseUrl || '',
            prometheusClusterLabel: detail?.monitoring.prometheus.clusterLabel || '',
            prometheusClusterLabelValue: detail?.monitoring.prometheus.clusterLabelValue || '',
          }
        : {
            connectionMode: 'direct_kubeconfig',
            provider: 'standard_kubernetes',
            prometheusTransport: 'direct',
            agentActionProfile: 'workbench',
            agentCustomResourceRules: [],
          },
    [cluster, detail],
  )
  const createMutation = usePlatformMutation(clusterMutations.create(queryClient), {
    label: ['创建集群', 'Create cluster'],
    target: (v) => String(v.name || 'Cluster'),
    receipt: () => ({ message: zh ? '集群配置已保存' : 'Cluster configuration saved' }),
  })
  const updateMutation = usePlatformMutation(clusterMutations.update(queryClient), {
    label: ['更新集群', 'Update cluster'],
    target: platformResourceTarget,
    receipt: () => ({ message: zh ? '集群配置已保存' : 'Cluster configuration saved' }),
  })
  const platformWriteBlocked = writeBlocked || createMutation.blocked || updateMutation.blocked
  const check = useClusterPrometheusCheck(savedId, Boolean(savedId))
  const close = () => {
    if (!platformWriteBlocked && !createMutation.isPending && !updateMutation.isPending)
      onClose(cluster ? undefined : created)
  }
  const handleSubmit = async (values: ClusterFormValues) => {
    if (platformWriteBlocked || (editingCluster ? !canUpdate : !canCreate)) return
    const labels = { ...(editingCluster?.labels ?? {}) }
    if (values.provider?.trim()) labels.provider = values.provider.trim()
    else delete labels.provider
    const prometheusClusterLabel = values.prometheusClusterLabel?.trim() || ''
    const payload: Record<string, unknown> = {
      ...values,
      region: editingCluster?.region || '',
      labels,
      prometheusClusterLabel,
      prometheusClusterLabelValue: prometheusClusterLabel
        ? values.prometheusClusterLabelValue?.trim() || ''
        : '',
      prometheusTransport:
        values.connectionMode === 'agent' ? values.prometheusTransport : 'direct',
    }
    delete payload.provider
    try {
      const saved = editingCluster
        ? await updateMutation.mutateAsync({
            scope: toScopeKey(editingCluster.id, null),
            values: payload,
          })
        : await createMutation.mutateAsync(payload)
      const id = saved.id || editingCluster?.id
      if (!cluster && saved.id) setCreated(saved)
      if (values.prometheusBearerToken?.trim()) setSavedTokenConfigured(true)
      form.setFieldValue('prometheusBearerToken', undefined)
      form.setFieldValue('kubeconfig', undefined)
      setKubeconfigExpanded(false)
      setTokenExpanded(false)
      if (values.prometheusBaseUrl?.trim() && id) setSavedId(id)
      else onClose(cluster ? undefined : saved)
    } catch {
      /* The shared mutation owner retains scoped error and write-fence feedback. */
    }
  }
  const allowed = editingCluster ? canUpdate : canCreate
  const hasSavedKubeconfig = Boolean(
    editingCluster &&
    (cluster
      ? detail?.connection.mode === 'direct_kubeconfig'
      : created?.connectionMode === 'direct_kubeconfig'),
  )
  const pending = createMutation.isPending || updateMutation.isPending
  const hasSavedToken =
    savedTokenConfigured ?? Boolean(detail?.monitoring.prometheus.hasBearerToken)
  const editAgain = () => {
    setSavedId(undefined)
    createMutation.reset()
    updateMutation.reset()
  }
  return (
    <Modal
      className="soha-cluster-editor"
      classNames={{ body: 'soha-cluster-editor__body', footer: 'soha-cluster-editor__footer' }}
      title={editingCluster ? (zh ? '编辑集群' : 'Edit cluster') : zh ? '添加集群' : 'Add cluster'}
      open
      destroyOnHidden
      width={840}
      style={{ top: 32 }}
      styles={{
        container: { maxHeight: '85dvh', display: 'flex', flexDirection: 'column' },
        header: { flexShrink: 0 },
        body: { minHeight: 0, overflowY: 'auto' },
        footer: { flexShrink: 0 },
      }}
      onCancel={close}
      mask={{ closable: !platformWriteBlocked }}
      closable={{ disabled: platformWriteBlocked }}
      footer={
        savedId ? (
          <>
            <Button
              key="edit-again"
              onClick={editAgain}
              disabled={platformWriteBlocked || !canUpdate}
            >
              {zh ? '继续编辑' : 'Edit again'}
            </Button>
            <Button key="done" type="primary" onClick={close} disabled={platformWriteBlocked}>
              {zh ? '完成' : 'Done'}
            </Button>
          </>
        ) : (
          <>
            <Button key="cancel" onClick={close} disabled={platformWriteBlocked}>
              {zh ? '取消' : 'Cancel'}
            </Button>
            <Button
              key="save"
              type="primary"
              htmlType="submit"
              form={formId}
              loading={pending}
              disabled={
                platformWriteBlocked ||
                !allowed ||
                Boolean(cluster && !detail) ||
                clusterDetailQuery.isError
              }
            >
              {check.canCheck ? (zh ? '保存并校验' : 'Save and check') : zh ? '保存' : 'Save'}
            </Button>
          </>
        )
      }
    >
      <div className="soha-cluster-editor__feedback" ref={resultRef} tabIndex={-1}>
        {createMutation.feedback}
        {updateMutation.feedback}
        {savedId && <PrometheusCheckResult check={check} clusterId={savedId} />}
      </div>
      <ReadFeedback
        canRead={Boolean(cluster?.id && canUpdate)}
        query={clusterDetailQuery}
        label={zh ? '集群详情' : 'Cluster detail'}
      />
      {!allowed ? (
        <Alert
          type="warning"
          title={zh ? '没有集群编辑权限' : 'Cluster edit permission required'}
        />
      ) : cluster && clusterDetailQuery.isLoading && !detail ? (
        <Spin />
      ) : cluster && !detail ? null : (
        <>
          {detail?.connection.sourceType === 'config' && (
            <Alert
              showIcon
              type="info"
              title={
                zh
                  ? '配置文件托管：重启以文件为准，请同步修改文件中的监控设置。'
                  : 'File-managed cluster: startup reloads the file. Keep its monitoring settings in sync.'
              }
            />
          )}
          <Form
            id={formId}
            scrollToFirstError={{ focus: true }}
            form={form}
            disabled={Boolean(savedId) || platformWriteBlocked}
            layout="vertical"
            onFinish={handleSubmit}
            onValuesChange={() => setSavedId(undefined)}
            initialValues={initialValues}
            preserve={false}
          >
            <section
              className="soha-cluster-editor__section"
              aria-label={zh ? '基本信息' : 'Basic information'}
            >
              <h3>{zh ? '基本信息' : 'Basic information'}</h3>
              <div className="soha-cluster-editor__grid">
                <Form.Item
                  name="name"
                  label={zh ? '集群名称' : 'Cluster name'}
                  rules={[{ required: true, message: '请输入集群名称' }]}
                >
                  <Input />
                </Form.Item>
                <Form.Item
                  name="provider"
                  label={localeCode === 'zh_CN' ? '集群类型' : 'Cluster Type'}
                  rules={[
                    {
                      required: !editingCluster,
                      message: localeCode === 'zh_CN' ? '请选择集群类型' : 'Select a cluster type',
                    },
                  ]}
                >
                  <Select
                    options={clusterTypeOptions.map((item) => ({
                      value: item.value,
                      label: localeCode === 'zh_CN' ? item.labelZh : item.labelEn,
                    }))}
                  />
                </Form.Item>
                <Form.Item name="environment" label={zh ? '环境' : 'Environment'}>
                  <Input />
                </Form.Item>
                <Form.Item name="connectionMode" label="连接方式">
                  <Select
                    options={[
                      { value: 'direct_kubeconfig', label: '直接 Kubeconfig' },
                      { value: 'agent', label: 'Agent' },
                    ]}
                    onChange={(value) => setConnectionMode(value as ConnectionMode)}
                  />
                </Form.Item>
              </div>
            </section>

            {connectionMode === 'direct_kubeconfig' ? (
              <section
                className="soha-cluster-editor__section"
                aria-label={zh ? '连接凭据' : 'Connection credentials'}
              >
                <div className="soha-cluster-editor__section-heading">
                  <h3>{zh ? '连接凭据' : 'Connection credentials'}</h3>
                  {hasSavedKubeconfig && (
                    <Button
                      type="link"
                      size="small"
                      aria-expanded={kubeconfigExpanded}
                      disabled={Boolean(savedId) || platformWriteBlocked}
                      onClick={() => setKubeconfigExpanded(!kubeconfigExpanded)}
                    >
                      {kubeconfigExpanded
                        ? zh
                          ? '收起'
                          : 'Collapse'
                        : zh
                          ? '更换 Kubeconfig'
                          : 'Replace Kubeconfig'}
                    </Button>
                  )}
                </div>
                {hasSavedKubeconfig && (
                  <Text type="secondary">
                    {zh
                      ? 'Kubeconfig 已配置；留空沿用现有凭据。'
                      : 'Kubeconfig is configured. Leave empty to keep existing credentials.'}
                  </Text>
                )}
                <Form.Item
                  name="kubeconfig"
                  label="Kubeconfig"
                  hidden={hasSavedKubeconfig && !kubeconfigExpanded}
                  rules={
                    hasSavedKubeconfig
                      ? undefined
                      : [{ required: true, message: '请输入 Kubeconfig' }]
                  }
                >
                  <Input.TextArea
                    placeholder={
                      hasSavedKubeconfig ? '留空则沿用现有 kubeconfig' : '请输入 kubeconfig 内容'
                    }
                    autoSize={{ minRows: 4, maxRows: 8 }}
                  />
                </Form.Item>
              </section>
            ) : null}

            {connectionMode === 'agent' ? (
              <Form.Item
                name="agentActionProfile"
                label={localeCode === 'zh_CN' ? 'Agent 动作预设' : 'Agent action preset'}
                extra={
                  localeCode === 'zh_CN'
                    ? '工作台预设启用资源 CRUD、维护/排水、终端、转发与 Helm；只读预设关闭写入和执行。保存不会改变运行中的 Agent，需重新生成并核对安装清单后应用。CRD 定义删除仍需单独启用。'
                    : 'Workbench enables resource CRUD, maintenance/drain, terminal, forwarding and Helm; read-only disables writes and execution. Saving does not change a running Agent. Regenerate and review its installation manifest before applying. CRD definition deletion remains a separate opt-in.'
                }
              >
                <Select
                  options={[
                    { value: 'workbench', label: localeCode === 'zh_CN' ? '工作台' : 'Workbench' },
                    { value: 'read-only', label: localeCode === 'zh_CN' ? '只读' : 'Read-only' },
                    {
                      value: 'legacy',
                      label:
                        localeCode === 'zh_CN'
                          ? '保留原安装权限'
                          : 'Preserve legacy installation permissions',
                    },
                  ]}
                />
              </Form.Item>
            ) : null}

            <section
              className="soha-cluster-editor__section"
              aria-label={zh ? '监控接入' : 'Monitoring'}
            >
              <h3>
                {zh ? '监控接入' : 'Monitoring'} <Text type="secondary">Prometheus</Text>
              </h3>
              <Form.Item
                name="prometheusTransport"
                label="访问方式"
                hidden={connectionMode !== 'agent'}
                extra={
                  connectionMode === 'agent'
                    ? '选择 Agent 时，保存后请重新应用 Agent 安装清单，以同步 Prometheus 地址和凭据。'
                    : undefined
                }
              >
                <Select
                  options={[
                    { value: 'direct', label: 'Core 直连' },
                    ...(connectionMode === 'agent'
                      ? [{ value: 'agent', label: 'Agent 代理' }]
                      : []),
                  ]}
                />
              </Form.Item>
              {connectionMode !== 'agent' && (
                <div className="soha-cluster-editor__transport">
                  <Text type="secondary">{zh ? '访问方式' : 'Transport'}</Text>{' '}
                  <span>{zh ? 'Core 直连' : 'Core direct'}</span>
                </div>
              )}
              <Form.Item name="prometheusBaseUrl" label="Prometheus URL">
                <Input placeholder="http://prometheus:9090" />
              </Form.Item>
              <div className="soha-cluster-editor__grid">
                <Form.Item
                  name="prometheusClusterLabel"
                  label={zh ? '集群标签名（选填）' : 'Cluster label name (optional)'}
                  rules={[
                    {
                      pattern: /^[a-zA-Z_][a-zA-Z0-9_]*$/,
                      message: zh
                        ? '请输入有效的 Prometheus 标签名'
                        : 'Enter a valid Prometheus label name',
                    },
                  ]}
                >
                  <Input
                    placeholder="cluster"
                    maxLength={128}
                    onChange={(event) => {
                      if (!event.target.value.trim())
                        form.setFieldValue('prometheusClusterLabelValue', '')
                    }}
                  />
                </Form.Item>
                <Form.Item
                  name="prometheusClusterLabelValue"
                  label={zh ? '集群标签值' : 'Cluster label value'}
                  rules={[{ max: 1024 }]}
                >
                  <Input
                    disabled={!hasPrometheusLabel}
                    placeholder={
                      hasPrometheusLabel ? editingCluster?.id || 'middleware-prod' : undefined
                    }
                    maxLength={1024}
                  />
                </Form.Item>
              </div>
              <div className="soha-cluster-editor__hint">
                <Text type="secondary">
                  {zh
                    ? '独立 Prometheus 可将标签留空。多个集群共用时，请填写指标里的同一个标签名，并为每个集群填写不同的值。填写标签名后，值留空会使用 Soha 集群 ID。'
                    : 'Leave the label blank for a dedicated Prometheus source. For a shared source, use the same metric label name with a distinct value for each cluster. If only the name is set, the value defaults to the Soha cluster ID.'}
                </Text>
              </div>
              <div className="soha-cluster-editor__section-heading">
                <Text type="secondary">
                  {zh ? 'Token 认证' : 'Token authentication'} ·{' '}
                  {hasSavedToken
                    ? zh
                      ? '已配置'
                      : 'Configured'
                    : zh
                      ? '未配置'
                      : 'Not configured'}
                </Text>
                <Button
                  type="link"
                  size="small"
                  aria-expanded={tokenExpanded}
                  disabled={Boolean(savedId) || platformWriteBlocked}
                  onClick={() => setTokenExpanded(!tokenExpanded)}
                >
                  {tokenExpanded
                    ? zh
                      ? '收起 Token'
                      : 'Collapse token'
                    : hasSavedToken
                      ? zh
                        ? '更换 Token'
                        : 'Replace token'
                      : zh
                        ? '配置 Token'
                        : 'Configure token'}
                </Button>
              </div>
              <Form.Item
                name="prometheusBearerToken"
                label="Prometheus Token"
                hidden={!tokenExpanded}
              >
                <Input.Password placeholder={editingCluster ? '留空则沿用现有 token' : ''} />
              </Form.Item>
            </section>

            {connectionMode === 'agent' ? (
              <Card className="soha-detail-card" title="自定义资源授权">
                <Text type="secondary">
                  按 API Group 和资源名称授权。保存后重新应用 Agent
                  安装清单；移除命名空间授权时，还需删除旧的 Role 和 RoleBinding。
                </Text>
                <Form.List name="agentCustomResourceRules">
                  {(fields, { add, remove }) => (
                    <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                      {fields.map(({ key, name, ...rest }) => (
                        <div key={key}>
                          <Form.Item
                            {...rest}
                            preserve
                            name={[name, 'apiGroup']}
                            label="API Group"
                            rules={[{ required: true }]}
                          >
                            <Input placeholder="example.io" />
                          </Form.Item>
                          <Form.Item
                            {...rest}
                            preserve
                            name={[name, 'resources']}
                            label="资源名称（复数）"
                            rules={[{ required: true }]}
                          >
                            <Select mode="tags" placeholder="widgets" />
                          </Form.Item>
                          <Form.Item
                            {...rest}
                            preserve
                            name={[name, 'verbs']}
                            label="权限"
                            rules={[{ required: true }]}
                          >
                            <Select
                              mode="multiple"
                              options={[
                                'get',
                                'list',
                                'watch',
                                'create',
                                'update',
                                'patch',
                                'delete',
                              ].map((value) => ({ value, label: value }))}
                            />
                          </Form.Item>
                          <Form.Item
                            {...rest}
                            preserve
                            name={[name, 'namespaces']}
                            label="命名空间"
                            extra="留空表示整个集群，包括集群级资源；填写后只授权指定命名空间。"
                          >
                            <Select mode="tags" placeholder="apps" />
                          </Form.Item>
                          <Button onClick={() => remove(name)} danger>
                            移除授权
                          </Button>
                        </div>
                      ))}
                      <Button
                        icon={<PlusOutlined />}
                        onClick={() =>
                          add({
                            apiGroup: '',
                            resources: [],
                            verbs: ['get', 'list', 'watch'],
                            namespaces: [],
                          })
                        }
                      >
                        添加授权
                      </Button>
                    </Space>
                  )}
                </Form.List>
              </Card>
            ) : null}
          </Form>
        </>
      )}
    </Modal>
  )
}
