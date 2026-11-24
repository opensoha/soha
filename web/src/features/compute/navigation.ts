import type { RuntimeMenuNode } from '@/types'

const COMPUTE_MENU_ORDER = new Map(
  [
    'compute-workbench-overview',
    'compute-workbench-tasks-operations',
    'virtualization-workbench',
    'virtualization-workbench-vms',
    'virtualization-workbench-clusters',
    'virtualization-workbench-images',
    'virtualization-workbench-storage',
    'virtualization-workbench-flavors',
    'docker-workbench',
    'docker-workbench-hosts',
    'docker-workbench-projects',
    'docker-workbench-templates',
  ].map((id, index) => [id, index]),
)

const COMPUTE_MENU_GROUP_IDS = new Set(['virtualization-workbench', 'docker-workbench'])

const COMPUTE_MENU_LABELS: Record<string, { labelEn: string; labelZh: string }> = {
  'compute-workbench-overview': { labelZh: '总览', labelEn: 'Overview' },
  'compute-workbench-tasks-operations': { labelZh: '任务中心', labelEn: 'Task Center' },
  'virtualization-workbench': { labelZh: '虚拟化', labelEn: 'Virtualization' },
  'virtualization-workbench-vms': { labelZh: '虚拟机', labelEn: 'Virtual Machines' },
  'virtualization-workbench-clusters': { labelZh: '集群', labelEn: 'Clusters' },
  'virtualization-workbench-images': { labelZh: '镜像与模板', labelEn: 'Images & Templates' },
  'virtualization-workbench-storage': { labelZh: '存储与卷', labelEn: 'Storage & Volumes' },
  'virtualization-workbench-flavors': { labelZh: '规格', labelEn: 'Flavors' },
  'docker-workbench': { labelZh: '容器运行时', labelEn: 'Container Runtime' },
  'docker-workbench-hosts': { labelZh: '运行时主机', labelEn: 'Runtime Hosts' },
  'docker-workbench-projects': { labelZh: '容器管理', labelEn: 'Container Management' },
  'docker-workbench-templates': { labelZh: '部署模板', labelEn: 'Deployment Templates' },
}

const COMPUTE_MENU_ICONS: Record<string, string> = {
  'compute-workbench-overview': 'gauge',
  'compute-workbench-tasks-operations': 'clipboard-list',
  'virtualization-workbench': 'desktop',
  'virtualization-workbench-vms': 'desktop',
  'virtualization-workbench-clusters': 'cluster',
  'virtualization-workbench-images': 'image',
  'virtualization-workbench-storage': 'waves',
  'virtualization-workbench-flavors': 'flavor',
  'docker-workbench': 'server',
  'docker-workbench-hosts': 'server',
  'docker-workbench-projects': 'boxes',
  'docker-workbench-templates': 'blocks',
}

const LEGACY_COMPUTE_TASK_MENU_IDS = new Set([
  'compute-workbench-tasks-sync',
  'compute-workbench-tasks-build',
])

const LEGACY_ICON_KEYS = new Set(['tasks', 'results', 'workbench', 'menu-square'])

export function normalizeComputeWorkbenchNav(nodes: RuntimeMenuNode[]): RuntimeMenuNode[] {
  return nodes
    .flatMap((node) => {
      if (LEGACY_COMPUTE_TASK_MENU_IDS.has(node.id)) return []
      const labels = COMPUTE_MENU_LABELS[node.id]
      const fallbackIcon = COMPUTE_MENU_ICONS[node.id]
      const rawIcon = String(node.iconKey || '').trim()
      const iconKey =
        !rawIcon || LEGACY_ICON_KEYS.has(rawIcon) ? (fallbackIcon ?? node.iconKey) : node.iconKey
      const children = node.children ? normalizeComputeWorkbenchNav(node.children) : undefined
      return [
        {
          ...node,
          ...labels,
          iconKey,
          children: children?.length ? children : undefined,
        },
      ]
    })
    .sort((left, right) => {
      const leftOrder = COMPUTE_MENU_ORDER.get(left.id) ?? Number.MAX_SAFE_INTEGER
      const rightOrder = COMPUTE_MENU_ORDER.get(right.id) ?? Number.MAX_SAFE_INTEGER
      return leftOrder - rightOrder || left.sortOrder - right.sortOrder
    })
}

export function isComputeWorkbenchMenuGroup(id: string) {
  return COMPUTE_MENU_GROUP_IDS.has(id)
}
