import { describe, expect, it } from 'vitest'
import {
  mergeWorkloadQuickEditManifest,
  workloadContainerNames,
  workloadQuickEditValuesFromManifest,
} from './workload-quick-edit-model'

const deployment = {
  apiVersion: 'apps/v1',
  kind: 'Deployment',
  metadata: { name: 'api', namespace: 'prod', annotations: { owner: 'platform' } },
  spec: {
    replicas: 2,
    strategy: { type: 'RollingUpdate', rollingUpdate: { maxUnavailable: 0 } },
    template: {
      metadata: { labels: { app: 'api' } },
      spec: {
        serviceAccountName: 'api',
        nodeSelector: { pool: 'apps' },
        volumes: [{ name: 'config', configMap: { name: 'api' } }],
        containers: [
          {
            name: 'api',
            image: 'registry/api:v1',
            ports: [{ name: 'http', containerPort: 8080 }],
            env: [
              { name: 'MODE', value: 'prod' },
              { name: 'TOKEN', valueFrom: { secretKeyRef: { name: 'api', key: 'token' } } },
            ],
            resources: {
              requests: { cpu: '100m', memory: '128Mi', 'ephemeral-storage': '1Gi' },
              limits: { cpu: '500m', memory: '512Mi' },
            },
          },
          { name: 'sidecar', image: 'registry/sidecar:v1' },
        ],
      },
    },
  },
}

describe('workload quick edit model', () => {
  it('reads selectable containers and edits only safe fields', () => {
    expect(workloadContainerNames(deployment, 'deployments')).toEqual(['api', 'sidecar'])
    const values = workloadQuickEditValuesFromManifest(deployment, 'deployments')
    expect(values).toMatchObject({
      containerName: 'api',
      image: 'registry/api:v1',
      replicas: 2,
      cpuRequest: '100m',
      memoryLimit: '512Mi',
      env: [{ key: 'MODE', value: 'prod' }],
      nodeSelector: [{ key: 'pool', value: 'apps' }],
    })

    const edited = mergeWorkloadQuickEditManifest(deployment, 'deployments', {
      ...values,
      replicas: 4,
      image: 'registry/api:v2',
      cpuRequest: '250m',
      env: [{ key: 'MODE', value: 'canary' }],
      nodeSelector: [{ key: 'pool', value: 'canary' }],
    })

    expect(edited).toMatchObject({
      metadata: { annotations: { owner: 'platform' } },
      spec: {
        replicas: 4,
        strategy: deployment.spec.strategy,
        template: {
          spec: {
            serviceAccountName: 'api',
            nodeSelector: { pool: 'canary' },
            volumes: deployment.spec.template.spec.volumes,
            containers: [
              {
                name: 'api',
                image: 'registry/api:v2',
                ports: deployment.spec.template.spec.containers[0].ports,
                env: [
                  deployment.spec.template.spec.containers[0].env![1],
                  { name: 'MODE', value: 'canary' },
                ],
                resources: {
                  requests: {
                    cpu: '250m',
                    memory: '128Mi',
                    'ephemeral-storage': '1Gi',
                  },
                  limits: { cpu: '500m', memory: '512Mi' },
                },
              },
              deployment.spec.template.spec.containers[1],
            ],
          },
        },
      },
    })
  })

  it('updates CronJob scheduling policy without replacing the job template', () => {
    const cronJob = {
      apiVersion: 'batch/v1',
      kind: 'CronJob',
      spec: {
        schedule: '0 * * * *',
        concurrencyPolicy: 'Allow',
        successfulJobsHistoryLimit: 3,
        jobTemplate: {
          spec: {
            backoffLimit: 6,
            template: {
              spec: {
                restartPolicy: 'Never',
                containers: [{ name: 'job', image: 'registry/job:v1' }],
              },
            },
          },
        },
      },
    }
    const values = workloadQuickEditValuesFromManifest(cronJob, 'cronjobs')
    const edited = mergeWorkloadQuickEditManifest(cronJob, 'cronjobs', {
      ...values,
      schedule: '*/10 * * * *',
      suspend: true,
      concurrencyPolicy: 'Forbid',
      failedJobsHistoryLimit: 2,
    })

    expect(edited).toMatchObject({
      spec: {
        schedule: '*/10 * * * *',
        suspend: true,
        concurrencyPolicy: 'Forbid',
        successfulJobsHistoryLimit: 3,
        failedJobsHistoryLimit: 2,
        jobTemplate: cronJob.spec.jobTemplate,
      },
    })
  })
})
