import type { RenderedDeliverySpec } from '../types'

export function serviceCreationPreset(spec: RenderedDeliverySpec, key: string) {
  const selected = spec.services?.find((service) => service.key === key)
  if (!selected) throw new Error('预设中的服务已移除，请重新选择')
  const service = structuredClone(selected)
  const source = spec.buildSources?.find((item) => item.id === service.buildSourceId)
  if (service.buildSourceId && !source) throw new Error('预设缺少对应的构建配置')
  delete service.id
  delete service.buildSourceId
  for (const container of service.containers ?? []) delete container.id
  if (service.deploymentTemplate) {
    delete service.deploymentTemplate.manifestPackageId
    delete service.deploymentTemplate.detached
  }
  return {
    service,
    buildSource: source
      ? {
          ...structuredClone(source),
          id: crypto.randomUUID(),
          isDefault: false,
          config: {
            ...structuredClone(source.config ?? {}),
            repositoryId: undefined,
            repositoryBindings: (source.config?.repositoryBindings?.length
              ? source.config.repositoryBindings
              : [{ checkoutPath: '.', defaultBranch: service.defaultBranch || 'main' }]
            ).map((binding) => ({ ...binding, repositoryId: '' })),
          },
        }
      : undefined,
  }
}
