import { lazy, Suspense, useEffect, useState } from 'react'
import { fetchCompanionAsset } from './api'
import { BUILTIN_COMPANION_PLUGIN_ID } from './builtin-pack'
import { SohaOrbit } from './soha-orbit'
import { companionStateAnimation, type CompanionInteractionMotion } from './motion'
import type { CompanionPackSelection, CompanionVisualState } from './types'

const Live2DCubismRenderer = lazy(async () => {
  const module = await import('./live2d-cubism-renderer')
  return { default: module.Live2DCubismRenderer }
})

interface CompanionRendererProps {
  interaction?: CompanionInteractionMotion
  pack: CompanionPackSelection
  state: CompanionVisualState
}

function CompanionImage({ interaction, pack, state }: CompanionRendererProps) {
  const [assetUrl, setAssetUrl] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let currentUrl: string | null = null
    let cancelled = false
    setFailed(false)
    setAssetUrl(null)
    void fetchCompanionAsset(pack.pluginId, pack.manifest.entryAsset)
      .then((blob) => {
        if (cancelled) return
        currentUrl = URL.createObjectURL(blob)
        setAssetUrl(currentUrl)
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
      if (currentUrl) URL.revokeObjectURL(currentUrl)
    }
  }, [pack.manifest.entryAsset, pack.pluginId])

  const stateAnimation = companionStateAnimation(pack.manifest, state)
  if (failed || !assetUrl) {
    return <SohaOrbit interaction={interaction} state={state} stateAnimation={stateAnimation} />
  }
  return <img alt="" className="soha-companion-pack-image" draggable={false} src={assetUrl} />
}

export function CompanionRenderer({ interaction, pack, state }: CompanionRendererProps) {
  const stateAnimation = companionStateAnimation(pack.manifest, state)
  if (pack.pluginId === BUILTIN_COMPANION_PLUGIN_ID) {
    return <SohaOrbit interaction={interaction} state={state} stateAnimation={stateAnimation} />
  }
  if (pack.manifest.renderer === 'live2d-cubism') {
    return (
      <Suspense fallback={<SohaOrbit state={state} stateAnimation={stateAnimation} />}>
        <Live2DCubismRenderer
          interaction={interaction}
          key={`${pack.pluginId}:${pack.version}:${pack.manifest.entryAsset}`}
          pack={pack}
          state={state}
          stateAnimation={stateAnimation}
        />
      </Suspense>
    )
  }
  return <CompanionImage interaction={interaction} pack={pack} state={state} />
}
