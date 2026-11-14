/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { disposeLive2DHost, Live2DCubismRenderer } from './live2d-cubism-renderer'
import type { CompanionPackSelection } from './types'

const loadLive2DAssetBundle = vi.hoisted(() => vi.fn())

vi.mock('./live2d-assets', () => ({ loadLive2DAssetBundle }))

const pack: CompanionPackSelection = {
  pluginId: 'example.live2d',
  version: '1.0.0',
  manifest: {
    renderer: 'live2d-cubism',
    entryAsset: 'avatar.model3.json',
    assets: [
      {
        path: 'avatar.model3.json',
        kind: 'entry',
        contentType: 'application/json',
        sizeBytes: 1,
        sha256: '0'.repeat(64),
      },
    ],
    license: {
      name: 'Example',
      url: 'https://example.com/license',
      redistributionAllowed: true,
      commercialUseAllowed: true,
    },
  },
}

describe('Live2D Cubism renderer', () => {
  beforeEach(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    loadLive2DAssetBundle.mockReset()
    document.body.replaceChildren()
  })

  it('mounts a host runtime, forwards motions, and releases resources', async () => {
    const dispose = vi.fn()
    const destroy = vi.fn()
    const playMotion = vi.fn()
    const setMotion = vi.fn()
    const mount = vi.fn(async () => ({ destroy, playMotion, setMotion }))
    loadLive2DAssetBundle.mockResolvedValue({ dispose, entryUrl: 'blob:model' })
    vi.stubGlobal('__SOHA_LIVE2D_CUBISM_RUNTIME__', { mount })

    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => {
      root.render(<Live2DCubismRenderer pack={pack} state="idle" stateAnimation="Idle" />)
    })

    const host = container.querySelector<HTMLElement>('.soha-companion-live2d-host')!
    const canvas = host.querySelector('canvas')!
    expect(mount).toHaveBeenCalledWith({
      canvas,
      initialMotion: 'Idle',
      modelUrl: 'blob:model',
    })
    expect(host.dataset.runtime).toBe('ready')
    expect(host.querySelector('.soha-companion-orbit')).toBeNull()

    await act(async () => {
      root.render(
        <Live2DCubismRenderer
          interaction={{ animation: 'Tap', sequence: 1 }}
          pack={pack}
          state="thinking"
          stateAnimation="Think"
        />,
      )
    })
    expect(setMotion).toHaveBeenLastCalledWith('Think')
    expect(playMotion).toHaveBeenLastCalledWith('Tap')

    const loseContext = vi.fn()
    vi.spyOn(canvas, 'getContext').mockImplementation(
      () => ({ getExtension: () => ({ loseContext }) }) as unknown as WebGLRenderingContext,
    )
    await act(async () => root.unmount())
    expect(destroy).toHaveBeenCalledOnce()
    expect(dispose).toHaveBeenCalledOnce()
    expect(loseContext).toHaveBeenCalledOnce()
  })

  it('keeps the built-in fallback when no licensed runtime is installed', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<Live2DCubismRenderer pack={pack} state="idle" stateAnimation="Idle" />)
    })

    const host = container.querySelector<HTMLElement>('.soha-companion-live2d-host')!
    expect(host.dataset.runtime).toBe('unavailable')
    expect(host.querySelector('.soha-companion-orbit')).not.toBeNull()
    expect(loadLive2DAssetBundle).not.toHaveBeenCalled()
    await act(async () => root.unmount())
  })

  it('tears down the model and asset bundle when a runtime action fails', async () => {
    const dispose = vi.fn()
    const destroy = vi.fn()
    const playMotion = vi.fn().mockRejectedValue(new Error('motion failed'))
    loadLive2DAssetBundle.mockResolvedValue({ dispose, entryUrl: 'blob:model' })
    vi.stubGlobal('__SOHA_LIVE2D_CUBISM_RUNTIME__', {
      mount: vi.fn(async () => ({ destroy, playMotion })),
    })

    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    await act(async () => {
      root.render(<Live2DCubismRenderer pack={pack} state="idle" stateAnimation="Idle" />)
    })
    const host = container.querySelector<HTMLElement>('.soha-companion-live2d-host')!
    const canvas = host.querySelector('canvas')!
    const loseContext = vi.fn()
    vi.spyOn(canvas, 'getContext').mockImplementation(
      () => ({ getExtension: () => ({ loseContext }) }) as unknown as WebGLRenderingContext,
    )

    await act(async () => {
      root.render(
        <Live2DCubismRenderer
          interaction={{ animation: 'Broken', sequence: 1 }}
          pack={pack}
          state="idle"
          stateAnimation="Idle"
        />,
      )
      await Promise.resolve()
    })

    expect(host.dataset.runtime).toBe('error')
    expect(host.querySelector('.soha-companion-orbit')).not.toBeNull()
    expect(destroy).toHaveBeenCalledOnce()
    expect(dispose).toHaveBeenCalledOnce()
    expect(loseContext).toHaveBeenCalledOnce()
    await act(async () => root.unmount())
    expect(destroy).toHaveBeenCalledOnce()
    expect(dispose).toHaveBeenCalledOnce()
    expect(loseContext).toHaveBeenCalledOnce()
  })
})

describe('Live2D host teardown', () => {
  it('loses WebGL contexts and clears mounted canvases', () => {
    const host = document.createElement('div')
    const canvas = document.createElement('canvas')
    const loseContext = vi.fn()
    vi.spyOn(canvas, 'getContext').mockImplementation(
      () => ({ getExtension: () => ({ loseContext }) }) as unknown as WebGLRenderingContext,
    )
    host.append(canvas)

    disposeLive2DHost(host)

    expect(loseContext).toHaveBeenCalledOnce()
    expect(host.childElementCount).toBe(0)
  })
})
