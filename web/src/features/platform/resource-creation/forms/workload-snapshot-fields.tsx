import { ReadFeedback } from '@/components/request-feedback'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { workloadQueries } from '@/features/platform/workloads/shared/queries'
import type { WorkloadKind } from '@/features/platform/workloads/shared/types'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Alert, Checkbox, Col, Form, Input, Row, Segmented, Select } from 'antd'
import { useEffect, useMemo, useRef } from 'react'
import { resourceCreationQueries } from '../queries'
import type {
  WorkloadSnapshotInheritance,
  WorkloadSnapshotRequest,
  WorkloadSnapshotSourceKind,
} from '../types'
import { PodTemplateFields } from './field-sections'
import { DEFAULT_WORKLOAD_SNAPSHOT_INHERITANCE, type JobFormValues } from './types'

const { TextArea } = Input

const SOURCE_OPTIONS: Array<{
  kind: WorkloadSnapshotSourceKind
  permission: string
  workloadKind: WorkloadKind
}> = [
  {
    kind: 'Deployment',
    permission: 'platform.deployment.view',
    workloadKind: 'deployments',
  },
  {
    kind: 'StatefulSet',
    permission: 'platform.workloads.stateful-sets.view',
    workloadKind: 'statefulsets',
  },
  {
    kind: 'DaemonSet',
    permission: 'platform.workloads.daemon-sets.view',
    workloadKind: 'daemonsets',
  },
]

const INHERITANCE_OPTIONS: Array<{
  en: string
  value: WorkloadSnapshotInheritance
  zh: string
}> = [
  { value: 'environment', zh: '环境变量', en: 'Environment variables' },
  { value: 'storage', zh: '卷与挂载', en: 'Volumes and mounts' },
  { value: 'resources', zh: '资源请求与限制', en: 'Resource requests and limits' },
  { value: 'securityContext', zh: '安全上下文', en: 'Security context' },
  { value: 'scheduling', zh: '运行身份与调度', en: 'Identity and scheduling' },
  { value: 'initContainers', zh: '初始化容器', en: 'Init containers' },
  { value: 'templateMetadata', zh: '模板标签与注解', en: 'Template labels and annotations' },
  { value: 'serviceRuntime', zh: '端口、探针与生命周期', en: 'Ports, probes and lifecycle' },
]

interface SourceSummary {
  name: string
  namespace: string
}

export function WorkloadSnapshotFields({
  clusterId = '',
  kind,
  localeCode,
}: {
  clusterId?: string
  kind: 'Job' | 'CronJob'
  localeCode?: string
}) {
  const isChinese = localeCode === 'zh_CN'
  const form = Form.useFormInstance<JobFormValues>()
  const runtimeSource = Form.useWatch('runtimeSource', form) ?? 'manual'
  const namespace = Form.useWatch('namespace', form)
  const imagePolicy = Form.useWatch('imagePolicy', form) ?? 'snapshot'
  const sourceKind = Form.useWatch('sourceKind', form) ?? 'Deployment'
  const sourceName = Form.useWatch('sourceName', form)
  const sourceContainer = Form.useWatch('sourceContainer', form)
  const inherit = Form.useWatch('inherit', form) ?? DEFAULT_WORKLOAD_SNAPSHOT_INHERITANCE
  const sourceScope = `${namespace?.trim() || ''}/${sourceKind}`
  const previousSourceScope = useRef(sourceScope)
  const previousSourceName = useRef(sourceName)
  const permissionQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionQuery.data?.data
  const sourceOption =
    SOURCE_OPTIONS.find((option) => option.kind === sourceKind) ?? SOURCE_OPTIONS[0]
  const canViewSource = hasPermission(permissionSnapshot, sourceOption.permission)
  const canUseWorkload = SOURCE_OPTIONS.some((option) =>
    hasPermission(permissionSnapshot, option.permission),
  )
  const canFollowSource = hasPermission(
    permissionSnapshot,
    'platform.extensions.custom-resources.create',
  )
  const scope = toScopeKey(clusterId, namespace)
  const sourceListOptions = workloadQueries.list<SourceSummary>(sourceOption.workloadKind, scope)
  const sourceListQuery = useQuery({
    ...sourceListOptions,
    enabled:
      runtimeSource === 'workload' &&
      canViewSource &&
      Boolean(clusterId.trim() && namespace?.trim()),
  })
  const visibleSourceListQueryData =
    runtimeSource === 'workload' && canViewSource && Boolean(clusterId.trim() && namespace?.trim())
      ? readableQueryData(sourceListQuery)
      : undefined
  const previewRequest = useMemo<WorkloadSnapshotRequest | undefined>(() => {
    if (runtimeSource !== 'workload' || !namespace?.trim() || !sourceName?.trim()) return undefined
    return {
      namespace: namespace.trim(),
      sourceKind,
      sourceName: sourceName.trim(),
      targetKind: kind === 'CronJob' && imagePolicy === 'follow' ? 'WorkloadCronJob' : kind,
      targetName: 'snapshot-preview',
      inherit: [...inherit],
      restartPolicy: 'Never',
      sourceContainer: sourceContainer?.trim() || undefined,
      ...(kind === 'CronJob' ? { schedule: '0 * * * *' } : {}),
    }
  }, [
    imagePolicy,
    inherit,
    kind,
    namespace,
    runtimeSource,
    sourceContainer,
    sourceKind,
    sourceName,
  ])
  const previewOptions = resourceCreationQueries.workloadSnapshot(clusterId, previewRequest)
  const previewQuery = useQuery({
    ...previewOptions,
    enabled: canViewSource && Boolean(clusterId.trim() && previewRequest),
  })
  const visiblePreviewQueryData =
    canViewSource && Boolean(clusterId.trim() && previewRequest)
      ? readableQueryData(previewQuery)
      : undefined
  const selectedImage = visiblePreviewQueryData?.containers.find(
    (container) => container.name === visiblePreviewQueryData?.selectedContainer,
  )?.image

  useEffect(() => {
    if (previousSourceScope.current === sourceScope) return
    previousSourceScope.current = sourceScope
    form.setFieldsValue({ sourceName: undefined, sourceContainer: undefined })
  }, [form, sourceScope])

  useEffect(() => {
    if (previousSourceName.current === sourceName) return
    previousSourceName.current = sourceName
    form.setFieldValue('sourceContainer', undefined)
  }, [form, sourceName])

  return (
    <>
      <ReadFeedback canRead={sourceListQuery.isEnabled} query={sourceListQuery} label="来源资源" />
      <ReadFeedback canRead={previewQuery.isEnabled} query={previewQuery} label="资源预览" />

      <Form.Item label={isChinese ? '运行环境来源' : 'Runtime source'} name="runtimeSource">
        <Segmented
          block
          options={[
            { label: isChinese ? '手动配置' : 'Manual', value: 'manual' },
            {
              disabled: !permissionQuery.isLoading && !canUseWorkload,
              label: isChinese ? '从工作负载生成' : 'From workload',
              value: 'workload',
            },
          ]}
        />
      </Form.Item>

      {runtimeSource === 'manual' ? (
        <PodTemplateFields />
      ) : (
        <>
          {!permissionQuery.isLoading && !canUseWorkload ? (
            <Alert
              showIcon
              title={
                isChinese
                  ? '当前账号没有可读取的工作负载权限'
                  : 'No readable workload source is available'
              }
              type="warning"
            />
          ) : null}
          {kind === 'CronJob' ? (
            <Form.Item label={isChinese ? '镜像策略' : 'Image policy'} name="imagePolicy">
              <Segmented
                block
                options={[
                  { label: isChinese ? '创建时快照' : 'Snapshot', value: 'snapshot' },
                  {
                    disabled: !permissionQuery.isLoading && !canFollowSource,
                    label: isChinese ? '跟随来源镜像' : 'Follow source image',
                    value: 'follow',
                  },
                ]}
              />
            </Form.Item>
          ) : null}
          <Row gutter={16}>
            <Col md={12} xs={24}>
              <Form.Item
                label={isChinese ? '来源类型' : 'Source kind'}
                name="sourceKind"
                rules={[{ required: true }]}
              >
                <Select
                  options={SOURCE_OPTIONS.map((option) => ({
                    disabled:
                      !permissionQuery.isLoading &&
                      !hasPermission(permissionSnapshot, option.permission),
                    label: option.kind,
                    value: option.kind,
                  }))}
                />
              </Form.Item>
            </Col>
            <Col md={12} xs={24}>
              <Form.Item
                label={isChinese ? '来源工作负载' : 'Source workload'}
                name="sourceName"
                rules={[
                  {
                    required: true,
                    message: isChinese ? '请选择来源工作负载' : 'Select a source workload',
                  },
                ]}
              >
                <Select
                  loading={sourceListQuery.isLoading}
                  options={(visibleSourceListQueryData ?? []).map((item) => ({
                    label: item.name,
                    value: item.name,
                  }))}
                  showSearch={{ optionFilterProp: 'label' }}
                />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item label={isChinese ? '运行容器' : 'Runtime container'} name="sourceContainer">
            <Select
              allowClear
              loading={previewQuery.isLoading}
              options={(visiblePreviewQueryData?.containers ?? []).map((container) => ({
                label: `${container.name} · ${container.image}`,
                value: container.name,
              }))}
              placeholder={
                visiblePreviewQueryData?.selectedContainer
                  ? isChinese
                    ? `默认使用 ${visiblePreviewQueryData.selectedContainer}`
                    : `Default: ${visiblePreviewQueryData.selectedContainer}`
                  : undefined
              }
            />
          </Form.Item>
          <Form.Item label={isChinese ? '继承运行配置' : 'Inherited runtime'} name="inherit">
            <Checkbox.Group
              className="soha-workload-snapshot-inheritance"
              options={INHERITANCE_OPTIONS.map((option) => ({
                label: isChinese ? option.zh : option.en,
                value: option.value,
              }))}
            />
          </Form.Item>
          {sourceListQuery.isError ? (
            <Alert showIcon title={sourceListQuery.error.message} type="error" />
          ) : null}
          {previewQuery.isError ? (
            <Alert showIcon title={previewQuery.error.message} type="error" />
          ) : null}
          {visiblePreviewQueryData ? (
            <Alert
              description={
                visiblePreviewQueryData.warnings.length
                  ? visiblePreviewQueryData.warnings.join('\n')
                  : undefined
              }
              showIcon
              title={`${visiblePreviewQueryData.selectedContainer}${selectedImage ? ` · ${selectedImage}` : ''}`}
              type={visiblePreviewQueryData.warnings.length ? 'warning' : 'success'}
            />
          ) : null}
          <Form.Item label={isChinese ? '备注' : 'Remark'} name="description">
            <TextArea maxLength={512} rows={2} />
          </Form.Item>
        </>
      )}

      <Row gutter={16}>
        <Col md={12} xs={24}>
          <Form.Item label={isChinese ? '命令' : 'Command'} name="commandText">
            <TextArea placeholder={'/bin/sh\n-c'} rows={3} />
          </Form.Item>
        </Col>
        <Col md={12} xs={24}>
          <Form.Item label={isChinese ? '参数' : 'Arguments'} name="argsText">
            <TextArea placeholder={'php artisan\ncleanup'} rows={3} />
          </Form.Item>
        </Col>
      </Row>
    </>
  )
}
