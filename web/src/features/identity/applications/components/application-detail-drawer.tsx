import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Descriptions, Drawer, Space, Tabs, Typography } from 'antd'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { identityOutpostQueries } from '../../outposts'
import {
  identityProviderMutations,
  identityProviderQueries,
  ProviderDetailContent,
  ProviderFormModal,
  type IdentityProvider,
} from '../../providers'
import { identityRuntimeQueries } from '../../runtime'
import type { IdentityApplication } from '../../shared/types'
import {
  buildIdentityApplicationInput,
  identityApplicationAccessPolicyFor,
  identityApplicationFormValuesFor,
} from '../application-form-model'
import { identityApplicationMutations } from '../mutations'
import { identityApplicationAssignmentsSummary } from '../presentation'
import { identityApplicationQueries } from '../queries'
import { ApplicationAuditPanel } from './application-audit-panel'
export function ApplicationDetailDrawer({
  id,
  onClose,
  onEdit,
}: {
  id: string
  onClose: () => void
  onEdit: (application: IdentityApplication) => void
}) {
  const client = useQueryClient()
  const [createOpen, setCreateOpen] = useState(false)
  const snapshot = usePermissionSnapshot().data?.data
  const canViewApplication = hasPermission(snapshot, 'identity.applications.view')
  const canViewProvider = hasPermission(snapshot, 'identity.providers.view')
  const canViewOutposts = hasPermission(snapshot, 'identity.outposts.view')
  const canConfigure =
    hasPermission(snapshot, 'identity.providers.create') &&
    hasPermission(snapshot, 'identity.applications.update')
  const applicationQuery = useQuery({
    ...identityApplicationQueries.detail(id),
    enabled: Boolean(id) && canViewApplication,
  })
  const visibleApplicationQueryData =
    applicationQuery.isEnabled === false ? undefined : readableQueryData(applicationQuery)
  const application = canViewApplication ? visibleApplicationQueryData : undefined
  const providersQuery = useQuery({
    ...identityProviderQueries.list({ applicationId: id }),
    enabled: Boolean(id) && canViewApplication && canViewProvider,
  })
  const visibleProvidersQueryData =
    providersQuery.isEnabled === false ? undefined : readableQueryData(providersQuery)
  const outpostsQuery = useQuery({
    ...identityOutpostQueries.list({}),
    enabled: createOpen && application?.providerType === 'proxy' && canViewOutposts,
  })
  const visibleOutpostsQueryData =
    outpostsQuery.isEnabled === false ? undefined : readableQueryData(outpostsQuery)
  const runtime = useQuery({ ...identityRuntimeQueries.capabilities(), enabled: createOpen })
  const visibleRuntimeData = runtime.isEnabled === false ? undefined : readableQueryData(runtime)
  const createProvider = useOperationMutation(identityProviderMutations.create(client), {
    label: ['创建 认证接入', 'Create provider'],
    target: (v) => v.name,
  })
  const updateApplication = useOperationMutation(identityApplicationMutations.update(client), {
    label: ['保存 应用', 'Save application'],
    target: (v) => v.applicationId,
  })
  const operationBlocked =
    createProvider.blocked ||
    updateApplication.blocked ||
    (applicationQuery.isEnabled !== false && queryAccessDenied(applicationQuery))
  const provider = canViewProvider
    ? ((visibleProvidersQueryData ?? []).find((item) => item.id === application?.providerId) ??
      (visibleProvidersQueryData ?? []).find((item) => item.type === application?.providerType))
    : undefined
  const bindProvider = async (item: IdentityProvider) => {
    if (!application) return
    try {
      await updateApplication.mutateAsync({
        applicationId: application.id,
        input: {
          ...buildIdentityApplicationInput(
            identityApplicationFormValuesFor(application),
            application,
          ),
          providerId: item.id,
        },
      })
      setCreateOpen(false)
    } catch {
      // The mutation owns persistent request feedback.
    }
  }
  const configuration =
    application?.providerType === 'link' ? (
      <Alert type="info" showIcon title="Link 只提供入口链接，目标系统自行认证。" />
    ) : !canViewProvider ? (
      <ManagementState kind="no-permission" title="没有查看认证接入的权限" />
    ) : providersQuery.isError && !visibleProvidersQueryData ? null : providersQuery.isPending ? (
      <ManagementState kind="loading" title="正在读取认证配置" />
    ) : provider ? (
      <>
        {provider.id !== application?.providerId && (
          <Alert
            type="warning"
            showIcon
            title="已有认证接入尚未完成绑定"
            action={
              <Button
                disabled={
                  operationBlocked || !hasPermission(snapshot, 'identity.applications.update')
                }
                loading={updateApplication.isPending}
                onClick={() => void bindProvider(provider)}
              >
                继续绑定
              </Button>
            }
          />
        )}
        <Link to={'/identity/providers?provider=' + encodeURIComponent(provider.id)}>
          打开认证接入
        </Link>
        <ProviderDetailContent provider={provider} />
      </>
    ) : (
      <ManagementState
        kind="empty"
        title="尚未配置认证"
        description={
          canConfigure ? '继续为当前应用创建认证接入。' : '需要创建认证接入和更新应用权限。'
        }
        actions={
          <Button disabled={operationBlocked || !canConfigure} onClick={() => setCreateOpen(true)}>
            继续配置
          </Button>
        }
      />
    )
  const conditions = application ? identityApplicationAccessPolicyFor(application) : null
  return (
    <>
      <Drawer
        open={Boolean(id)}
        onClose={() => {
          if (!createProvider.blocked && !updateApplication.blocked) onClose()
        }}
        title={application?.name ?? '应用详情'}
        size={860}
        destroyOnHidden
        extra={
          application && (
            <Button
              disabled={
                operationBlocked || !hasPermission(snapshot, 'identity.applications.update')
              }
              onClick={() => onEdit(application)}
            >
              编辑应用
            </Button>
          )
        }
      >
        {!createOpen && (
          <>
            {createProvider.feedback}
            {updateApplication.feedback}
          </>
        )}

        <ReadFeedback
          canRead={applicationQuery.isEnabled}
          query={applicationQuery}
          label="身份数据"
        />
        <ReadFeedback canRead={providersQuery.isEnabled} query={providersQuery} label="认证接入" />
        <ReadFeedback canRead={outpostsQuery.isEnabled} query={outpostsQuery} label="Outpost" />
        <ReadFeedback canRead={runtime.isEnabled} query={runtime} label="身份数据" />

        {!canViewApplication ? (
          <ManagementState kind="no-permission" title="没有查看应用的权限" />
        ) : applicationQuery.isError && !visibleApplicationQueryData ? null : !application ? (
          <ManagementState kind="loading" title="正在读取应用" />
        ) : (
          <Tabs
            items={[
              {
                key: 'overview',
                label: '概览',
                children: (
                  <>
                    <Alert
                      type="info"
                      showIcon
                      title="未验证真实登录"
                      description={
                        application.status === 'enabled'
                          ? '应用已启用；启用状态不代表真实登录已验证。'
                          : '应用未启用。完成协议与访问范围检查后，可在编辑中启用。'
                      }
                    />
                    <Descriptions
                      column={1}
                      items={[
                        {
                          key: 'id',
                          label: 'ID',
                          children: <Typography.Text copyable>{application.id}</Typography.Text>,
                        },
                        { key: 'slug', label: 'Slug', children: application.slug },
                        {
                          key: 'type',
                          label: '接入方式',
                          children: application.providerType.toUpperCase(),
                        },
                        {
                          key: 'url',
                          label: '启动地址',
                          children: application.launchUrl || '未设置',
                        },
                        {
                          key: 'portal',
                          label: '门户显示',
                          children: application.portalVisible ? '显示' : '隐藏；不会撤销访问权限',
                        },
                        {
                          key: 'featured',
                          label: '推荐',
                          children: application.featured ? '是' : '否',
                        },
                        { key: 'updated', label: '更新时间', children: application.updatedAt },
                      ]}
                    />
                  </>
                ),
              },
              { key: 'authentication', label: '认证配置', children: configuration },
              {
                key: 'access',
                label: '访问范围',
                children: (
                  <Space orientation="vertical">
                    {identityApplicationAssignmentsSummary(application, '所有已登录用户')}
                    {(application.assignments ?? []).length > 0 && (
                      <ul>
                        {application.assignments?.map((assignment, index) => (
                          <li key={index}>
                            {assignment.effect === 'deny' ? '拒绝' : '允许'} ·{' '}
                            {assignment.subjectType} · {assignment.subjectId}
                          </li>
                        ))}
                      </ul>
                    )}
                    <Typography.Text>
                      拒绝规则优先；SSO 授权不授予 VPN 或网络资源权限。
                    </Typography.Text>
                    <Descriptions
                      column={1}
                      items={[
                        {
                          key: 'mfa',
                          label: 'MFA',
                          children: conditions?.requireMfa ? '必须完成' : '不要求',
                        },
                        {
                          key: 'cidr',
                          label: '来源 CIDR',
                          children: conditions?.allowedCidrs.join(', ') || '不限制',
                        },
                        {
                          key: 'time',
                          label: 'UTC 时段',
                          children: conditions?.startTimeUtc
                            ? conditions.startTimeUtc + '–' + conditions.endTimeUtc
                            : '不限制',
                        },
                      ]}
                    />
                  </Space>
                ),
              },
              {
                key: 'records',
                label: '接入记录',
                children: (
                  <ApplicationAuditPanel
                    id={id}
                    providerId={application.providerId || provider?.id}
                  />
                ),
              },
            ]}
          />
        )}
      </Drawer>
      {application && application.providerType !== 'link' && (
        <ProviderFormModal
          blocked={operationBlocked}
          feedback={
            <>
              {createProvider.feedback}
              {updateApplication.feedback}
              <ReadFeedback
                query={providersQuery}
                canRead={providersQuery.isEnabled}
                label="认证接入"
              />
            </>
          }
          applicationOptions={[{ label: application.name, value: application.id }]}
          applicationsLoading={false}
          editing={null}
          initialApplicationId={application.id}
          lockedType={application.providerType}
          providerType={application.providerType}
          onProviderTypeChange={() => undefined}
          open={
            createOpen &&
            !(applicationQuery.isEnabled !== false && queryAccessDenied(applicationQuery))
          }
          onCancel={() => {
            if (!operationBlocked) (() => setCreateOpen(false))()
          }}
          submitting={createProvider.isPending || updateApplication.isPending}
          outpostLoading={outpostsQuery.isFetching}
          outpostOptions={(canViewOutposts ? (visibleOutpostsQueryData ?? []) : []).map((item) => ({
            label: item.name,
            value: item.id,
          }))}
          samlAvailable={visibleRuntimeData?.samlApplicationProvider.available === true}
          onSubmit={async (input) => {
            try {
              const created = await createProvider.mutateAsync(input)
              await (providersQuery.isEnabled === false ? undefined : providersQuery.refetch())
              await bindProvider(created)
            } catch {
              // The mutation owns persistent request feedback.
            }
          }}
        />
      )}
    </>
  )
}
