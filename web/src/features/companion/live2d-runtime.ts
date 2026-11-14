export interface Live2DCubismModel {
  destroy: () => void
  playMotion?: (motion: string) => Promise<void> | void
  setMotion?: (motion: string | undefined) => Promise<void> | void
}

export interface Live2DCubismRuntime {
  /** A rejected mount must release every runtime resource it created before rejecting. */
  mount: (options: {
    canvas: HTMLCanvasElement
    initialMotion?: string
    modelUrl: string
  }) => Live2DCubismModel | Promise<Live2DCubismModel>
}

declare global {
  interface Window {
    __SOHA_LIVE2D_CUBISM_RUNTIME__?: Live2DCubismRuntime
  }
}

export function getLive2DCubismRuntime() {
  if (typeof window === 'undefined') return undefined
  const runtime = window.__SOHA_LIVE2D_CUBISM_RUNTIME__
  return runtime && typeof runtime.mount === 'function' ? runtime : undefined
}
