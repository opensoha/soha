import { AdminTable } from '@/components/admin-table'
import { ChartPackageOverview } from './package-overview'
import { ChartReadme } from './readme'
import { buildHelmReleaseRoutePath } from '../paths'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { hasAllowedAction, hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { CloudDownloadOutlined, LinkOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  Button,
  Card,
  Checkbox,
  Collapse,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  App,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import { useEffect, useMemo, useState, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { helmMutations } from '../mutations'
import { helmQueries } from '../queries'
import type {
  HelmChart,
  HelmChartDetailInput,
  HelmChartInstallFormValues,
  HelmChartInstallResource,
  HelmChartInstallResult,
  HelmChartInstallTarget,
  HelmChartValuesInput,
  HelmReleaseTarget,
} from '../types'
import {
  defaultHelmChartInstallForm,
  formatHelmInstallError,
  getHelmChartBadges,
  getHelmChartVersionOptions,
  isHelmReleaseNameConflictError,
  retryHelmReleaseName,
} from './utils'

const { Text } = Typography

interface HelmChartDrawerProps {
  chart: HelmChart
  initialTab: 'overview' | 'install'
  onClose: () => void
}

type HelmChartDrawerTab = 'overview' | 'readme' | 'values' | 'install'

export function HelmChartDrawer({ chart, initialTab, onClose }: HelmChartDrawerProps) {
  const { localeCode } = useI18n()
  const { modal } = App.useApp()
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canRead = hasPermission(permissions, 'platform.helm.view')
  const submitFence = useRef(false)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  const initialNamespace = useRef<string | null>(null)
  const { clusterId, namespace } = usePlatformScopeStore()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const capability = useClusterCapability('helm.releases', localeCode)
  const [form] = Form.useForm<HelmChartInstallFormValues>()
  const [activeTab, setActiveTab] = useState<HelmChartDrawerTab>(initialTab)
  const [selectedVersion, setSelectedVersion] = useState(chart.latestVersion || '')
  const [installTarget, setInstallTarget] = useState<HelmChartInstallTarget | null>(null)
  const [installResult, setInstallResult] = useState<HelmChartInstallResult | null>(null)
  const [installError, setInstallError] = useState('')
  const [installStartedAt, setInstallStartedAt] = useState<number | null>(null)
  const [installElapsedSeconds, setInstallElapsedSeconds] = useState(0)
  const detailInput: HelmChartDetailInput | null =
    clusterId && chart.repositoryName
      ? {
          chartName: chart.name,
          clusterId,
          repositoryName: chart.repositoryName,
          version: selectedVersion,
        }
      : null
  const detailQuery = useQuery({
    ...helmQueries.chartDetail(detailInput),
    enabled: canRead && Boolean(detailInput),
  })
  const visibleDetailQueryData = readableQueryData(detailQuery)
  const detail = visibleDetailQueryData
  const activeChart = detail ?? chart
  const activeVersion = selectedVersion || activeChart.latestVersion || ''
  const valuesInput: HelmChartValuesInput | null =
    clusterId && activeChart.packageId && activeVersion
      ? {
          clusterId,
          packageId: activeChart.packageId,
          name: activeChart.name,
          version: activeVersion,
        }
      : null
  const valuesQuery = useQuery(
    helmQueries.chartValues(
      valuesInput,
      canRead && (activeTab === 'values' || activeTab === 'install'),
    ),
  )
  const visibleValuesQueryData = readableQueryData(valuesQuery)
  const valuesState = useYamlDraft(JSON.stringify(valuesInput), visibleValuesQueryData?.content)
  const { value: valuesDraft, setValue: setValuesDraft } = valuesState
  const mutationsDisabled = capability.status !== 'available'
  const capabilityReason = mutationsDisabled ? capability.reason : ''
  const canInstall =
    canRead &&
    hasPermission(permissions, 'platform.helm.releases.create') &&
    hasAllowedAction(activeChart.allowedActions, 'create') &&
    !mutationsDisabled
  const installConflict = isHelmReleaseNameConflictError(installError)
  const progressTarget: HelmReleaseTarget | null =
    clusterId && installTarget
      ? {
          clusterId,
          name: installTarget.releaseName,
          namespace: installTarget.namespace,
        }
      : null
  const progressQuery = useQuery(
    helmQueries.installProgress(
      progressTarget,
      Boolean(
        installStartedAt && !installResult && (!installError || installConflict) && progressTarget,
      ),
    ),
  )
  const visibleProgressQueryData = readableQueryData(progressQuery)
  const installMutation = usePlatformMutation(helmMutations.installChart(queryClient), {
    label: ['安装 Helm Chart', 'Install Helm chart'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = installMutation.blocked

  useEffect(() => {
    if (initialNamespace.current === null) {
      initialNamespace.current = namespace || 'default'
      form.setFieldsValue(defaultHelmChartInstallForm(chart, initialNamespace.current))
    }
  }, [chart, form, namespace])

  useEffect(() => {
    const nextVersion = selectedVersion || detail?.latestVersion || chart.latestVersion || ''
    form.setFieldsValue({
      repositoryName: activeChart.repositoryName || chart.repositoryName,
      repositoryUrl: activeChart.repositoryUrl || chart.repositoryUrl || chart.urls?.[0] || '',
      chartName: activeChart.name || chart.name,
      version: nextVersion,
    })
  }, [activeChart, chart, detail?.latestVersion, form, selectedVersion])

  useEffect(() => {
    if (!installStartedAt || installResult || installError) return undefined
    const updateElapsed = () =>
      setInstallElapsedSeconds(Math.max(0, Math.floor((Date.now() - installStartedAt) / 1000)))
    updateElapsed()
    const timer = window.setInterval(updateElapsed, 1000)
    return () => window.clearInterval(timer)
  }, [installError, installResult, installStartedAt])

  const versionOptions = getHelmChartVersionOptions(detail, chart)
  const drawerTitle = `Chart: ${activeChart.repositoryName ? `${activeChart.repositoryName}/` : ''}${activeChart.name}`

  const submitInstall = async () => {
    if (activeTab !== 'install') {
      setActiveTab('install')
      return
    }
    if (
      !canInstall ||
      !clusterId ||
      submitFence.current ||
      installMutation.blocked ||
      installMutation.isPending ||
      detailQuery.error ||
      valuesQuery.error ||
      valuesQuery.isPending ||
      !visibleValuesQueryData
    ) {
      setActiveTab('install')
      return
    }
    submitFence.current = true
    let values: HelmChartInstallFormValues
    try {
      values = await form.validateFields()
    } catch {
      submitFence.current = false
      return
    }
    if (!mounted.current) {
      submitFence.current = false
      return
    }
    setActiveTab('install')
    setInstallTarget({
      chartName: values.chartName,
      namespace: values.namespace,
      releaseName: values.releaseName,
      timeoutSeconds: values.timeoutSeconds,
      version: values.version,
      wait: values.wait,
    })
    setInstallResult(null)
    setInstallError('')
    setInstallStartedAt(Date.now())
    setInstallElapsedSeconds(0)
    installMutation.mutate(
      { ...values, clusterId, valuesYaml: valuesDraft },
      {
        onSuccess: (result) => {
          if (!mounted.current) return
          setInstallResult(result)
          setInstallError('')
        },
        onError: (error) => {
          if (mounted.current) setInstallError(error.message)
        },
        onSettled: () => {
          submitFence.current = false
        },
      },
    )
  }

  const resourceColumns: TableColumnsType<HelmChartInstallResource> = useMemo(
    () => [
      { title: 'Kind', dataIndex: 'kind', width: 150 },
      { title: 'Name', dataIndex: 'name' },
      {
        title: localeCode === 'zh_CN' ? '命名空间' : 'Namespace',
        dataIndex: 'namespace',
        width: 160,
      },
      { title: 'API Version', dataIndex: 'apiVersion', width: 160 },
    ],
    [localeCode],
  )
  const observedRelease = visibleProgressQueryData
  const installInFlight = Boolean(installStartedAt && !installResult && !installError)
  const statusColor = installMutation.isPending
    ? 'processing'
    : installResult
      ? 'success'
      : installError
        ? installConflict
          ? 'warning'
          : 'error'
        : 'default'
  const statusLabel = installMutation.isPending
    ? localeCode === 'zh_CN'
      ? '安装中'
      : 'Installing'
    : installResult
      ? localeCode === 'zh_CN'
        ? '已完成'
        : 'Completed'
      : installError
        ? localeCode === 'zh_CN'
          ? '失败'
          : 'Failed'
        : localeCode === 'zh_CN'
          ? '未开始'
          : 'Not started'

  return (
    <Drawer
      destroyOnHidden
      open
      title={drawerTitle}
      size="large"
      width="min(1120px, calc(100vw - 48px))"
      maskClosable={!installMutation.isPending && !platformWriteBlocked}
      keyboard={!installMutation.isPending && !platformWriteBlocked}
      closable={!installMutation.isPending && !platformWriteBlocked}
      onClose={() => {
        if (!platformWriteBlocked && !installMutation.isPending) onClose?.()
      }}
      extra={
        <Space>
          <Button
            autoInsertSpace={false}
            disabled={platformWriteBlocked || installMutation.isPending}
            onClick={onClose}
          >
            {localeCode === 'zh_CN' ? '取消' : 'Cancel'}
          </Button>
          <Button
            autoInsertSpace={false}
            icon={<CloudDownloadOutlined />}
            disabled={
              !canInstall ||
              platformWriteBlocked ||
              (activeTab === 'install' &&
                (valuesQuery.isPending ||
                  !visibleValuesQueryData ||
                  Boolean(valuesQuery.error) ||
                  Boolean(detailQuery.error)))
            }
            loading={installMutation.isPending}
            type="primary"
            onClick={() => void submitInstall()}
          >
            {activeTab === 'install'
              ? localeCode === 'zh_CN'
                ? '确认安装'
                : 'Install'
              : localeCode === 'zh_CN'
                ? '配置安装'
                : 'Configure install'}
          </Button>
        </Space>
      }
    >
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="资源详情" />
      <ReadFeedback canRead={valuesQuery.isEnabled} query={valuesQuery} label="Helm Values" />
      <ReadFeedback canRead={progressQuery.isEnabled} query={progressQuery} label="安装进度" />

      {installMutation.feedback}

      <Space orientation="vertical" size={14} style={{ width: '100%' }}>
        <Space align="start" size={14}>
          {activeChart.logoImageUrl ? (
            <img
              src={activeChart.logoImageUrl}
              alt=""
              style={{ width: 72, height: 72, borderRadius: 6, objectFit: 'contain' }}
            />
          ) : null}
          <Space orientation="vertical" size={6}>
            <Space size={6} wrap>
              <Text strong style={{ fontSize: 16 }}>
                {activeChart.name}
              </Text>
              {getHelmChartBadges(activeChart).map((badge) => (
                <Tag key={badge.label} color={badge.color}>
                  {badge.label}
                </Tag>
              ))}
            </Space>
            <Text type="secondary">{activeChart.description || '-'}</Text>
            {activeChart.artifactHubUrl ? (
              <Button
                href={activeChart.artifactHubUrl}
                icon={<LinkOutlined />}
                size="small"
                target="_blank"
                type="link"
              >
                Artifact Hub
              </Button>
            ) : null}
          </Space>
        </Space>
        <Space wrap>
          <Text>{localeCode === 'zh_CN' ? 'Chart 版本' : 'Chart version'}</Text>
          <Select
            aria-label="Chart version"
            style={{ minWidth: 200 }}
            value={selectedVersion}
            options={versionOptions}
            showSearch
            disabled={installMutation.isPending || platformWriteBlocked}
            onChange={(version) => {
              const change = () => {
                setSelectedVersion(version)
                setInstallResult(null)
                setInstallError('')
                setInstallTarget(null)
                setInstallStartedAt(null)
                installMutation.reset()
              }
              if (valuesDraft !== (valuesState.original ?? ''))
                modal.confirm({
                  title:
                    localeCode === 'zh_CN'
                      ? '切换版本会清除当前 Values 草稿'
                      : 'Changing versions clears the values draft',
                  onOk: change,
                })
              else change()
            }}
          />
        </Space>
        <Tabs
          size="small"
          activeKey={activeTab}
          onChange={(value) => setActiveTab(value as HelmChartDrawerTab)}
          items={[
            {
              key: 'overview',
              label: localeCode === 'zh_CN' ? '概览' : 'Overview',
              children: <ChartPackageOverview chart={activeChart} version={activeVersion} />,
            },
            {
              key: 'readme',
              label: 'README',
              children: activeTab === 'readme' ? <ChartReadme value={detail?.readme} /> : null,
            },
            {
              key: 'values',
              label: localeCode === 'zh_CN' ? '默认 Values' : 'Default values',
              children:
                activeTab === 'values' ? (
                  <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                    {valuesQuery.isLoading ? <Spin size="small" /> : null}
                    {valuesQuery.isError ? (
                      <Text type="secondary">Artifact Hub did not return default values.</Text>
                    ) : null}
                    {visibleValuesQueryData ? (
                      <pre
                        style={{
                          margin: 0,
                          maxHeight: '60vh',
                          overflow: 'auto',
                          whiteSpace: 'pre',
                          background: 'var(--soha-bg-surface-muted)',
                          color: 'var(--soha-text-primary)',
                          border: '1px solid var(--soha-border-color)',
                          borderRadius: 6,
                          padding: 16,
                        }}
                      >
                        <code>{visibleValuesQueryData.content}</code>
                      </pre>
                    ) : null}
                  </Space>
                ) : null,
            },
            {
              key: 'install',
              label: localeCode === 'zh_CN' ? '安装配置' : 'Install configuration',
              forceRender: true,
              children: (
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  {!canInstall ? (
                    <Alert
                      showIcon
                      type="warning"
                      title={
                        mutationsDisabled ? 'Helm installs limited' : 'Install permission required'
                      }
                      description={capabilityReason}
                    />
                  ) : null}
                  <Form
                    form={form}
                    layout="vertical"
                    size="small"
                    disabled={!canInstall || platformWriteBlocked || installMutation.isPending}
                  >
                    <Form.Item name="releaseName" label="Release" rules={[{ required: true }]}>
                      <Input
                        disabled={!canInstall || platformWriteBlocked || Boolean(valuesQuery.error)}
                      />
                    </Form.Item>
                    <Form.Item name="namespace" label="Namespace" rules={[{ required: true }]}>
                      <Input
                        disabled={!canInstall || platformWriteBlocked || Boolean(valuesQuery.error)}
                      />
                    </Form.Item>
                    <Form.Item name="version" hidden>
                      <Input />
                    </Form.Item>
                    <Collapse
                      items={[
                        {
                          key: 'advanced',
                          label:
                            localeCode === 'zh_CN'
                              ? '高级安装选项'
                              : 'Advanced installation options',
                          forceRender: true,
                          children: (
                            <>
                              {' '}
                              <Form.Item
                                name="repositoryUrl"
                                label="Chart Repository URL"
                                rules={[{ required: true }]}
                              >
                                <Input readOnly />
                              </Form.Item>
                              <Form.Item
                                name="chartName"
                                label="Chart"
                                rules={[{ required: true }]}
                              >
                                <Input
                                  disabled={
                                    !canInstall ||
                                    platformWriteBlocked ||
                                    Boolean(valuesQuery.error)
                                  }
                                />
                              </Form.Item>
                              <Form.Item
                                name="timeoutSeconds"
                                label="Timeout seconds"
                                rules={[{ required: true }]}
                              >
                                <InputNumber
                                  min={30}
                                  max={3600}
                                  step={30}
                                  style={{ width: '100%' }}
                                />
                              </Form.Item>
                              <Space size={16} wrap>
                                <Form.Item name="createNamespace" valuePropName="checked">
                                  <Checkbox>Create namespace</Checkbox>
                                </Form.Item>
                                <Form.Item name="wait" valuePropName="checked">
                                  <Checkbox>Wait for resources</Checkbox>
                                </Form.Item>
                              </Space>
                            </>
                          ),
                        },
                      ]}
                    />
                  </Form>
                  <Card
                    className="soha-detail-card"
                    title={localeCode === 'zh_CN' ? '本次安装的 Values' : 'Installation values'}
                  >
                    {valuesState.serverChanged ? (
                      <Alert
                        type="warning"
                        showIcon
                        title={
                          localeCode === 'zh_CN'
                            ? '默认参数已变化，草稿已保留'
                            : 'Default values changed; draft retained'
                        }
                        action={
                          <Button onClick={valuesState.compareLatest}>
                            {localeCode === 'zh_CN' ? '比对最新' : 'Compare latest'}
                          </Button>
                        }
                      />
                    ) : null}
                    {valuesQuery.isPending ? <Spin /> : null}
                    <Input.TextArea
                      aria-label="Installation values YAML"
                      value={valuesDraft}
                      onChange={(e) => setValuesDraft(e.target.value)}
                      disabled={
                        !canInstall ||
                        platformWriteBlocked ||
                        installMutation.isPending ||
                        Boolean(valuesQuery.error) ||
                        !visibleValuesQueryData
                      }
                      rows={16}
                      style={{ fontFamily: 'ui-monospace,monospace' }}
                    />
                    <Button
                      disabled={installMutation.isPending || platformWriteBlocked}
                      onClick={valuesState.reset}
                    >
                      {localeCode === 'zh_CN' ? '恢复默认参数' : 'Reset defaults'}
                    </Button>
                  </Card>
                  {installTarget ? (
                    <Card size="small" title="Install progress">
                      <Space orientation="vertical" size={10} style={{ width: '100%' }}>
                        <Space wrap>
                          <Tag color={statusColor}>{statusLabel}</Tag>
                          <Text strong>{installTarget.releaseName}</Text>
                          <Text type="secondary">{installTarget.namespace}</Text>
                          <Text type="secondary">
                            {installElapsedSeconds}s / {installTarget.timeoutSeconds ?? 300}s
                          </Text>
                        </Space>
                        <Descriptions
                          bordered
                          column={1}
                          size="small"
                          items={[
                            {
                              key: 'submitted',
                              label: localeCode === 'zh_CN' ? '安装请求' : 'Install request',
                              children: (
                                <Tag
                                  color={
                                    installResult
                                      ? 'success'
                                      : installError
                                        ? 'error'
                                        : 'processing'
                                  }
                                >
                                  {installResult
                                    ? localeCode === 'zh_CN'
                                      ? '后端已返回结果'
                                      : 'Backend result received'
                                    : installError
                                      ? localeCode === 'zh_CN'
                                        ? '请求失败'
                                        : 'Request failed'
                                      : localeCode === 'zh_CN'
                                        ? '等待后端结果'
                                        : 'Awaiting backend result'}
                                </Tag>
                              ),
                            },
                            {
                              key: 'release',
                              label: 'Cluster release',
                              children: observedRelease ? (
                                <StatusTag value={observedRelease.status || 'detected'} />
                              ) : installInFlight ? (
                                <Spin size="small" />
                              ) : (
                                '-'
                              ),
                            },
                          ]}
                        />
                        {installError ? (
                          <Alert
                            type={installConflict ? 'warning' : 'error'}
                            showIcon
                            title={
                              installConflict ? 'Release name already in use' : 'Install failed'
                            }
                            description={formatHelmInstallError(
                              installError,
                              localeCode,
                              installTarget,
                            )}
                            action={
                              installConflict ? (
                                <Space>
                                  <Button
                                    size="small"
                                    onClick={() => {
                                      form.setFieldsValue({
                                        releaseName: retryHelmReleaseName(
                                          installTarget.releaseName,
                                          installTarget.chartName,
                                        ),
                                      })
                                      setInstallTarget(null)
                                      setInstallResult(null)
                                      setInstallError('')
                                      setInstallStartedAt(null)
                                    }}
                                  >
                                    Use new name
                                  </Button>
                                  <Button
                                    size="small"
                                    onClick={() => {
                                      onClose()
                                      navigate(
                                        '/helm/releases?' +
                                          new URLSearchParams({
                                            clusterId: clusterId || '',
                                            namespace: installTarget.namespace,
                                          }),
                                      )
                                    }}
                                  >
                                    View releases
                                  </Button>
                                </Space>
                              ) : undefined
                            }
                          />
                        ) : null}
                        {installResult ? (
                          <Space orientation="vertical" style={{ width: '100%' }}>
                            <Descriptions
                              bordered
                              column={1}
                              size="small"
                              items={[
                                {
                                  key: 'status',
                                  label: 'Status',
                                  children: <StatusTag value={installResult.status || 'unknown'} />,
                                },
                                {
                                  key: 'revision',
                                  label: 'Revision',
                                  children: installResult.revision || '-',
                                },
                                {
                                  key: 'chart',
                                  label: 'Chart',
                                  children: installResult.chart || installResult.chartName || '-',
                                },
                              ]}
                            />
                            <AdminTable
                              columns={resourceColumns}
                              dataSource={installResult.resources ?? []}
                              rowKey={(record) =>
                                `${record.apiVersion}:${record.kind}:${record.namespace}:${record.name}`
                              }
                              tableSize="small"
                              pagination={{ pageSize: 8, size: 'small', showSizeChanger: false }}
                              scroll={{ x: 720 }}
                            />
                            <Button
                              onClick={() => {
                                onClose()
                                navigate(
                                  buildHelmReleaseRoutePath(
                                    installResult.name,
                                    installResult.namespace,
                                    { clusterId },
                                  ),
                                )
                              }}
                            >
                              {localeCode === 'zh_CN' ? '查看 Release' : 'View release'}
                            </Button>
                            {installResult.notes ? (
                              <pre className="soha-helm-install-output">{installResult.notes}</pre>
                            ) : null}
                          </Space>
                        ) : null}
                      </Space>
                    </Card>
                  ) : null}
                </Space>
              ),
            },
          ]}
        />
      </Space>
    </Drawer>
  )
}
