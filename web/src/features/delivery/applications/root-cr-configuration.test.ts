import { expect, it } from 'vitest'
import {
  rootCRConfiguration,
  rootCRControlPayload,
  rootCRPackageInput,
  validateRootCRDraft,
} from './root-cr-configuration'
import { rootWorkload, rootEnvironment, rootYAML } from './root-cr-fixtures.test-data'

it('copies only the pinned root spec into a CAS Manifest draft without runtime metadata', () => {
  const source = rootCRConfiguration(rootYAML, rootWorkload)
  expect(source.generation).toBe(2)
  expect(source.content).toContain('synthetic-secret')
  expect(source.content).not.toMatch(/status:|finalizers:|resourceVersion:|uid:/)
  const input = rootCRPackageInput(rootEnvironment, rootWorkload, source.content)
  expect(input.bindings[0].applicationEnvironmentId).toBe('env')
  expect(input.files[0].path).toBe('root-cr-uid-1.yaml')
})
it('keeps approvals, unrelated settings and root identity when explicitly configuring publication', () => {
  const input = rootCRControlPayload(rootEnvironment, rootWorkload, 'manifest-binding', 2, true)
  expect(input.expectedUpdatedAt).toBe(rootEnvironment.updatedAt)
  expect(input.releasePolicy?.requiresApproval).toBe(true)
  expect(input.targets[0]).toMatchObject({
    id: 'target',
    configRef: 'manifest-binding',
    executorKind: 'manifest_ssa',
    metadata: {
      ownershipMode: 'managed',
      rootCRGeneration: 2,
      resourceSource: JSON.stringify(rootWorkload.source),
    },
  })
  expect(
    rootCRControlPayload(rootEnvironment, rootWorkload, 'manifest-binding', 2, false).targets[0]
      .executorKind,
  ).toBe('observe_only')
  expect(rootEnvironment.targets?.[0].executorKind).toBe('observe_only')
})
it.each(['status', 'finalizers', 'uid', 'replacement'])(
  'rejects identity or runtime mutation: %s',
  (mode) => {
    const source = JSON.parse(rootYAML)
    if (mode === 'replacement') {
      source.metadata.uid = 'other'
      expect(() => rootCRConfiguration(JSON.stringify(source), rootWorkload)).toThrow()
      return
    }
    const draft = JSON.parse(
      JSON.stringify({
        apiVersion: source.apiVersion,
        kind: source.kind,
        metadata: { name: 'db', namespace: 'team' },
        spec: source.spec,
      }),
    )
    if (mode === 'status') draft.status = { ready: true }
    else draft.metadata[mode] = 'forbidden'
    expect(() => validateRootCRDraft(JSON.stringify(draft), rootWorkload)).toThrow()
  },
)
it('directs external ownership to its authoritative source', () => {
  const source = JSON.parse(rootYAML)
  source.metadata.labels = { 'app.kubernetes.io/managed-by': 'Helm' }
  expect(rootCRConfiguration(JSON.stringify(source), rootWorkload).blocked).toContain('权威来源')
})
