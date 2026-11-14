import {
  getConfigurationDetail,
  listConfigurationResources,
  updateConfigurationData,
} from '../shared/api'
import type { ConfigurationTarget } from '../shared/types'
import type { ConfigMapDetail, ConfigMapResource, UpdateConfigMapDataPayload } from './types'

export const configMapKind = 'configmaps' as const

export function listConfigMaps(scope: ConfigurationTarget['scope']) {
  return listConfigurationResources<ConfigMapResource>(configMapKind, scope)
}

export async function getConfigMapDetail(target: ConfigurationTarget) {
  const detail = await getConfigurationDetail<ConfigMapDetail>(configMapKind, target)
  if (detail.name !== target.name || detail.namespace !== target.scope.namespace) {
    throw new Error('ConfigMap identity does not match the requested resource')
  }
  return detail
}

export function updateConfigMapData(
  target: ConfigurationTarget,
  payload: UpdateConfigMapDataPayload,
) {
  return updateConfigurationData<ConfigMapDetail, UpdateConfigMapDataPayload>(
    configMapKind,
    target,
    payload,
  )
}
