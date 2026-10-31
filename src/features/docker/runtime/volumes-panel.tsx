import { readableQueryData } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'

import { useEffect, useState } from 'react'
import { FileOutlined, FolderOutlined, ReloadOutlined } from '@ant-design/icons'
import { Button, Card, Input, Select, Space, Spin, Typography } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import '@/components/resource-operation-panels.css'
import { useAuthStore } from '@/stores/auth-store'
import { usePermissionSnapshot } from '@/features/auth'
import { VolumeContentPreview } from './volume-content-preview'
import { dockerQueries } from '../queries'
import { DockerContentDLP } from '../content-dlp'
import { DockerDLPActivity } from '../content-dlp-activity'
import { localeText, useI18n } from '@/i18n'
import type { DockerProjectVolumeFileEntry } from '../docker-types'
import { runtimeServiceSelector, type DockerRuntimePanelProps } from './shared'
import './styles.css'

const { Text } = Typography

export function DockerProjectVolumesPanel({
  enabled,
  projectId,
  serviceName,
  serviceOptions,
  servicesLoading,
  servicesError,
  onServiceChange,
}: DockerRuntimePanelProps) {
  const { localeCode } = useI18n()
  const viewer = useAuthStore((s) => s.user?.userId ?? '')
  const permissions = usePermissionSnapshot()
  const permissionScope = JSON.stringify(readableQueryData(permissions)?.data ?? null)
  const [target, setTarget] = useState('')
  const [currentPath, setCurrentPath] = useState('/')
  const [previewPath, setPreviewPath] = useState('')
  const canBrowseRuntime =
    enabled &&
    Boolean(projectId && serviceName) &&
    serviceOptions.some((item) => item.value === serviceName)
  const volumesQuery = useQuery(
    dockerQueries.projectVolumes(projectId, { serviceName }, canBrowseRuntime),
  )
  const volumes = readableQueryData(volumesQuery) ?? []
  const filesQuery = useQuery(
    dockerQueries.projectVolumeFiles(
      projectId,
      { serviceName, target, path: currentPath, limit: 300 },
      canBrowseRuntime && Boolean(target),
    ),
  )

  useEffect(() => {
    setTarget('')
    setCurrentPath('/')
    setPreviewPath('')
  }, [projectId, serviceName])

  useEffect(() => {
    const firstTarget = volumes[0]?.target || ''
    setTarget((current) => current || firstTarget)
  }, [volumes])

  const selectedVolume = volumes.find((item) => item.target === target)
  const entries = readableQueryData(filesQuery)?.items ?? []
  const goParent = () => {
    const clean = currentPath.replace(/\/+$/, '')
    const parent = clean.includes('/') ? clean.slice(0, clean.lastIndexOf('/')) || '/' : '/'
    setCurrentPath(parent)
    setPreviewPath('')
  }
  const openEntry = (entry: DockerProjectVolumeFileEntry) => {
    if (entry.kind === 'directory') {
      setCurrentPath(entry.path || '/')
      setPreviewPath('')
      return
    }
    setPreviewPath(entry.path)
  }

  if (!enabled) {
    return (
      <Card className="soha-detail-card" size="small">
        <DockerDLPActivity projectId={projectId} />
        <ManagementState
          compact
          kind="no-permission"
          title={localeText(localeCode, '卷文件不可用', 'Volume files unavailable')}
          description={localeText(
            localeCode,
            'Docker 模块或当前权限不允许浏览运行时卷文件。',
            'The Docker module or your permissions do not allow browsing runtime volume files.',
          )}
        />
      </Card>
    )
  }

  if (serviceOptions.length === 0) {
    return (
      <Card className="soha-detail-card" size="small">
        <DockerDLPActivity projectId={projectId} />
        <ManagementState
          compact
          kind={servicesLoading ? 'loading' : servicesError ? 'error' : 'empty'}
          title={
            servicesLoading
              ? localeText(localeCode, '正在读取服务', 'Loading services')
              : servicesError
                ? localeText(localeCode, '服务列表读取失败', 'Service list could not be read')
                : localeText(localeCode, '没有可用服务', 'No available services')
          }
          description={
            servicesLoading
              ? undefined
              : localeText(
                  localeCode,
                  servicesError
                    ? '请通过页面上方的“重试读取”刷新项目服务。'
                    : '该项目还没有同步到可用于运行时访问的服务记录。',
                  servicesError
                    ? 'Retry reading project services using the action above.'
                    : 'No service records are available for runtime access yet.',
                )
          }
        />
      </Card>
    )
  }

  return (
    <Card
      className="soha-detail-card"
      size="small"
      title={localeText(localeCode, '卷文件', 'Volume files')}
      extra={
        <Space size={8} wrap>
          {runtimeServiceSelector({
            disabled: !enabled,
            loading: servicesLoading,
            options: serviceOptions,
            serviceName,
            localeCode,
            onChange: onServiceChange,
          })}
          <Select
            disabled={!canBrowseRuntime || volumes.length === 0}
            loading={volumesQuery.isFetching}
            options={volumes.map((volume) => ({ label: volume.target, value: volume.target }))}
            placeholder={localeText(localeCode, '选择卷', 'Select volume')}
            popupMatchSelectWidth={false}
            size="small"
            style={{ minWidth: 180 }}
            value={target || undefined}
            onChange={(value) => {
              setTarget(value)
              setCurrentPath('/')
              setPreviewPath('')
            }}
          />
          <Button
            disabled={!canBrowseRuntime}
            icon={<ReloadOutlined />}
            loading={filesQuery.isFetching || volumesQuery.isFetching}
            size="small"
            onClick={() => {
              volumesQuery.refetch()
              filesQuery.refetch()
            }}
          >
            {localeText(localeCode, '刷新', 'Refresh')}
          </Button>
        </Space>
      }
    >
      <ReadFeedback
        query={volumesQuery}
        label={localeText(localeCode, '卷挂载', 'Volume mounts')}
      />
      {target ? (
        <ReadFeedback
          query={filesQuery}
          label={localeText(localeCode, '卷目录', 'Volume directory')}
        />
      ) : null}
      {!canBrowseRuntime ? (
        <ManagementState
          compact
          kind="empty"
          title={localeText(localeCode, '请选择可用服务', 'Select an available service')}
        />
      ) : volumesQuery.isError && !readableQueryData(volumesQuery) ? null : volumes.length === 0 &&
        !volumesQuery.isFetching ? (
        <ManagementState
          compact
          kind="empty"
          title={localeText(localeCode, '没有可浏览的卷', 'No browsable volumes')}
          description={localeText(
            localeCode,
            '该服务没有声明可从容器内浏览的卷挂载。',
            'This service does not expose a volume mount that can be browsed from the container.',
          )}
        />
      ) : (
        <>
          <div className="soha-docker-volume-toolbar">
            <Button disabled={currentPath === '/'} size="small" onClick={goParent}>
              {localeText(localeCode, '上级', 'Parent')}
            </Button>
            <Input
              size="small"
              value={currentPath}
              onChange={(event) => setCurrentPath(event.target.value || '/')}
            />
            {selectedVolume ? (
              <Space size={6} wrap>
                {selectedVolume.readOnly ? (
                  <MetadataTag label={localeText(localeCode, '只读', 'Read-only')} />
                ) : null}
                {selectedVolume.source ? (
                  <Text type="secondary">{selectedVolume.source}</Text>
                ) : null}
              </Space>
            ) : null}
          </div>
          <div className="soha-docker-volume-browser">
            <div className="soha-docker-volume-list">
              {filesQuery.isFetching && !readableQueryData(filesQuery) ? (
                <div className="soha-docker-volume-list-state">
                  <Spin size="small" />
                </div>
              ) : entries.length === 0 ? (
                <div className="soha-docker-volume-list-state">
                  <Text type="secondary">
                    {localeText(
                      localeCode,
                      filesQuery.isError ? '目录读取失败' : '暂无文件',
                      filesQuery.isError ? 'Directory read failed' : 'No files',
                    )}
                  </Text>
                </div>
              ) : (
                entries.map((entry) => (
                  <button
                    className="soha-docker-volume-file-row"
                    key={entry.path}
                    type="button"
                    onClick={() => openEntry(entry)}
                  >
                    <Space size={8}>
                      {entry.kind === 'directory' ? <FolderOutlined /> : <FileOutlined />}
                      <Text>{entry.name}</Text>
                    </Space>
                    <Text type="secondary">
                      {entry.kind === 'directory' ? '-' : `${entry.sizeBytes ?? 0} B`}
                    </Text>
                  </button>
                ))
              )}
            </div>
            <div className="soha-docker-volume-preview">
              <DockerContentDLP projectId={projectId} />
              <VolumeContentPreview
                key={`${projectId}:${serviceName}:${target}:${previewPath}:${viewer}:${permissionScope}:${canBrowseRuntime}`}
                projectId={projectId}
                viewer={viewer}
                input={{ serviceName, target, path: previewPath, limitBytes: 262144 }}
                enabled={
                  canBrowseRuntime &&
                  Boolean(target && previewPath) &&
                  permissions.isSuccess &&
                  !permissions.isFetching
                }
              />
            </div>
          </div>
        </>
      )}
    </Card>
  )
}
