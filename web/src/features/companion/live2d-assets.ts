import { fetchCompanionAsset } from './api'
import type { CompanionPackSelection } from './types'

interface Live2DAssetLoaderDependencies {
  createObjectURL?: (blob: Blob) => string
  fetchAsset?: typeof fetchCompanionAsset
  revokeObjectURL?: (url: string) => void
}

export interface Live2DAssetBundle {
  dispose: () => void
  entryUrl: string
}

interface AssetReference {
  path: string
  replace: (url: string) => void
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function assertSafeLive2DAssetPath(path: string) {
  const parts = path.split('/')
  if (
    !path ||
    path.startsWith('/') ||
    path.includes('\\') ||
    path.includes('?') ||
    path.includes('#') ||
    /^[a-z][a-z\d+.-]*:/i.test(path) ||
    parts.some((part) => !part || part === '.' || part === '..')
  ) {
    throw new Error(`Live2D asset path is unsafe: ${path}`)
  }
}

function resolveReference(entryAsset: string, reference: string, declared: Set<string>) {
  assertSafeLive2DAssetPath(reference)
  const slash = entryAsset.lastIndexOf('/')
  const path = `${slash < 0 ? '' : entryAsset.slice(0, slash + 1)}${reference}`
  assertSafeLive2DAssetPath(path)
  if (!declared.has(path)) throw new Error(`Live2D asset is not declared: ${path}`)
  return path
}

function collectReferences(
  model: Record<string, unknown>,
  entryAsset: string,
  declared: Set<string>,
) {
  const fileReferences = model.FileReferences
  if (!isRecord(fileReferences)) throw new Error('Live2D asset has no FileReferences object')
  const references: AssetReference[] = []

  const add = (record: Record<string, unknown>, key: string, required = false) => {
    const value = record[key]
    if (value === undefined && !required) return
    if (typeof value !== 'string') throw new Error(`Live2D asset reference ${key} is invalid`)
    references.push({
      path: resolveReference(entryAsset, value, declared),
      replace: (url) => {
        record[key] = url
      },
    })
  }

  add(fileReferences, 'Moc', true)
  for (const key of ['Physics', 'Pose', 'DisplayInfo', 'UserData']) add(fileReferences, key)

  if (!Array.isArray(fileReferences.Textures)) {
    throw new Error('Live2D asset reference Textures is invalid')
  }
  fileReferences.Textures.forEach((value, index) => {
    if (typeof value !== 'string') throw new Error('Live2D asset texture reference is invalid')
    references.push({
      path: resolveReference(entryAsset, value, declared),
      replace: (url) => {
        ;(fileReferences.Textures as unknown[])[index] = url
      },
    })
  })

  if (fileReferences.Motions !== undefined) {
    if (!isRecord(fileReferences.Motions)) {
      throw new Error('Live2D asset reference Motions is invalid')
    }
    for (const motions of Object.values(fileReferences.Motions)) {
      if (!Array.isArray(motions)) throw new Error('Live2D asset motion group is invalid')
      for (const motion of motions) {
        if (!isRecord(motion)) throw new Error('Live2D asset motion reference is invalid')
        add(motion, 'File', true)
        add(motion, 'Sound')
      }
    }
  }

  if (fileReferences.Expressions !== undefined) {
    if (!Array.isArray(fileReferences.Expressions)) {
      throw new Error('Live2D asset reference Expressions is invalid')
    }
    for (const expression of fileReferences.Expressions) {
      if (!isRecord(expression)) throw new Error('Live2D asset expression reference is invalid')
      add(expression, 'File', true)
    }
  }

  return references
}

export async function loadLive2DAssetBundle(
  pack: CompanionPackSelection,
  dependencies: Live2DAssetLoaderDependencies = {},
): Promise<Live2DAssetBundle> {
  const createObjectURL = dependencies.createObjectURL ?? URL.createObjectURL.bind(URL)
  const loadAsset =
    dependencies.fetchAsset ??
    (pack.loadAsset
      ? (_pluginId: string, path: string) => pack.loadAsset!(path)
      : fetchCompanionAsset)
  const revokeObjectURL = dependencies.revokeObjectURL ?? URL.revokeObjectURL.bind(URL)
  const declared = new Set<string>()

  for (const asset of pack.manifest.assets) {
    assertSafeLive2DAssetPath(asset.path)
    if (declared.has(asset.path)) throw new Error(`Duplicate Live2D asset path: ${asset.path}`)
    declared.add(asset.path)
  }
  const entryAsset = pack.manifest.entryAsset
  assertSafeLive2DAssetPath(entryAsset)
  if (!entryAsset.endsWith('.model3.json') || !declared.has(entryAsset)) {
    throw new Error(`Live2D asset entry is invalid: ${entryAsset}`)
  }

  const entryBlob = await loadAsset(pack.pluginId, entryAsset)
  let parsed: unknown
  try {
    parsed = JSON.parse(await entryBlob.text())
  } catch {
    throw new Error(`Live2D asset entry is not valid JSON: ${entryAsset}`)
  }
  if (!isRecord(parsed)) throw new Error(`Live2D asset entry is invalid: ${entryAsset}`)

  const references = collectReferences(parsed, entryAsset, declared)
  const paths = [...new Set(references.map(({ path }) => path))]
  const blobs = await Promise.all(paths.map((path) => loadAsset(pack.pluginId, path)))
  const urls: string[] = []

  try {
    const assetUrls = new Map(
      paths.map((path, index) => {
        const url = createObjectURL(blobs[index])
        urls.push(url)
        return [path, url] as const
      }),
    )
    for (const reference of references) reference.replace(assetUrls.get(reference.path)!)
    const entryUrl = createObjectURL(
      new Blob([JSON.stringify(parsed)], { type: 'application/json' }),
    )
    urls.push(entryUrl)
    let disposed = false
    return {
      entryUrl,
      dispose: () => {
        if (disposed) return
        disposed = true
        for (const url of urls) revokeObjectURL(url)
      },
    }
  } catch (error) {
    for (const url of urls) revokeObjectURL(url)
    throw error
  }
}
