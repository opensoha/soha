import { describe, expect, it } from 'vitest'
import type { RuntimeMenuNode } from '@/types'
import { isComputeWorkbenchMenuGroup, normalizeComputeWorkbenchNav } from './navigation'

function menuNode(id: string, children?: RuntimeMenuNode[]): RuntimeMenuNode {
  return {
    id,
    path: `/${id}`,
    labelZh: id,
    labelEn: id,
    iconKey: 'server',
    section: 'ops',
    sortOrder: 1,
    enabled: true,
    children,
  }
}

describe('compute workbench navigation', () => {
  it('matches the unified information architecture', () => {
    const result = normalizeComputeWorkbenchNav([
      menuNode('compute-workbench-tasks-sync'),
      menuNode('compute-workbench-tasks-build'),
      menuNode('compute-workbench-tasks-operations'),
      menuNode('virtualization-workbench', [
        menuNode('virtualization-workbench-clusters'),
        menuNode('virtualization-workbench-storage'),
        menuNode('virtualization-workbench-vms'),
        menuNode('virtualization-workbench-images'),
        menuNode('virtualization-workbench-flavors'),
      ]),
      menuNode('docker-workbench', [
        menuNode('docker-workbench-hosts'),
        menuNode('docker-workbench-projects'),
        menuNode('docker-workbench-templates'),
      ]),
      menuNode('compute-workbench-overview'),
    ])

    expect(result.map((node) => node.labelZh)).toEqual(['总览', '任务中心', '虚拟化', '容器运行时'])
    expect(result[2].children?.map((node) => node.labelZh)).toEqual([
      '虚拟机',
      '集群',
      '镜像与模板',
      '存储与卷',
      '规格',
    ])
    expect(result[3].children?.map((node) => node.labelZh)).toEqual([
      '运行时主机',
      '容器管理',
      '部署模板',
    ])
    expect(isComputeWorkbenchMenuGroup(result[2].id)).toBe(true)
    expect(isComputeWorkbenchMenuGroup(result[3].id)).toBe(true)
  })
})
