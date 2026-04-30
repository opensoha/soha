import { useMemo } from 'react'
import { useLocation } from '@umijs/max'
import { useQuery } from '@tanstack/react-query'
import {
  ApplicationsPage,
  RegistriesPage,
  ReleasesPage,
  WorkflowsPage,
} from '@/features/delivery/delivery-pages'
import {
  ApplicationEnvironmentDetailPage,
  ApplicationEnvironmentsPage,
  BusinessLinesPage,
  DeliveryEnvironmentsPage,
  ReleaseBoardPage,
  WorkflowTemplatesPage,
} from '@/features/delivery/delivery-catalog-pages'
import {
  deliveryApplicationsQueryOptions,
  deliveryRegistriesQueryOptions,
  deliveryWorkflowsQueryOptions,
} from '@/features/delivery/queries'
import { usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { NonPlatformWorkspace } from '@/pages/nonplatform-workspace'

const deliveryItems = [
  { key: 'applications', path: '/applications', label: '应用管理', description: '管理应用仓库、构建参数与启停状态。', permissionKey: 'delivery.applications.view' },
  { key: 'business-lines', path: '/business-lines', label: '业务线', description: '维护交付主数据中的业务线范围。', permissionKey: 'delivery.business-lines.view' },
  { key: 'delivery-environments', path: '/delivery-environments', label: '交付环境', description: '维护环境定义和可用集群范围。', permissionKey: 'delivery.environments.view' },
  { key: 'application-environments', path: '/application-environments', label: '应用环境', description: '绑定应用、环境与目标工作负载。', permissionKey: 'delivery.application-environments.view' },
  { key: 'workflow-templates', path: '/workflow-templates', label: '工作流模板', description: '配置发布流程模板与节点定义。', permissionKey: 'delivery.workflow-templates.view' },
  { key: 'release-board', path: '/release-board', label: '发布看板', description: '查看应用环境最近的构建、流程和发布活动。', permissionKey: 'delivery.release-board.view' },
  { key: 'workflows', path: '/workflows', label: '工作流', description: '触发和回看工作流执行记录。', permissionKey: 'delivery.workflows.view' },
  { key: 'releases', path: '/releases', label: '发布记录', description: '检查发布编排与目标环境结果。', permissionKey: 'delivery.releases.view' },
  { key: 'registries', path: '/registries', label: '镜像仓库', description: '维护镜像仓库连接与默认命名空间。', permissionKey: 'delivery.registries.view' },
] as const

function renderDeliveryContent(pathname: string) {
  if (pathname.startsWith('/business-lines')) return <BusinessLinesPage embedded />
  if (pathname.startsWith('/delivery-environments')) return <DeliveryEnvironmentsPage embedded />
  if (pathname.startsWith('/application-environments/')) return <ApplicationEnvironmentDetailPage />
  if (pathname.startsWith('/application-environments')) return <ApplicationEnvironmentsPage embedded />
  if (pathname.startsWith('/workflow-templates')) return <WorkflowTemplatesPage embedded />
  if (pathname.startsWith('/release-board')) return <ReleaseBoardPage embedded />
  if (pathname.startsWith('/workflows')) return <WorkflowsPage embedded />
  if (pathname.startsWith('/releases')) return <ReleasesPage embedded />
  if (pathname.startsWith('/registries')) return <RegistriesPage embedded />
  return <ApplicationsPage />
}

export default function DeliveryPage() {
  const { pathname } = useLocation()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data

  const applicationsQuery = useQuery(deliveryApplicationsQueryOptions())
  const workflowsQuery = useQuery(deliveryWorkflowsQueryOptions())
  const registriesQuery = useQuery(deliveryRegistriesQueryOptions())

  const stats = useMemo(() => {
    const applications = applicationsQuery.data?.data ?? []
    const workflows = workflowsQuery.data?.data ?? []
    const registries = registriesQuery.data?.data ?? []
    return [
      { label: '可见模块', value: String(deliveryItems.filter((item) => !item.permissionKey || snapshot?.permissionKeys.includes(item.permissionKey)).length), hint: '按当前权限可访问的交付工作区数量。' },
      { label: '应用数', value: String(applications.length), hint: `${applications.filter((item) => item.enabled).length} 个应用处于启用状态。` },
      { label: '工作流执行', value: String(workflows.length), hint: `${workflows.filter((item) => item.status === 'running').length} 条工作流正在运行。` },
      { label: '仓库连接', value: String(registries.length), hint: `${registries.filter((item) => item.enabled).length} 个镜像仓库连接已启用。` },
    ]
  }, [applicationsQuery.data?.data, registriesQuery.data?.data, snapshot?.permissionKeys, workflowsQuery.data?.data])

  return (
    <NonPlatformWorkspace
      rootPath="/applications"
      title="应用交付工作台"
      description="面向应用、环境、工作流与发布编排的 Pro-native 交付入口。"
      workspaceLabel="Delivery"
      workspaceSummary="当前入口把交付主数据、发布流程和执行记录收敛到同一工作台中。路由结构仍与 kubecrux 现有权限和 API 保持一致，但入口页现在直接承担分区导航和上下文摘要，不再只是 pathname 转发器。"
      items={[...deliveryItems]}
      stats={stats}
      highlights={[
        { title: '主数据与运行编排分层清晰', description: '业务线、环境、应用环境与流程模板继续保留现有路由语义，但入口页先把交付主数据和执行面板的职责区分清楚。' },
        { title: '从应用到发布看板的一致跳转', description: '当前分区按钮直接映射到应用、工作流、发布记录和发布看板，减少先进入空壳页再二次切换的视觉损耗。' },
      ]}
      actions={[
        { label: '配置交付主数据', description: '优先维护业务线、环境、应用环境绑定和流程模板，保证发布看板能展示完整上下文。' },
        { label: '推进执行动作', description: '在工作流、发布记录和发布看板之间回看最近执行结果，再进入单条环境详情触发动作。' },
      ]}
    >
      {renderDeliveryContent(pathname)}
    </NonPlatformWorkspace>
  )
}
