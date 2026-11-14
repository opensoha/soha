import { afterEach, describe, expect, it } from 'vitest'
import {
  createLocalLive2DPack,
  LOCAL_LIVE2D_PLUGIN_ID,
  useLocalCompanionPackStore,
} from './local-live2d-pack'

function localFile(path: string, contents: BlobPart, type = 'application/octet-stream') {
  const pathParts = path.split('/')
  const file = new File([contents], pathParts[pathParts.length - 1], { type })
  Object.defineProperty(file, 'webkitRelativePath', { value: `avatar/${path}` })
  return file
}

const model = JSON.stringify({
  FileReferences: {
    Moc: 'avatar.moc3',
    Textures: ['textures/face.png'],
  },
})

afterEach(() => useLocalCompanionPackStore.getState().clear())

describe('local Live2D pack', () => {
  it('validates a local model directory and keeps its files in memory', async () => {
    const pack = await createLocalLive2DPack([
      localFile('avatar.model3.json', model, 'application/json'),
      localFile('avatar.moc3', 'moc'),
      localFile('textures/face.png', 'texture', 'image/png'),
    ])

    expect(pack).toMatchObject({
      local: true,
      pluginId: LOCAL_LIVE2D_PLUGIN_ID,
      manifest: {
        entryAsset: 'avatar.model3.json',
        renderer: 'live2d-cubism',
      },
    })
    expect(await pack.loadAsset?.('textures/face.png')).toBeInstanceOf(File)

    useLocalCompanionPackStore.getState().setPack(pack)
    expect(useLocalCompanionPackStore.getState().pack).toBe(pack)
  })

  it.each([
    {
      files: [localFile('avatar.model.json', '{}', 'application/json')],
      message: '.model3.json',
    },
    {
      files: [
        localFile('one.model3.json', model, 'application/json'),
        localFile('two.model3.json', model, 'application/json'),
      ],
      message: '多个模型入口',
    },
  ])('rejects an unsupported model directory', async ({ files, message }) => {
    await expect(createLocalLive2DPack(files)).rejects.toThrow(message)
  })

  it('selects the directory-named main model when variants are present', async () => {
    const pack = await createLocalLive2DPack([
      localFile('avatar.model3.json', model, 'application/json'),
      localFile('cosplay.model3.json', model, 'application/json'),
      localFile('avatar.moc3', 'moc'),
      localFile('textures/face.png', 'texture', 'image/png'),
    ])

    expect(pack.manifest.entryAsset).toBe('avatar.model3.json')
  })

  it('rejects model references that leave the selected directory', async () => {
    const unsafeModel = JSON.stringify({
      FileReferences: { Moc: '../avatar.moc3', Textures: [] },
    })

    await expect(
      createLocalLive2DPack([
        localFile('avatar.model3.json', unsafeModel, 'application/json'),
        localFile('avatar.moc3', 'moc'),
      ]),
    ).rejects.toThrow(/unsafe/i)
  })
})
