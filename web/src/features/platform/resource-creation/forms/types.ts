import type { ReactNode } from 'react'
import type { WorkloadSnapshotInheritance } from '../types'

export const DEFAULT_WORKLOAD_SNAPSHOT_INHERITANCE = [
  'environment',
  'storage',
  'resources',
  'securityContext',
  'scheduling',
] satisfies WorkloadSnapshotInheritance[]

export type ResourceFormKind =
  | 'Deployment'
  | 'StatefulSet'
  | 'DaemonSet'
  | 'Job'
  | 'CronJob'
  | 'Service'
  | 'Ingress'
  | 'ConfigMap'
  | 'Secret'
  | 'PersistentVolumeClaim'
  | 'Namespace'
  | 'ServiceAccount'

export type ResourceScopeMode = 'namespace' | 'cluster'

export interface KeyValueEntry {
  key: string
  value: string
}

export interface ResourceFormContext {
  clusterId?: string | null
  namespace?: string | null
}

export interface PreparedResourceManifest {
  content: string
  expectedApiVersion: string
  expectedKind: string
  resourceGroup: string
}

export type KubernetesManifest = {
  apiVersion: string
  kind: ResourceFormKind
  metadata: {
    name: string
    namespace?: string
    labels?: Record<string, string>
    annotations?: Record<string, string>
  }
  [key: string]: unknown
}

export interface ResourceFormRendererProps<Values> {
  clusterId?: string
  identityDisabled?: boolean
  localeCode?: string
  disabled?: boolean
  loading?: boolean
  namespaceLoading?: boolean
  namespaceOptions?: readonly string[]
  onChange: (value: Values) => void
  onSubmit?: (value: Values) => void
  submitText?: ReactNode
  value: Values
}

export interface ResourceFormDefinition {
  apiVersion: string
  buildManifest: (values: unknown) => KubernetesManifest
  defaultValues: (context: ResourceFormContext) => unknown
  kind: ResourceFormKind
  label: string
  prepareManifest?: (
    values: unknown,
    context: ResourceFormContext,
  ) => Promise<PreparedResourceManifest>
  renderForm: (props: ResourceFormRendererProps<unknown>) => ReactNode
  scopeMode: ResourceScopeMode
}

export function defineResourceForm<Values>(
  definition: Omit<
    ResourceFormDefinition,
    'buildManifest' | 'defaultValues' | 'prepareManifest' | 'renderForm'
  > & {
    buildManifest: (values: Values) => KubernetesManifest
    defaultValues: (context: ResourceFormContext) => Values
    prepareManifest?: (
      values: Values,
      context: ResourceFormContext,
    ) => Promise<PreparedResourceManifest>
    renderForm: (props: ResourceFormRendererProps<Values>) => ReactNode
  },
): ResourceFormDefinition {
  return {
    ...definition,
    buildManifest: (values) => definition.buildManifest(values as Values),
    defaultValues: definition.defaultValues,
    prepareManifest: definition.prepareManifest
      ? (values, context) => definition.prepareManifest!(values as Values, context)
      : undefined,
    renderForm: (props) =>
      definition.renderForm({ ...props, value: props.value as Values, onChange: props.onChange }),
  }
}

export interface MetadataFormValues {
  annotations?: KeyValueEntry[]
  labels?: KeyValueEntry[]
  name: string
  namespace?: string
}

export interface ContainerFormValues {
  containerName: string
  containerPort?: number
  cpuLimit?: string
  cpuRequest?: string
  env?: KeyValueEntry[]
  image: string
  memoryLimit?: string
  memoryRequest?: string
}

export interface PodTemplateFormValues extends ContainerFormValues {
  nodeSelector?: KeyValueEntry[]
  serviceAccountName?: string
  volumeClaimName?: string
  volumeMountPath?: string
}

export interface WorkloadFormValues extends MetadataFormValues, PodTemplateFormValues {
  replicas?: number
  serviceName?: string
}

export interface JobFormValues extends MetadataFormValues, PodTemplateFormValues {
  activeDeadlineSeconds?: number
  argsText?: string
  backoffLimit?: number
  commandText?: string
  completions?: number
  description?: string
  imagePolicy?: 'snapshot' | 'follow'
  inherit?: WorkloadSnapshotInheritance[]
  parallelism?: number
  restartPolicy: 'Never' | 'OnFailure'
  runtimeSource: 'manual' | 'workload'
  schedule?: string
  sourceContainer?: string
  sourceKind: 'Deployment' | 'StatefulSet' | 'DaemonSet'
  sourceName?: string
  suspend?: boolean
}

export interface ServicePortFormValue {
  name?: string
  nodePort?: number
  port: number
  protocol: 'TCP' | 'UDP' | 'SCTP'
  targetPort: number | string
}

export interface ServiceFormValues extends MetadataFormValues {
  externalName?: string
  ports: ServicePortFormValue[]
  selector?: KeyValueEntry[]
  type: 'ClusterIP' | 'NodePort' | 'LoadBalancer' | 'ExternalName'
}

export interface IngressPathFormValue {
  path: string
  serviceName: string
  servicePort: number
}

export interface IngressFormValues extends MetadataFormValues {
  host?: string
  ingressClassName?: string
  paths: IngressPathFormValue[]
  tlsSecretName?: string
}

export interface ConfigMapFormValues extends MetadataFormValues {
  data?: KeyValueEntry[]
}

export interface SecretFormValues extends MetadataFormValues {
  data?: KeyValueEntry[]
  immutable?: boolean
  type: string
}

export interface PersistentVolumeClaimFormValues extends MetadataFormValues {
  accessModes: Array<'ReadWriteOnce' | 'ReadOnlyMany' | 'ReadWriteMany' | 'ReadWriteOncePod'>
  storage: string
  storageClassName?: string
  volumeMode: 'Filesystem' | 'Block'
}

export type NamespaceFormValues = Omit<MetadataFormValues, 'namespace'>

export interface ServiceAccountFormValues extends MetadataFormValues {
  automountServiceAccountToken?: boolean
  imagePullSecrets?: string[]
}
