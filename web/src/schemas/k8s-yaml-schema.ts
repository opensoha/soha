import type { JSONSchema } from 'monaco-yaml'

const metadataSchema: JSONSchema = {
  type: 'object',
  properties: {
    name: { type: 'string', description: 'Kubernetes resource name' },
    namespace: { type: 'string', description: 'Namespace of the resource' },
    labels: {
      type: 'object',
      additionalProperties: { type: 'string' },
    },
    annotations: {
      type: 'object',
      additionalProperties: { type: 'string' },
    },
  },
  required: ['name'],
}

const containerSchema: JSONSchema = {
  type: 'object',
  properties: {
    name: { type: 'string' },
    image: { type: 'string' },
    imagePullPolicy: { type: 'string', enum: ['Always', 'IfNotPresent', 'Never'] },
    command: { type: 'array', items: { type: 'string' } },
    args: { type: 'array', items: { type: 'string' } },
    env: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          name: { type: 'string' },
          value: { type: 'string' },
        },
        required: ['name'],
      },
    },
    ports: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          containerPort: { type: 'number' },
          protocol: { type: 'string', enum: ['TCP', 'UDP', 'SCTP'] },
        },
        required: ['containerPort'],
      },
    },
  },
  required: ['name', 'image'],
}

const podTemplateSchema: JSONSchema = {
  type: 'object',
  properties: {
    metadata: metadataSchema,
    spec: {
      type: 'object',
      properties: {
        serviceAccountName: { type: 'string' },
        nodeSelector: { type: 'object', additionalProperties: { type: 'string' } },
        containers: {
          type: 'array',
          items: containerSchema,
          minItems: 1,
        },
        initContainers: {
          type: 'array',
          items: containerSchema,
        },
      },
      required: ['containers'],
    },
  },
  required: ['spec'],
}

function workloadSchema(kind: string, apiVersion: string): JSONSchema {
  return {
    type: 'object',
    properties: {
      apiVersion: { const: apiVersion },
      kind: { const: kind },
      metadata: metadataSchema,
      spec: {
        type: 'object',
        properties: {
          replicas: { type: 'number' },
          selector: {
            type: 'object',
            properties: {
              matchLabels: { type: 'object', additionalProperties: { type: 'string' } },
            },
          },
          template: podTemplateSchema,
          serviceName: { type: 'string' },
          updateStrategy: { type: 'object' },
          strategy: { type: 'object' },
          schedule: { type: 'string' },
          suspend: { type: 'boolean' },
          jobTemplate: {
            type: 'object',
            properties: {
              spec: {
                type: 'object',
                properties: {
                  template: podTemplateSchema,
                },
              },
            },
          },
        },
      },
    },
    required: ['apiVersion', 'kind', 'metadata', 'spec'],
  }
}

const podSchema: JSONSchema = {
  type: 'object',
  properties: {
    apiVersion: { const: 'v1' },
    kind: { const: 'Pod' },
    metadata: metadataSchema,
    spec: podTemplateSchema.properties?.spec as JSONSchema,
  },
  required: ['apiVersion', 'kind', 'metadata', 'spec'],
}

export const k8sYamlSchema: JSONSchema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  title: 'Kubernetes Resource',
  description: 'Minimal Kubernetes schema hints for common workload resources.',
  oneOf: [
    podSchema,
    workloadSchema('Deployment', 'apps/v1'),
    workloadSchema('StatefulSet', 'apps/v1'),
    workloadSchema('DaemonSet', 'apps/v1'),
    workloadSchema('Job', 'batch/v1'),
    workloadSchema('CronJob', 'batch/v1'),
  ],
}
