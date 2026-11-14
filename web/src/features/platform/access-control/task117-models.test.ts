import { describe, expect, it } from 'vitest'
import { stringify } from 'yaml'
import { roleConfiguration } from './roles/detail-model'
import { clusterRoleConfiguration } from './clusterroles/detail-model'
import { bindingConfiguration as roleBindingConfiguration } from './rolebindings/detail-model'
import { bindingConfiguration as clusterBindingConfiguration } from './clusterrolebindings/detail-model'
import { saConfiguration } from './serviceaccounts/detail-model'

const name = 'synthetic-access'
const namespace = 'monitoring'
const rules = [
  {
    apiGroups: [''],
    resources: ['pods/log', '*'],
    verbs: ['get', '*'],
    resourceNames: ['api:blue'],
  },
  { nonResourceURLs: ['/healthz/*'], verbs: ['get'] },
]
const subjects = [
  { kind: 'User', name: 'oidc:team/alice@example.test', apiGroup: 'rbac.authorization.k8s.io' },
  {
    kind: 'Group',
    name: 'system:serviceaccounts:monitoring',
    apiGroup: 'rbac.authorization.k8s.io',
  },
  { kind: 'ServiceAccount', name: 'api', namespace },
]
const roleRef = { kind: 'ClusterRole', name: 'reader', apiGroup: 'rbac.authorization.k8s.io' }
function document(kind: string) {
  return {
    apiVersion: kind === 'ServiceAccount' ? 'v1' : 'rbac.authorization.k8s.io/v1',
    kind,
    metadata: { name, ...(!kind.startsWith('Cluster') ? { namespace } : {}) },
    ...(kind.endsWith('Binding') ? { roleRef, subjects } : { rules }),
  }
}
const models = [
  { kind: 'Role', parse: (content: string) => roleConfiguration(content, name, namespace) },
  { kind: 'ClusterRole', parse: (content: string) => clusterRoleConfiguration(content, name) },
  {
    kind: 'RoleBinding',
    parse: (content: string) => roleBindingConfiguration(content, name, namespace),
  },
  {
    kind: 'ClusterRoleBinding',
    parse: (content: string) => clusterBindingConfiguration(content, name),
  },
  { kind: 'ServiceAccount', parse: (content: string) => saConfiguration(content, name, namespace) },
]

describe('task117 raw Kubernetes RBAC configuration', () => {
  it('preserves core groups, wildcards, subresources, resource names and non-resource URLs', () => {
    expect(roleConfiguration(stringify(document('Role')), name, namespace).rules).toEqual(rules)
    const selectors = [
      {
        matchLabels: { aggregate: 'true' },
        matchExpressions: [{ key: 'team', operator: 'In', values: ['ops'] }],
      },
    ]
    expect(
      clusterRoleConfiguration(
        stringify({
          ...document('ClusterRole'),
          aggregationRule: { clusterRoleSelectors: selectors },
        }),
        name,
      ),
    ).toEqual({ rules, selectors, aggregated: true })
  })

  it('preserves raw subject identities without splitting colons or slashes', () => {
    for (const parse of [roleBindingConfiguration, clusterBindingConfiguration]) {
      const kind = parse === roleBindingConfiguration ? 'RoleBinding' : 'ClusterRoleBinding'
      expect(parse(stringify(document(kind)), name, namespace)).toEqual({ roleRef, subjects })
    }
    const content = { ...document('RoleBinding'), roleRef: { ...roleRef, kind: 'Role' } }
    expect(roleBindingConfiguration(stringify(content), name, namespace).roleRef.kind).toBe('Role')
    expect(() =>
      clusterBindingConfiguration(
        stringify({ ...content, kind: 'ClusterRoleBinding', metadata: { name } }),
        name,
      ),
    ).toThrow(/reference/)
  })

  for (const model of models) {
    it(`${model.kind} rejects wrong name, kind, version and scope`, () => {
      const valid = document(model.kind)
      expect(() => model.parse(stringify(valid))).not.toThrow()
      for (const invalid of [
        { ...valid, metadata: { ...valid.metadata, name: 'another-resource' } },
        { ...valid, metadata: { ...valid.metadata, namespace: 'another-namespace' } },
        { ...valid, kind: 'ConfigMap' },
        { ...valid, apiVersion: 'invalid/v1' },
      ])
        expect(() => model.parse(stringify(invalid))).toThrow(/identity/i)
    })
  }

  it('all five models reject oversized UTF-8 documents, malformed YAML and alias amplification', () => {
    for (const model of models) {
      expect(() => model.parse('#' + '界'.repeat(1_400_000))).toThrow(/budget/i)
      expect(() => model.parse('metadata: [unterminated')).toThrow(/YAML/)
      const aliases =
        stringify(document(model.kind)) +
        '\na: &a [x, x, x, x, x, x, x, x, x, x]\nb: &b [*a, *a, *a, *a, *a, *a, *a, *a, *a, *a]\nc: [*b, *b, *b, *b, *b, *b, *b, *b, *b, *b]\n'
      expect(() => model.parse(aliases)).toThrow(/alias/i)
    }
  })

  it('rejects invalid rule/selector/subject/reference field types and string automount values', () => {
    expect(() =>
      roleConfiguration(
        stringify({ ...document('Role'), rules: [{ verbs: [true] }] }),
        name,
        namespace,
      ),
    ).toThrow()
    expect(() =>
      clusterRoleConfiguration(
        stringify({
          ...document('ClusterRole'),
          aggregationRule: { clusterRoleSelectors: [{ matchLabels: { team: true } }] },
        }),
        name,
      ),
    ).toThrow()
    for (const model of models.filter((model) => model.kind.endsWith('Binding'))) {
      for (const fields of [
        { subjects: {} },
        { subjects: [{ kind: 'User', name: true }] },
        { roleRef: { ...roleRef, apiGroup: '' } },
        { roleRef: { ...roleRef, name: ' ' } },
      ])
        expect(() => model.parse(stringify({ ...document(model.kind), ...fields }))).toThrow()
    }
    expect(
      saConfiguration(
        stringify({ ...document('ServiceAccount'), automountServiceAccountToken: false }),
        name,
        namespace,
      ),
    ).toEqual({ automount: false })
    expect(() =>
      saConfiguration(
        stringify({ ...document('ServiceAccount'), automountServiceAccountToken: 'false' }),
        name,
        namespace,
      ),
    ).toThrow(/automount/)
  })
})
