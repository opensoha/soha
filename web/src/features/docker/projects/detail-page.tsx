import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeRelationsPanel } from '@/features/compute'
import { ManagementRefreshButton, ManagementState } from '@/components/management-list'
import { lazy, Suspense, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Card, Descriptions, Input, Space, Tabs, Tag } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { Navigate, useParams } from 'react-router-dom'
import { useAIPageContext } from '@/features/copilot'
import { localeText, useI18n } from '@/i18n'
import { formatStatusLabel } from '@/i18n/status'
import { formatDateTime } from '@/utils/time'
import { dockerQueries } from '../queries'
import { PortsTable } from '../ports/table'
import { ServicesTable } from '../services/table'
import {
  architectureTag,
  DockerTableHeader,
  configArrayCount,
  configTextValue,
  normalizePage,
  stringValue,
  useDockerPermissions,
} from '../shared/ui'

const { TextArea } = Input

const DockerProjectLogsPanel = lazy(() =>
  import('../runtime/logs-panel').then((module) => ({ default: module.DockerProjectLogsPanel })),
)
const DockerProjectTerminalPanel = lazy(() =>
  import('../runtime/terminal-panel').then((module) => ({
    default: module.DockerProjectTerminalPanel,
  })),
)
const DockerProjectVolumesPanel = lazy(() =>
  import('../runtime/volumes-panel').then((module) => ({
    default: module.DockerProjectVolumesPanel,
  })),
)

function RuntimeBoundary({ children }: { children: ReactNode }) {
  return <Suspense fallback={<Card loading />}>{children}</Suspense>
}

function ProjectDetailWorkspace() {
  const { projectId } = useParams()
  const { localeCode } = useI18n()
  const resolvedProjectId = projectId ?? ''
  const {
    dockerModuleEnabled,
    canViewServices,
    canViewServiceLogs,
    canAccessServiceTerminal,
    canReadServiceFiles,
    canViewPorts,
  } = useDockerPermissions()
  const [runtimeServiceName, setRuntimeServiceName] = useState('')
  const projectQuery = useQuery(dockerQueries.project(resolvedProjectId, dockerModuleEnabled))
  const detailServicesQuery = useQuery(
    dockerQueries.projectServices(resolvedProjectId, dockerModuleEnabled && canViewServices),
  )
  const project = readableQueryData(projectQuery)
  const isSingleContainerProject = project?.sourceKind === 'single_container'
  const projectConfig = project?.config
  const runtimeServicePage = normalizePage(
    canViewServices ? readableQueryData(detailServicesQuery) : undefined,
    1,
    100,
  )
  const runtimeServices = runtimeServicePage.items
  const runningServiceCount = runtimeServices.filter(
    (service) => service.status?.toLowerCase() === 'running',
  ).length
  const serviceRestartCount = runtimeServices.reduce(
    (total, service) => total + (service.restartCount ?? 0),
    0,
  )
  const runtimeServiceOptions = useMemo(() => {
    const options = runtimeServices
      .map((service) => ({ label: service.name || service.id, value: service.name || service.id }))
      .filter((option) => option.value)
    const configServiceName = stringValue(projectConfig?.serviceName)
    if (configServiceName && !options.some((option) => option.value === configServiceName)) {
      options.push({ label: configServiceName, value: configServiceName })
    }
    return options
  }, [projectConfig, runtimeServices])
  const defaultRuntimeServiceName = runtimeServiceOptions[0]?.value || ''
  useAIPageContext({
    sourceWorkbench: 'compute',
    sourceTitle: project?.name
      ? localeText(localeCode, `Docker 项目 ${project.name}`, `Docker project ${project.name}`)
      : localeText(localeCode, 'Docker 项目详情', 'Docker project details'),
    entityKind: 'docker.project',
    entityName: project?.name ?? resolvedProjectId,
    dockerHostId: project?.hostId,
    dockerServiceId: runtimeServiceName || defaultRuntimeServiceName || undefined,
    visibleFilters: {
      runtimeServiceName: runtimeServiceName || defaultRuntimeServiceName,
      sourceKind: project?.sourceKind,
      status: project?.status,
    },
    pinnedData: {
      projectId: resolvedProjectId,
      serviceCount: runtimeServices.length,
      hostId: project?.hostId,
    },
  })
  useEffect(() => {
    if (!defaultRuntimeServiceName) {
      setRuntimeServiceName('')
      return
    }
    if (
      !runtimeServiceName ||
      !runtimeServiceOptions.some((option) => option.value === runtimeServiceName)
    ) {
      setRuntimeServiceName(defaultRuntimeServiceName)
    }
  }, [defaultRuntimeServiceName, runtimeServiceName, runtimeServiceOptions])
  if (!resolvedProjectId) {
    return <Navigate to="/compute/runtimes/projects" replace />
  }
  const runtimeConfigTab = isSingleContainerProject
    ? {
        key: 'config',
        label: localeText(localeCode, '配置', 'Configuration'),
        children: (
          <Card className="soha-detail-card" loading={projectQuery.isLoading}>
            <Descriptions
              size="small"
              column={{ xs: 1, sm: 2, lg: 3 }}
              items={[
                {
                  key: 'image',
                  label: localeText(localeCode, '镜像', 'Image'),
                  children: configTextValue(projectConfig, 'image'),
                },
                {
                  key: 'architecture',
                  label: localeText(localeCode, '架构', 'Architecture'),
                  children: architectureTag(configTextValue(projectConfig, 'architecture')),
                },
                {
                  key: 'platform',
                  label: localeText(localeCode, '平台', 'Platform'),
                  children: configTextValue(projectConfig, 'platform'),
                },
                {
                  key: 'serviceName',
                  label: localeText(localeCode, '服务名', 'Service name'),
                  children: configTextValue(projectConfig, 'serviceName'),
                },
                {
                  key: 'restartPolicy',
                  label: localeText(localeCode, '重启策略', 'Restart policy'),
                  children: configTextValue(projectConfig, 'restartPolicy'),
                },
                {
                  key: 'command',
                  label: localeText(localeCode, '启动命令', 'Command'),
                  children: configTextValue(projectConfig, 'command'),
                },
                {
                  key: 'ports',
                  label: localeText(localeCode, '端口', 'Ports'),
                  children: localeText(
                    localeCode,
                    `${configArrayCount(projectConfig, 'ports')} 个`,
                    `${configArrayCount(projectConfig, 'ports')}`,
                  ),
                },
                {
                  key: 'volumes',
                  label: localeText(localeCode, '卷', 'Volumes'),
                  children: localeText(
                    localeCode,
                    `${configArrayCount(projectConfig, 'volumes')} 个`,
                    `${configArrayCount(projectConfig, 'volumes')}`,
                  ),
                },
              ]}
            />
            <div className="mt-4">
              <TextArea
                rows={12}
                spellCheck={false}
                value={JSON.stringify(projectConfig ?? {}, null, 2)}
                readOnly
              />
            </div>
          </Card>
        ),
      }
    : {
        key: 'compose',
        label: 'Compose',
        children: (
          <Card className="soha-detail-card">
            <Tabs
              items={[
                {
                  key: 'composeContent',
                  label: 'compose.yaml',
                  children: (
                    <TextArea
                      rows={18}
                      spellCheck={false}
                      value={project?.composeContent || ''}
                      readOnly
                    />
                  ),
                },
                {
                  key: 'envContent',
                  label: '.env',
                  children: (
                    <TextArea
                      rows={12}
                      spellCheck={false}
                      value={project?.envContent || ''}
                      readOnly
                    />
                  ),
                },
              ]}
            />
          </Card>
        ),
      }
  const detailTabItems = [
    {
      key: 'overview',
      label: localeText(localeCode, '概览', 'Overview'),
      children: (
        <div className="soha-detail-stack">
          <Card className="soha-detail-card" loading={projectQuery.isLoading}>
            <Descriptions
              size="small"
              column={{ xs: 1, sm: 2, lg: 3 }}
              items={[
                {
                  key: 'host',
                  label: localeText(localeCode, 'Docker 主机', 'Docker host'),
                  children: project?.hostId || '-',
                },
                {
                  key: 'environment',
                  label: localeText(localeCode, '环境', 'Environment'),
                  children: project?.environment || '-',
                },
                {
                  key: 'owner',
                  label: localeText(localeCode, '负责人', 'Owner'),
                  children: project?.owner || project?.team || '-',
                },
                {
                  key: 'desiredState',
                  label: localeText(localeCode, '目标态', 'Desired state'),
                  children: formatStatusLabel(project?.desiredState, localeCode),
                },
                {
                  key: 'lastDeployedAt',
                  label: localeText(localeCode, '部署时间', 'Deployed at'),
                  children: formatDateTime(project?.lastDeployedAt),
                },
                {
                  key: 'expiresAt',
                  label: localeText(localeCode, '到期', 'Expires at'),
                  children: formatDateTime(project?.expiresAt),
                },
              ]}
            />
          </Card>
          {canViewServices ? (
            <Card
              className="soha-detail-card"
              loading={detailServicesQuery.isLoading}
              title={localeText(localeCode, '容器状态', 'Container status')}
              extra={
                readableQueryData(detailServicesQuery) ? (
                  <Space size={6} wrap>
                    <Tag color="blue">
                      {localeText(
                        localeCode,
                        `容器 ${runtimeServicePage.total}`,
                        `Containers ${runtimeServicePage.total}`,
                      )}
                    </Tag>
                    <Tag color="green">
                      {localeText(
                        localeCode,
                        `运行 ${runningServiceCount}`,
                        `Running ${runningServiceCount}`,
                      )}
                    </Tag>
                    <Tag color="orange">
                      {localeText(
                        localeCode,
                        `重启 ${serviceRestartCount}`,
                        `Restarts ${serviceRestartCount}`,
                      )}
                    </Tag>
                  </Space>
                ) : undefined
              }
            >
              <ServicesTable embedded fixedProjectId={resolvedProjectId} />
            </Card>
          ) : null}
        </div>
      ),
    },
    ...(canViewServiceLogs
      ? [
          {
            key: 'logs',
            label: localeText(localeCode, '日志', 'Logs'),
            children: (
              <RuntimeBoundary>
                <DockerProjectLogsPanel
                  enabled={canViewServiceLogs}
                  projectId={resolvedProjectId}
                  projectName={project?.name}
                  serviceName={runtimeServiceName}
                  serviceOptions={runtimeServiceOptions}
                  servicesLoading={detailServicesQuery.isFetching}
                  servicesError={detailServicesQuery.isError}
                  onServiceChange={setRuntimeServiceName}
                />
              </RuntimeBoundary>
            ),
          },
        ]
      : []),
    ...(canAccessServiceTerminal
      ? [
          {
            key: 'terminal',
            label: 'Shell',
            children: (
              <RuntimeBoundary>
                <DockerProjectTerminalPanel
                  enabled={canAccessServiceTerminal}
                  projectId={resolvedProjectId}
                  projectName={project?.name}
                  serviceName={runtimeServiceName}
                  serviceOptions={runtimeServiceOptions}
                  servicesLoading={detailServicesQuery.isFetching}
                  servicesError={detailServicesQuery.isError}
                  onServiceChange={setRuntimeServiceName}
                />
              </RuntimeBoundary>
            ),
          },
        ]
      : []),
    ...(canViewServices && canReadServiceFiles
      ? [
          {
            key: 'volumes',
            label: localeText(localeCode, '卷文件', 'Volume files'),
            children: (
              <RuntimeBoundary>
                <DockerProjectVolumesPanel
                  enabled={canViewServices && canReadServiceFiles}
                  projectId={resolvedProjectId}
                  projectName={project?.name}
                  serviceName={runtimeServiceName}
                  serviceOptions={runtimeServiceOptions}
                  servicesLoading={detailServicesQuery.isFetching}
                  servicesError={detailServicesQuery.isError}
                  onServiceChange={setRuntimeServiceName}
                />
              </RuntimeBoundary>
            ),
          },
        ]
      : []),
    ...(canViewPorts
      ? [
          {
            key: 'ports',
            label: localeText(localeCode, '端口映射', 'Port mappings'),
            children: (
              <div className="soha-page-section">
                <PortsTable fixedProjectId={resolvedProjectId} fixedHostId={project?.hostId} />
              </div>
            ),
          },
        ]
      : []),
    {
      key: 'relations',
      label: localeText(localeCode, '资源关系', 'Relations'),
      children: (
        <Card size="small">
          <ComputeRelationsPanel
            domain="container_runtime"
            kind="project"
            resourceId={resolvedProjectId}
          />
        </Card>
      ),
    },
    runtimeConfigTab,
  ]
  return (
    <div className="soha-page soha-docker-page">
      <ReadFeedback query={projectQuery} label={localeText(localeCode, '项目', 'Project')} />
      <DockerTableHeader
        title={project?.name || localeText(localeCode, '项目详情', 'Project details')}
        status={project?.status}
        meta={[resolvedProjectId, formatDateTime(project?.updatedAt)]}
        actions={
          <ManagementRefreshButton
            tooltip={localeText(localeCode, '刷新', 'Refresh')}
            aria-label={localeText(localeCode, '刷新项目', 'Refresh project')}
            loading={projectQuery.isFetching}
            onClick={() => {
              void projectQuery.refetch()
              if (canViewServices) void detailServicesQuery.refetch()
            }}
          />
        }
      />
      {canViewServices ? (
        <ReadFeedback
          query={detailServicesQuery}
          label={localeText(localeCode, '项目服务', 'Project services')}
        />
      ) : null}
      {!project && projectQuery.isError ? null : !project && !projectQuery.isLoading ? (
        <ManagementState
          kind="not-found"
          title={localeText(localeCode, '未找到项目', 'Project not found')}
        />
      ) : (
        <Tabs className="soha-resource-tabs soha-workload-detail-tabs" items={detailTabItems} />
      )}
    </div>
  )
}

export function DockerProjectDetailPage() {
  return <ProjectDetailWorkspace />
}
