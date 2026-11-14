import { describe, expect, it } from 'vitest'
import { releaseOwnedDiffModels } from './yaml-diff-model-lifecycle'

describe('owned YAML diff models', () => {
  it('detaches the live widget before releasing either model', () => {
    const order: string[] = []
    let attached = true
    const dispose = (name: string) => {
      if (attached) throw new Error('TextModel disposed while the DiffEditor is attached')
      order.push(name)
    }
    releaseOwnedDiffModels({
      getModel: () => ({
        original: { dispose: () => dispose('original') },
        modified: { dispose: () => dispose('modified') },
      }),
      setModel: () => {
        attached = false
        order.push('detach')
      },
    })
    expect(order).toEqual(['detach', 'original', 'modified'])
  })
  it('handles an editor without a model', () => {
    expect(() => releaseOwnedDiffModels({ getModel: () => null, setModel: () => {} })).not.toThrow()
  })
})
