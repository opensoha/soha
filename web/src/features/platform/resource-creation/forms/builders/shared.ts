import type {
  KeyValueEntry,
  KubernetesManifest,
  MetadataFormValues,
  PodTemplateFormValues,
  ResourceFormKind,
} from '../types'

export function entriesToRecord(entries?: KeyValueEntry[]): Record<string, string> | undefined {
  const result: Record<string, string> = {}
  for (const entry of entries ?? []) {
    const key = entry.key.trim()
    if (key) result[key] = entry.value
  }
  return Object.keys(result).length ? result : undefined
}

export function compactObject<T extends Record<string, unknown>>(value: T): T {
  return Object.fromEntries(
    Object.entries(value).filter(([, item]) => item !== undefined && item !== ''),
  ) as T
}

export function buildMetadata(values: MetadataFormValues, namespaced = true) {
  return compactObject({
    name: values.name.trim(),
    namespace: namespaced ? values.namespace?.trim() : undefined,
    labels: entriesToRecord(values.labels),
    annotations: entriesToRecord(values.annotations),
  })
}

export function appLabels(values: MetadataFormValues) {
  return {
    'app.kubernetes.io/name': values.name.trim(),
    ...entriesToRecord(values.labels),
  }
}

export function buildPodSpec(values: PodTemplateFormValues) {
  const resources = compactObject({
    requests: compactObject({
      cpu: values.cpuRequest?.trim(),
      memory: values.memoryRequest?.trim(),
    }),
    limits: compactObject({ cpu: values.cpuLimit?.trim(), memory: values.memoryLimit?.trim() }),
  })
  const hasResources = Object.values(resources).some((item) => Object.keys(item).length > 0)
  const hasVolume = Boolean(values.volumeClaimName?.trim() && values.volumeMountPath?.trim())

  return compactObject({
    serviceAccountName: values.serviceAccountName?.trim(),
    nodeSelector: entriesToRecord(values.nodeSelector),
    containers: [
      compactObject({
        name: values.containerName?.trim() || 'app',
        image: values.image?.trim() || '',
        ports: values.containerPort
          ? [{ name: 'http', containerPort: values.containerPort, protocol: 'TCP' }]
          : undefined,
        env: values.env
          ?.filter((entry) => entry.key.trim())
          .map((entry) => ({ name: entry.key.trim(), value: entry.value })),
        resources: hasResources ? resources : undefined,
        volumeMounts: hasVolume
          ? [{ name: 'data', mountPath: values.volumeMountPath?.trim() }]
          : undefined,
      }),
    ],
    volumes: hasVolume
      ? [{ name: 'data', persistentVolumeClaim: { claimName: values.volumeClaimName?.trim() } }]
      : undefined,
  })
}

export function manifest(
  apiVersion: string,
  kind: ResourceFormKind,
  metadata: KubernetesManifest['metadata'],
  body: Record<string, unknown> = {},
): KubernetesManifest {
  return { apiVersion, kind, metadata, ...body }
}
