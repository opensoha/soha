import { describe, expect, it } from 'vitest'
import { enUS } from './locales/en_US'
import { zhCN } from './locales/zh_CN'

describe('locale dictionaries', () => {
  it('keeps English and Chinese keys in parity', () => {
    expect(Object.keys(enUS).sort()).toEqual(Object.keys(zhCN).sort())
  })

  it('localizes the global resource creation action', () => {
    expect(enUS['platform.resourceCreation.createResource']).toBe('Create resource')
    expect(zhCN['platform.resourceCreation.createResource']).toBe('创建资源')
  })

  it('localizes Compute role permissions and route labels', () => {
    expect(enUS['permission.key.virtualization.vms.view']).toBe('View virtual machines')
    expect(zhCN['permission.key.virtualization.vms.view']).toBe('查看虚拟机')
    expect(enUS['permission.key.virtualization.sync.sync']).toBe(
      'Synchronize virtualization resources',
    )
    expect(enUS['route.compute-workbench-tasks-operations.title']).toBe('Task Center')
  })
})
