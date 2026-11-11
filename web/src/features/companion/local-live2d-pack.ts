import { assertSafeLive2DAssetPath, loadLive2DAssetBundle } from './live2d-assets'
import type { CompanionPackSelection, CompanionRenderManifest } from './types'
import { LOCAL_LIVE2D_PLUGIN_ID } from './local-live2d-store'

export { LOCAL_LIVE2D_PLUGIN_ID, useLocalCompanionPackStore } from './local-live2d-store'

const MAX_FILE_COUNT = 512
const MAX_FILE_SIZE = 64 * 1024 * 1024
const MAX_TOTAL_SIZE = 256 * 1024 * 1024

type CompanionAsset = CompanionRenderManifest['assets'][number]

function localDirectory(files: File[]) {
  const paths = files.map((file) => file.webkitRelativePath || file.name)
  const roots = paths.map((path) => path.split('/')[0])
  const sharedRoot = paths.every((path) => path.includes('/')) && new Set(roots).size === 1
  return {
    name: sharedRoot ? roots[0] : undefined,
    paths: paths.map((path) => (sharedRoot ? path.slice(path.indexOf('/') + 1) : path)),
  }
}

function assetKind(path: string, entryAsset: string): CompanionAsset['kind'] {
  if (path === entryAsset) return 'entry'
  if (/\.(?:png|jpe?g|webp)$/i.test(path)) return 'texture'
  if (/\.motion3\.json$/i.test(path)) return 'motion'
  if (/\.exp3\.json$/i.test(path)) return 'expression'
  if (/\.physics3\.json$/i.test(path)) return 'physics'
  if (/\.pose3\.json$/i.test(path)) return 'pose'
  if (/\.(?:mp3|ogg|wav)$/i.test(path)) return 'audio'
  return 'model'
}

function contentType(path: string, file: File): CompanionAsset['contentType'] {
  if (/\.json$/i.test(path)) return 'application/json'
  if (/\.png$/i.test(path)) return 'image/png'
  if (/\.jpe?g$/i.test(path)) return 'image/jpeg'
  if (/\.webp$/i.test(path)) return 'image/webp'
  if (/\.svg$/i.test(path)) return 'image/svg+xml'
  if (/\.mp3$/i.test(path)) return 'audio/mpeg'
  if (/\.ogg$/i.test(path)) return 'audio/ogg'
  return file.type === 'application/json' ? 'application/json' : 'application/octet-stream'
}

let localPackVersion = 0

export async function createLocalLive2DPack(filesLike: ArrayLike<File>) {
  const files = Array.from(filesLike).filter((file) => file.name !== '.DS_Store')
  if (!files.length) throw new Error('请选择包含 Live2D 模型的目录')
  if (files.length > MAX_FILE_COUNT) throw new Error(`本地模型不能超过 ${MAX_FILE_COUNT} 个文件`)

  const directory = localDirectory(files)
  const paths = directory.paths
  const byPath = new Map<string, File>()
  let totalSize = 0
  for (let index = 0; index < files.length; index += 1) {
    const file = files[index]
    const path = paths[index]
    assertSafeLive2DAssetPath(path)
    if (file.size > MAX_FILE_SIZE) throw new Error(`本地模型单文件不能超过 64 MiB: ${path}`)
    if (byPath.has(path)) throw new Error(`本地模型包含重复路径: ${path}`)
    byPath.set(path, file)
    totalSize += file.size
  }
  if (totalSize > MAX_TOTAL_SIZE) throw new Error('本地模型总大小不能超过 256 MiB')

  const entries = paths.filter((path) => path.endsWith('.model3.json'))
  if (!entries.length) throw new Error('目录中未找到 .model3.json')
  const namedEntries = directory.name
    ? entries.filter((path) => path.split('/').slice(-1)[0] === `${directory.name}.model3.json`)
    : []
  const entryAsset =
    entries.length === 1 ? entries[0] : namedEntries.length === 1 ? namedEntries[0] : null
  if (!entryAsset) throw new Error('目录包含多个模型入口，且无法确定与目录同名的主模型')
  const pack: CompanionPackSelection = {
    pluginId: LOCAL_LIVE2D_PLUGIN_ID,
    version: `session-${(localPackVersion += 1)}`,
    local: true,
    loadAsset: async (path) => {
      const file = byPath.get(path)
      if (!file) throw new Error(`本地模型缺少资源: ${path}`)
      return file
    },
    manifest: {
      renderer: 'live2d-cubism',
      entryAsset,
      modelFormatVersion: 'Cubism 3+',
      assets: paths.map((path, index) => ({
        path,
        kind: assetKind(path, entryAsset),
        contentType: contentType(path, files[index]),
        sizeBytes: files[index].size,
      })),
    },
  }

  const validationBundle = await loadLive2DAssetBundle(pack, {
    createObjectURL: () => 'blob:soha-live2d-validation',
    revokeObjectURL: () => undefined,
  })
  validationBundle.dispose()
  return pack
}
