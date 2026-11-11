import { defineRoutes } from '@/routes/definitions'

export const storageViewPermissions = [
  'platform.storage.persistent-volume-claims.view',
  'platform.storage.persistent-volumes.view',
  'platform.storage.storage-classes.view',
]

export const storageRoutes = defineRoutes([
  {
    meta: {
      id: 'storage-pvc',
      path: '/storage/persistentvolumeclaims',
      title: 'PVC',
      description: '持久卷声明',
      icon: 'IconServer',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'storage',
      menuId: 'storage-pvc',
      permissionKey: 'platform.storage.persistent-volume-claims.view',
      scopeMode: 'namespace',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./persistent-volume-claims/list-page')
      return { default: module.StoragePvcPage }
    },
  },
  {
    meta: {
      id: 'storage-pvc-detail',
      path: '/storage/persistentvolumeclaims/:name',
      title: 'PVC Detail',
      description: '持久卷声明详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'namespace',
    },
    shell: 'app',
    inheritMetaFrom: 'storage-pvc',
    load: async () => {
      const module = await import('./persistent-volume-claims/detail-page')
      return { default: module.StoragePvcDetailPage }
    },
  },
  {
    meta: {
      id: 'storage-pv',
      path: '/storage/persistentvolumes',
      title: 'PV',
      description: '持久卷',
      icon: 'IconServer',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'storage',
      menuId: 'storage-pv',
      permissionKey: 'platform.storage.persistent-volumes.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./persistent-volumes/list-page')
      return { default: module.StoragePvPage }
    },
  },
  {
    meta: {
      id: 'storage-pv-detail',
      path: '/storage/persistentvolumes/:name',
      title: 'PV Detail',
      description: '持久卷详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'storage-pv',
    load: async () => {
      const module = await import('./persistent-volumes/detail-page')
      return { default: module.StoragePvDetailPage }
    },
  },
  {
    meta: {
      id: 'storage-classes',
      path: '/storage/storageclasses',
      title: 'StorageClasses',
      description: '存储类',
      icon: 'IconServer',
      group: 'platform',
      requiresAuth: true,
      tabbar: true,
      navVisible: true,
      parentId: 'storage',
      menuId: 'storage-classes',
      permissionKey: 'platform.storage.storage-classes.view',
      scopeMode: 'cluster',
      workspace: 'resource',
    },
    shell: 'app',
    load: async () => {
      const module = await import('./storage-classes/list-page')
      return { default: module.StorageClassesPage }
    },
  },
  {
    meta: {
      id: 'storage-class-detail',
      path: '/storage/storageclasses/:name',
      title: 'StorageClass Detail',
      description: '存储类详情',
      requiresAuth: true,
      tabbar: false,
      navVisible: false,
      scopeMode: 'cluster',
    },
    shell: 'app',
    inheritMetaFrom: 'storage-classes',
    load: async () => {
      const module = await import('./storage-classes/detail-page')
      return { default: module.StorageClassDetailPage }
    },
  },
] as const)
