import { ConfigurationDataWorkspace } from '../shared/data-workspace'
import type { ConfigMapDetail } from './types'

export function ConfigMapDataTab(props: {
  editRequest?: number
  blocked?: boolean
  applying?: boolean
  canEdit: boolean
  detail: ConfigMapDetail
  onApply: (data: Record<string, string>) => void | Promise<unknown>
}) {
  return <ConfigurationDataWorkspace searchable {...props} />
}
