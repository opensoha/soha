import { describe, expect, it } from 'vitest'
import { accessControlKeys } from './keys'
import {
  buildAccessControlDetailPath,
  buildAccessControlDetailRoute,
  buildAccessControlListPath,
  buildAccessControlYAMLPath,
} from './paths'
import { parseAccessControlSubject, splitAccessControlRoleRef } from './relationships'
import { accessControlScopeMode, normalizeAccessControlScope } from './scope'

const namespacedScope = { clusterId: 'cluster-a', namespace: 'team/a' }

describe('access-control contracts', () => {
  it('normalizes namespace and cluster-scoped resource paths', () => {
    expect(accessControlScopeMode('roles')).toBe('namespace')
    expect(accessControlScopeMode('clusterroles')).toBe('cluster')
    expect(normalizeAccessControlScope('clusterroles', namespacedScope)).toEqual({
      clusterId: 'cluster-a',
      namespace: null,
    })
    expect(buildAccessControlListPath('roles', namespacedScope)).toBe(
      '/clusters/cluster-a/access-control/roles?namespace=team%2Fa',
    )
    expect(buildAccessControlListPath('clusterroles', namespacedScope)).toBe(
      '/clusters/cluster-a/access-control/clusterroles',
    )
  })

  it('encodes detail, YAML, and UI routes with canonical scope', () => {
    expect(buildAccessControlDetailPath('roles', namespacedScope, 'demo/reader')).toBe(
      '/clusters/cluster-a/access-control/roles/demo%2Freader/detail?namespace=team%2Fa',
    )
    expect(buildAccessControlYAMLPath('clusterroles', namespacedScope, 'view/all')).toBe(
      '/clusters/cluster-a/access-control/clusterroles/view%2Fall/yaml',
    )
    expect(buildAccessControlDetailRoute('roles', 'demo/reader', 'team/a')).toBe(
      '/platform-access-control/roles/demo%2Freader?namespace=team%2Fa',
    )
  })

  it('uses one key hierarchy and parses RBAC references', () => {
    expect(accessControlKeys.list('clusterroles', namespacedScope)).toEqual([
      'platform',
      'access-control',
      'clusterroles',
      'list',
      { clusterId: 'cluster-a', namespace: null },
    ])
    expect(accessControlKeys.yaml('roles', namespacedScope, 'reader')).toEqual([
      ...accessControlKeys.detail('roles', namespacedScope, 'reader'),
      'yaml',
    ])
    expect(accessControlKeys.accessReview(namespacedScope, ' builder ')).toEqual([
      ...accessControlKeys.resource('serviceaccounts'),
      'access-review',
      { clusterId: 'cluster-a', namespace: 'team/a' },
      'builder',
    ])
    expect(
      accessControlKeys.list('rolebindings', namespacedScope, {
        subjectKind: ' ServiceAccount ',
        subjectName: ' builder ',
        subjectNamespace: ' team/a ',
      }),
    ).toEqual([
      ...accessControlKeys.list('rolebindings', namespacedScope),
      { subjectKind: 'ServiceAccount', subjectName: 'builder', subjectNamespace: 'team/a' },
    ])
    expect(parseAccessControlSubject('ServiceAccount:team-a/builder')).toEqual({
      kind: 'ServiceAccount',
      label: 'ServiceAccount team-a/builder',
      name: 'builder',
      namespace: 'team-a',
    })
    expect(splitAccessControlRoleRef('ClusterRole/viewer')).toEqual({
      kind: 'ClusterRole',
      name: 'viewer',
    })
  })
})
