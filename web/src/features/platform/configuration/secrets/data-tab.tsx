import { useI18n } from '@/i18n'
import { ConfigurationDataWorkspace } from '../shared/data-workspace'
import type { SecretDetail } from './types'

export function SecretDataTab({
  applying,
  blocked,
  canEdit,
  detail,
  onApply,
  active = true,
  editRequest = 0,
}: {
  active?: boolean
  editRequest?: number
  blocked?: boolean
  applying?: boolean
  canEdit: boolean
  detail: SecretDetail
  onApply: (decodedData: Record<string, string>) => void | Promise<unknown>
}) {
  const { localeCode } = useI18n()
  const data: Record<string, string> = Object.create(null)
  const binaryData: Record<string, string> = Object.create(null)
  for (const [key, value] of Object.entries(detail.data ?? {})) {
    try {
      const bytes = Uint8Array.from(atob(value), (character) => character.charCodeAt(0))
      data[key] = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes)
    } catch {
      binaryData[key] = value
    }
  }
  // ponytail: the text endpoint replaces all keys; binary edits stay in YAML until it accepts raw bytes.
  const hasBinary = Object.keys(binaryData).length > 0
  return (
    <ConfigurationDataWorkspace
      active={active}
      searchable
      editRequest={editRequest}
      applying={applying}
      blocked={blocked}
      canEdit={canEdit && !hasBinary}
      detail={{ data, binaryData, immutable: detail.immutable }}
      encodedData={detail.data ?? {}}
      readOnlyReason={
        hasBinary
          ? localeCode === 'zh_CN'
            ? '此 Secret 含无法无损解码的二进制值。请在 YAML 页编辑，以保留原始字节。'
            : 'This Secret contains binary values. Edit its YAML to preserve the original bytes.'
          : undefined
      }
      onApply={onApply}
    />
  )
}
