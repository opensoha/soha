import { describe, expect, it, vi } from 'vitest'
import { loadLive2DAssetBundle } from './live2d-assets'
import type { CompanionPackSelection } from './types'

const sha256 = '0'.repeat(64)

function pack(model: unknown): {
  selection: CompanionPackSelection
  fetchAsset: ReturnType<typeof vi.fn>
} {
  const entryAsset = 'models/avatar.model3.json'
  const files = new Map<string, Blob>([
    [entryAsset, new Blob([JSON.stringify(model)], { type: 'application/json' })],
    ['models/avatar.moc3', new Blob(['moc'])],
    ['models/textures/face.png', new Blob(['texture'], { type: 'image/png' })],
    ['models/motions/idle.motion3.json', new Blob(['motion'])],
    ['models/sounds/idle.ogg', new Blob(['sound'], { type: 'audio/ogg' })],
    ['models/expressions/smile.exp3.json', new Blob(['expression'])],
  ])
  const fetchAsset = vi.fn(async (_pluginId: string, path: string) => {
    const blob = files.get(path)
    if (!blob) throw new Error(`missing ${path}`)
    return blob
  })
  return {
    fetchAsset,
    selection: {
      pluginId: 'example.live2d',
      version: '1.0.0',
      manifest: {
        renderer: 'live2d-cubism',
        entryAsset,
        assets: [...files].map(([path, blob]) => ({
          path,
          kind: path === entryAsset ? 'entry' : path.endsWith('.png') ? 'texture' : 'model',
          contentType:
            path === entryAsset
              ? 'application/json'
              : blob.type === 'image/png'
                ? 'image/png'
                : 'application/octet-stream',
          sizeBytes: blob.size,
          sha256,
        })),
        license: {
          name: 'Example',
          url: 'https://example.com/license',
          redistributionAllowed: true,
          commercialUseAllowed: true,
        },
      },
    },
  }
}

describe('loadLive2DAssetBundle', () => {
  it('loads declared Cubism assets and rewrites model references to object URLs', async () => {
    const { fetchAsset, selection } = pack({
      FileReferences: {
        Moc: 'avatar.moc3',
        Textures: ['textures/face.png'],
        Motions: {
          Idle: [{ File: 'motions/idle.motion3.json', Sound: 'sounds/idle.ogg' }],
        },
        Expressions: [{ Name: 'smile', File: 'expressions/smile.exp3.json' }],
      },
    })
    const created: Array<{ blob: Blob; url: string }> = []
    const createObjectURL = vi.fn((blob: Blob) => {
      const url = `blob:asset-${created.length}`
      created.push({ blob, url })
      return url
    })
    const revokeObjectURL = vi.fn()

    const bundle = await loadLive2DAssetBundle(selection, {
      createObjectURL,
      fetchAsset,
      revokeObjectURL,
    })

    expect(fetchAsset.mock.calls.map(([, path]) => path)).toEqual([
      'models/avatar.model3.json',
      'models/avatar.moc3',
      'models/textures/face.png',
      'models/motions/idle.motion3.json',
      'models/sounds/idle.ogg',
      'models/expressions/smile.exp3.json',
    ])
    const rewritten = JSON.parse(await created[created.length - 1].blob.text())
    expect(rewritten.FileReferences).toMatchObject({
      Moc: 'blob:asset-0',
      Textures: ['blob:asset-1'],
      Motions: { Idle: [{ File: 'blob:asset-2', Sound: 'blob:asset-3' }] },
      Expressions: [{ Name: 'smile', File: 'blob:asset-4' }],
    })
    expect(bundle.entryUrl).toBe('blob:asset-5')

    bundle.dispose()
    bundle.dispose()
    expect(revokeObjectURL.mock.calls.flat()).toEqual(created.map(({ url }) => url))
  })

  it.each(['https://example.com/avatar.moc3', '../avatar.moc3', 'missing.moc3'])(
    'rejects an unsafe or undeclared reference: %s',
    async (moc) => {
      const { fetchAsset, selection } = pack({
        FileReferences: { Moc: moc, Textures: [] },
      })

      await expect(loadLive2DAssetBundle(selection, { fetchAsset })).rejects.toThrow(/Live2D asset/)
      expect(fetchAsset).toHaveBeenCalledOnce()
    },
  )

  it('rejects duplicate asset paths before loading the package', async () => {
    const { fetchAsset, selection } = pack({
      FileReferences: { Moc: 'avatar.moc3', Textures: [] },
    })
    selection.manifest.assets.push(selection.manifest.assets[0])

    await expect(loadLive2DAssetBundle(selection, { fetchAsset })).rejects.toThrow(/duplicate/i)
    expect(fetchAsset).not.toHaveBeenCalled()
  })

  it('uses a local pack asset loader without requesting plugin assets', async () => {
    const { fetchAsset, selection } = pack({
      FileReferences: { Moc: 'avatar.moc3', Textures: [] },
    })
    selection.loadAsset = async (path) => {
      if (path === 'models/avatar.model3.json') {
        return new Blob([JSON.stringify({ FileReferences: { Moc: 'avatar.moc3', Textures: [] } })])
      }
      if (path === 'models/avatar.moc3') return new Blob(['moc'])
      throw new Error(`missing ${path}`)
    }

    const bundle = await loadLive2DAssetBundle(selection, {
      createObjectURL: () => 'blob:local',
      revokeObjectURL: () => undefined,
    })

    expect(bundle.entryUrl).toBe('blob:local')
    expect(fetchAsset).not.toHaveBeenCalled()
    bundle.dispose()
  })
})
