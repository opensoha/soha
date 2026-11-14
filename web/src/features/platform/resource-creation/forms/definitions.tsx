import { ServiceFields } from './service-fields'
import type { StepFormStep } from '@/components/step-form'
import { MinusCircleOutlined, PlusOutlined } from '@ant-design/icons'
import { Button, Col, Form, Input, InputNumber, Row, Select, Space } from 'antd'
import { generateWorkloadSnapshot } from '../api'
import { buildNamespaceManifest, buildServiceAccountManifest } from './builders/access-control'
import { buildConfigMapManifest, buildSecretManifest } from './builders/configuration'
import { buildIngressManifest, buildServiceManifest } from './builders/network'
import { buildPersistentVolumeClaimManifest } from './builders/storage'
import {
  buildControllerManifest,
  buildJobManifest,
  buildWorkloadSnapshotRequest,
} from './builders/workloads'
import {
  BooleanField,
  JobPolicyFields,
  KeyValueFields,
  MetadataFields,
  PodTemplateFields,
  WorkloadPolicyFields,
} from './field-sections'
import { ResourceStepForm } from './resource-step-form'
import {
  DEFAULT_WORKLOAD_SNAPSHOT_INHERITANCE,
  defineResourceForm,
  type ConfigMapFormValues,
  type IngressFormValues,
  type JobFormValues,
  type NamespaceFormValues,
  type PersistentVolumeClaimFormValues,
  type ResourceFormContext,
  type ResourceFormDefinition,
  type ResourceFormKind,
  type ResourceFormRendererProps,
  type SecretFormValues,
  type ServiceAccountFormValues,
  type ServiceFormValues,
  type WorkloadFormValues,
} from './types'
import { WorkloadSnapshotFields } from './workload-snapshot-fields'

function metadataDefaults(context: ResourceFormContext) {
  return {
    name: '',
    namespace: context.namespace || undefined,
    labels: [],
    annotations: [],
  }
}

function NamespacedMetadataFields<Values>({
  identityDisabled,
  namespaceLoading,
  namespaceOptions,
}: Pick<
  ResourceFormRendererProps<Values>,
  'identityDisabled' | 'namespaceLoading' | 'namespaceOptions'
>) {
  return (
    <MetadataFields
      identityDisabled={identityDisabled}
      namespaceLoading={namespaceLoading}
      namespaceOptions={namespaceOptions}
    />
  )
}

function podDefaults() {
  return {
    containerName: 'app',
    image: '',
    env: [],
    nodeSelector: [],
  }
}

function renderWithSteps<Values extends object>(
  props: ResourceFormRendererProps<Values>,
  steps: StepFormStep[],
) {
  return <ResourceStepForm {...props} steps={steps} />
}

function workloadDefinition(kind: 'Deployment' | 'StatefulSet' | 'DaemonSet') {
  return defineResourceForm<WorkloadFormValues>({
    apiVersion: 'apps/v1',
    kind,
    label: kind,
    scopeMode: 'namespace',
    defaultValues: (context) => ({
      ...metadataDefaults(context),
      ...podDefaults(),
      replicas: kind === 'DaemonSet' ? undefined : 1,
    }),
    buildManifest: (values) => buildControllerManifest(kind, values),
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name', 'namespace'],
          children: <NamespacedMetadataFields {...props} />,
        },
        {
          title: '工作负载',
          fieldNames: kind === 'DaemonSet' ? [] : ['replicas'],
          children: <WorkloadPolicyFields kind={kind} />,
        },
        {
          title: 'Pod 模板',
          fieldNames: ['containerName', 'image'],
          children: <PodTemplateFields />,
        },
      ]),
  })
}

function jobDefinition(kind: 'Job' | 'CronJob') {
  return defineResourceForm<JobFormValues>({
    apiVersion: 'batch/v1',
    kind,
    label: kind,
    scopeMode: 'namespace',
    defaultValues: (context) => ({
      ...metadataDefaults(context),
      ...podDefaults(),
      backoffLimit: 6,
      parallelism: 1,
      completions: 1,
      imagePolicy: 'snapshot',
      inherit: [...DEFAULT_WORKLOAD_SNAPSHOT_INHERITANCE],
      restartPolicy: 'Never',
      runtimeSource: 'manual',
      schedule: kind === 'CronJob' ? '0 * * * *' : undefined,
      sourceKind: 'Deployment',
      suspend: false,
    }),
    buildManifest: (values) => buildJobManifest(kind, values),
    prepareManifest: async (values, context) => {
      const followsSource = kind === 'CronJob' && values.imagePolicy === 'follow'
      if (values.runtimeSource !== 'workload') {
        return {
          content: JSON.stringify(buildJobManifest(kind, values), null, 2),
          expectedApiVersion: 'batch/v1',
          expectedKind: kind,
          resourceGroup: 'workloads',
        }
      }
      const clusterId = context.clusterId?.trim()
      if (!clusterId) throw new Error('A cluster is required for workload snapshots')
      const snapshot = await generateWorkloadSnapshot(
        clusterId,
        buildWorkloadSnapshotRequest(kind, values),
      )
      return {
        content: snapshot.content,
        expectedApiVersion: followsSource ? 'workloads.soha.io/v1alpha1' : 'batch/v1',
        expectedKind: followsSource ? 'WorkloadCronJob' : kind,
        resourceGroup: followsSource ? 'extensions' : 'workloads',
      }
    },
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name', 'namespace'],
          children: <NamespacedMetadataFields {...props} />,
        },
        {
          title: kind === 'CronJob' ? '调度与策略' : '执行策略',
          fieldNames: kind === 'CronJob' ? ['schedule', 'restartPolicy'] : ['restartPolicy'],
          children: <JobPolicyFields cron={kind === 'CronJob'} />,
        },
        {
          title: '运行环境',
          fieldNames: [
            'runtimeSource',
            'imagePolicy',
            'containerName',
            'image',
            'inherit',
            'sourceKind',
            'sourceName',
          ],
          children: (
            <WorkloadSnapshotFields
              clusterId={props.clusterId}
              kind={kind}
              localeCode={props.localeCode}
            />
          ),
        },
      ]),
  })
}

function serviceDefinition() {
  return defineResourceForm<ServiceFormValues>({
    apiVersion: 'v1',
    kind: 'Service',
    label: 'Service',
    scopeMode: 'namespace',
    defaultValues: (context) => ({
      ...metadataDefaults(context),
      type: 'ClusterIP',
      selector: [{ key: 'app.kubernetes.io/name', value: '' }],
      ports: [{ name: 'http', port: 80, targetPort: 8080, protocol: 'TCP' }],
    }),
    buildManifest: buildServiceManifest,
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name', 'namespace'],
          children: <NamespacedMetadataFields {...props} />,
        },
        {
          title: '服务配置',
          fieldNames: ['type', 'externalName', 'ports'],
          children: <ServiceFields clusterId={props.clusterId} />,
        },
      ]),
  })
}

function IngressPathFields() {
  return (
    <Form.List name="paths">
      {(fields, { add, remove }) => (
        <Form.Item label="转发规则">
          <Space orientation="vertical" size={8} style={{ display: 'flex' }}>
            {fields.map((field) => (
              <Space key={field.key} wrap>
                <Form.Item name={[field.name, 'path']} noStyle>
                  <Input placeholder="/" />
                </Form.Item>
                <Form.Item name={[field.name, 'serviceName']} noStyle rules={[{ required: true }]}>
                  <Input placeholder="Service 名称" />
                </Form.Item>
                <Form.Item name={[field.name, 'servicePort']} noStyle rules={[{ required: true }]}>
                  <InputNumber min={1} max={65535} placeholder="端口" />
                </Form.Item>
                <Button icon={<MinusCircleOutlined />} onClick={() => remove(field.name)} />
              </Space>
            ))}
            <Button icon={<PlusOutlined />} onClick={() => add({ path: '/', servicePort: 80 })}>
              添加规则
            </Button>
          </Space>
        </Form.Item>
      )}
    </Form.List>
  )
}

function ingressDefinition() {
  return defineResourceForm<IngressFormValues>({
    apiVersion: 'networking.k8s.io/v1',
    kind: 'Ingress',
    label: 'Ingress',
    scopeMode: 'namespace',
    defaultValues: (context) => ({
      ...metadataDefaults(context),
      paths: [{ path: '/', serviceName: '', servicePort: 80 }],
    }),
    buildManifest: buildIngressManifest,
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name', 'namespace'],
          children: <NamespacedMetadataFields {...props} />,
        },
        {
          title: '路由规则',
          fieldNames: ['paths'],
          children: (
            <>
              <Row gutter={16}>
                <Col span={12}>
                  <Form.Item label="域名" name="host">
                    <Input />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item label="IngressClass" name="ingressClassName">
                    <Input />
                  </Form.Item>
                </Col>
              </Row>
              <Form.Item label="TLS Secret" name="tlsSecretName">
                <Input />
              </Form.Item>
              <IngressPathFields />
            </>
          ),
        },
      ]),
  })
}

function configMapDefinition() {
  return defineResourceForm<ConfigMapFormValues>({
    apiVersion: 'v1',
    kind: 'ConfigMap',
    label: 'ConfigMap',
    scopeMode: 'namespace',
    defaultValues: (context) => ({ ...metadataDefaults(context), data: [] }),
    buildManifest: buildConfigMapManifest,
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name', 'namespace'],
          children: <NamespacedMetadataFields {...props} />,
        },
        {
          title: '配置数据',
          fieldNames: ['data'],
          children: <KeyValueFields label="数据" name="data" />,
        },
      ]),
  })
}

function secretDefinition() {
  return defineResourceForm<SecretFormValues>({
    apiVersion: 'v1',
    kind: 'Secret',
    label: 'Secret',
    scopeMode: 'namespace',
    defaultValues: (context) => ({ ...metadataDefaults(context), type: 'Opaque', data: [] }),
    buildManifest: buildSecretManifest,
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name', 'namespace'],
          children: <NamespacedMetadataFields {...props} />,
        },
        {
          title: 'Secret 数据',
          fieldNames: ['type', 'data'],
          children: (
            <>
              <Form.Item label="类型" name="type" rules={[{ required: true }]}>
                <Input />
              </Form.Item>
              <BooleanField label="创建后不允许修改" name="immutable" />
              <KeyValueFields label="明文数据（提交时编码）" name="data" />
            </>
          ),
        },
      ]),
  })
}

function pvcDefinition() {
  return defineResourceForm<PersistentVolumeClaimFormValues>({
    apiVersion: 'v1',
    kind: 'PersistentVolumeClaim',
    label: 'PersistentVolumeClaim',
    scopeMode: 'namespace',
    defaultValues: (context) => ({
      ...metadataDefaults(context),
      accessModes: ['ReadWriteOnce'],
      storage: '1Gi',
      volumeMode: 'Filesystem',
    }),
    buildManifest: buildPersistentVolumeClaimManifest,
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name', 'namespace'],
          children: <NamespacedMetadataFields {...props} />,
        },
        {
          title: '存储配置',
          fieldNames: ['accessModes', 'storage'],
          children: (
            <>
              <Form.Item label="容量" name="storage" rules={[{ required: true }]}>
                <Input placeholder="10Gi" />
              </Form.Item>
              <Form.Item label="StorageClass" name="storageClassName">
                <Input />
              </Form.Item>
              <Form.Item label="访问模式" name="accessModes" rules={[{ required: true }]}>
                <Select
                  mode="multiple"
                  options={[
                    'ReadWriteOnce',
                    'ReadOnlyMany',
                    'ReadWriteMany',
                    'ReadWriteOncePod',
                  ].map((value) => ({ value }))}
                />
              </Form.Item>
              <Form.Item label="卷模式" name="volumeMode">
                <Select options={[{ value: 'Filesystem' }, { value: 'Block' }]} />
              </Form.Item>
            </>
          ),
        },
      ]),
  })
}

function namespaceDefinition() {
  return defineResourceForm<NamespaceFormValues>({
    apiVersion: 'v1',
    kind: 'Namespace',
    label: 'Namespace',
    scopeMode: 'cluster',
    defaultValues: () => ({ name: '', labels: [], annotations: [] }),
    buildManifest: buildNamespaceManifest,
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name'],
          children: <MetadataFields namespaced={false} />,
        },
        { title: '确认', children: <div>Namespace 是集群级资源，创建前将执行额外权限检查。</div> },
      ]),
  })
}

function serviceAccountDefinition() {
  return defineResourceForm<ServiceAccountFormValues>({
    apiVersion: 'v1',
    kind: 'ServiceAccount',
    label: 'ServiceAccount',
    scopeMode: 'namespace',
    defaultValues: (context) => ({
      ...metadataDefaults(context),
      automountServiceAccountToken: false,
      imagePullSecrets: [],
    }),
    buildManifest: buildServiceAccountManifest,
    renderForm: (props) =>
      renderWithSteps(props, [
        {
          title: '基本信息',
          fieldNames: ['name', 'namespace'],
          children: <NamespacedMetadataFields {...props} />,
        },
        {
          title: '账户配置',
          children: (
            <>
              <BooleanField label="自动挂载 API 凭据" name="automountServiceAccountToken" />
              <Form.Item label="镜像拉取 Secret" name="imagePullSecrets">
                <Select mode="tags" />
              </Form.Item>
            </>
          ),
        },
      ]),
  })
}

export const resourceFormDefinitions: Readonly<Record<ResourceFormKind, ResourceFormDefinition>> = {
  Deployment: workloadDefinition('Deployment'),
  StatefulSet: workloadDefinition('StatefulSet'),
  DaemonSet: workloadDefinition('DaemonSet'),
  Job: jobDefinition('Job'),
  CronJob: jobDefinition('CronJob'),
  Service: serviceDefinition(),
  Ingress: ingressDefinition(),
  ConfigMap: configMapDefinition(),
  Secret: secretDefinition(),
  PersistentVolumeClaim: pvcDefinition(),
  Namespace: namespaceDefinition(),
  ServiceAccount: serviceAccountDefinition(),
}

export function getResourceFormDefinition(kind: string): ResourceFormDefinition | undefined {
  return resourceFormDefinitions[kind as ResourceFormKind]
}
