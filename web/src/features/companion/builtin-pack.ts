import type { CompanionPackSelection } from './types'

export const BUILTIN_COMPANION_PLUGIN_ID = 'builtin.soha-companion'

export const builtinCompanionPack: CompanionPackSelection = {
  pluginId: BUILTIN_COMPANION_PLUGIN_ID,
  version: '1.0.0',
  manifest: {
    renderer: 'svg',
    entryAsset: 'builtin://soha-orbit',
    assets: [
      {
        path: 'builtin://soha-orbit',
        kind: 'entry',
        contentType: 'image/svg+xml',
        sizeBytes: 0,
        sha256: '0000000000000000000000000000000000000000000000000000000000000000',
      },
    ],
    animations: {
      idle: 'idle',
      listening: 'listening',
      thinking: 'thinking',
      speaking: 'speaking',
      error: 'error',
      dragging: 'dragging',
      hover: 'hover',
      tap: 'tap',
    },
    interactions: [
      { id: 'tap', action: 'tap', label: '轻触' },
      { id: 'pet', action: 'pet', animation: 'pet', label: '摸摸' },
      { id: 'greet', action: 'greet', label: '打招呼' },
    ],
    unlocks: [
      { id: 'mood.happy', kind: 'animation', level: 2, ref: 'happy' },
      { id: 'action.play', kind: 'interaction', level: 3, ref: 'play' },
    ],
    license: {
      name: 'Apache License 2.0',
      spdxId: 'Apache-2.0',
      url: 'https://www.apache.org/licenses/LICENSE-2.0',
      sourceUrl: 'https://github.com/opensoha/soha-web',
      redistributionAllowed: true,
      commercialUseAllowed: true,
    },
  },
}
