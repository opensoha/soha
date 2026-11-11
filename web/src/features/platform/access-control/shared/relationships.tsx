import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { TableCellLink } from '@/components/table-cell-content'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Button, Card, Descriptions, Tag, Typography } from 'antd'
import type { ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { buildAccessControlDetailRoute } from './paths'
import { accessControlQueries } from './queries'
import { accessControlScopeFromSelection } from './scope'
import type { AccessControlBindingRecord, AccessControlKind, AccessControlSubject } from './types'

const { Text } = Typography

export function parseAccessControlSubject(value: string): AccessControlSubject {
  const normalized = value.trim()
  const [kindPart, remainder = ''] = normalized.split(':', 2)
  const kind = kindPart || 'Subject'
  const parts = remainder.split('/').filter(Boolean)
  if (parts.length >= 2) {
    const namespace = parts[0]
    const name = parts.slice(1).join('/')
    return { kind, namespace, name, label: `${kind} ${namespace}/${name}` }
  }
  const name = remainder || normalized
  return { kind, name, label: `${kind} ${name}`.trim() }
}

export function splitAccessControlRoleRef(value?: string) {
  if (!value) return null
  const [kind, ...nameParts] = value.split('/')
  const name = nameParts.join('/')
  return { kind: kind || 'Role', name: name || value }
}

function detailKindForRelationship(kind: string): AccessControlKind | null {
  switch (kind) {
    case 'ServiceAccount':
      return 'serviceaccounts'
    case 'Role':
      return 'roles'
    case 'ClusterRole':
      return 'clusterroles'
    case 'RoleBinding':
      return 'rolebindings'
    case 'ClusterRoleBinding':
      return 'clusterrolebindings'
    default:
      return null
  }
}

export function AccessControlNameLink({
  kind,
  label,
  name,
  namespace,
}: {
  kind: AccessControlKind
  label?: string
  name: string
  namespace?: string
}) {
  const navigate = useNavigate()
  return (
    <TableCellLink
      label={label ?? name}
      onClick={() => navigate(buildAccessControlDetailRoute(kind, name, namespace))}
    />
  )
}

export function AccessControlRelationshipLink({
  kind,
  name,
  namespace,
}: {
  kind: string
  name: string
  namespace?: string
}) {
  const navigate = useNavigate()
  const resourceKind = detailKindForRelationship(kind)
  const label = namespace ? `${kind} ${namespace}/${name}` : `${kind} ${name}`
  if (!resourceKind) return <Tag className="soha-rbac-subject-chip">{label}</Tag>
  const path = buildAccessControlDetailRoute(resourceKind, name, namespace)
  return (
    <Tag className="soha-rbac-subject-chip">
      <Button type="text" onClick={() => navigate(path)}>
        {label}
      </Button>
    </Tag>
  )
}

function RelationshipPanel({
  items,
  loading,
}: {
  items: Array<{ key: string; label: string; children: ReactNode }>
  loading?: boolean
}) {
  return (
    <Card className="soha-detail-card">
      {loading ? (
        <ManagementState compact kind="loading" />
      ) : (
        <Descriptions column={{ xs: 1, sm: 2 }} items={items} size="small" />
      )}
    </Card>
  )
}

export function AccessControlSubjectLinks({
  namespace,
  subjects,
}: {
  namespace?: string
  subjects?: string[]
}) {
  const { localeCode } = useI18n()
  if (!subjects?.length) {
    return <Text type="secondary">{localeCode === 'zh_CN' ? '暂无主体' : 'No subjects'}</Text>
  }
  return (
    <div className="soha-rbac-subject-list">
      {subjects.map((value) => {
        const subject = parseAccessControlSubject(value)
        return (
          <AccessControlRelationshipLink
            key={value}
            kind={subject.kind}
            name={subject.name}
            namespace={subject.namespace || namespace}
          />
        )
      })}
    </div>
  )
}

export function AccessControlBindingRelationships({
  namespace,
  roleRef,
  subjects,
}: {
  namespace?: string
  roleRef?: string
  subjects?: string[]
}) {
  const { localeCode } = useI18n()
  const ref = splitAccessControlRoleRef(roleRef)
  return (
    <RelationshipPanel
      items={[
        {
          key: 'roleRef',
          label: localeCode === 'zh_CN' ? '绑定到角色' : 'Bound Role',
          children: ref ? (
            <AccessControlRelationshipLink
              kind={ref.kind}
              name={ref.name}
              namespace={ref.kind === 'Role' ? namespace : undefined}
            />
          ) : (
            <Text type="secondary">{localeCode === 'zh_CN' ? '暂无 RoleRef' : 'No roleRef'}</Text>
          ),
        },
        {
          key: 'subjects',
          label: localeCode === 'zh_CN' ? '授权主体' : 'Subjects',
          children: <AccessControlSubjectLinks namespace={namespace} subjects={subjects} />,
        },
      ]}
    />
  )
}

export function AccessControlReferencedByRelationships({
  kind,
  name,
  namespace,
}: {
  kind: 'ClusterRole' | 'Role' | 'ServiceAccount'
  name: string
  namespace?: string
}) {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()
  const roleBindingScope = accessControlScopeFromSelection(
    'rolebindings',
    clusterId,
    kind === 'ClusterRole' ? null : namespace,
  )
  const clusterRoleBindingScope = accessControlScopeFromSelection(
    'clusterrolebindings',
    clusterId,
    null,
  )
  const subjectFilter =
    kind === 'ServiceAccount'
      ? { subjectKind: kind, subjectName: name, subjectNamespace: namespace }
      : undefined
  const roleBindingsQuery = useQuery(
    accessControlQueries.list<AccessControlBindingRecord>(
      'rolebindings',
      roleBindingScope,
      subjectFilter,
    ),
  )
  const visibleRoleBindingsQueryData = readableQueryData(roleBindingsQuery)
  const clusterRoleBindingsQuery = useQuery(
    accessControlQueries.list<AccessControlBindingRecord>(
      'clusterrolebindings',
      clusterRoleBindingScope,
      subjectFilter,
    ),
  )
  const visibleClusterRoleBindingsQueryData = readableQueryData(clusterRoleBindingsQuery)
  const roleBindings = (visibleRoleBindingsQueryData ?? []).filter((binding) => {
    if (kind === 'ServiceAccount') return true
    const ref = splitAccessControlRoleRef(binding.roleRef)
    return (
      ref?.kind === kind &&
      ref.name === name &&
      (kind === 'ClusterRole' || binding.namespace === namespace)
    )
  })
  const clusterRoleBindings = (visibleClusterRoleBindingsQueryData ?? []).filter((binding) => {
    if (kind === 'ServiceAccount') return true
    const ref = splitAccessControlRoleRef(binding.roleRef)
    return ref?.kind === kind && ref.name === name
  })
  const loading = roleBindingsQuery.isLoading || clusterRoleBindingsQuery.isLoading
  return (
    <>
      <ReadFeedback
        canRead={roleBindingsQuery.isEnabled}
        query={roleBindingsQuery}
        label="RoleBinding"
      />
      <ReadFeedback
        canRead={clusterRoleBindingsQuery.isEnabled}
        query={clusterRoleBindingsQuery}
        label="ClusterRoleBinding"
      />
      <RelationshipPanel
        loading={loading}
        items={[
          {
            key: 'roleBindings',
            label: localeCode === 'zh_CN' ? 'RoleBindings 引用' : 'Referenced by RoleBindings',
            children: roleBindings.length ? (
              <div className="soha-rbac-subject-list">
                {roleBindings.map((binding) => (
                  <AccessControlRelationshipLink
                    key={`${binding.namespace}/${binding.name}`}
                    kind="RoleBinding"
                    name={binding.name}
                    namespace={binding.namespace}
                  />
                ))}
              </div>
            ) : (
              <Text type="secondary">
                {localeCode === 'zh_CN' ? '暂无 RoleBinding 引用' : 'No RoleBinding references'}
              </Text>
            ),
          },
          {
            key: 'clusterRoleBindings',
            label:
              localeCode === 'zh_CN'
                ? 'ClusterRoleBindings 引用'
                : 'Referenced by ClusterRoleBindings',
            children: clusterRoleBindings.length ? (
              <div className="soha-rbac-subject-list">
                {clusterRoleBindings.map((binding) => (
                  <AccessControlRelationshipLink
                    key={binding.name}
                    kind="ClusterRoleBinding"
                    name={binding.name}
                  />
                ))}
              </div>
            ) : (
              <Text type="secondary">
                {localeCode === 'zh_CN'
                  ? '暂无 ClusterRoleBinding 引用'
                  : 'No ClusterRoleBinding references'}
              </Text>
            ),
          },
        ]}
      />
    </>
  )
}

export function AccessControlRoleRefLink({
  namespace,
  value,
}: {
  namespace?: string
  value?: string
}) {
  const ref = splitAccessControlRoleRef(value)
  if (!ref) return <Text type="secondary">-</Text>
  return (
    <AccessControlRelationshipLink
      kind={ref.kind}
      name={ref.name}
      namespace={ref.kind === 'Role' ? namespace : undefined}
    />
  )
}

export function AccessControlRoleRefNameLink({
  namespace,
  value,
}: {
  namespace?: string
  value?: string
}) {
  const ref = splitAccessControlRoleRef(value)
  const resourceKind = ref ? detailKindForRelationship(ref.kind) : null
  if (!ref || !resourceKind) return <Text type="secondary">-</Text>
  return (
    <AccessControlNameLink
      kind={resourceKind}
      label={value ?? ref.name}
      name={ref.name}
      namespace={ref.kind === 'Role' ? namespace : undefined}
    />
  )
}
