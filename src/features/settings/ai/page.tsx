import {
  ManagementIconButton,
  ManagementState,
  ManagementToolbarSearch,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  BUILTIN_COMPANION_PLUGIN_ID,
  companionApi,
  companionKeys,
  createLocalLive2DPack,
  getLive2DCubismRuntime,
  LOCAL_LIVE2D_PLUGIN_ID,
  useLocalCompanionPackStore,
} from '@/features/companion'
import { pluginQueries } from '@/features/plugins'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  CheckCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  FolderOpenOutlined,
  LinkOutlined,
  PlusOutlined,
  PoweroffOutlined,
  SaveOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Alert,
  App,
  Button,
  Card,
  Col,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Segmented,
  Select,
  Space,
  Spin,
  Switch,
  Typography,
} from 'antd'
import type { ChangeEvent } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type {
  AISkillSetting,
  AIWorkbenchModelSettings,
  AnalysisProfile,
  DataSource,
} from '../ai-settings-model'
import {
  AI_DATA_SOURCE_BACKENDS,
  buildDataSourceFormValues,
  buildDataSourcePayload,
  buildProfileFormValues,
  buildProfilePayload,
  isRetiredAIDataSource,
  PLAYBOOK_OPTIONS,
} from '../ai-settings-model'
import { settingsMutations } from '../mutations'
import { normalizeWorkbenchModelSettings, settingsQueries } from '../queries'
import {
  DEFAULT_FORM_LAYOUT,
  fullWidthStyle,
  SectionCallout,
  SettingsAdminTable,
  SettingsCard,
  TagSelect,
  WIDE_FORM_LAYOUT,
} from '../shared/components'
import '../shared/styles.css'
import type { AISettingsPageProps } from '../types'
import './styles.css'
import { RuntimeAssetsSettings } from './runtime-assets'
import { RuntimeProfilesSettings } from './runtime-profiles'

const { Paragraph, Text } = Typography

export function AISettingsPage({ embedded = false, section = 'model' }: AISettingsPageProps = {}) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const companionOwner = useAuthStore((state) => state.user?.userId ?? '')
  const companionOwnerRef = useRef(companionOwner)
  companionOwnerRef.current = companionOwner
  const navigate = useNavigate()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [dataSourceModalVisible, setDataSourceModalVisible] = useState(false)
  const [profileModalVisible, setProfileModalVisible] = useState(false)
  const [editingDataSource, setEditingDataSource] = useState<DataSource | null>(null)
  const [editingProfile, setEditingProfile] = useState<AnalysisProfile | null>(null)
  const [skillsModalVisible, setSkillsModalVisible] = useState(false)
  const [editingSkill, setEditingSkill] = useState<AISkillSetting | null>(null)
  const [skillsRegistryDraft, setSkillsRegistryDraft] = useState<AISkillSetting[]>([])
  const [skillsDirty, setSkillsDirty] = useState(false)
  const [skillsFilter, setSkillsFilter] = useState('')
  const [dataSourceForm] = Form.useForm()
  const [dataSourceSourceKind, setDataSourceSourceKind] = useState('logs')
  const canViewAISettings = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'settings.ai.view',
  )
  const canManageAISettings = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'settings.ai.update',
  )
  const canViewDataSources =
    hasPermission(readableQueryData(permissionSnapshotQuery)?.data, 'ai.data-sources.view') ||
    canViewAISettings
  const canCreateDataSources =
    hasPermission(readableQueryData(permissionSnapshotQuery)?.data, 'ai.data-sources.create') ||
    canManageAISettings
  const canUpdateDataSources =
    hasPermission(readableQueryData(permissionSnapshotQuery)?.data, 'ai.data-sources.update') ||
    canManageAISettings
  const canValidateDataSources =
    hasPermission(readableQueryData(permissionSnapshotQuery)?.data, 'ai.data-sources.validate') ||
    canManageAISettings
  const canViewSkills =
    hasPermission(readableQueryData(permissionSnapshotQuery)?.data, 'ai.gateway.skills.view') ||
    canViewAISettings
  const canManageSkills =
    hasPermission(readableQueryData(permissionSnapshotQuery)?.data, 'ai.gateway.skills.update') ||
    canManageAISettings
  const canUseCompanion = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'observe.ai.chat',
  )
  const canViewPlugins = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'plugin.view',
  )
  const showModel = section === 'model'
  const showCompanion = section === 'companion'
  const showSkills = section === 'skills'
  const showDataSources = section === 'data-sources'
  const showProfiles = section === 'profiles'
  const canViewSection = showCompanion
    ? canUseCompanion
    : showSkills
      ? canViewSkills
      : showDataSources
        ? canViewDataSources
        : canViewAISettings
  const companionMode = usePreferencesStore((state) => state.companionMode)
  const companionBubbleEnabled = usePreferencesStore((state) => state.companionBubbleEnabled)
  const selectedCompanionPluginId = usePreferencesStore((state) => state.selectedCompanionPluginId)
  const localCompanionPack = useLocalCompanionPackStore((state) => state.pack)
  const clearLocalCompanionPack = useLocalCompanionPackStore((state) => state.clear)
  const setLocalCompanionPack = useLocalCompanionPackStore((state) => state.setPack)
  const localLive2DInputRef = useRef<HTMLInputElement>(null)
  const [localLive2DImporting, setLocalLive2DImporting] = useState(false)
  const setCompanionMode = usePreferencesStore((state) => state.setCompanionMode)
  const setCompanionBubbleEnabled = usePreferencesStore((state) => state.setCompanionBubbleEnabled)
  const setSelectedCompanionPluginId = usePreferencesStore(
    (state) => state.setSelectedCompanionPluginId,
  )

  const settingsQuery = useQuery(settingsQueries.ai.detail(canViewAISettings && showModel))
  const data = settingsQuery.isEnabled ? readableQueryData(settingsQuery) : undefined
  const skillsQuery = useQuery(settingsQueries.ai.skills(canViewSkills && showSkills))
  const modelRoutesQuery = useQuery(settingsQueries.ai.modelRoutes(canViewAISettings && showModel))
  const dataSourcesQuery = useQuery(
    settingsQueries.ai.dataSources(
      (canViewDataSources && showDataSources) || (canViewAISettings && showProfiles),
    ),
  )
  const profilesQuery = useQuery(
    settingsQueries.ai.analysisProfiles(canViewAISettings && showProfiles),
  )
  const capabilitiesQuery = useQuery(
    settingsQueries.ai.dataSourceCapabilities(canViewDataSources && showDataSources),
  )
  const companionPacksQuery = useQuery(
    pluginQueries.installed(showCompanion && canUseCompanion && canViewPlugins),
  )
  const isPageLoading =
    permissionSnapshotQuery.isLoading ||
    (showModel && settingsQuery.isLoading) ||
    (showSkills && skillsQuery.isLoading) ||
    (showModel && modelRoutesQuery.isLoading) ||
    ((showDataSources || showProfiles) && dataSourcesQuery.isLoading) ||
    (showProfiles && profilesQuery.isLoading) ||
    (showDataSources && capabilitiesQuery.isLoading) ||
    (showCompanion && canViewPlugins && companionPacksQuery.isLoading)
  const resetCompanionMutation = useOperationMutation(
    {
      mutationFn: (input: { owner: string; pluginId: string }) =>
        companionApi.reset({ pluginId: input.pluginId }),
      onSuccess: (profile, input) => {
        if (companionOwnerRef.current !== input.owner) return
        queryClient.setQueryData(companionKeys.profile(input.owner), profile)
      },
    },
    {
      label: ['重置 Companion 成长', 'Reset Companion growth'],
      target: (input) => input.pluginId,
    },
  )

  const importLocalLive2D = async (event: ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files
    event.target.value = ''
    if (!files?.length) return
    setLocalLive2DImporting(true)
    try {
      const pack = await createLocalLive2DPack(files)
      setLocalCompanionPack(pack)
      setCompanionMode('companion')
      void message.success(`本地模型已校验：${pack.manifest.entryAsset}`)
    } catch (error) {
      void message.error(error instanceof Error ? error.message : '本地 Live2D 模型导入失败')
    } finally {
      setLocalLive2DImporting(false)
    }
  }

  const saveWorkbenchModelMutation = useOperationMutation(
    settingsMutations.ai.saveWorkbenchModel(queryClient),
    {
      label: ['保存 Workbench 默认模型', 'Save Workbench model'],
      target: () => 'Workbench 默认模型',
    },
  )
  const saveSkillsMutation = useOperationMutation(settingsMutations.ai.saveSkills(queryClient), {
    label: ['保存 Skills registry', 'Save Skills registry'],
    target: () => 'Skills registry',
  })
  const dataSourceMutation = useOperationMutation(
    settingsMutations.ai.upsertDataSource(queryClient),
    {
      label: ['保存 AI 数据源', 'Save AI data source'],
      target: (input) => String(input.id || input.values.name || '新数据源'),
    },
  )
  const validateDataSourceMutation = useOperationMutation(
    settingsMutations.ai.validateDataSource(queryClient),
    { label: ['校验 AI 数据源', 'Validate AI data source'], target: (id) => id, readOnly: true },
  )
  const profileMutation = useOperationMutation(
    settingsMutations.ai.upsertAnalysisProfile(queryClient),
    {
      label: ['保存分析模板', 'Save analysis profile'],
      target: (input) => String(input.id || input.values.name || '新分析模板'),
    },
  )

  const blocked =
    saveWorkbenchModelMutation.blocked ||
    saveSkillsMutation.blocked ||
    dataSourceMutation.blocked ||
    profileMutation.blocked ||
    resetCompanionMutation.blocked ||
    validateDataSourceMutation.isPending
  const dataSourceDenied =
    queryAccessDenied(dataSourcesQuery) ||
    queryAccessDenied(dataSourceMutation) ||
    !(editingDataSource ? canUpdateDataSources : canCreateDataSources)
  const profileDenied =
    queryAccessDenied(profilesQuery) || queryAccessDenied(profileMutation) || !canManageAISettings
  const skillsDenied = queryAccessDenied(skillsQuery) || !canViewSkills
  useEffect(() => {
    if (dataSourceDenied) {
      setDataSourceModalVisible(false)
      setEditingDataSource(null)
    }
  }, [dataSourceDenied])
  useEffect(() => {
    if (profileDenied) {
      setProfileModalVisible(false)
      setEditingProfile(null)
    }
  }, [profileDenied])
  useEffect(() => {
    if (skillsDenied) {
      setSkillsModalVisible(false)
      setEditingSkill(null)
      setSkillsRegistryDraft([])
      setSkillsDirty(false)
    }
  }, [skillsDenied])
  const saveWorkbenchModel = (values: AIWorkbenchModelSettings) =>
    saveWorkbenchModelMutation.mutate(values, {})
  const saveSkills = () =>
    saveSkillsMutation.mutate(skillsRegistryDraft, {
      onSuccess: () => setSkillsDirty(false),
    })
  const saveDataSource = (input: { id?: string; values: Record<string, unknown> }) =>
    dataSourceMutation.mutate(
      { ...input, values: buildDataSourcePayload(input.values) },
      {
        onSuccess: () => {
          setDataSourceModalVisible(false)
          setEditingDataSource(null)
        },
      },
    )
  const validateDataSource = (dataSourceID: string) =>
    validateDataSourceMutation.mutate(dataSourceID)
  const saveProfile = (input: { id?: string; values: Record<string, unknown> }) =>
    profileMutation.mutate(
      { ...input, values: buildProfilePayload(input.values) },
      {
        onSuccess: () => {
          setProfileModalVisible(false)
          setEditingProfile(null)
        },
      },
    )
  const settings = data
  const modelRoutes = readableQueryData(modelRoutesQuery) ?? []
  const enabledModelRoutes = modelRoutes.filter((route) => route.enabled)
  const routeOptions = enabledModelRoutes.map((route) => ({
    value: route.id,
    label: `${route.publicModel} / ${route.id}`,
  }))
  const publicModelOptions = [
    ...new Map(
      enabledModelRoutes
        .filter((route) => route.publicModel)
        .map((route) => [
          route.publicModel,
          { value: route.publicModel, label: route.publicModel },
        ]),
    ).values(),
  ]
  const selectedRoute = enabledModelRoutes.find(
    (route) => route.id === settings?.workbenchModel?.defaultRouteId,
  )

  const loadedSkills = readableQueryData(skillsQuery)
  useEffect(() => {
    if (!showSkills || !loadedSkills || skillsDirty) return
    setSkillsRegistryDraft(
      loadedSkills.map((item) => ({
        id: item.id,
        name: item.name,
        category: item.category,
        ownerModule: item.ownerModule,
        description: item.description,
        enabled: item.enabled,
        scopes: item.scopes ?? [],
        capabilityRefs: item.capabilityRefs ?? [],
        blueprintRefs: item.blueprintRefs ?? [],
        scopeRules: item.scopeRules ?? [],
        inputSchema: item.inputSchema ?? {},
        outputSchema: item.outputSchema ?? {},
      })),
    )
  }, [showSkills, loadedSkills, skillsDirty])

  const visibleSkills = useMemo(() => {
    const keyword = skillsFilter.trim().toLowerCase()
    if (!keyword) return skillsRegistryDraft
    return skillsRegistryDraft.filter((item) =>
      [item.id, item.name, item.category, item.ownerModule, item.description]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword)),
    )
  }, [skillsFilter, skillsRegistryDraft])

  if (isPageLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }

  if (!canViewSection) {
    return (
      <div className="soha-page">
        <ManagementState
          kind="no-permission"
          description={
            showCompanion
              ? '当前账号没有使用 Companion 的权限。'
              : showSkills
                ? '当前账号没有查看 Skills 的权限。'
                : showDataSources
                  ? '当前账号没有查看 Data Sources 的权限。'
                  : '当前账号没有查看 AI 设置的权限。'
          }
        />
      </div>
    )
  }

  const dataSources = readableQueryData(dataSourcesQuery) ?? []
  const profiles = readableQueryData(profilesQuery) ?? []
  const capabilityOptions = readableQueryData(capabilitiesQuery) ?? []
  const dataSourceBackendType = AI_DATA_SOURCE_BACKENDS.get(dataSourceSourceKind)
  const filteredCapabilityOptions = capabilityOptions.filter(
    (item) =>
      item.sourceKind === dataSourceSourceKind &&
      (!item.supportedBackends?.length ||
        (dataSourceBackendType && item.supportedBackends.includes(dataSourceBackendType))),
  )

  const dataSourceColumns: TableColumnsType<DataSource> = [
    { title: '名称', dataIndex: 'name' },
    { title: '能力层', dataIndex: 'mcpAdapter' },
    {
      title: '源类型',
      dataIndex: 'sourceKind',
      render: (value: string, record: DataSource) => `${value} / ${record.backendType}`,
    },
    {
      title: '校验状态',
      dataIndex: 'validationStatus',
      render: (value: string | undefined, record: DataSource) => {
        if (isRetiredAIDataSource(record)) {
          return <StatusTag value="unsupported" label="后端已退役" />
        }
        const isPending =
          validateDataSourceMutation.isPending && validateDataSourceMutation.variables === record.id
        if (isPending) return <StatusTag value="checking" label="校验中" />
        if (!value) return <StatusTag value="unknown" label="未校验" />
        const normalized = value.toLowerCase()
        const label = normalized === 'success' ? '已通过' : normalized === 'error' ? '失败' : value
        return (
          <div className="flex max-w-[240px] flex-col gap-1">
            <StatusTag value={normalized} label={label} />
            {record.validationMessage && normalized === 'error' ? (
              <div className="text-xs text-[var(--ant-colorTextSecondary)]">
                {record.validationMessage}
              </div>
            ) : null}
          </div>
        )
      },
    },
    {
      title: '最近校验',
      dataIndex: 'lastValidatedAt',
      render: (value: string | undefined) => (value ? formatDateTime(value) : '-'),
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <StatusTag value={value ? 'success' : 'default'} />,
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: DataSource) => {
        if (isRetiredAIDataSource(record)) {
          if (!canUpdateDataSources || !record.enabled) return '-'
          return (
            <Popconfirm
              title={`停用 AI 数据源 ${record.name}？`}
              okText="停用"
              cancelText="取消"
              disabled={blocked}
              onConfirm={() => {
                const { name, sourceKind, backendType } = record
                dataSourceMutation.mutate({
                  id: record.id,
                  values: { name, sourceKind, backendType, enabled: false },
                })
              }}
            >
              <ManagementIconButton
                aria-label="停用旧 AI 数据源"
                disabled={blocked}
                icon={<PoweroffOutlined />}
                size="small"
                tooltip="停用"
              />
            </Popconfirm>
          )
        }
        if (AI_DATA_SOURCE_BACKENDS.get(record.sourceKind) !== record.backendType) return '-'
        return canValidateDataSources || canUpdateDataSources ? (
          <Space className="soha-row-action-icons">
            {canValidateDataSources ? (
              <ManagementIconButton
                disabled={blocked}
                aria-label="校验数据源连接"
                tooltip="校验连接"
                icon={<CheckCircleOutlined />}
                loading={
                  validateDataSourceMutation.isPending &&
                  validateDataSourceMutation.variables === record.id
                }
                size="small"
                onClick={() => validateDataSource(record.id)}
              />
            ) : null}
            {canUpdateDataSources ? (
              <ManagementIconButton
                disabled={blocked}
                aria-label="编辑数据源"
                tooltip="编辑"
                icon={<EditOutlined />}
                size="small"
                onClick={() => {
                  setEditingDataSource(record)
                  setDataSourceSourceKind(record.sourceKind)
                  dataSourceMutation.reset()
                  setDataSourceModalVisible(true)
                }}
              />
            ) : null}
          </Space>
        ) : (
          '-'
        )
      },
    },
  ]

  const profileColumns: TableColumnsType<AnalysisProfile> = [
    { title: '名称', dataIndex: 'name' },
    { title: '模式', dataIndex: 'mode' },
    {
      title: '数据源',
      dataIndex: 'enabledSources',
      render: (value: string[]) => (
        <div className="flex flex-wrap gap-1">
          {(value ?? []).map((item) => (
            <MetadataTag key={item} label={item} />
          ))}
        </div>
      ),
    },
    {
      title: 'Playbooks',
      dataIndex: 'enabledPlaybooks',
      render: (value: string[]) => (
        <div className="flex flex-wrap gap-1">
          {(value ?? []).map((item) => (
            <MetadataTag key={item} label={item} />
          ))}
        </div>
      ),
    },
    { title: '策略', dataIndex: 'remediationPolicy' },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: unknown, record: AnalysisProfile) =>
        canManageAISettings ? (
          <ManagementIconButton
            disabled={blocked}
            aria-label="编辑分析模板"
            tooltip="编辑"
            icon={<EditOutlined />}
            size="small"
            onClick={() => {
              setEditingProfile(record)
              profileMutation.reset()
              setProfileModalVisible(true)
            }}
          />
        ) : (
          '-'
        ),
    },
  ]

  const workbenchModelCard = (
    <section data-testid="ai-workbench-model-section" className="soha-settings-table-section">
      <SettingsCard
        title="Workbench 默认模型"
        extra={
          <Space>
            <Button
              icon={<LinkOutlined />}
              onClick={() => navigate('/ai-gateway/relay?tab=upstreams')}
            >
              上游管理
            </Button>
            <Button
              icon={<LinkOutlined />}
              onClick={() => navigate('/ai-gateway/relay?tab=model-routes')}
            >
              模型路由
            </Button>
          </Space>
        }
      >
        <Alert
          showIcon
          type="info"
          style={{ marginBottom: 16 }}
          title="模型 Provider 在 AI Gateway 管理。这里仅选择 AI Workbench 使用的默认模型与路由。"
        />
        <Form
          disabled={blocked}
          data-testid="ai-workbench-model-form"
          {...WIDE_FORM_LAYOUT}
          initialValues={normalizeWorkbenchModelSettings(settings?.workbenchModel)}
          onFinish={(values) => {
            if (!canManageAISettings) return
            saveWorkbenchModel(normalizeWorkbenchModelSettings(values))
          }}
        >
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item name="defaultPublicModel" label="默认 public model">
                <Select
                  allowClear
                  showSearch={{ optionFilterProp: 'label' }}
                  loading={modelRoutesQuery.isLoading}
                  options={publicModelOptions}
                  placeholder="从 Gateway model routes 选择"
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="defaultRouteId" label="默认 route">
                <Select
                  allowClear
                  showSearch={{ optionFilterProp: 'label' }}
                  loading={modelRoutesQuery.isLoading}
                  options={routeOptions}
                  placeholder="优先使用稳定 route id"
                />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item name="defaultEndpoint" label="默认 endpoint">
                <Select
                  options={[
                    { value: 'chat/completions', label: 'chat/completions' },
                    { value: 'responses', label: 'responses' },
                    { value: 'messages', label: 'messages' },
                  ]}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="enabled" label="启用 Workbench 模型" valuePropName="checked">
                <Switch disabled={blocked} />
              </Form.Item>
            </Col>
          </Row>
          <div className="mb-4 flex flex-wrap gap-2">
            <MetadataTag
              label={selectedRoute ? `route: ${selectedRoute.id}` : '未选择 route'}
              tone={selectedRoute ? 'cyan' : 'default'}
            />
            <MetadataTag
              label={settings?.workbenchModel?.defaultPublicModel || '未选择 public model'}
              tone={settings?.workbenchModel?.defaultPublicModel ? 'blue' : 'default'}
            />
            <MetadataTag label={`${enabledModelRoutes.length} active routes`} tone="purple" />
          </div>
          {canManageAISettings ? (
            <div className="soha-form-actions">
              <Button
                disabled={blocked}
                htmlType="submit"
                type="primary"
                loading={saveWorkbenchModelMutation.isPending}
              >
                保存默认模型
              </Button>
            </div>
          ) : null}
        </Form>
      </SettingsCard>
    </section>
  )

  const companionPackOptions = [
    { value: BUILTIN_COMPANION_PLUGIN_ID, label: 'Soha Orbit' },
    ...(localCompanionPack
      ? [
          {
            value: LOCAL_LIVE2D_PLUGIN_ID,
            label: `本地 · ${localCompanionPack.manifest.entryAsset.split('/').slice(-1)[0]}`,
          },
        ]
      : []),
    ...(Array.isArray(readableQueryData(companionPacksQuery))
      ? (readableQueryData(companionPacksQuery) ?? [])
      : []
    )
      .filter(
        (item) =>
          item.type === 'companion-pack' &&
          item.status === 'enabled' &&
          Boolean(item.manifest.companionPack),
      )
      .map((item) => ({
        value: item.id,
        label: `${item.name} (${item.activeVersion || item.version})`,
      })),
  ]

  const companionCard = (
    <section data-testid="ai-companion-section" className="soha-settings-table-section">
      <SettingsCard title="桌面宠物">
        <div className="soha-companion-settings-grid">
          <label>
            <span>悬浮模式</span>
            <Segmented
              disabled={blocked}
              options={[
                { value: 'companion', label: '宠物' },
                { value: 'icon', label: '图标' },
              ]}
              value={companionMode}
              onChange={(value) => setCompanionMode(value as 'companion' | 'icon')}
            />
          </label>
          <label>
            <span>当前模型</span>
            <Select
              loading={companionPacksQuery.isLoading}
              options={companionPackOptions}
              value={localCompanionPack ? LOCAL_LIVE2D_PLUGIN_ID : selectedCompanionPluginId}
              onChange={(pluginId) => {
                clearLocalCompanionPack()
                setSelectedCompanionPluginId(pluginId)
              }}
            />
          </label>
          <div className="soha-companion-local-import">
            <span>本地 Live2D</span>
            <div className="soha-companion-local-import__actions">
              <Button
                disabled={blocked}
                icon={<FolderOpenOutlined />}
                loading={localLive2DImporting}
                onClick={() => localLive2DInputRef.current?.click()}
              >
                导入模型目录
              </Button>
              {localCompanionPack ? (
                <Button disabled={blocked} onClick={clearLocalCompanionPack}>
                  清除
                </Button>
              ) : null}
            </div>
            <input
              ref={localLive2DInputRef}
              hidden
              multiple
              type="file"
              onChange={(event) => void importLocalLive2D(event)}
              {...({ webkitdirectory: '' } as { webkitdirectory: string })}
            />
          </div>
          <label>
            <span>对话气泡</span>
            <Switch
              disabled={blocked}
              checked={companionBubbleEnabled}
              onChange={setCompanionBubbleEnabled}
            />
          </label>
          <Popconfirm
            disabled={blocked}
            title="重置宠物成长数据？"
            okText="重置"
            cancelText="取消"
            onConfirm={() =>
              resetCompanionMutation.mutate({
                owner: companionOwner,
                pluginId: selectedCompanionPluginId,
              })
            }
          >
            <Button
              danger
              disabled={blocked || Boolean(localCompanionPack)}
              loading={resetCompanionMutation.isPending}
            >
              重置成长数据
            </Button>
          </Popconfirm>
          {localCompanionPack ? (
            <Alert
              className="soha-companion-local-import__status"
              description={
                getLive2DCubismRuntime()
                  ? '模型只保存在当前页面会话，不会上传到服务器。'
                  : '模型资源校验已通过，但当前宿主没有 Cubism runtime，悬浮窗会暂时显示 Soha Orbit。'
              }
              showIcon
              title="本地模型仅用于测试；模型与 Cubism Core 授权由使用者负责。"
              type={getLive2DCubismRuntime() ? 'info' : 'warning'}
            />
          ) : null}
        </div>
      </SettingsCard>
    </section>
  )

  const content = (
    <>
      <ReadFeedback label="AI 默认模型" query={settingsQuery} canRead={settingsQuery.isEnabled} />
      <ReadFeedback label="Skills registry" query={skillsQuery} canRead={skillsQuery.isEnabled} />
      <ReadFeedback
        label="模型路由选项"
        query={modelRoutesQuery}
        canRead={modelRoutesQuery.isEnabled}
      />
      <ReadFeedback
        label="AI 数据源"
        query={dataSourcesQuery}
        canRead={dataSourcesQuery.isEnabled}
      />
      <ReadFeedback label="分析模板" query={profilesQuery} canRead={profilesQuery.isEnabled} />
      <ReadFeedback
        label="数据源能力"
        query={capabilitiesQuery}
        canRead={capabilitiesQuery.isEnabled}
      />
      <ReadFeedback
        label="Companion 资源包"
        query={companionPacksQuery}
        canRead={companionPacksQuery.isEnabled}
      />
      {saveWorkbenchModelMutation.feedback}
      {saveSkillsMutation.feedback}
      {validateDataSourceMutation.feedback}
      {resetCompanionMutation.feedback}
      {!dataSourceModalVisible ? dataSourceMutation.feedback : null}
      {!profileModalVisible ? profileMutation.feedback : null}
      {showModel ? workbenchModelCard : null}
      {showModel && canViewAISettings && companionOwner && (
        <RuntimeAssetsSettings
          key={`runtime-assets:${companionOwner}`}
          owner={companionOwner}
          canManage={canManageAISettings}
        />
      )}
      {showModel && canViewAISettings && (
        <RuntimeProfilesSettings
          key={`runtime-profiles:${companionOwner}`}
          owner={companionOwner}
          canManage={canManageAISettings}
          publicModels={enabledModelRoutes.map((route) => route.publicModel)}
        />
      )}
      {showCompanion ? companionCard : null}
      {showSkills ? (
        <section className="soha-skill-registry">
          <Flex className="soha-skill-registry__toolbar" align="center" justify="space-between">
            <ManagementToolbarSearch
              placeholder="搜索 Skills"
              size={280}
              value={skillsFilter}
              onChange={setSkillsFilter}
            />
            {canManageSkills ? (
              <Space size={8}>
                <Button
                  disabled={blocked}
                  icon={<PlusOutlined />}
                  size="small"
                  onClick={() => {
                    setEditingSkill(null)
                    setSkillsModalVisible(true)
                  }}
                >
                  新增
                </Button>
                <Button
                  disabled={blocked}
                  type="primary"
                  icon={<SaveOutlined />}
                  size="small"
                  loading={saveSkillsMutation.isPending}
                  onClick={() => saveSkills()}
                >
                  保存 Skills
                </Button>
              </Space>
            ) : null}
          </Flex>

          {visibleSkills.length === 0 ? (
            <ManagementState
              compact
              title={skillsRegistryDraft.length === 0 ? '暂无全局 Skills' : '没有匹配的 Skills'}
            />
          ) : (
            <div className="soha-skill-card-list" role="list">
              {visibleSkills.map((record) => (
                <Card className="soha-skill-card" key={record.id} role="listitem" size="small">
                  <div className="soha-skill-card__identity">
                    <Flex align="center" gap={8} wrap>
                      <Text strong>{record.name}</Text>
                      <StatusTag value={record.enabled ? 'enabled' : 'disabled'} />
                    </Flex>
                    <Text type="secondary" copyable={{ text: record.id }}>
                      {record.id}
                    </Text>
                  </div>

                  <div className="soha-skill-card__content">
                    <Paragraph type="secondary" ellipsis={{ rows: 2 }}>
                      {record.description || '暂无说明'}
                    </Paragraph>
                    <Flex gap={6} wrap>
                      {record.category ? <MetadataTag label={record.category} /> : null}
                      {record.ownerModule ? <MetadataTag label={record.ownerModule} /> : null}
                      {(record.scopes ?? []).map((item) => (
                        <MetadataTag key={`scope:${item}`} label={item} />
                      ))}
                      {(record.capabilityRefs ?? []).slice(0, 3).map((item) => (
                        <MetadataTag key={`capability:${item}`} tone="blue" label={item} />
                      ))}
                    </Flex>
                  </div>

                  {canManageSkills ? (
                    <Space className="soha-skill-card__actions" size={2}>
                      <ManagementIconButton
                        aria-label="上移 Skill"
                        tooltip="上移"
                        icon={<ArrowUpOutlined />}
                        size="small"
                        disabled={blocked || skillsRegistryDraft[0]?.id === record.id}
                        onClick={() => {
                          setSkillsDirty(true)
                          setSkillsRegistryDraft((current) => {
                            const index = current.findIndex((item) => item.id === record.id)
                            if (index <= 0) return current
                            const next = [...current]
                            ;[next[index - 1], next[index]] = [next[index], next[index - 1]]
                            return next
                          })
                        }}
                      />
                      <ManagementIconButton
                        aria-label="下移 Skill"
                        tooltip="下移"
                        icon={<ArrowDownOutlined />}
                        size="small"
                        disabled={
                          blocked ||
                          skillsRegistryDraft[skillsRegistryDraft.length - 1]?.id === record.id
                        }
                        onClick={() => {
                          setSkillsDirty(true)
                          setSkillsRegistryDraft((current) => {
                            const index = current.findIndex((item) => item.id === record.id)
                            if (index < 0 || index >= current.length - 1) return current
                            const next = [...current]
                            ;[next[index], next[index + 1]] = [next[index + 1], next[index]]
                            return next
                          })
                        }}
                      />
                      <ManagementIconButton
                        disabled={blocked}
                        aria-label="编辑 Skill"
                        tooltip="编辑"
                        icon={<EditOutlined />}
                        size="small"
                        onClick={() => {
                          setEditingSkill(record)
                          setSkillsModalVisible(true)
                        }}
                      />
                      <Popconfirm
                        disabled={blocked}
                        title="确认删除 Skill？"
                        description="保存设置后生效。"
                        okButtonProps={{ danger: true }}
                        onConfirm={() => {
                          setSkillsDirty(true)
                          setSkillsRegistryDraft((current) =>
                            current.filter((item) => item.id !== record.id),
                          )
                        }}
                      >
                        <ManagementIconButton
                          disabled={blocked}
                          aria-label="删除 Skill"
                          tooltip="删除"
                          danger
                          icon={<DeleteOutlined />}
                          size="small"
                        />
                      </Popconfirm>
                    </Space>
                  ) : null}
                </Card>
              ))}
            </div>
          )}
        </section>
      ) : null}
      {showDataSources ? (
        <div className="soha-settings-table-section">
          <SettingsAdminTable
            headerExtra={
              canCreateDataSources ? (
                <Button
                  disabled={blocked}
                  type="primary"
                  onClick={() => {
                    setEditingDataSource(null)
                    setDataSourceSourceKind('logs')
                    dataSourceMutation.reset()
                    setDataSourceModalVisible(true)
                  }}
                >
                  新增
                </Button>
              ) : null
            }
            columns={dataSourceColumns}
            dataSource={dataSources}
            rowKey="id"
            loading={dataSourcesQuery.isLoading}
          />
        </div>
      ) : null}
      {showProfiles ? (
        <div className="soha-settings-table-section">
          <SettingsAdminTable
            headerExtra={
              canManageAISettings ? (
                <Button
                  disabled={blocked}
                  type="primary"
                  onClick={() => {
                    setEditingProfile(null)
                    profileMutation.reset()
                    setProfileModalVisible(true)
                  }}
                >
                  新增
                </Button>
              ) : null
            }
            columns={profileColumns}
            dataSource={profiles}
            rowKey="id"
            loading={profilesQuery.isLoading}
          />
        </div>
      ) : null}
      {showSkills ? (
        <Modal
          title={editingSkill ? '编辑 Skill' : '新增 Skill'}
          open={skillsModalVisible && !skillsDenied}
          closable={!blocked}
          keyboard={!blocked}
          mask={{ closable: !blocked }}
          footer={null}
          onCancel={() => {
            if (blocked) return
            setSkillsModalVisible(false)
            setEditingSkill(null)
          }}
          destroyOnHidden
        >
          <Form
            disabled={blocked}
            {...DEFAULT_FORM_LAYOUT}
            initialValues={{
              id: editingSkill?.id ?? '',
              name: editingSkill?.name ?? '',
              category: editingSkill?.category ?? '',
              ownerModule: editingSkill?.ownerModule ?? '',
              description: editingSkill?.description ?? '',
              enabled: editingSkill?.enabled ?? true,
              scopes: editingSkill?.scopes ?? [],
              capabilityRefs: editingSkill?.capabilityRefs ?? [],
              blueprintRefs: editingSkill?.blueprintRefs ?? [],
              scopeRules: editingSkill?.scopeRules ?? [],
              inputSchemaText: JSON.stringify(editingSkill?.inputSchema ?? {}, null, 2),
              outputSchemaText: JSON.stringify(editingSkill?.outputSchema ?? {}, null, 2),
            }}
            onFinish={(values) => {
              let inputSchema: Record<string, unknown>
              let outputSchema: Record<string, unknown>
              try {
                inputSchema = values.inputSchemaText
                  ? JSON.parse(String(values.inputSchemaText))
                  : {}
                outputSchema = values.outputSchemaText
                  ? JSON.parse(String(values.outputSchemaText))
                  : {}
              } catch {
                void message.error('Input/Output Schema 需要是合法 JSON')
                return
              }
              const next: AISkillSetting = {
                id: String(values.id ?? '').trim(),
                name: String(values.name ?? '').trim(),
                category: String(values.category ?? '').trim(),
                ownerModule: String(values.ownerModule ?? '').trim(),
                description: String(values.description ?? '').trim(),
                enabled: Boolean(values.enabled),
                scopes: Array.isArray(values.scopes) ? (values.scopes as string[]) : [],
                capabilityRefs: Array.isArray(values.capabilityRefs)
                  ? (values.capabilityRefs as string[])
                  : [],
                blueprintRefs: Array.isArray(values.blueprintRefs)
                  ? (values.blueprintRefs as string[])
                  : [],
                scopeRules: Array.isArray(values.scopeRules) ? (values.scopeRules as string[]) : [],
                inputSchema,
                outputSchema,
              }
              if (!next.id || !next.name) {
                void message.error('Skill ID 和名称不能为空')
                return
              }
              const duplicate = skillsRegistryDraft.find(
                (item) => item.id === next.id && item.id !== editingSkill?.id,
              )
              if (duplicate) {
                void message.error(`Skill ID 已存在: ${next.id}`)
                return
              }
              setSkillsDirty(true)
              setSkillsRegistryDraft((current) => {
                const rest = current.filter((item) => item.id !== next.id)
                return [...rest, next]
              })
              setSkillsModalVisible(false)
              setEditingSkill(null)
            }}
          >
            <Form.Item name="id" label="ID" rules={[{ required: true, message: '请输入 ID' }]}>
              <Input />
            </Form.Item>
            <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
              <Input />
            </Form.Item>
            <Form.Item name="category" label="分类">
              <Input placeholder="delivery / observability / platform" />
            </Form.Item>
            <Form.Item name="ownerModule" label="归属模块">
              <Input placeholder="delivery / ai / monitoring" />
            </Form.Item>
            <Form.Item name="description" label="说明">
              <Input.TextArea rows={3} />
            </Form.Item>
            <Form.Item name="scopes" label="作用域">
              <TagSelect mode="tags" />
            </Form.Item>
            <Form.Item name="capabilityRefs" label="能力引用">
              <TagSelect mode="tags" />
            </Form.Item>
            <Form.Item name="blueprintRefs" label="蓝图引用">
              <TagSelect mode="tags" />
            </Form.Item>
            <Form.Item name="scopeRules" label="范围规则">
              <TagSelect mode="tags" />
            </Form.Item>
            <Form.Item name="inputSchemaText" label="Input Schema(JSON)">
              <Input.TextArea rows={4} spellCheck={false} />
            </Form.Item>
            <Form.Item name="outputSchemaText" label="Output Schema(JSON)">
              <Input.TextArea rows={4} spellCheck={false} />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch disabled={blocked} />
            </Form.Item>
            <div className="text-sm text-[var(--ant-colorTextSecondary)]">
              ID 需要在全局 registry 中唯一；作用域用于提示这个 skill
              主要服务于哪些工作区或资源，不直接替代权限判断。
            </div>
            <div className="soha-form-actions">
              <Button
                disabled={blocked}
                onClick={() => {
                  setSkillsModalVisible(false)
                  setEditingSkill(null)
                }}
              >
                取消
              </Button>
              <Button disabled={blocked} htmlType="submit" type="primary">
                保存
              </Button>
            </div>
          </Form>
        </Modal>
      ) : null}

      {showDataSources ? (
        <Modal
          title={editingDataSource ? '编辑数据源' : '新增数据源'}
          open={dataSourceModalVisible && !dataSourceDenied}
          closable={!blocked}
          keyboard={!blocked}
          mask={{ closable: !blocked }}
          footer={null}
          onCancel={() => {
            if (blocked) return
            setDataSourceModalVisible(false)
            setEditingDataSource(null)
            setDataSourceSourceKind('logs')
          }}
          destroyOnHidden
        >
          {dataSourceMutation.feedback}
          <Form
            clearOnDestroy
            disabled={blocked}
            form={dataSourceForm}
            {...DEFAULT_FORM_LAYOUT}
            initialValues={buildDataSourceFormValues(editingDataSource)}
            onFinish={(values) => {
              if (editingDataSource ? !canUpdateDataSources : !canCreateDataSources) return
              if (AI_DATA_SOURCE_BACKENDS.get(values.sourceKind) !== values.backendType) return
              saveDataSource({
                id: editingDataSource?.id,
                values: values as Record<string, unknown>,
              })
            }}
          >
            <Form.Item name="id" hidden>
              <Input />
            </Form.Item>
            <SectionCallout
              title="1. 基础信息"
              description="先选择数据源的能力类别和后端类型，再填写连接与查询约束。"
            />
            <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
              <Input />
            </Form.Item>
            <Form.Item name="sourceKind" label="源类型">
              <Select
                options={[...AI_DATA_SOURCE_BACKENDS.keys()].map((value) => ({ value, label: value }))}
                onChange={(value: string) => {
                  setDataSourceSourceKind(value)
                  dataSourceForm.setFieldsValue({
                    backendType: AI_DATA_SOURCE_BACKENDS.get(value),
                    mcpAdapter: `${value}.v1`,
                  })
                }}
              />
            </Form.Item>
            <Form.Item name="backendType" label="后端类型">
              <Select
                options={
                  dataSourceBackendType
                    ? [{ value: dataSourceBackendType, label: dataSourceBackendType }]
                    : []
                }
              />
            </Form.Item>
            <Form.Item name="mcpAdapter" label="能力层">
              <Select
                options={filteredCapabilityOptions.map((item) => ({
                  value: item.id,
                  label: item.name,
                }))}
              />
            </Form.Item>
            <Form.Item name="credentialRef" label="凭据引用">
              <Input />
            </Form.Item>
            <SectionCallout
              title="2. 作用范围与预算"
              description="限制这个数据源在 AI 分析中的默认作用范围、查询次数和输出规模。"
            />
            <Form.Item name="scopeClusterId" label="Scope Cluster">
              <Input />
            </Form.Item>
            <Form.Item name="scopeNamespace" label="Scope Namespace">
              <Input />
            </Form.Item>
            <Form.Item name="scopeService" label="Scope Service">
              <Input />
            </Form.Item>
            <Form.Item name="scopeWorkload" label="Scope Workload">
              <Input />
            </Form.Item>
            <Form.Item name="budgetMaxQueries" label="Max Queries">
              <InputNumber min={1} style={fullWidthStyle} />
            </Form.Item>
            <Form.Item name="budgetMaxLogBytes" label="Max Log Bytes">
              <InputNumber min={1024} style={fullWidthStyle} />
            </Form.Item>
            <Form.Item name="budgetTimeoutSeconds" label="Timeout(s)">
              <InputNumber min={1} style={fullWidthStyle} />
            </Form.Item>
            <Form.Item name="redactionMaskFields" label="Mask Fields">
              <TagSelect mode="tags" />
            </Form.Item>
            <Form.Item name="redactionMaskPatterns" label="Mask Patterns">
              <TagSelect mode="tags" />
            </Form.Item>
            <Form.Item
              name="redactionTruncateLongLines"
              label="Truncate Long Lines"
              valuePropName="checked"
            >
              <Switch disabled={blocked} />
            </Form.Item>
            <SectionCallout
              title="3. 后端连接"
              description="这里只展示当前后端类型需要的关键字段，避免无关配置干扰。"
            />
            <Form.Item
              name="configEndpoint"
              label="Endpoint"
              rules={[{ required: true, message: '请输入 Endpoint' }]}
            >
              <Input />
            </Form.Item>
            {dataSourceBackendType !== 'platform' ? (
              <Form.Item name="configBearerToken" label="Bearer Token">
                <Input.Password />
              </Form.Item>
            ) : null}
            {dataSourceBackendType === 'loki' ? (
              <Form.Item
                name="lokiLabelCluster"
                label="Loki Cluster Label"
                rules={[{ required: true, message: '请输入 Loki Cluster Label' }]}
              >
                <Input />
              </Form.Item>
            ) : null}
            {dataSourceBackendType === 'loki' ? (
              <Form.Item
                name="lokiLabelNamespace"
                label="Loki Namespace Label"
                rules={[{ required: true, message: '请输入 Loki Namespace Label' }]}
              >
                <Input />
              </Form.Item>
            ) : null}
            {dataSourceBackendType === 'loki' ? (
              <Form.Item
                name="lokiLabelService"
                label="Loki Service Label"
                rules={[{ required: true, message: '请输入 Loki Service Label' }]}
              >
                <Input />
              </Form.Item>
            ) : null}
            {dataSourceBackendType === 'loki' ? (
              <Form.Item
                name="lokiLabelWorkload"
                label="Loki Workload Label"
                rules={[{ required: true, message: '请输入 Loki Workload Label' }]}
              >
                <Input />
              </Form.Item>
            ) : null}
            {dataSourceBackendType === 'loki' ? (
              <Form.Item
                name="lokiLabelSeverity"
                label="Loki Severity Label"
                rules={[{ required: true, message: '请输入 Loki Severity Label' }]}
              >
                <Input />
              </Form.Item>
            ) : null}
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch disabled={blocked} />
            </Form.Item>
            <div className="soha-form-actions">
              <Button
                disabled={blocked}
                onClick={() => {
                  setDataSourceModalVisible(false)
                  setEditingDataSource(null)
                  setDataSourceSourceKind('logs')
                }}
              >
                取消
              </Button>
              {(editingDataSource ? canUpdateDataSources : canCreateDataSources) ? (
                <Button
                  disabled={blocked}
                  htmlType="submit"
                  type="primary"
                  loading={dataSourceMutation.isPending}
                >
                  保存
                </Button>
              ) : null}
            </div>
          </Form>
        </Modal>
      ) : null}

      {showProfiles ? (
        <Modal
          title={editingProfile ? '编辑分析模板' : '新增分析模板'}
          open={profileModalVisible && !profileDenied}
          closable={!blocked}
          keyboard={!blocked}
          mask={{ closable: !blocked }}
          footer={null}
          onCancel={() => {
            if (blocked) return
            setProfileModalVisible(false)
            setEditingProfile(null)
          }}
          destroyOnHidden
        >
          {profileMutation.feedback}
          <Form
            disabled={blocked}
            {...DEFAULT_FORM_LAYOUT}
            initialValues={buildProfileFormValues(editingProfile)}
            onFinish={(values) => {
              if (!canManageAISettings) return
              saveProfile({
                id: editingProfile?.id,
                values: values as Record<string, unknown>,
              })
            }}
          >
            <Form.Item name="id" hidden>
              <Input />
            </Form.Item>
            <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
              <Input />
            </Form.Item>
            <Form.Item name="mode" label="模式">
              <Select
                options={[
                  { value: 'root_cause', label: 'root_cause' },
                  { value: 'inspection', label: 'inspection' },
                  { value: 'performance', label: 'performance' },
                  { value: 'trace', label: 'trace' },
                ]}
              />
            </Form.Item>
            <Form.Item name="enabledSources" label="数据源">
              <Select
                mode="multiple"
                options={dataSources.map((item) => ({
                  value: item.id,
                  label: `${item.name} (${item.sourceKind}/${item.backendType})${isRetiredAIDataSource(item) ? ' · 后端已退役' : ''}`,
                  disabled:
                    isRetiredAIDataSource(item) && !editingProfile?.enabledSources?.includes(item.id),
                }))}
              />
            </Form.Item>
            <Form.Item name="enabledPlaybooks" label="Playbooks">
              <Select mode="multiple" options={PLAYBOOK_OPTIONS} />
            </Form.Item>
            <Form.Item name="remediationPolicy" label="修复策略">
              <Input />
            </Form.Item>
            <Form.Item name="defaultTimeRangeMinutes" label="默认时间范围(分钟)">
              <InputNumber min={5} style={fullWidthStyle} />
            </Form.Item>
            <Form.Item name="timeoutSeconds" label="超时(秒)">
              <InputNumber min={10} style={fullWidthStyle} />
            </Form.Item>
            <Form.Item name="budgetMaxQueries" label="Max Queries">
              <InputNumber min={1} style={fullWidthStyle} />
            </Form.Item>
            <Form.Item name="budgetMaxLogBytes" label="Max Log Bytes">
              <InputNumber min={1024} style={fullWidthStyle} />
            </Form.Item>
            <Form.Item name="budgetMaxEvidenceItems" label="Max Evidence Items">
              <InputNumber min={1} style={fullWidthStyle} />
            </Form.Item>
            <Form.Item name="outputSummaryLevel" label="Summary Level">
              <Select
                options={[
                  { value: 'compact', label: 'compact' },
                  { value: 'standard', label: 'standard' },
                  { value: 'detailed', label: 'detailed' },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="outputIncludeEvidenceDetail"
              label="Include Evidence Detail"
              valuePropName="checked"
            >
              <Switch disabled={blocked} />
            </Form.Item>
            <Form.Item
              name="outputIncludeRecommendations"
              label="Include Recommendations"
              valuePropName="checked"
            >
              <Switch disabled={blocked} />
            </Form.Item>
            <Form.Item
              name="outputIncludeTimeline"
              label="Include Timeline"
              valuePropName="checked"
            >
              <Switch disabled={blocked} />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch disabled={blocked} />
            </Form.Item>
            <div className="soha-form-actions">
              <Button
                disabled={blocked}
                onClick={() => {
                  setProfileModalVisible(false)
                  setEditingProfile(null)
                }}
              >
                取消
              </Button>
              {canManageAISettings ? (
                <Button
                  disabled={blocked}
                  htmlType="submit"
                  type="primary"
                  loading={profileMutation.isPending}
                >
                  保存
                </Button>
              ) : null}
            </div>
          </Form>
        </Modal>
      ) : null}
    </>
  )

  if (embedded) {
    return content
  }

  return <div className="soha-page">{content}</div>
}
