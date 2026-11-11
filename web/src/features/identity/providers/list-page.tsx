import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementSearchableListPane,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Pagination, Popconfirm, Space, Typography } from 'antd'
import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { identityApplicationQueries } from '../applications'
import { identityOutpostQueries } from '../outposts'
import { identityRuntimeQueries } from '../runtime'
import '../shared/application-access.css'
import type { IdentityApplication } from '../shared/types'
import { ProviderDetailContent } from './components/provider-detail-content'
import { ProviderFormModal } from './components/provider-form-modal'
import { ProviderConfigurationStatus } from './components/provider-setup-panel'
import { identityProviderMutations } from './mutations'
import { identityProviderQueries } from './queries'
import './styles.css'
import type { IdentityProvider, IdentityProviderInput, IdentityRuntimeProviderType } from './types'
const { Text } = Typography
export function IdentityProvidersPage() {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [searchText, setSearchText] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [pagination, setPagination] = useState({ current: 1, pageSize: 15 })
  const [editing, setEditing] = useState<IdentityProvider | null>(null)
  const [params, setParams] = useSearchParams()
  const clearSelection = () =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.delete('provider')
      return next
    })
  const selectProvider = (id: string) =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      next.set('provider', id)
      return next
    })
  const [formProviderType, setFormProviderType] = useState<IdentityRuntimeProviderType>('oidc')
  const snapshot = usePermissionSnapshot().data?.data
  const canCreate = hasPermission(snapshot, 'identity.providers.create')
  const canUpdate = hasPermission(snapshot, 'identity.providers.update')
  const canDelete = hasPermission(snapshot, 'identity.providers.delete')
  const canViewApplications = hasPermission(snapshot, 'identity.applications.view')
  const canViewOutposts = hasPermission(snapshot, 'identity.outposts.view')
  const providersQuery = useQuery(identityProviderQueries.list({}))
  const visibleProvidersQueryData =
    providersQuery.isEnabled === false ? undefined : readableQueryData(providersQuery)
  const applicationsQuery = useQuery({
    ...identityApplicationQueries.list({}),
    enabled: canViewApplications,
  })
  const visibleApplicationsQueryData =
    applicationsQuery.isEnabled === false ? undefined : readableQueryData(applicationsQuery)
  const outpostsQuery = useQuery({
    ...identityOutpostQueries.list(),
    enabled: modalOpen && formProviderType === 'proxy' && canViewOutposts,
  })
  const visibleOutpostsQueryData =
    outpostsQuery.isEnabled === false ? undefined : readableQueryData(outpostsQuery)
  const runtimeQuery = useQuery(identityRuntimeQueries.capabilities())
  const visibleRuntimeQueryData =
    runtimeQuery.isEnabled === false ? undefined : readableQueryData(runtimeQuery)
  const samlCapability = visibleRuntimeQueryData?.samlApplicationProvider
  const samlAvailable = samlCapability?.available === true
  const createMutation = useOperationMutation(identityProviderMutations.create(queryClient), {
    label: ['创建 认证接入', 'Create provider'],
    target: (v) => v.name,
  })
  const updateMutation = useOperationMutation(identityProviderMutations.update(queryClient), {
    label: ['保存 认证接入', 'Save provider'],
    target: (v) => v.providerId,
  })
  const deleteMutation = useOperationMutation(identityProviderMutations.remove(queryClient), {
    label: ['删除 认证接入', 'Delete provider'],
    target: (v) => v,
  })
  const applications = canViewApplications ? (visibleApplicationsQueryData ?? []) : []
  const applicationById = useMemo(() => {
    const result = new Map<string, IdentityApplication>()
    applications.forEach((application) => result.set(application.id, application))
    return result
  }, [applications])
  const applicationOptions = useMemo(
    () =>
      applications.map((application) => ({
        label: `${application.name} (${application.slug})`,
        value: application.id,
      })),
    [applications],
  )
  const outpostOptions = useMemo(
    () =>
      (canViewOutposts ? (visibleOutpostsQueryData ?? []) : []).map((outpost) => ({
        label: `${outpost.name} (${outpost.mode})`,
        value: outpost.id,
      })),
    [canViewOutposts, visibleOutpostsQueryData],
  )
  const closeModal = () => {
    setModalOpen(false)
    setEditing(null)
  }
  const openCreate = () => {
    if (operationBlocked) return
    setEditing(null)
    setFormProviderType('oidc')
    setModalOpen(true)
  }
  const openEdit = (provider: IdentityProvider) => {
    if (operationBlocked) return
    setEditing(provider)
    setFormProviderType(provider.type)
    setModalOpen(true)
  }
  const submitForm = (input: IdentityProviderInput) => {
    if (editing) {
      updateMutation.mutate(
        { providerId: editing.id, input },
        {
          onSuccess: () => {
            closeModal()
          },
        },
      )
      return
    }
    createMutation.mutate(input, {
      onSuccess: (provider) => {
        closeModal()
        selectProvider(provider.id)
      },
    })
  }
  const filteredProviders = useMemo(() => {
    const query = searchText.trim().toLowerCase()
    const providers = visibleProvidersQueryData ?? []
    if (!query) return providers
    return providers.filter((provider) => {
      const application = applicationById.get(provider.applicationId)
      return [
        provider.name,
        provider.id,
        provider.applicationId,
        application?.name,
        application?.slug,
      ].some((value) =>
        String(value ?? '')
          .toLowerCase()
          .includes(query),
      )
    })
  }, [applicationById, searchText, visibleProvidersQueryData])
  const requestedIndex = filteredProviders.findIndex(
    (provider) => provider.id === params.get('provider'),
  )
  const currentPage =
    requestedIndex >= 0
      ? Math.floor(requestedIndex / pagination.pageSize) + 1
      : Math.min(
          pagination.current,
          Math.max(1, Math.ceil(filteredProviders.length / pagination.pageSize)),
        )
  const visibleProviders = filteredProviders.slice(
    (currentPage - 1) * pagination.pageSize,
    currentPage * pagination.pageSize,
  )
  const selectedId = params.get('provider') ?? visibleProviders[0]?.id ?? ''
  const detailQuery = useQuery(identityProviderQueries.detail(selectedId))
  const visibleDetailQueryData =
    detailQuery.isEnabled === false ? undefined : readableQueryData(detailQuery)
  const operationBlocked =
    createMutation.blocked ||
    updateMutation.blocked ||
    deleteMutation.blocked ||
    (providersQuery.isEnabled !== false && queryAccessDenied(providersQuery))
  const selectedProvider = visibleDetailQueryData
  const selectedApplication =
    selectedProvider && applicationById.get(selectedProvider.applicationId)
  const providerActions = (provider: IdentityProvider) => (
    <Space size={4}>
      <ManagementIconButton
        aria-label={t('common.edit', '编辑')}
        disabled={operationBlocked || !canUpdate}
        icon={<EditOutlined />}
        onClick={() => openEdit(provider)}
        tooltip={t('common.edit', '编辑')}
      />
      <Popconfirm
        cancelText={t('common.cancel', '取消')}
        disabled={operationBlocked || !canDelete}
        okButtonProps={{
          ...{ danger: true, loading: deleteMutation.isPending },
          disabled: operationBlocked,
        }}
        okText={t('common.delete', '删除')}
        title={t('identity.providers.deleteConfirm', `删除 ${provider.name}`)}
        onConfirm={() =>
          deleteMutation.mutate(provider.id, {
            onSuccess: () => {
              if (selectedId === provider.id) clearSelection()
            },
          })
        }
      >
        <ManagementIconButton
          aria-label={t('common.delete', '删除')}
          danger
          disabled={!canDelete}
          icon={<DeleteOutlined />}
          tooltip={t('common.delete', '删除')}
        />
      </Popconfirm>
    </Space>
  )
  return (
    <>
      {!(
        modalOpen && !(providersQuery.isEnabled !== false && queryAccessDenied(providersQuery))
      ) && (
        <>
          {' '}
          {createMutation.feedback}
          {updateMutation.feedback}
          {deleteMutation.feedback}{' '}
        </>
      )}

      <ReadFeedback canRead={providersQuery.isEnabled} query={providersQuery} label="认证接入" />
      <ReadFeedback
        canRead={applicationsQuery.isEnabled}
        query={applicationsQuery}
        label="应用目录"
      />
      <ReadFeedback canRead={outpostsQuery.isEnabled} query={outpostsQuery} label="Outpost" />
      <ReadFeedback canRead={runtimeQuery.isEnabled} query={runtimeQuery} label="运行能力" />
      <ReadFeedback canRead={detailQuery.isEnabled} query={detailQuery} label="身份数据" />

      <ManagementDataPage
        className="soha-identity-access-page soha-identity-providers-page"
        tableNode={
          <section className="soha-identity-provider-workbench" aria-label="认证接入配置工作区">
            <div className="soha-identity-provider-workspace">
              <div className="soha-identity-provider-picker">
                <ManagementSearchableListPane
                  activeKey={selectedId}
                  items={visibleProviders}
                  getItemKey={(provider) => provider.id}
                  isLoading={providersQuery.isLoading}
                  isError={providersQuery.isError && !visibleProvidersQueryData}
                  onRetry={() =>
                    void (providersQuery.isEnabled === false ? undefined : providersQuery.refetch())
                  }
                  searchActions={
                    <ManagementTableToolbar>
                      <ManagementIconButton
                        aria-label={t('identity.providers.create', '新建 Provider')}
                        color="primary"
                        disabled={operationBlocked || !canCreate}
                        icon={<PlusOutlined />}
                        onClick={openCreate}
                        tooltip={t('identity.providers.create', '新建 Provider')}
                        variant="solid"
                      />
                      <ManagementRefreshButton
                        aria-label={t('common.refresh', '刷新')}
                        loading={providersQuery.isFetching || detailQuery.isFetching}
                        onClick={() => {
                          void (providersQuery.isEnabled === false
                            ? undefined
                            : providersQuery.refetch())
                          if (selectedId)
                            void (detailQuery.isEnabled === false
                              ? undefined
                              : detailQuery.refetch())
                        }}
                        tooltip={t('common.refresh', '刷新')}
                      />
                    </ManagementTableToolbar>
                  }
                  searchPlaceholder={t('identity.providers.search', '搜索 Provider 或应用')}
                  searchValue={searchText}
                  onSearchChange={(query) => {
                    setSearchText(query)
                    setPagination((current) => ({ ...current, current: 1 }))
                    clearSelection()
                  }}
                  onItemSelect={(provider) => selectProvider(provider.id)}
                  emptyTitle={t('identity.providers.empty', '暂无 Provider')}
                  emptyDescription={
                    searchText
                      ? '没有符合条件的认证接入，请调整筛选条件。'
                      : '创建认证接入，为应用配置统一登录。'
                  }
                  renderItem={(provider) => (
                    <>
                      <span className="soha-identity-provider-list-heading">
                        <Text strong>{provider.name}</Text>
                        <MetadataTag label={provider.type.toUpperCase()} />
                      </span>
                      {applicationById.get(provider.applicationId)?.name ? (
                        <Text type="secondary">
                          {applicationById.get(provider.applicationId)?.name}
                        </Text>
                      ) : null}
                      <span className="soha-identity-provider-list-state">
                        <StatusTag
                          value={
                            provider.enabled && provider.status === 'enabled'
                              ? 'enabled'
                              : 'disabled'
                          }
                          label={
                            provider.enabled && provider.status === 'enabled'
                              ? t('identity.providers.enabled', '已启用')
                              : t('identity.providers.disabled', '已停用')
                          }
                        />
                        <ProviderConfigurationStatus providerId={provider.id} />
                      </span>
                    </>
                  )}
                />
                {(!providersQuery.isError || Boolean(visibleProvidersQueryData)) &&
                  !providersQuery.isLoading &&
                  filteredProviders.length > 0 && (
                    <Pagination
                      className="soha-identity-provider-pagination"
                      current={currentPage}
                      pageSize={pagination.pageSize}
                      total={filteredProviders.length}
                      size="small"
                      simple={{ readOnly: true }}
                      showSizeChanger={false}
                      showTotal={(total) => `共 ${total} 条`}
                      onChange={(current, pageSize) => {
                        setPagination({ current, pageSize })
                        clearSelection()
                      }}
                    />
                  )}
              </div>
              <section
                className="soha-identity-provider-detail"
                aria-label="认证接入详情"
                aria-busy={detailQuery.isFetching}
              >
                {selectedId ? (
                  detailQuery.isError && !visibleDetailQueryData ? null : selectedProvider ? (
                    <>
                      <div className="soha-identity-provider-detail-heading">
                        <div>
                          <Text strong>{selectedProvider.name}</Text>
                          {selectedApplication ? (
                            <div>
                              <Link
                                to={
                                  '/identity/applications?application=' +
                                  encodeURIComponent(selectedApplication.id)
                                }
                              >
                                {selectedApplication.name}
                              </Link>
                            </div>
                          ) : null}
                        </div>
                        {providerActions(selectedProvider)}
                      </div>
                      <ProviderDetailContent
                        key={selectedProvider.id}
                        provider={selectedProvider}
                      />
                    </>
                  ) : (
                    <ManagementState kind="loading" title="正在读取认证接入" />
                  )
                ) : (
                  <ManagementState
                    kind={providersQuery.isLoading ? 'loading' : 'select-scope'}
                    title={providersQuery.isLoading ? '正在读取认证接入' : '选择认证接入'}
                    description="选择左侧应用的认证接入，查看资料并继续配置。"
                  />
                )}
              </section>
            </div>
          </section>
        }
      />

      <ProviderFormModal
        blocked={operationBlocked}
        feedback={
          <>
            {createMutation.feedback}
            {updateMutation.feedback}
          </>
        }
        applicationOptions={applicationOptions}
        applicationsLoading={applicationsQuery.isLoading || applicationsQuery.isFetching}
        editing={editing}
        onCancel={() => {
          if (!operationBlocked) closeModal()
        }}
        onProviderTypeChange={setFormProviderType}
        onSubmit={submitForm}
        open={
          modalOpen && !(providersQuery.isEnabled !== false && queryAccessDenied(providersQuery))
        }
        outpostLoading={outpostsQuery.isLoading || outpostsQuery.isFetching}
        outpostOptions={outpostOptions}
        providerType={formProviderType}
        samlAvailable={samlAvailable}
        samlUnavailableReason={samlCapability?.reason}
        submitting={createMutation.isPending || updateMutation.isPending}
      />
    </>
  )
}
