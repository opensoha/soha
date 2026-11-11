import { describe, expect, it } from 'vitest'
import { dockerKeys, dockerMutationKeys, normalizeDockerOperationListParams } from './keys'

describe('dockerKeys', () => {
  it('normalizes list filters exactly as the API serializer omits empty values', () => {
    expect(
      dockerKeys.hostList({
        search: '',
        status: 'online',
        hostId: '',
        page: 0,
        pageSize: 20,
      }),
    ).toEqual(['docker', 'hosts', 'list', { status: 'online', page: 0, pageSize: 20 }])
  })

  it('keeps detail and runtime keys under one normalized project hierarchy', () => {
    const detail = dockerKeys.projectDetail(' project-1 ')

    expect(detail).toEqual(['docker', 'projects', 'detail', 'project-1'])
    expect(dockerKeys.projectRuntimeLogs(' project-1 ', 'api', 200)).toEqual([
      ...detail,
      'runtime',
      'logs',
      { serviceName: 'api', tailLines: 200 },
    ])
    expect(dockerKeys.projectRuntimeVolumeFile('project-1', 'api', '/data', '/a.txt')).toEqual([
      ...detail,
      'runtime',
      'volume-file',
      { serviceName: 'api', target: '/data', path: '/a.txt', limitBytes: 262_144 },
    ])
  })

  it('uses the canonical service list key for project services and stable option prefixes', () => {
    expect(dockerKeys.projectServices(' project-1 ')).toEqual(
      dockerKeys.serviceList({ projectId: 'project-1', page: 1, pageSize: 100 }),
    )
    expect(dockerKeys.hostOptions()).toEqual(['docker', 'hosts', 'list', 'options'])
    expect(dockerKeys.serviceOptions()).toEqual(['docker', 'services', 'list', 'options'])
  })

  it('preserves false operation filters because the wire serializer sends them', () => {
    expect(
      normalizeDockerOperationListParams({
        operationKind: '',
        abnormal: false,
        pending: true,
      }),
    ).toEqual({ abnormal: false, pending: true })
    expect(dockerMutationKeys.project('deploy')).toEqual([
      'docker',
      'mutation',
      'project',
      'deploy',
    ])
  })
})
