import { describe, expect, it } from 'vitest'
import { containerLabel, containerTone } from './container-status'
import type { PodContainerStatus } from './types'

const cases: Array<[Partial<PodContainerStatus>, string]> = [
  [{ state: 'running', ready: true }, 'success'],
  [{ state: 'running', ready: false }, 'warning'],
  [{ kind: 'init', state: 'running', ready: true }, 'warning'],
  [{ state: 'waiting', reason: 'ContainerCreating' }, 'warning'],
  [{ state: 'waiting', reason: 'CrashLoopBackOff' }, 'danger'],
  [{ state: 'waiting', reason: 'ImagePullBackOff' }, 'danger'],
  [{ state: 'waiting', reason: 'CreateContainerConfigError' }, 'danger'],
  [{ kind: 'init', state: 'terminated', exitCode: 0 }, 'success'],
  [{ kind: 'init', state: 'terminated', exitCode: 1 }, 'danger'],
  [{ kind: 'sidecar', state: 'terminated', exitCode: 0 }, 'warning'],
  [{ state: 'terminated', exitCode: 0 }, 'success'],
  [{ state: 'terminated', exitCode: 137 }, 'danger'],
  [{ state: 'terminated' }, 'unknown'],
  [{ state: 'unknown', ready: true }, 'unknown'],
]

describe('container status evidence', () => {
  it.each(cases)('maps %j to %s', (status, tone) => {
    expect(
      containerTone({ name: 'api', kind: 'container', ready: false, state: 'unknown', ...status }),
    ).toBe(tone)
  })

  it('distinguishes init completion from a restartable init exit', () => {
    const init: PodContainerStatus = {
      name: 'migrate',
      kind: 'init',
      ready: false,
      state: 'terminated',
      exitCode: 0,
    }
    expect(containerLabel(init, true)).toContain('初始化容器 migrate：正常完成')
    expect(containerLabel({ ...init, kind: 'sidecar' }, true)).toContain('等待重启')
    expect(containerLabel(init, false)).toContain('Init container migrate：Completed')
  })
})
