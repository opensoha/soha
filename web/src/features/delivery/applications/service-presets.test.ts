import { describe, expect, it } from 'vitest'
import { serviceCreationPreset } from './service-presets'
import type { RenderedDeliverySpec } from '../types'

describe('service creation presets', () => {
  it('copies the selected template and build inputs without reusing instance or repository identities', () => {
    const spec: RenderedDeliverySpec = {
      applicationDraft: { key: 'sample', name: 'Sample', group: '', language: '', enabled: true },
      services: [
        {
          id: 'old-service',
          key: 'web',
          name: 'Web',
          serviceKind: 'kubernetes_workload',
          enabled: true,
          buildSourceId: 'build',
          deploymentTemplate: {
            templateId: 'http',
            version: 2,
            parameters: { enabled: false, replicas: 0 },
            manifestPackageId: 'old-package',
          },
          containers: [{ id: 'old-container', name: 'main' }],
        },
      ],
      buildSources: [
        {
          id: 'build',
          name: 'Build',
          type: 'platform_build_template',
          enabled: true,
          isDefault: true,
          config: {
            buildTemplateId: 'go',
            buildTemplateVersion: 1,
            repositoryId: 'foreign',
            repositoryBindings: [
              { repositoryId: 'foreign', checkoutPath: 'src', defaultBranch: 'stable' },
            ],
          },
        },
      ],
    }
    const result = serviceCreationPreset(spec, 'web')
    expect(result.service.id).toBeUndefined()
    expect(result.service.containers?.[0].id).toBeUndefined()
    expect(result.service.deploymentTemplate).toEqual({
      templateId: 'http',
      version: 2,
      parameters: { enabled: false, replicas: 0 },
    })
    expect(result.buildSource?.id).not.toBe('build')
    expect(result.buildSource?.config).toMatchObject({
      buildTemplateVersion: 1,
      repositoryBindings: [{ repositoryId: '', checkoutPath: 'src', defaultBranch: 'stable' }],
    })
    expect(result.buildSource?.config.repositoryId).toBeUndefined()
    expect(spec.buildSources?.[0].config?.repositoryId).toBe('foreign')
    expect(() => serviceCreationPreset({ ...spec, buildSources: [] }, 'web')).toThrow('构建配置')
  })
})
