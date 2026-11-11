import { useCallback, useEffect, useRef, useState } from 'react'
import { loadLive2DAssetBundle, type Live2DAssetBundle } from './live2d-assets'
import { getLive2DCubismRuntime, type Live2DCubismModel } from './live2d-runtime'
import type { CompanionInteractionMotion } from './motion'
import { SohaOrbit } from './soha-orbit'
import type { CompanionPackSelection, CompanionVisualState } from './types'

function disposeLive2DCanvas(canvas: HTMLCanvasElement) {
  const gl = (canvas.getContext('webgl2') ??
    canvas.getContext('webgl') ??
    canvas.getContext('experimental-webgl')) as WebGLRenderingContext | null
  const loseContext = gl?.getExtension('WEBGL_lose_context') as { loseContext: () => void } | null
  loseContext?.loseContext()
}

function destroyLive2DModel(model: Live2DCubismModel) {
  try {
    model.destroy()
  } catch {
    // Runtime teardown failures must not block Soha-owned resource cleanup.
  }
}

export function disposeLive2DHost(host: HTMLElement) {
  for (const canvas of host.querySelectorAll('canvas')) disposeLive2DCanvas(canvas)
  host.replaceChildren()
}

interface Live2DCubismRendererProps {
  interaction?: CompanionInteractionMotion
  pack: CompanionPackSelection
  state: CompanionVisualState
  stateAnimation?: string
}

export function Live2DCubismRenderer({
  interaction,
  pack,
  state,
  stateAnimation,
}: Live2DCubismRendererProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const canvasReleasedRef = useRef(false)
  const canvasUsedRef = useRef(false)
  const stateAnimationRef = useRef(stateAnimation)
  const bundleRef = useRef<Live2DAssetBundle | null>(null)
  const modelRef = useRef<Live2DCubismModel | null>(null)
  const [runtimeStatus, setRuntimeStatus] = useState<'error' | 'loading' | 'ready' | 'unavailable'>(
    'loading',
  )
  stateAnimationRef.current = stateAnimation
  const interactionAnimation = interaction?.animation
  const interactionSequence = interaction?.sequence
  const releaseCanvas = useCallback((canvas = canvasRef.current) => {
    if (!canvasUsedRef.current || canvasReleasedRef.current || !canvas) return
    canvasReleasedRef.current = true
    disposeLive2DCanvas(canvas)
  }, [])
  const failRuntime = useCallback(
    (model: Live2DCubismModel) => {
      if (modelRef.current !== model) return
      modelRef.current = null
      destroyLive2DModel(model)
      bundleRef.current?.dispose()
      bundleRef.current = null
      releaseCanvas()
      setRuntimeStatus('error')
    },
    [releaseCanvas],
  )

  useEffect(() => {
    const canvas = canvasRef.current
    const runtime = getLive2DCubismRuntime()
    if (!canvas || !runtime) {
      setRuntimeStatus('unavailable')
      return
    }
    let cancelled = false
    let bundle: Live2DAssetBundle | undefined
    let model: Live2DCubismModel | undefined
    const disposeBundle = () => {
      if (!bundle || bundleRef.current !== bundle) return
      bundleRef.current = null
      bundle.dispose()
    }
    setRuntimeStatus('loading')
    void loadLive2DAssetBundle(pack)
      .then(async (loadedBundle) => {
        bundle = loadedBundle
        if (cancelled) {
          loadedBundle.dispose()
          return
        }
        bundleRef.current = loadedBundle
        canvasUsedRef.current = true
        model = await runtime.mount({
          canvas,
          initialMotion: stateAnimationRef.current,
          modelUrl: loadedBundle.entryUrl,
        })
        if (cancelled) {
          destroyLive2DModel(model)
          return
        }
        modelRef.current = model
        setRuntimeStatus('ready')
      })
      .catch(() => {
        disposeBundle()
        if (!cancelled) setRuntimeStatus('error')
      })
    return () => {
      cancelled = true
      if (model && modelRef.current === model) {
        modelRef.current = null
        destroyLive2DModel(model)
      }
      disposeBundle()
    }
  }, [pack])

  useEffect(() => {
    const canvas = canvasRef.current
    return () => releaseCanvas(canvas)
  }, [releaseCanvas])

  useEffect(() => {
    const model = modelRef.current
    if (!model) return
    try {
      void Promise.resolve(model.setMotion?.(stateAnimation)).catch(() => {
        failRuntime(model)
      })
    } catch {
      failRuntime(model)
    }
  }, [failRuntime, stateAnimation])

  useEffect(() => {
    const model = modelRef.current
    if (!model || !interactionAnimation) return
    try {
      void Promise.resolve(model.playMotion?.(interactionAnimation)).catch(() => {
        failRuntime(model)
      })
    } catch {
      failRuntime(model)
    }
  }, [failRuntime, interactionAnimation, interactionSequence])

  return (
    <div
      className="soha-companion-live2d-host"
      data-interaction={interaction?.animation}
      data-motion={stateAnimation}
      data-runtime={runtimeStatus}
    >
      <canvas aria-hidden="true" className="soha-companion-live2d-canvas" ref={canvasRef} />
      {runtimeStatus === 'ready' ? null : (
        <SohaOrbit interaction={interaction} state={state} stateAnimation={stateAnimation} />
      )}
    </div>
  )
}
