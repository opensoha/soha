import { afterEach, describe, expect, it, vi } from 'vitest'
import { publishAuthSessionAvailable, subscribeAuthSessionAvailable } from './auth-session-channel'

class BroadcastChannelMock {
  static instances: BroadcastChannelMock[] = []

  readonly close = vi.fn()
  readonly postMessage = vi.fn()
  listener?: (event: MessageEvent<{ type: 'authenticated' }>) => void

  constructor(readonly name: string) {
    BroadcastChannelMock.instances.push(this)
  }

  addEventListener(
    _type: string,
    listener: (event: MessageEvent<{ type: 'authenticated' }>) => void,
  ) {
    this.listener = listener
  }

  removeEventListener() {
    this.listener = undefined
  }
}

describe('auth session channel', () => {
  afterEach(() => {
    BroadcastChannelMock.instances = []
    vi.unstubAllGlobals()
  })

  it('signals another webview without copying credentials', () => {
    vi.stubGlobal('BroadcastChannel', BroadcastChannelMock)
    const callback = vi.fn()
    const unsubscribe = subscribeAuthSessionAvailable(callback)

    BroadcastChannelMock.instances[0]?.listener?.(
      new MessageEvent('message', { data: { type: 'authenticated' } }),
    )
    expect(callback).toHaveBeenCalledOnce()

    publishAuthSessionAvailable()
    expect(BroadcastChannelMock.instances[1]?.postMessage).toHaveBeenCalledWith({
      type: 'authenticated',
    })
    expect(BroadcastChannelMock.instances[1]?.close).toHaveBeenCalledOnce()

    unsubscribe()
    expect(BroadcastChannelMock.instances[0]?.close).toHaveBeenCalledOnce()
  })
})
