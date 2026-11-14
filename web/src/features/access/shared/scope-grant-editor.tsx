import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { AccessMutationFooter, accessMutationModalStyles } from './mutation-footer'
import { useEffect, useMemo, type ReactNode } from 'react'
import { Form, Modal, Segmented, Select, Switch } from 'antd'
import { useQuery } from '@tanstack/react-query'
import { visuallyHiddenModalTitleStyle } from '@/components/modal-styles'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { accessQueries } from './queries'
import type { AccessMutationValues, AccessScopeGrant } from './types'

export type ScopeGrantFormValues = {
  applicationIds?: string[]
  businessLineId?: string
  clusterIds?: string[]
  effect: 'allow' | 'deny'
  enabled: boolean
  environmentIds?: string[]
  role?: string
  scopeType: 'delivery' | 'legacy' | 'platform'
  subjectId?: string
  subjectType?: 'team' | 'user'
}

interface ScopeGrantEditorProps {
  editing: AccessScopeGrant | null
  fixedApplication?: { businessLineId: string; id: string; name?: string }
  fixedSubject?: { id: string; type: 'team' | 'user' }
  onCancel: () => void
  onSubmit: (values: AccessMutationValues) => void
  open: boolean
  pending?: boolean
  blocked?: boolean
  error?: Error | null
  onCheck?: () => void
  readBlocked?: boolean
  readFeedback?: ReactNode
}

function optionLabel(name: string, id: string) {
  return name && name !== id ? `${name} (${id})` : name || id
}

function withFallback(
  options: Array<{ label: string; value: string }>,
  values: string[] | undefined,
) {
  const seen = new Set(options.map((option) => option.value))
  return [
    ...options,
    ...(values ?? [])
      .filter((value) => value && !seen.has(value))
      .map((value) => ({ label: value, value })),
  ]
}

export function buildScopeGrantPayload(
  values: ScopeGrantFormValues,
  editing: AccessScopeGrant | null,
  fixedSubject?: { id: string; type: 'team' | 'user' },
  fixedApplication?: { businessLineId: string; id: string },
): AccessMutationValues {
  const scopeType = fixedApplication ? 'delivery' : values.scopeType
  const preservedRestrictions = editing
    ? {
        applicationIds: editing.applicationIds ?? [],
        businessLineId: editing.businessLineId,
        clusterIds: editing.clusterIds ?? [],
        environmentIds: editing.environmentIds ?? [],
        namespaceSelector: editing.namespaceSelector ?? '',
        namespaces: editing.namespaces ?? [],
        resourceGroups: editing.resourceGroups ?? [],
        resourceKinds: editing.resourceKinds ?? [],
      }
    : {}
  const common = {
    ...preservedRestrictions,
    effect: values.effect ?? editing?.effect ?? 'allow',
    enabled: values.enabled,
    role: values.role,
    scopeType,
    subjectId: fixedSubject?.id ?? values.subjectId,
    subjectType: fixedSubject?.type ?? values.subjectType,
  }

  return scopeType === 'platform'
    ? {
        ...common,
        businessLineId: '',
        clusterIds: values.clusterIds ?? [],
      }
    : {
        ...common,
        applicationIds: fixedApplication ? [fixedApplication.id] : (values.applicationIds ?? []),
        businessLineId: fixedApplication?.businessLineId ?? values.businessLineId?.trim() ?? '',
        environmentIds: values.environmentIds ?? [],
      }
}

export function ScopeGrantEditor({
  editing,
  fixedApplication,
  fixedSubject,
  onCancel,
  onSubmit,
  open,
  pending = false,
  blocked = false,
  error = null,
  onCheck,
  readBlocked = false,
  readFeedback,
}: ScopeGrantEditorProps) {
  const [form] = Form.useForm<ScopeGrantFormValues>()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = permissionSnapshotQuery.data?.data
  const canViewRoles = hasPermission(snapshot, 'access.roles.view')
  const canViewUsers = hasPermission(snapshot, 'access.users.view')
  const canViewTeams = hasPermission(snapshot, 'access.groups.view')
  const canViewApplications = hasPermission(snapshot, 'delivery.applications.view')
  const canViewApplicationEnvironments = hasPermission(
    snapshot,
    'delivery.application-environments.view',
  )
  const canViewClusters = hasPermission(snapshot, 'platform.clusters.view')
  const canViewDelivery = canViewApplications && canViewApplicationEnvironments
  const defaultSubjectType = canViewTeams ? 'team' : 'user'

  const rolesQuery = useQuery(accessQueries.roles(open && canViewRoles))
  const usersQuery = useQuery(accessQueries.users(open && !fixedSubject && canViewUsers))
  const teamsQuery = useQuery(accessQueries.teams(open && !fixedSubject && canViewTeams))
  const applicationsQuery = useQuery(accessQueries.applicationOptions(open && canViewApplications))
  const applicationEnvironmentsQuery = useQuery(
    accessQueries.applicationEnvironments(open && canViewApplicationEnvironments),
  )
  const clustersQuery = useQuery(accessQueries.clusterOptions(open && canViewClusters))

  const defaultScopeType = fixedApplication || canViewDelivery ? 'delivery' : 'platform'
  const initialScopeType = fixedApplication ? 'delivery' : (editing?.scopeType ?? defaultScopeType)
  const scopeType = Form.useWatch('scopeType', form) ?? initialScopeType
  const subjectType =
    Form.useWatch('subjectType', form) ?? editing?.subjectType ?? defaultSubjectType
  const businessLineId = Form.useWatch('businessLineId', form)
  const applicationIds = Form.useWatch('applicationIds', form) ?? []
  const preserveDraft = writeOutcomeUnknown(error)

  useEffect(() => {
    if (!open || preserveDraft) return
    form.resetFields()
    form.setFieldsValue(
      editing
        ? {
            applicationIds: fixedApplication
              ? [fixedApplication.id]
              : (editing.applicationIds ?? []),
            businessLineId: fixedApplication?.businessLineId ?? editing.businessLineId,
            clusterIds: editing.clusterIds ?? [],
            effect: editing.effect,
            enabled: editing.enabled,
            environmentIds: editing.environmentIds ?? [],
            role: editing.role,
            scopeType: fixedApplication ? 'delivery' : editing.scopeType,
            subjectId: fixedSubject?.id ?? editing.subjectId,
            subjectType: fixedSubject?.type ?? editing.subjectType,
          }
        : {
            applicationIds: fixedApplication ? [fixedApplication.id] : [],
            businessLineId: fixedApplication?.businessLineId,
            clusterIds: [],
            effect: 'allow',
            enabled: true,
            environmentIds: [],
            scopeType: defaultScopeType,
            subjectId: fixedSubject?.id,
            subjectType: fixedSubject?.type ?? defaultSubjectType,
          },
    )
  }, [
    preserveDraft,
    defaultScopeType,
    defaultSubjectType,
    editing,
    fixedApplication?.businessLineId,
    fixedApplication?.id,
    fixedSubject?.id,
    fixedSubject?.type,
    form,
    open,
  ])

  const roleOptions = withFallback(
    ((canViewRoles ? readableQueryData(rolesQuery) : undefined) ?? []).map((role) => ({
      label: optionLabel(role.name, role.id),
      value: role.id,
    })),
    editing?.role ? [editing.role] : [],
  )
  const userOptions = withFallback(
    ((canViewUsers ? readableQueryData(usersQuery) : undefined) ?? []).map((user) => ({
      label: optionLabel(user.displayName || user.username, user.id),
      value: user.id,
    })),
    editing?.subjectType === 'user' ? [editing.subjectId] : [],
  )
  const teamOptions = withFallback(
    ((canViewTeams ? readableQueryData(teamsQuery) : undefined) ?? []).map((team) => ({
      label: optionLabel(team.name, team.id),
      value: team.id,
    })),
    editing?.subjectType === 'team' ? [editing.subjectId] : [],
  )

  const bindings =
    (canViewApplicationEnvironments
      ? readableQueryData(applicationEnvironmentsQuery)
      : undefined) ?? []
  const businessLineOptions = withFallback(
    Array.from(
      new Set(
        [
          ...((canViewApplications ? readableQueryData(applicationsQuery) : undefined) ?? []).map(
            (item) => item.businessLineId || item.group,
          ),
          ...bindings.map((item) => item.businessLineId || item.applicationGroup),
        ].filter((value): value is string => Boolean(value?.trim())),
      ),
    )
      .sort()
      .map((value) => ({ label: value, value })),
    fixedApplication?.businessLineId
      ? [fixedApplication.businessLineId]
      : editing?.businessLineId
        ? [editing.businessLineId]
        : [],
  )
  const applicationOptions = withFallback(
    ((canViewApplications ? readableQueryData(applicationsQuery) : undefined) ?? [])
      .filter(
        (application) =>
          fixedApplication?.id === application.id ||
          (!fixedApplication &&
            (!businessLineId ||
              application.businessLineId === businessLineId ||
              application.group === businessLineId ||
              bindings.some(
                (binding) =>
                  binding.applicationId === application.id &&
                  (binding.businessLineId === businessLineId ||
                    binding.applicationGroup === businessLineId),
              ))),
      )
      .map((application) => ({
        label: optionLabel(application.name, application.id),
        value: application.id,
      })),
    fixedApplication ? [fixedApplication.id] : editing?.applicationIds,
  )
  const environmentOptions = withFallback(
    Array.from(
      new Map(
        bindings
          .filter(
            (binding) =>
              (!businessLineId ||
                binding.businessLineId === businessLineId ||
                binding.applicationGroup === businessLineId) &&
              (applicationIds.length === 0 || applicationIds.includes(binding.applicationId)),
          )
          .map((binding) => [
            binding.environmentId,
            {
              label: optionLabel(binding.environmentKey ?? '', binding.environmentId),
              value: binding.environmentId,
            },
          ]),
      ).values(),
    ),
    editing?.environmentIds,
  )
  const clusterOptions = withFallback(
    ((canViewClusters ? readableQueryData(clustersQuery) : undefined) ?? []).map((cluster) => ({
      label: optionLabel(cluster.name, cluster.id),
      value: cluster.id,
    })),
    editing?.clusterIds,
  )

  const scopeOptions = useMemo(
    () => [
      ...(editing?.scopeType === 'legacy'
        ? [{ label: '交付范围（兼容）', value: 'legacy' as const }]
        : []),
      {
        disabled: !canViewDelivery && editing?.scopeType !== 'delivery',
        label: '交付范围',
        tooltip: canViewDelivery ? undefined : '需要应用与应用环境查看权限',
        value: 'delivery' as const,
      },
      {
        disabled: !canViewClusters && editing?.scopeType !== 'platform',
        label: '平台范围',
        tooltip: canViewClusters ? undefined : '需要集群查看权限',
        value: 'platform' as const,
      },
    ],
    [canViewClusters, canViewDelivery, editing?.scopeType],
  )

  const requiredQueries = [
    ...(canViewRoles ? [rolesQuery] : []),
    ...(!fixedSubject ? [subjectType === 'team' ? teamsQuery : usersQuery] : []),
    ...(scopeType === 'platform'
      ? canViewClusters
        ? [clustersQuery]
        : []
      : canViewDelivery
        ? [applicationsQuery, applicationEnvironmentsQuery]
        : []),
  ]
  const dependenciesBlocked = requiredQueries.some((query) => query.isPending || query.isError)
  const submit = async () => {
    if (pending || blocked || readBlocked || dependenciesBlocked) return
    try {
      const values = await form.validateFields()
      onSubmit(buildScopeGrantPayload(values, editing, fixedSubject, fixedApplication))
    } catch {
      return
    }
  }

  return (
    <Modal
      title={editing ? '编辑授权项' : '新建授权项'}
      styles={{ ...accessMutationModalStyles, title: visuallyHiddenModalTitleStyle }}
      open={open}
      onCancel={onCancel}
      onOk={() => void submit()}
      okText={editing ? '更新' : '创建'}
      cancelText="取消"
      confirmLoading={pending}
      okButtonProps={{
        disabled: blocked || readBlocked || dependenciesBlocked,
        'aria-label': editing ? '更新' : '创建',
      }}
      cancelButtonProps={{ disabled: pending, 'aria-label': '取消' }}
      closable={{ disabled: pending }}
      keyboard={!pending}
      style={{ top: 32, marginTop: 0 }}
      footer={(buttons) => (
        <AccessMutationFooter
          error={error}
          saving={pending}
          target={editing?.id ?? fixedSubject?.id}
          onCheck={onCheck}
        >
          {buttons}
        </AccessMutationFooter>
      )}
      width={680}
      destroyOnHidden={!preserveDraft}
      mask={{ closable: false }}
    >
      {readFeedback}
      <ReadFeedback query={rolesQuery} label="角色目录" />
      <ReadFeedback query={usersQuery} label="用户目录" />
      <ReadFeedback query={teamsQuery} label="组织目录" />
      <ReadFeedback query={applicationsQuery} label="应用目录" />
      <ReadFeedback query={applicationEnvironmentsQuery} label="环境目录" />
      <ReadFeedback query={clustersQuery} label="集群目录" />
      <Form
        form={form}
        layout="vertical"
        disabled={pending || blocked || readBlocked || dependenciesBlocked}
      >
        {!fixedSubject ? (
          <>
            <Form.Item name="subjectType" label="授权主体">
              <Segmented
                block
                options={[
                  { disabled: !canViewTeams, label: '组织', value: 'team' },
                  { disabled: !canViewUsers, label: '用户', value: 'user' },
                ]}
                onChange={() => form.setFieldValue('subjectId', undefined)}
              />
            </Form.Item>
            <Form.Item
              name="subjectId"
              label={subjectType === 'team' ? '组织' : '用户'}
              rules={[
                { required: true, message: `请选择${subjectType === 'team' ? '组织' : '用户'}` },
              ]}
            >
              <Select
                showSearch={{ optionFilterProp: 'label' }}
                options={subjectType === 'team' ? teamOptions : userOptions}
                loading={subjectType === 'team' ? teamsQuery.isLoading : usersQuery.isLoading}
              />
            </Form.Item>
          </>
        ) : null}

        <Form.Item name="scopeType" label="授权范围">
          <Segmented
            block
            className="soha-form-segmented"
            disabled={Boolean(editing || fixedApplication)}
            options={scopeOptions}
            onChange={() =>
              form.setFieldsValue({
                applicationIds: [],
                businessLineId: undefined,
                clusterIds: [],
                environmentIds: [],
              })
            }
          />
        </Form.Item>

        {scopeType === 'platform' ? (
          <Form.Item
            name="clusterIds"
            label="集群（全部命名空间）"
            rules={[{ required: true, message: '请选择至少一个集群' }]}
          >
            <Select
              disabled={Boolean(editing) && !canViewClusters}
              mode="multiple"
              maxTagCount="responsive"
              showSearch={{ optionFilterProp: 'label' }}
              options={clusterOptions}
              loading={clustersQuery.isLoading}
            />
          </Form.Item>
        ) : (
          <>
            <Form.Item
              name="businessLineId"
              label="业务范围"
              rules={[{ required: true, message: '请选择业务范围' }]}
            >
              <Select
                disabled={Boolean(fixedApplication) || (Boolean(editing) && !canViewDelivery)}
                showSearch={{ optionFilterProp: 'label' }}
                options={businessLineOptions}
                loading={applicationsQuery.isLoading || applicationEnvironmentsQuery.isLoading}
                onChange={() => form.setFieldsValue({ applicationIds: [], environmentIds: [] })}
              />
            </Form.Item>
            <Form.Item
              name="applicationIds"
              label={fixedApplication ? '应用' : '应用（留空为全部）'}
            >
              <Select
                disabled={Boolean(fixedApplication) || (Boolean(editing) && !canViewDelivery)}
                mode="multiple"
                maxTagCount="responsive"
                showSearch={{ optionFilterProp: 'label' }}
                options={applicationOptions}
                loading={applicationsQuery.isLoading}
                onChange={() => form.setFieldValue('environmentIds', [])}
              />
            </Form.Item>
            <Form.Item
              name="environmentIds"
              label={fixedApplication ? '环境覆盖（留空为应用默认）' : '环境（留空为全部）'}
            >
              <Select
                disabled={Boolean(editing) && !canViewDelivery}
                mode="multiple"
                maxTagCount="responsive"
                showSearch={{ optionFilterProp: 'label' }}
                options={environmentOptions}
                loading={applicationEnvironmentsQuery.isLoading}
              />
            </Form.Item>
          </>
        )}

        <Form.Item
          name="role"
          label="范围内角色"
          rules={[{ required: true, message: '请选择角色' }]}
        >
          <Select
            disabled={Boolean(editing) && !canViewRoles}
            showSearch={{ optionFilterProp: 'label' }}
            options={roleOptions}
            loading={rolesQuery.isLoading}
          />
        </Form.Item>
        <Form.Item
          name="effect"
          label="授权效果"
          rules={[{ required: true, message: '请选择授权效果' }]}
        >
          <Segmented
            block
            options={[
              { label: '允许', value: 'allow' },
              { label: '拒绝', value: 'deny' },
            ]}
          />
        </Form.Item>
        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch />
        </Form.Item>
      </Form>
    </Modal>
  )
}
