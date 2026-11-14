import { describe, expect, it } from 'vitest'
import {
  parseReleaseTargets,
  releaseTargetKey,
  releaseTargetsFromCandidates,
  summarizeReleaseTargets,
  manifestReleaseTargets,
} from './release-targets'
import type { ManifestPackage } from './manifests/types'

it('accepts Docker targets without a cluster and separates project identities', () => {
  const target = {
    id: 'docker-target',
    clusterId: '',
    namespace: '',
    targetKind: 'host_service',
    executorKind: 'docker_compose',
    workloadKind: 'ComposeProject',
    workloadName: 'project',
    docker: { hostId: 'host', projectId: 'project', imageMappings: { api: 'main' } },
    enabled: true,
  }
  expect(parseReleaseTargets(JSON.stringify([target]))[0]).toMatchObject(target)
  expect(releaseTargetKey(target)).not.toBe(
    releaseTargetKey({ ...target, docker: { ...target.docker, projectId: 'other' } }),
  )
  expect(() => parseReleaseTargets(JSON.stringify([{ ...target, clusterId: 'cluster' }]))).toThrow(
    '不能包含',
  )
  expect(() =>
    parseReleaseTargets(
      JSON.stringify([{ ...target, docker: { ...target.docker, imageMappings: {} } }]),
    ),
  ).toThrow('镜像映射')
})

describe('release target matrix', () => {
  it('binds only Manifest packages in the selected environment scope and preserves target IDs', () => {
    const item: ManifestPackage = {
      id: 'package-1',
      applicationId: 'app-1',
      serviceId: 'svc-1',
      name: 'Config',
      renderer: 'kustomize',
      status: 'published',
      currentRevision: 2,
      files: [],
      bindings: [
        {
          id: 'binding-1',
          applicationEnvironmentId: 'env-1',
          environmentKey: 'test',
          clusterId: 'c1',
          namespace: 'app',
        },
        {
          id: 'binding-2',
          applicationEnvironmentId: 'env-2',
          environmentKey: 'prod',
          clusterId: 'c2',
          namespace: 'prod',
        },
      ],
      createdAt: '',
      updatedAt: '',
    }
    const targets = manifestReleaseTargets([item], 'env-1', 'c1', 'app')
    expect(targets).toHaveLength(1)
    expect(targets[0]).toMatchObject({
      executorKind: 'manifest_ssa',
      configRef: 'binding-1',
      metadata: { serviceId: 'svc-1' },
    })
    expect(manifestReleaseTargets([item], 'env-1', 'c1', 'wrong')).toEqual([])
    const existing = { ...targets[0]!, id: 'stable-target' }
    expect(
      releaseTargetsFromCandidates([], [releaseTargetKey(existing)], [existing], targets),
    ).toEqual([existing])
    expect(parseReleaseTargets(JSON.stringify(targets))).toEqual(targets)
    expect(() => parseReleaseTargets(JSON.stringify([{ ...targets[0], configRef: '' }]))).toThrow(
      'Manifest 环境绑定',
    )
  })
  it('accepts typed YAML, Helm and Kustomize targets', () => {
    const targets = parseReleaseTargets(
      JSON.stringify([
        {
          clusterId: 'c1',
          namespace: 'app',
          targetKind: 'k8s_workload',
          workloadKind: 'Deployment',
          workloadName: 'api',
          configRef: 'deploy/api.yaml',
        },
        {
          clusterId: 'c1',
          namespace: 'app',
          targetKind: 'helm_release',
          workloadKind: 'Release',
          workloadName: 'web',
          metadata: { chartRef: 'charts/web', valuesRef: 'values/prod.yaml' },
        },
        {
          clusterId: 'c2',
          namespace: 'app',
          targetKind: 'kustomize_overlay',
          workloadKind: 'Kustomization',
          workloadName: 'worker',
          metadata: { basePath: 'deploy/base', overlayPath: 'deploy/overlays/prod' },
        },
      ]),
    )
    expect(targets).toHaveLength(3)
    expect(targets.every((target) => target.enabled)).toBe(true)
    expect(summarizeReleaseTargets(targets as never)).toBe(
      'k8s_workload 1 · helm_release 1 · kustomize_overlay 1',
    )
  })

  it('rejects targets missing deployment-specific configuration', () => {
    expect(() =>
      parseReleaseTargets(
        JSON.stringify([
          {
            clusterId: 'c1',
            namespace: 'app',
            targetKind: 'helm_release',
            workloadKind: 'Release',
            workloadName: 'web',
          },
        ]),
      ),
    ).toThrow('metadata.chartRef')
  })

  it('turns selected workload candidates into executable release targets', () => {
    const existing = {
      id: 'target-1',
      clusterId: 'c1',
      namespace: 'app',
      workloadKind: 'Deployment',
      workloadName: 'api',
      enabled: true,
    }
    const worker = {
      clusterId: 'c1',
      namespace: 'app',
      workloadKind: 'StatefulSet',
      workloadName: 'worker',
      desiredReplicas: 1,
      readyReplicas: 1,
      relatedResources: [],
    }

    expect(
      releaseTargetsFromCandidates(
        [worker],
        [releaseTargetKey(existing), releaseTargetKey(worker)],
        [existing],
      ),
    ).toEqual([
      existing,
      {
        clusterId: 'c1',
        namespace: 'app',
        targetKind: 'k8s_workload',
        executorKind: 'k8s_job_runner',
        workloadKind: 'StatefulSet',
        workloadName: 'worker',
        metadata: {},
        enabled: true,
      },
    ])
  })
})

it('keeps native Helm identity separate from a workload and accepts typed sources', () => {
  const target = {
    id: 'helm-target',
    clusterId: 'cluster',
    namespace: 'test',
    workloadKind: 'HelmRelease',
    workloadName: 'api',
    targetKind: 'helm_release',
    executorKind: 'helm_sdk',
    enabled: true,
    helm: {
      releaseName: 'api',
      source: {
        repositoryUrl: 'oci://registry.example.com/charts',
        chart: 'api',
        version: '1.0.0',
        values: {},
      },
    },
  }
  expect(parseReleaseTargets(JSON.stringify([target]))[0]).toMatchObject(target)
  expect(releaseTargetKey(target)).not.toBe(
    releaseTargetKey({ ...target, targetKind: 'k8s_workload', helm: undefined }),
  )
  expect(() =>
    parseReleaseTargets(
      JSON.stringify([
        { ...target, helm: { ...target.helm, source: { ...target.helm.source, version: '' } } },
      ]),
    ),
  ).toThrow('helm.source.version')
})
