import type { PersonalAssistantAvatarInput } from './types'
import type { WorkbenchImageAttachment } from '@opensoha/contracts/gen/ts/sohaapi'

const rasterTypes = ['image/png', 'image/jpeg', 'image/gif', 'image/webp']

export function readAvatarFile(file: Blob) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(new Error('读取图片失败'))
    reader.readAsDataURL(file)
  })
}

function loadAvatarImage(source: string) {
  return new Promise<HTMLImageElement>((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('图片无法打开，请选择有效的图片'))
    image.src = source
  })
}

function avatarCanvas(image: HTMLImageElement, size: number, limit: number) {
  const canvas = document.createElement('canvas')
  canvas.width = canvas.height = size
  const context = canvas.getContext('2d')
  if (!context) throw new Error('当前环境无法处理图片')
  const edge = Math.min(image.naturalWidth, image.naturalHeight)
  context.drawImage(
    image,
    (image.naturalWidth - edge) / 2,
    (image.naturalHeight - edge) / 2,
    edge,
    edge,
    0,
    0,
    size,
    size,
  )
  const png = canvas.toDataURL('image/png')
  const value = png.length <= limit ? png : canvas.toDataURL('image/jpeg', 0.85)
  if (value.length > limit) throw new Error('图片处理后仍过大，请选择较小图片')
  return value
}

export async function prepareAssistantAvatar(file: Blob): Promise<PersonalAssistantAvatarInput> {
  if (!rasterTypes.includes(file.type)) throw new Error('支持 PNG、JPEG、GIF 和 WebP 图片')
  const animatedFormat = ['image/gif', 'image/webp'].includes(file.type)
  if (file.size > (animatedFormat ? 512 * 1024 : 5 * 1024 * 1024))
    throw new Error(animatedFormat ? 'GIF/WebP 不能超过 512KB' : '图片不能超过 5MB')
  const source = await readAvatarFile(file)
  const image = await loadAvatarImage(source)
  const maxEdge = animatedFormat ? 1024 : 4096
  if (
    image.naturalWidth < 1 ||
    image.naturalHeight < 1 ||
    image.naturalWidth > maxEdge ||
    image.naturalHeight > maxEdge
  )
    throw new Error(`图片边长不能超过 ${maxEdge}px`)
  const poster = avatarCanvas(image, 256, 150000)
  return {
    avatarUrl: animatedFormat ? source : avatarCanvas(image, 512, 700000),
    avatarPosterUrl: poster,
    avatarMotion: 'auto',
  }
}

export async function prepareChatImage(file: File): Promise<WorkbenchImageAttachment> {
  if (!rasterTypes.includes(file.type)) throw new Error('支持 PNG、JPEG、GIF 和 WebP 图片')
  if (file.size > 5 * 1024 * 1024) throw new Error('图片不能超过 5MB')
  const image = await loadAvatarImage(await readAvatarFile(file))
  if (
    !image.naturalWidth ||
    !image.naturalHeight ||
    Math.max(image.naturalWidth, image.naturalHeight) > 4096
  )
    throw new Error('图片边长不能超过 4096px')
  const scale = Math.min(1, 1024 / Math.max(image.naturalWidth, image.naturalHeight))
  const canvas = document.createElement('canvas')
  canvas.width = Math.max(1, Math.round(image.naturalWidth * scale))
  canvas.height = Math.max(1, Math.round(image.naturalHeight * scale))
  const context = canvas.getContext('2d')
  if (!context) throw new Error('当前环境无法处理图片')
  context.drawImage(image, 0, 0, canvas.width, canvas.height)
  const png = canvas.toDataURL('image/png')
  const dataUrl = png.length <= 700000 ? png : canvas.toDataURL('image/jpeg', 0.85)
  if (dataUrl.length > 700000) throw new Error('图片处理后仍过大，请选择较小图片')
  return { id: crypto.randomUUID(), name: (file.name || '图片').slice(0, 256), dataUrl }
}
