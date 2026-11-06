import { describe, it, expect } from 'vitest'
import {
  daemonSetState,
  daemonSetPodModel,
  daemonSetCurrentTemplate,
} from './daemonset-detail-model'
import { readDaemonSetConfiguration } from './daemonset-configuration'
import { readControllerRevision } from '../shared/controller-revision'
import type { DaemonSetDetail } from './types'
import type { RolloutHistory } from '@/types'
const yaml = `apiVersion: apps/v1
kind: DaemonSet
metadata: {name: agent, namespace: team, uid: uid, generation: 3}
spec:
 updateStrategy: {type: OnDelete}
 revisionHistoryLimit: 5
 template:
  spec:
   nodeSelector: {os: linux}
   tolerations: [{operator: Exists, effect: NoSchedule}]
   containers: [{name: agent, image: agent:v2}]
status: {observedGeneration: 3, numberMisscheduled: 0, numberUnavailable: 0}
`
const config = readDaemonSetConfiguration(yaml, 'agent', 'team')
const detail: DaemonSetDetail = {
  name: 'agent',
  namespace: 'team',
  createdAt: '',
  desiredNumber: 2,
  readyNumber: 2,
  availableNumber: 2,
  updatedNumber: 1,
}
const row = (name: string, revision: string, hash?: string, match?: boolean): RolloutHistory => ({
  name,
  namespace: 'team',
  revision,
  podRevisionHash: hash,
  matchesCurrentTemplate: match,
  replicas: 0,
  readyReplicas: 0,
})
describe('DaemonSet native state and revisions', () => {
  it('keeps OnDelete old Pods healthy and reports native node readiness separately', () => {
    expect(daemonSetState(detail, config)).toBe('healthy')
    expect(daemonSetState(detail, { ...config, strategy: 'RollingUpdate' })).toBe('progressing')
    expect(daemonSetState({ ...detail, readyNumber: 1 }, config)).toBe('degraded')
    expect(daemonSetState(detail, { ...config, misscheduled: 1 })).toBe('degraded')
    expect(daemonSetState({ ...detail, desiredNumber: 0 }, config)).toBe('no-targets')
    expect(daemonSetState(detail)).toBe('unknown')
    expect(daemonSetState(detail, { ...config, observedGeneration: 2 })).toBe('progressing')
  })
  it('never infers current template from greatest revision or generation', () => {
    expect(
      daemonSetCurrentTemplate([row('newest', '9', 'a', false), row('match', '3', 'b', true)])
        ?.name,
    ).toBe('match')
    expect(daemonSetCurrentTemplate([row('newest', '9')])).toBeUndefined()
  })
  it('maps observed short hashes only and preserves unmapped hashes without calling them revisions', () => {
    const d = {
      ...detail,
      pods: [
        {
          name: 'p',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          ageSeconds: 0,
          namespace: 'team',
          labels: { 'controller-revision-hash': 'abc' },
        },
        {
          name: 'q',
          phase: 'Running',
          readyContainers: '1/1',
          restarts: 0,
          ageSeconds: 0,
          namespace: 'team',
          labels: { 'controller-revision-hash': 'agent-xyz' },
        },
      ],
    } as DaemonSetDetail
    const owners = daemonSetPodModel(d, [
      row('agent-abc', '7', 'abc'),
      row('agent-xyz', '8'),
    ]).podOwners
    expect(owners.get('team/p')?.record?.revision).toBe('7')
    expect(owners.get('team/q')?.record?.revision).toBe('hash:agent-xyz')
  })
  it('validates YAML identity and reads scheduling and retained template data', () => {
    expect(config.nodeSelector).toEqual({ os: 'linux' })
    expect(config.containers[0].image).toBe('agent:v2')
    expect(() => readDaemonSetConfiguration(yaml, 'other', 'team')).toThrow(/identity/)
    const revision = row('agent-abc', '7')
    expect(
      readControllerRevision(
        'kind: ControllerRevision\nmetadata: {name: agent-abc, namespace: team}\nrevision: 7\ndata: {spec: {template: {spec: {containers: [{name: a, image: a:v1}]}}}}',
        revision,
      ).template,
    ).toContain('a:v1')
    expect(() => readControllerRevision('kind: DaemonSet', revision)).toThrow(/identity/)
  })
})
