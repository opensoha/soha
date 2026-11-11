import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useWorkbenchModuleEnabled } from '@/features/modules'

export function useVirtualizationPermissions() {
  const permissionSnapshotQuery = usePermissionSnapshot()
  const { moduleEnabled: virtualizationModuleEnabled } = useWorkbenchModuleEnabled('virtualization')
  const snapshot = permissionSnapshotQuery.data?.data
  const hasVirtualizationPermission = (key: string) =>
    virtualizationModuleEnabled && hasPermission(snapshot, key)
  const canCreateVMs = hasVirtualizationPermission('virtualization.vms.create')
  const canPowerVMs = hasVirtualizationPermission('virtualization.vms.power')
  const canResizeVMs = hasVirtualizationPermission('virtualization.vms.resize')
  const canDeleteVMs = hasVirtualizationPermission('virtualization.vms.delete')
  const canSync = hasVirtualizationPermission('virtualization.sync.sync')
  const canViewWorkerPools =
    hasVirtualizationPermission('virtualization.clusters.view') &&
    hasVirtualizationPermission('platform.clusters.view') &&
    hasVirtualizationPermission('platform.nodes.view')
  const canSupplyWorkers =
    canViewWorkerPools &&
    hasVirtualizationPermission('platform.nodes.create') &&
    hasVirtualizationPermission('virtualization.images.view')

  return {
    canConnectVMs:
      hasVirtualizationPermission('virtualization.vms.connect') &&
      hasVirtualizationPermission('secret.use'),
    canManageVMAccess:
      hasVirtualizationPermission('virtualization.vm_access.update') &&
      hasVirtualizationPermission('secret.use'),
    virtualizationModuleEnabled,
    canViewWorkerPools,
    canCreateWorkers: canSupplyWorkers && canCreateVMs,
    canCreateWorkerPools:
      canSupplyWorkers && hasVirtualizationPermission('virtualization.clusters.create'),
    canUpdateWorkerPools:
      canSupplyWorkers && hasVirtualizationPermission('virtualization.clusters.update'),
    canDeleteWorkerPools:
      canViewWorkerPools &&
      hasVirtualizationPermission('platform.nodes.create') &&
      hasVirtualizationPermission('virtualization.clusters.delete'),
    canCreateVMs,
    canPowerVMs,
    canResizeVMs,
    canDeleteVMs,
    canManageVMs: canCreateVMs || canPowerVMs || canResizeVMs || canDeleteVMs,
    canCreateClusters: hasVirtualizationPermission('virtualization.clusters.create'),
    canUpdateClusters: hasVirtualizationPermission('virtualization.clusters.update'),
    canDeleteClusters: hasVirtualizationPermission('virtualization.clusters.delete'),
    canTestClusters: hasVirtualizationPermission('virtualization.clusters.test'),
    canSyncClusters: canSync,
    canCreateImages: hasVirtualizationPermission('virtualization.images.create'),
    canUpdateImages: hasVirtualizationPermission('virtualization.images.update'),
    canDeleteImages: hasVirtualizationPermission('virtualization.images.delete'),
    canCreateFlavors: hasVirtualizationPermission('virtualization.flavors.create'),
    canUpdateFlavors: hasVirtualizationPermission('virtualization.flavors.update'),
    canDeleteFlavors: hasVirtualizationPermission('virtualization.flavors.delete'),
    canViewTasks:
      hasVirtualizationPermission('virtualization.operations.view') ||
      hasVirtualizationPermission('virtualization.sync.view'),
    canViewVMOperations: hasVirtualizationPermission('virtualization.operations.view'),
    canCancelOperations: hasVirtualizationPermission('virtualization.operations.cancel'),
    canRetryOperations: hasVirtualizationPermission('virtualization.operations.retry'),
    canSync,
    canViewMetrics: hasVirtualizationPermission('virtualization.vms.metrics'),
    canAccessConsole: hasVirtualizationPermission('virtualization.vms.console'),
  }
}
