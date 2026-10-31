import { ManagementProofAction } from './management-proof'
import { ManagementDataPage } from '@/components/management-data-page'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { DeleteOutlined, EditOutlined, EyeOutlined, PlusOutlined } from '@ant-design/icons'
import type {
  EndpointDevice,
  EndpointDeviceInput,
  EndpointDeviceManagementState,
  NetworkDeviceGovernanceMode,
  EndpointDeviceOwnershipType,
  EndpointDevicePostureStatus,
  EndpointDeviceStatus,
  EndpointDeviceType,
  NetworkGateway,
  NetworkGatewayAdministrativeStatus,
  NetworkGatewayInput,
  NetworkGatewayRoutingMode,
  NetworkPathMode,
  NetworkResource,
  NetworkResourceInput,
  NetworkResourceKind,
  NetworkResourceProtocol,
  NetworkSite,
  NetworkSiteInput,
  NetworkSiteStatus,
  NetworkSpace,
  NetworkSpaceInput,
  NetworkSpaceStatus,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tabs,
} from 'antd'
import { useEffect, useState, type ReactNode } from 'react'
import { useLocation, useSearchParams } from 'react-router-dom'
import { NetworkStatusTag } from './network-status-tag'
import './styles.css'
import { NetworkAccessGrantPane } from './access-grant-pane'
import {
  EndpointDeviceDetailDrawer,
  endpointDeviceTypeLabel,
  endpointOwnershipLabel,
  endpointManagementLabel,
} from './device-detail-drawer'
import { NetworkEnrollmentPane } from './enrollment-pane'
import { NetworkMihomoPane } from './mihomo-pane'
import { networkAccessMutations } from './mutations'
import {
  NetworkNASBindingsPane,
  NetworkSessionsPane,
  NetworkSiteProfileBindingsPane,
} from './nac-pane'
import { NetworkPolicyPane } from './policy-pane'
import { NetworkWorkloadsPane } from './workloads-pane'
import { networkAccessQueries } from './queries'
import { TablePane } from './table-pane'
import {
  NetworkProxyConnectionsPane,
  NetworkProxyOverviewPane,
  NetworkTelemetryPane,
} from './telemetry-pane'
type Editor =
  | {
      kind: 'device'
      record: EndpointDevice
    }
  | {
      kind: 'site'
      record?: NetworkSite
    }
  | {
      kind: 'space'
      record?: NetworkSpace
    }
  | {
      kind: 'resource'
      record?: NetworkResource
    }
  | {
      kind: 'gateway'
      record?: NetworkGateway
    }
interface EditorValues {
  region?: string
  providerCode?: string
  providerName?: string
  selectionPriority?: number
  acceptNewConnections?: boolean
  maxSessions?: number
  probeURL?: string
  administrativeStatus?: NetworkGatewayAdministrativeStatus
  advertisedCidrsText?: string
  cidrsText?: string
  description?: string
  deviceType?: EndpointDeviceType
  dnsServersText?: string
  hubGatewayId?: string
  kind?: NetworkResourceKind
  location?: string
  mtu?: number
  name: string
  ownershipType?: EndpointDeviceOwnershipType
  managementState?: EndpointDeviceManagementState
  managementIssuerKey?: string
  deviceGovernance?: NetworkDeviceGovernanceMode
  overlayCidr?: string
  pathMode?: NetworkPathMode
  persistentKeepaliveSeconds?: number
  portsText?: string
  postureStatus?: EndpointDevicePostureStatus
  protected?: boolean
  protocol?: NetworkResourceProtocol
  publicEndpointHost?: string
  publicEndpointPort?: number
  routingMode?: NetworkGatewayRoutingMode
  runtimeId?: string
  siteId?: string
  spaceId?: string
  status?: EndpointDeviceStatus | NetworkSiteStatus | NetworkSpaceStatus
  target?: string
}
interface NetworkAccessSection {
  children: ReactNode
  key: string
}
const LIST_FILTER = { limit: 200 } as const
function statusTag(status?: string) {
  return status ? <NetworkStatusTag value={status} /> : '-'
}
function parseTextList(value?: string) {
  return String(value ?? '')
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}
function parsePorts(value?: string) {
  return parseTextList(value).map(Number)
}
export function NetworkAccessPage() {
  const { localeCode, t } = useI18n()
  const { pathname } = useLocation()
  const [searchParams, setSearchParams] = useSearchParams()
  const queryClient = useQueryClient()
  const permissionQuery = usePermissionSnapshot()
  const snapshot = permissionQuery.data?.data
  const can = (key: string) => hasPermission(snapshot, key)
  const canViewDevices = can('network_access.endpoint_devices.view')
  const canUpdateDevices = can('network_access.endpoint_devices.update')
  const canViewSites = can('network_access.sites.view')
  const canCreateSites = can('network_access.sites.create')
  const canUpdateSites = can('network_access.sites.update')
  const canDeleteSites = can('network_access.sites.delete')
  const canViewSpaces = can('network_access.spaces.view')
  const canCreateSpaces = can('network_access.spaces.create')
  const canUpdateSpaces = can('network_access.spaces.update')
  const canDeleteSpaces = can('network_access.spaces.delete')
  const canViewResources = can('network_access.resources.view')
  const canCreateResources = can('network_access.resources.create')
  const canUpdateResources = can('network_access.resources.update')
  const canDeleteResources = can('network_access.resources.delete')
  const canViewGateways = can('network_access.gateways.view')
  const canCreateGateways = can('network_access.gateways.create')
  const canUpdateGateways = can('network_access.gateways.update')
  const canViewMihomoProfiles = can('network_access.mihomo_profiles.view')
  const canCreateMihomoProfiles = can('network_access.mihomo_profiles.create')
  const canUpdateMihomoProfiles = can('network_access.mihomo_profiles.update')
  const canDeleteMihomoProfiles = can('network_access.mihomo_profiles.delete')
  const canViewTelemetry = can('network_access.telemetry.view')
  const canViewEnrollments = can('network_access.enrollments.view')
  const canCreateEnrollments = can('network_access.enrollments.create')
  const canRevokeEnrollments = can('network_access.enrollments.revoke')
  const canViewAccessGrants = can('network_access.access_grants.view')
  const canCreateAccessGrants = can('network_access.access_grants.create')
  const canRevokeAccessGrants = can('network_access.access_grants.revoke')
  const canPreviewPolicy = can('network_access.policy.view')
  const canCreatePolicies = can('network_access.policy.create')
  const canUpdatePolicies = can('network_access.policy.update')
  const canDeletePolicies = can('network_access.policy.delete')
  const hasAnyView =
    canViewDevices ||
    canViewSites ||
    canViewSpaces ||
    canViewResources ||
    canViewGateways ||
    canViewMihomoProfiles ||
    canViewTelemetry ||
    canViewEnrollments ||
    canViewAccessGrants ||
    canPreviewPolicy
  const devices = useQuery(networkAccessQueries.devices(LIST_FILTER, canViewDevices))
  const visibleDevicesData = devices.isEnabled === false ? undefined : readableQueryData(devices)
  const sites = useQuery(networkAccessQueries.sites(LIST_FILTER, canViewSites))
  const visibleSitesData = sites.isEnabled === false ? undefined : readableQueryData(sites)
  const spaces = useQuery(networkAccessQueries.spaces(LIST_FILTER, canViewSpaces))
  const visibleSpacesData = spaces.isEnabled === false ? undefined : readableQueryData(spaces)
  const resources = useQuery(networkAccessQueries.resources(LIST_FILTER, canViewResources))
  const visibleResourcesData =
    resources.isEnabled === false ? undefined : readableQueryData(resources)
  const gateways = useQuery(networkAccessQueries.gateways(LIST_FILTER, canViewGateways))
  const visibleGatewaysData = gateways.isEnabled === false ? undefined : readableQueryData(gateways)
  const deviceUpdate = useOperationMutation(networkAccessMutations.devices.update(queryClient), {
    label: ['保存 设备', 'Save device'],
    target: (v) => v.deviceId,
  })
  const siteCreate = useOperationMutation(networkAccessMutations.sites.create(queryClient), {
    label: ['创建 站点', 'Create site'],
    target: (v) => v.name,
  })
  const siteUpdate = useOperationMutation(networkAccessMutations.sites.update(queryClient), {
    label: ['保存 站点', 'Save site'],
    target: (v) => v.siteId,
  })
  const siteDelete = useOperationMutation(networkAccessMutations.sites.remove(queryClient), {
    label: ['删除 站点', 'Delete site'],
    target: (v) => v,
  })
  const spaceCreate = useOperationMutation(networkAccessMutations.spaces.create(queryClient), {
    label: ['创建 网络空间', 'Create space'],
    target: (v) => v.name,
  })
  const spaceUpdate = useOperationMutation(networkAccessMutations.spaces.update(queryClient), {
    label: ['保存 网络空间', 'Save space'],
    target: (v) => v.spaceId,
  })
  const spaceDelete = useOperationMutation(networkAccessMutations.spaces.remove(queryClient), {
    label: ['删除 网络空间', 'Delete space'],
    target: (v) => v,
  })
  const resourceCreate = useOperationMutation(
    networkAccessMutations.resources.create(queryClient),
    { label: ['创建 网络资源', 'Create resource'], target: (v) => v.name },
  )
  const resourceUpdate = useOperationMutation(
    networkAccessMutations.resources.update(queryClient),
    { label: ['保存 网络资源', 'Save resource'], target: (v) => v.resourceId },
  )
  const resourceDelete = useOperationMutation(
    networkAccessMutations.resources.remove(queryClient),
    { label: ['删除 网络资源', 'Delete resource'], target: (v) => v },
  )
  const gatewayCreate = useOperationMutation(networkAccessMutations.gateways.create(queryClient), {
    label: ['创建 网关', 'Create gateway'],
    target: (v) => v.name,
  })
  const gatewayUpdate = useOperationMutation(networkAccessMutations.gateways.update(queryClient), {
    label: ['保存 网关', 'Save gateway'],
    target: (v) => v.gatewayId,
  })
  const activeRead:
    | {
        error: Error | null
      }
    | undefined = (
    { devices, sites, spaces, resources, gateways } as Record<
      string,
      {
        error: Error | null
      }
    >
  )[pathname.split('/').pop() ?? '']
  const operationBlocked =
    deviceUpdate.blocked ||
    siteCreate.blocked ||
    siteUpdate.blocked ||
    siteDelete.blocked ||
    spaceCreate.blocked ||
    spaceUpdate.blocked ||
    spaceDelete.blocked ||
    resourceCreate.blocked ||
    resourceUpdate.blocked ||
    resourceDelete.blocked ||
    gatewayCreate.blocked ||
    gatewayUpdate.blocked ||
    Boolean(activeRead && queryAccessDenied(activeRead))
  const [editor, setEditor] = useState<Editor | null>(null)
  const [deviceDetail, setDeviceDetail] = useState<EndpointDevice | null>(null)
  const [editorForm] = Form.useForm<EditorValues>()
  const editorRead = editor
    ? { device: devices, site: sites, space: spaces, resource: resources, gateway: gateways }[
        editor.kind
      ]
    : undefined
  useEffect(() => {
    if (editorRead && queryAccessDenied(editorRead)) {
      setEditor(null)
      editorForm.resetFields()
    }
    if (queryAccessDenied(devices)) setDeviceDetail(null)
  }, [editorRead?.error, devices.error, editorForm])
  function openEditor(next: Editor) {
    if (operationBlocked) return
    editorForm.resetFields()
    setEditor(next)
    if (next.kind === 'device') {
      editorForm.setFieldsValue({
        name: next.record.name,
        deviceType: next.record.deviceType,
        ownershipType: next.record.ownershipType,
        managementState: next.record.managementState ?? 'unknown',
        siteId: next.record.siteId,
        status: next.record.status,
        postureStatus: next.record.postureStatus,
      })
      return
    }
    if (next.kind === 'site') {
      editorForm.setFieldsValue({
        ...(next.record ?? { name: '', status: 'active' as const }),
        deviceGovernance: next.record?.deviceGovernance ?? 'legacy',
        managementIssuerKey: next.record?.managementIssuerKey ?? '',
      })
      return
    }
    if (next.kind === 'space') {
      editorForm.setFieldsValue(
        next.record
          ? { ...next.record, cidrsText: next.record.cidrs.join('\n') }
          : { name: '', status: 'active', cidrsText: '' },
      )
      return
    }
    if (next.kind === 'gateway') {
      editorForm.setFieldsValue(
        next.record
          ? {
              name: next.record.name,
              region: next.record.region,
              providerCode: next.record.providerCode,
              providerName: next.record.providerName,
              selectionPriority: next.record.selectionPriority ?? 100,
              acceptNewConnections: next.record.acceptNewConnections ?? true,
              maxSessions: next.record.maxSessions ?? 0,
              probeURL: next.record.probeURL,
              runtimeId: next.record.runtimeId,
              siteId: next.record.siteId,
              administrativeStatus: next.record.administrativeStatus,
              publicEndpointHost: next.record.publicEndpointHost,
              publicEndpointPort: next.record.publicEndpointPort,
              overlayCidr: next.record.overlayCidr,
              routingMode: next.record.routingMode,
              hubGatewayId: next.record.hubGatewayId,
              advertisedCidrsText: next.record.advertisedCidrs?.join('\n') ?? '',
              mtu: next.record.mtu,
              persistentKeepaliveSeconds: next.record.persistentKeepaliveSeconds,
              dnsServersText: next.record.dnsServers.join('\n'),
            }
          : {
              name: '',
              selectionPriority: 100,
              acceptNewConnections: true,
              maxSessions: 0,
              administrativeStatus: 'active',
              advertisedCidrsText: '',
              dnsServersText: '',
              mtu: 1420,
              persistentKeepaliveSeconds: 25,
              publicEndpointPort: 51820,
              routingMode: 'routed',
            },
      )
      return
    }
    editorForm.setFieldsValue(
      next.record
        ? { ...next.record, portsText: next.record.ports?.join(', ') ?? '' }
        : {
            name: '',
            kind: 'fqdn',
            protected: true,
            protocol: 'tcp',
            pathMode: 'wireguard_ztna',
            portsText: '',
          },
    )
  }
  function closeEditor() {
    setEditor(null)
    editorForm.resetFields()
  }
  async function submitEditor() {
    if (!editor) return
    const values = await editorForm.validateFields()
    if (editor.kind === 'device') {
      const input: EndpointDeviceInput = {
        name: values.name.trim(),
        status: values.status as EndpointDeviceStatus,
        ...(values.deviceType ? { deviceType: values.deviceType } : {}),
        ...(values.ownershipType ? { ownershipType: values.ownershipType } : {}),
        ...(values.managementState &&
        values.managementState !== (editor.record.managementState ?? 'unknown')
          ? { managementState: values.managementState }
          : {}),
        ...(values.postureStatus ? { postureStatus: values.postureStatus } : {}),
        ...(values.siteId?.trim() ? { siteId: values.siteId.trim() } : {}),
      }
      await deviceUpdate.mutateAsync({ deviceId: editor.record.id, input })
    } else if (editor.kind === 'site') {
      const input: NetworkSiteInput = {
        ...(values.managementIssuerKey?.trim() !== (editor.record?.managementIssuerKey ?? '')
          ? { managementIssuerKey: values.managementIssuerKey?.trim() ?? '' }
          : {}),
        ...(!editor.record ||
        values.deviceGovernance !== (editor.record.deviceGovernance ?? 'legacy')
          ? { deviceGovernance: values.deviceGovernance ?? 'legacy' }
          : {}),
        name: values.name.trim(),
        status: values.status as NetworkSiteStatus,
        ...(values.description?.trim() ? { description: values.description.trim() } : {}),
        ...(values.location?.trim() ? { location: values.location.trim() } : {}),
      }
      if (editor.record) await siteUpdate.mutateAsync({ siteId: editor.record.id, input })
      else await siteCreate.mutateAsync(input)
    } else if (editor.kind === 'space') {
      const input: NetworkSpaceInput = {
        siteId: values.siteId!.trim(),
        name: values.name.trim(),
        status: values.status as NetworkSpaceStatus,
        cidrs: parseTextList(values.cidrsText),
      }
      if (editor.record) await spaceUpdate.mutateAsync({ spaceId: editor.record.id, input })
      else await spaceCreate.mutateAsync(input)
    } else if (editor.kind === 'resource') {
      const ports = parsePorts(values.portsText)
      const input: NetworkResourceInput = {
        spaceId: values.spaceId!.trim(),
        name: values.name.trim(),
        kind: values.kind!,
        target: values.target!.trim(),
        protected: Boolean(values.protected),
        protocol: values.protocol!,
        pathMode: values.pathMode!,
        ...(ports.length ? { ports } : {}),
      }
      if (editor.record) await resourceUpdate.mutateAsync({ resourceId: editor.record.id, input })
      else await resourceCreate.mutateAsync(input)
    } else {
      const input: NetworkGatewayInput = {
        region: values.region?.trim() || '',
        providerCode: values.providerCode?.trim() || '',
        providerName: values.providerName?.trim() || '',
        selectionPriority: values.selectionPriority ?? 100,
        acceptNewConnections: values.acceptNewConnections ?? true,
        maxSessions: values.maxSessions ?? 0,
        probeURL: values.probeURL?.trim() || '',
        runtimeId: values.runtimeId!.trim(),
        siteId: values.siteId!.trim(),
        name: values.name.trim(),
        administrativeStatus: values.administrativeStatus!,
        publicEndpointHost: values.publicEndpointHost!.trim(),
        publicEndpointPort: values.publicEndpointPort!,
        overlayCidr: values.overlayCidr!.trim(),
        routingMode: values.routingMode!,
        mtu: values.mtu!,
        persistentKeepaliveSeconds: values.persistentKeepaliveSeconds!,
        dnsServers: parseTextList(values.dnsServersText),
        ...(values.hubGatewayId?.trim() ? { hubGatewayId: values.hubGatewayId.trim() } : {}),
        ...(parseTextList(values.advertisedCidrsText).length
          ? { advertisedCidrs: parseTextList(values.advertisedCidrsText) }
          : {}),
      }
      if (editor.record) await gatewayUpdate.mutateAsync({ gatewayId: editor.record.id, input })
      else await gatewayCreate.mutateAsync(input)
    }
    closeEditor()
  }
  function editorTitle() {
    if (!editor) return ''
    if (editor.kind === 'device') return t('networkAccess.edit.device', '编辑设备')
    if (editor.record) {
      if (editor.kind === 'site') return t('networkAccess.edit.site', '编辑站点')
      if (editor.kind === 'space') return t('networkAccess.edit.space', '编辑网络空间')
      if (editor.kind === 'gateway') return t('networkAccess.edit.gateway', '编辑网关')
      return t('networkAccess.edit.resource', '编辑资源')
    }
    if (editor.kind === 'site') return t('networkAccess.add.site', '新增站点')
    if (editor.kind === 'space') return t('networkAccess.add.space', '新增网络空间')
    if (editor.kind === 'gateway') return t('networkAccess.add.gateway', '新增网关')
    return t('networkAccess.add.resource', '新增资源')
  }
  const deviceColumns: TableColumnsType<EndpointDevice> = [
    {
      title: t('networkAccess.name', '名称'),
      dataIndex: 'name',
      width: 200,
      render: (_, device) => (
        <Button
          type="link"
          className="soha-network-name"
          title={device.name}
          onClick={() => setDeviceDetail(device)}
        >
          {device.name}
        </Button>
      ),
    },
    {
      title: t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 120,
      render: statusTag,
    },

    {
      title: t('networkAccess.device.type', '终端类型'),
      dataIndex: 'deviceType',
      width: 110,
      render: (value) => <MetadataTag label={endpointDeviceTypeLabel(value, localeCode)} />,
    },
    {
      title: t('networkAccess.device.ownership', '设备归属'),
      dataIndex: 'ownershipType',
      width: 130,
      render: (value) => <MetadataTag label={endpointOwnershipLabel(value, localeCode)} />,
    },
    {
      title: t('networkAccess.device.managementState', '受管状态'),
      dataIndex: 'managementState',
      width: 120,
      render: (value) => <MetadataTag label={endpointManagementLabel(value, localeCode)} />,
    },
    {
      title: t('networkAccess.device.os', '操作系统'),
      key: 'os',
      width: 170,
      render: (_, record) =>
        [record.reportedFacts?.osName, record.reportedFacts?.osVersion].filter(Boolean).join(' ') ||
        record.platform,
    },
    { title: t('networkAccess.owner', '用户'), dataIndex: 'ownerUserId', width: 220 },
    {
      title: t('networkAccess.device.proofExpiry', '可信证明有效期'),
      dataIndex: 'managementVerifiedUntil',
      width: 180,
      render: (value?: string) => (value ? formatDateTime(value) : '—'),
    },
    {
      title: t('networkAccess.device.lastSeen', '最近活跃'),
      dataIndex: 'lastSeenAt',
      width: 180,
      render: formatDateTime,
    },
    {
      key: 'actions',
      render: (_: unknown, record: EndpointDevice) => (
        <Space className="soha-row-action-icons">
          <ManagementIconButton
            aria-label={t('networkAccess.device.view', '查看终端详情')}
            icon={<EyeOutlined />}
            tooltip={t('common.view', '查看')}
            onClick={() => setDeviceDetail(record)}
          />
          {canUpdateDevices ? (
            <ManagementProofAction device={record} disabled={operationBlocked} />
          ) : null}
          {canUpdateDevices ? (
            <ManagementIconButton
              aria-label={t('networkAccess.edit.device', '编辑设备')}
              icon={<EditOutlined />}
              tooltip={t('common.edit', '编辑')}
              onClick={() => openEditor({ kind: 'device', record })}
              disabled={operationBlocked}
            />
          ) : null}
        </Space>
      ),
    },
  ]
  const siteColumns: TableColumnsType<NetworkSite> = [
    { title: t('networkAccess.name', '名称'), dataIndex: 'name', width: 180 },
    {
      title: t('networkAccess.location', '位置'),
      dataIndex: 'location',
      width: 180,
      render: (value) => value || '-',
    },
    {
      title: t('networkAccess.descriptionField', '说明'),
      dataIndex: 'description',
      render: (value) => value || '-',
    },
    {
      title: t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 120,
      render: statusTag,
    },
    ...(canUpdateSites || canDeleteSites
      ? [
          {
            key: 'actions',
            render: (_: unknown, record: NetworkSite) => (
              <Space className="soha-row-action-icons">
                {canUpdateSites ? (
                  <ManagementIconButton
                    aria-label={t('networkAccess.edit.site', '编辑站点')}
                    icon={<EditOutlined />}
                    tooltip={t('common.edit', '编辑')}
                    onClick={() => openEditor({ kind: 'site', record })}
                    disabled={operationBlocked}
                  />
                ) : null}
                {canDeleteSites ? (
                  <Popconfirm
                    title={t('networkAccess.confirmDelete.site', '确认删除站点？')}
                    onConfirm={() => siteDelete.mutate(record.id)}
                    okButtonProps={{ disabled: operationBlocked }}
                    disabled={operationBlocked}
                  >
                    <ManagementIconButton
                      aria-label={t('networkAccess.delete.site', '删除站点')}
                      danger
                      icon={<DeleteOutlined />}
                      tooltip={t('common.delete', '删除')}
                    />
                  </Popconfirm>
                ) : null}
              </Space>
            ),
          },
        ]
      : []),
  ]
  const spaceColumns: TableColumnsType<NetworkSpace> = [
    { title: t('networkAccess.name', '名称'), dataIndex: 'name', width: 180 },
    { title: t('networkAccess.siteId', '站点 ID'), dataIndex: 'siteId', width: 220 },
    { title: 'CIDR', dataIndex: 'cidrs', render: (value: string[]) => value.join(', ') },
    {
      title: t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 120,
      render: statusTag,
    },
    ...(canUpdateSpaces || canDeleteSpaces
      ? [
          {
            key: 'actions',
            render: (_: unknown, record: NetworkSpace) => (
              <Space className="soha-row-action-icons">
                {canUpdateSpaces ? (
                  <ManagementIconButton
                    aria-label={t('networkAccess.edit.space', '编辑网络空间')}
                    icon={<EditOutlined />}
                    tooltip={t('common.edit', '编辑')}
                    onClick={() => openEditor({ kind: 'space', record })}
                    disabled={operationBlocked}
                  />
                ) : null}
                {canDeleteSpaces ? (
                  <Popconfirm
                    title={t('networkAccess.confirmDelete.space', '确认删除网络空间？')}
                    onConfirm={() => spaceDelete.mutate(record.id)}
                    okButtonProps={{ disabled: operationBlocked }}
                    disabled={operationBlocked}
                  >
                    <ManagementIconButton
                      aria-label={t('networkAccess.delete.space', '删除网络空间')}
                      danger
                      icon={<DeleteOutlined />}
                      tooltip={t('common.delete', '删除')}
                    />
                  </Popconfirm>
                ) : null}
              </Space>
            ),
          },
        ]
      : []),
  ]
  const resourceColumns: TableColumnsType<NetworkResource> = [
    { title: t('networkAccess.name', '名称'), dataIndex: 'name', width: 160 },
    { title: t('networkAccess.target', '目标'), dataIndex: 'target', width: 220 },
    { title: t('networkAccess.kind', '类型'), dataIndex: 'kind', width: 100 },
    { title: t('networkAccess.protocol', '协议'), dataIndex: 'protocol', width: 100 },
    {
      title: t('networkAccess.ports', '端口'),
      dataIndex: 'ports',
      width: 120,
      render: (value?: number[]) => value?.join(', ') || '-',
    },
    {
      title: 'ProtectedSet',
      dataIndex: 'protected',
      width: 130,
      render: (value: boolean) => (
        <BooleanTag
          value={value}
          trueLabel={t('networkAccess.protected', localeCode === 'zh_CN' ? '受保护' : 'Protected')}
          falseLabel={t('networkAccess.standard', localeCode === 'zh_CN' ? '普通' : 'Standard')}
          trueColor="warning"
        />
      ),
    },
    { title: t('networkAccess.pathMode', '路径'), dataIndex: 'pathMode', width: 150 },
    ...(canUpdateResources || canDeleteResources
      ? [
          {
            key: 'actions',
            render: (_: unknown, record: NetworkResource) => (
              <Space className="soha-row-action-icons">
                {canUpdateResources ? (
                  <ManagementIconButton
                    aria-label={t('networkAccess.edit.resource', '编辑资源')}
                    icon={<EditOutlined />}
                    tooltip={t('common.edit', '编辑')}
                    onClick={() => openEditor({ kind: 'resource', record })}
                    disabled={operationBlocked}
                  />
                ) : null}
                {canDeleteResources ? (
                  <Popconfirm
                    title={t('networkAccess.confirmDelete.resource', '确认删除资源？')}
                    onConfirm={() => resourceDelete.mutate(record.id)}
                    okButtonProps={{ disabled: operationBlocked }}
                    disabled={operationBlocked}
                  >
                    <ManagementIconButton
                      aria-label={t('networkAccess.delete.resource', '删除资源')}
                      danger
                      icon={<DeleteOutlined />}
                      tooltip={t('common.delete', '删除')}
                    />
                  </Popconfirm>
                ) : null}
              </Space>
            ),
          },
        ]
      : []),
  ]
  const gatewayColumns: TableColumnsType<NetworkGateway> = [
    { title: t('networkAccess.gateway.provider', '供应商'), dataIndex: 'providerName' },
    { title: t('networkAccess.gateway.region', '地区'), dataIndex: 'region' },
    { title: t('networkAccess.gateway.priority', '优先级'), dataIndex: 'selectionPriority' },
    {
      title: t('networkAccess.gateway.accepting', '接受新连接'),
      dataIndex: 'acceptNewConnections',
      render: (value: boolean) => <BooleanTag value={value} />,
    },
    {
      title: t('networkAccess.gateway.maxSessions', '会话上限'),
      dataIndex: 'maxSessions',
      render: (value: number) => value || t('networkAccess.gateway.unlimited', '不限'),
    },
    { title: t('networkAccess.name', '名称'), dataIndex: 'name', width: 180 },
    { title: t('networkAccess.siteId', '站点 ID'), dataIndex: 'siteId', width: 220 },
    {
      title: t('networkAccess.gateway.hub', '总部网关'),
      dataIndex: 'hubGatewayId',
      width: 180,
      render: (value) => value || t('networkAccess.gateway.root', '总部'),
    },
    {
      title: t('networkAccess.gateway.advertisedCidrs', '发布网段'),
      dataIndex: 'advertisedCidrs',
      width: 220,
      render: (value?: string[]) => value?.join(', ') || '-',
    },
    {
      title: t('networkAccess.status', '状态'),
      dataIndex: 'status',
      width: 120,
      render: statusTag,
    },
    {
      title: t('networkAccess.version', '版本'),
      dataIndex: 'version',
      width: 120,
      render: (value) => value || '-',
    },
    {
      title: t('networkAccess.policyVersion', '策略版本'),
      dataIndex: 'policyVersion',
      width: 120,
      render: (value) => value ?? '-',
    },
    {
      title: t('networkAccess.capabilities', '能力'),
      dataIndex: 'capabilities',
      render: (value?: string[]) => value?.join(', ') || '-',
    },
    {
      title: t('networkAccess.lastHeartbeat', '最近心跳'),
      dataIndex: 'lastHeartbeatAt',
      width: 200,
      render: (value) => value || '-',
    },
    ...(canUpdateGateways
      ? [
          {
            key: 'actions',
            render: (_: unknown, record: NetworkGateway) => (
              <ManagementIconButton
                aria-label={t('networkAccess.edit.gateway', '编辑网关')}
                icon={<EditOutlined />}
                tooltip={t('common.edit', '编辑')}
                onClick={() => openEditor({ kind: 'gateway', record })}
                disabled={operationBlocked}
              />
            ),
          },
        ]
      : []),
  ]
  if (!hasAnyView) {
    return (
      <div className="soha-page">
        <ManagementState
          kind={permissionQuery.isLoading ? 'loading' : 'no-permission'}
          description={t('networkAccess.noPermission', '当前账号没有网络访问工作台权限。')}
        />
      </div>
    )
  }
  const admissionSettingsTabs = [
    canViewSites
      ? {
          key: 'nas-bindings',
          label: t('networkAccess.settings.networkDevices', '网络设备'),
          children: <NetworkNASBindingsPane canManage={canUpdateSites} />,
        }
      : null,
    canViewSites
      ? {
          key: 'ssids',
          label: 'SSID',
          children: <NetworkNASBindingsPane canManage={canUpdateSites} view="ssid" />,
        }
      : null,
    canViewEnrollments
      ? {
          key: 'radius-services',
          label: t('networkAccess.settings.radius', 'RADIUS 接入'),
          children: (
            <NetworkEnrollmentPane
              canCreate={canCreateEnrollments}
              canRevoke={canRevokeEnrollments}
              runtimeKind="nas"
            />
          ),
        }
      : null,
  ].filter(Boolean) as Array<{
    children: ReactNode
    key: string
    label: string
  }>
  const requestedAdmissionSettingsTab = searchParams.get('tab')
  const activeAdmissionSettingsTab = admissionSettingsTabs.some(
    (item) => item.key === requestedAdmissionSettingsTab,
  )
    ? requestedAdmissionSettingsTab!
    : admissionSettingsTabs[0]?.key
  function setAdmissionSettingsTab(key: string) {
    const next = new URLSearchParams(searchParams)
    next.set('tab', key)
    setSearchParams(next)
  }
  const sections = [
    canViewDevices
      ? {
          key: 'devices',
          children: (
            <TablePane
              columns={deviceColumns}
              items={visibleDevicesData}
              loading={devices.isLoading}
              refreshing={devices.isFetching}
              error={devices.isEnabled === false ? null : devices.error}
              onRefresh={() => void (devices.isEnabled === false ? undefined : devices.refetch())}
              searchPlaceholder={t(
                'networkAccess.search.devices',
                '搜索终端、用户、系统或网络地址',
              )}
              getSearchValues={(item) => [
                item.name,
                item.ownerUserId,
                item.platform,
                item.deviceType,
                item.ownershipType,
                item.hostname,
                item.siteId,
                item.status,
                item.reportedFacts?.osName,
                item.reportedFacts?.osVersion,
                item.reportedFacts?.manufacturer,
                item.reportedFacts?.model,
                item.reportedFacts?.serialNumber,
                ...(item.reportedFacts?.networkInterfaces.flatMap((networkInterface) => [
                  networkInterface.name,
                  networkInterface.macAddress,
                  ...networkInterface.ipv4Addresses,
                  ...networkInterface.ipv6Addresses,
                ]) ?? []),
              ]}
            />
          ),
        }
      : null,
    canViewSites
      ? {
          key: 'sites',
          children: (
            <TablePane
              columns={siteColumns}
              items={visibleSitesData}
              loading={sites.isLoading}
              refreshing={sites.isFetching}
              error={sites.isEnabled === false ? null : sites.error}
              onRefresh={() => void (sites.isEnabled === false ? undefined : sites.refetch())}
              searchPlaceholder={t('networkAccess.search.sites', '搜索站点、位置或说明')}
              getSearchValues={(item) => [item.name, item.location, item.description, item.status]}
              createAction={
                canCreateSites ? (
                  <Button
                    size="small"
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => openEditor({ kind: 'site' })}
                    disabled={operationBlocked}
                  >
                    {t('networkAccess.add.site', '新增站点')}
                  </Button>
                ) : null
              }
            />
          ),
        }
      : null,
    canViewSites
      ? {
          key: 'user-admission',
          children: <NetworkNASBindingsPane canManage={canUpdateSites} view="user-admission" />,
        }
      : null,
    admissionSettingsTabs.length
      ? {
          key: 'settings',
          children: (
            <Tabs
              activeKey={activeAdmissionSettingsTab}
              className="soha-resource-tabs"
              destroyOnHidden
              indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
              items={admissionSettingsTabs}
              onChange={setAdmissionSettingsTab}
              size="small"
              tabBarGutter={18}
            />
          ),
        }
      : null,
    canViewSites
      ? {
          key: 'site-profile-bindings',
          children: <NetworkSiteProfileBindingsPane canManage={canUpdateSites} />,
        }
      : null,
    canViewSites
      ? {
          key: 'sessions',
          children: <NetworkSessionsPane canManage={canUpdateSites} />,
        }
      : null,
    canViewSpaces
      ? {
          key: 'spaces',
          children: (
            <TablePane
              columns={spaceColumns}
              items={visibleSpacesData}
              loading={spaces.isLoading}
              refreshing={spaces.isFetching}
              error={spaces.isEnabled === false ? null : spaces.error}
              onRefresh={() => void (spaces.isEnabled === false ? undefined : spaces.refetch())}
              searchPlaceholder={t('networkAccess.search.spaces', '搜索网络空间、站点或 CIDR')}
              getSearchValues={(item) => [item.name, item.siteId, ...item.cidrs, item.status]}
              createAction={
                canCreateSpaces ? (
                  <Button
                    size="small"
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => openEditor({ kind: 'space' })}
                    disabled={operationBlocked}
                  >
                    {t('networkAccess.add.space', '新增网络空间')}
                  </Button>
                ) : null
              }
            />
          ),
        }
      : null,
    canViewResources
      ? {
          key: 'resources',
          children: (
            <TablePane
              columns={resourceColumns}
              items={visibleResourcesData}
              loading={resources.isLoading}
              refreshing={resources.isFetching}
              error={resources.isEnabled === false ? null : resources.error}
              onRefresh={() =>
                void (resources.isEnabled === false ? undefined : resources.refetch())
              }
              searchPlaceholder={t('networkAccess.search.resources', '搜索资源、目标或路径')}
              getSearchValues={(item) => [
                item.name,
                item.target,
                item.kind,
                item.protocol,
                item.pathMode,
              ]}
              createAction={
                canCreateResources ? (
                  <Button
                    size="small"
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => openEditor({ kind: 'resource' })}
                    disabled={operationBlocked}
                  >
                    {t('networkAccess.add.resource', '新增资源')}
                  </Button>
                ) : null
              }
            />
          ),
        }
      : null,
    canViewGateways
      ? {
          key: 'gateways',
          children: (
            <TablePane
              columns={gatewayColumns}
              items={visibleGatewaysData}
              loading={gateways.isLoading}
              refreshing={gateways.isFetching}
              error={gateways.isEnabled === false ? null : gateways.error}
              onRefresh={() => void (gateways.isEnabled === false ? undefined : gateways.refetch())}
              searchPlaceholder={t('networkAccess.search.gateways', '搜索网关、站点或能力')}
              getSearchValues={(item) => [
                item.name,
                item.siteId,
                item.status,
                item.version,
                item.hubGatewayId,
                ...(item.advertisedCidrs ?? []),
                ...(item.capabilities ?? []),
              ]}
              createAction={
                canCreateGateways ? (
                  <Button
                    size="small"
                    type="primary"
                    icon={<PlusOutlined />}
                    onClick={() => openEditor({ kind: 'gateway' })}
                    disabled={operationBlocked}
                  >
                    {t('networkAccess.add.gateway', '新增网关')}
                  </Button>
                ) : null
              }
            />
          ),
        }
      : null,
    canViewMihomoProfiles
      ? {
          key: 'mihomo-profiles',
          children: (
            <NetworkMihomoPane
              canCreate={canCreateMihomoProfiles}
              canUpdate={canUpdateMihomoProfiles}
              canDelete={canDeleteMihomoProfiles}
            />
          ),
        }
      : null,
    canViewTelemetry
      ? {
          key: 'proxy-overview',
          children: <NetworkProxyOverviewPane />,
        }
      : null,
    canViewTelemetry
      ? {
          key: 'proxy-connections',
          children: (
            <NetworkProxyConnectionsPane
              canViewDevices={canViewDevices}
              canViewProfiles={canViewMihomoProfiles}
            />
          ),
        }
      : null,
    canViewTelemetry
      ? {
          key: 'telemetry',
          children: <NetworkTelemetryPane />,
        }
      : null,
    canViewEnrollments
      ? {
          key: 'enrollments',
          children: (
            <NetworkEnrollmentPane
              canCreate={canCreateEnrollments}
              canRevoke={canRevokeEnrollments}
            />
          ),
        }
      : null,
    canViewAccessGrants
      ? {
          key: 'access-grants',
          children: (
            <NetworkAccessGrantPane
              canCreate={canCreateAccessGrants}
              canRevoke={canRevokeAccessGrants}
            />
          ),
        }
      : null,
    canPreviewPolicy
      ? {
          key: 'policy',
          children: (
            <Tabs
              items={[
                {
                  key: 'policies',
                  label: t('networkAccess.workloads.policies', '访问策略'),
                  children: (
                    <NetworkPolicyPane
                      canCreate={canCreatePolicies}
                      canUpdate={canUpdatePolicies}
                      canDelete={canDeletePolicies}
                    />
                  ),
                },
                {
                  key: 'workloads',
                  label: t('networkAccess.workloads.title', '工作负载绑定'),
                  children: <NetworkWorkloadsPane canUpdate={canUpdatePolicies} />,
                },
              ]}
            />
          ),
        }
      : null,
  ].filter(Boolean) as NetworkAccessSection[]
  const activeSectionKey = pathname.slice(pathname.lastIndexOf('/') + 1)
  const activeSection = sections.find((section) => section.key === activeSectionKey) ?? sections[0]
  const saving =
    deviceUpdate.isPending ||
    siteCreate.isPending ||
    siteUpdate.isPending ||
    spaceCreate.isPending ||
    spaceUpdate.isPending ||
    resourceCreate.isPending ||
    resourceUpdate.isPending ||
    gatewayCreate.isPending ||
    gatewayUpdate.isPending
  return (
    <ManagementDataPage
      className="soha-network-page"
      tableNode={<section key={activeSection?.key}>{activeSection?.children}</section>}
    >
      {!(Boolean(editor) && !(activeRead && queryAccessDenied(activeRead))) && (
        <>
          {' '}
          {deviceUpdate.feedback}
          {siteCreate.feedback}
          {siteUpdate.feedback}
          {siteDelete.feedback}
          {spaceCreate.feedback}
          {spaceUpdate.feedback}
          {spaceDelete.feedback}
          {resourceCreate.feedback}
          {resourceUpdate.feedback}
          {resourceDelete.feedback}
          {gatewayCreate.feedback}
          {gatewayUpdate.feedback}{' '}
        </>
      )}

      <EndpointDeviceDetailDrawer
        device={deviceDetail}
        onClose={() => {
          if (!operationBlocked) (() => setDeviceDetail(null))()
        }}
      />
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        centered
        destroyOnHidden
        mask={{ closable: false }}
        open={Boolean(editor) && !(activeRead && queryAccessDenied(activeRead))}
        title={editorTitle()}
        confirmLoading={saving}
        okText={t('common.save', '保存')}
        cancelText={t('common.cancel', '取消')}
        onCancel={() => {
          if (!operationBlocked) closeEditor()
        }}
        onOk={() => void submitEditor()}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {deviceUpdate.feedback}
        {siteCreate.feedback}
        {siteUpdate.feedback}
        {siteDelete.feedback}
        {spaceCreate.feedback}
        {spaceUpdate.feedback}
        {spaceDelete.feedback}
        {resourceCreate.feedback}
        {resourceUpdate.feedback}
        {resourceDelete.feedback}
        {gatewayCreate.feedback}
        {gatewayUpdate.feedback}

        <ReadFeedback canRead={devices.isEnabled} query={devices} label="设备" />
        <ReadFeedback canRead={sites.isEnabled} query={sites} label="站点" />
        <ReadFeedback canRead={spaces.isEnabled} query={spaces} label="网络空间" />
        <ReadFeedback canRead={resources.isEnabled} query={resources} label="网络资源" />
        <ReadFeedback canRead={gateways.isEnabled} query={gateways} label="网关" />
        <Form form={editorForm} layout="vertical" disabled={operationBlocked}>
          <Form.Item
            name="name"
            label={t('networkAccess.name', '名称')}
            rules={[
              {
                required: true,
                whitespace: true,
                message: t('networkAccess.form.requiredName', '请输入名称'),
              },
            ]}
          >
            <Input maxLength={200} />
          </Form.Item>
          {editor?.kind === 'device' ? (
            <>
              <Form.Item name="deviceType" label={t('networkAccess.device.type', '终端类型')}>
                <Select
                  options={(
                    [
                      'desktop',
                      'laptop',
                      'server',
                      'mobile',
                      'tablet',
                      'virtual',
                      'unknown',
                    ] as EndpointDeviceType[]
                  ).map((value) => ({
                    value,
                    label: endpointDeviceTypeLabel(value, localeCode),
                  }))}
                />
              </Form.Item>
              <Form.Item
                name="ownershipType"
                label={t('networkAccess.device.ownership', '设备归属')}
              >
                <Select
                  options={(
                    [
                      'company',
                      'personal',
                      'temporary',
                      'unassigned',
                    ] as EndpointDeviceOwnershipType[]
                  ).map((value) => ({
                    value,
                    label: endpointOwnershipLabel(value, localeCode),
                  }))}
                />
              </Form.Item>
              <Form.Item
                name="managementState"
                label={t('networkAccess.device.managementState', '受管状态')}
                extra={t(
                  'networkAccess.device.managementHelp',
                  '管理员分类；设备归属和终端上报不能作为受管证明。',
                )}
              >
                <Select
                  options={(
                    ['managed', 'unmanaged', 'unknown'] as EndpointDeviceManagementState[]
                  ).map((value) => ({ value, label: endpointManagementLabel(value, localeCode) }))}
                />
              </Form.Item>
              <Form.Item name="siteId" label={t('networkAccess.siteId', '站点 ID')}>
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item
                name="status"
                label={t('networkAccess.status', '状态')}
                rules={[{ required: true }]}
              >
                <Select
                  options={['pending', 'active', 'quarantined', 'revoked'].map((value) => ({
                    value,
                    label: value,
                  }))}
                />
              </Form.Item>
              <Form.Item
                name="postureStatus"
                label={t('networkAccess.posture', '设备状态')}
                rules={[{ required: true }]}
              >
                <Select
                  options={['unknown', 'compliant', 'non_compliant'].map((value) => ({
                    value,
                    label: value,
                  }))}
                />
              </Form.Item>
            </>
          ) : null}
          {editor?.kind === 'site' ? (
            <>
              <Form.Item
                name="status"
                label={t('networkAccess.status', '状态')}
                rules={[{ required: true }]}
              >
                <Select
                  options={['active', 'disabled'].map((value) => ({ value, label: value }))}
                />
              </Form.Item>
              <Form.Item
                name="deviceGovernance"
                label={t('networkAccess.site.deviceGovernance', '设备治理')}
                extra={t(
                  'networkAccess.site.governanceHelp',
                  '启用后可能收窄现有会话；非受管设备仅可按明确策略访问未保护资源级 ZTNA 或受限准入网络。',
                )}
              >
                <Select
                  options={[
                    {
                      value: 'legacy',
                      label: t('networkAccess.site.governanceLegacy', '兼容模式（治理关闭）'),
                    },
                    {
                      value: 'managed_only',
                      label: t('networkAccess.site.governanceManaged', '仅受管设备'),
                    },
                    {
                      value: 'verified_managed_only',
                      label: t('networkAccess.site.governanceVerified', '仅持有有效管理证明的设备'),
                    },
                    {
                      value: 'restricted_unmanaged',
                      label: t('networkAccess.site.governanceRestricted', '受限非受管访问'),
                    },
                  ]}
                />
              </Form.Item>
              <Form.Item
                name="managementIssuerKey"
                label={t('networkAccess.site.managementIssuerKey', 'MDM 发行者公钥')}
                extra={t(
                  'networkAccess.site.managementIssuerHelp',
                  '仅接受该发行者的短期签名证明；轮换或清空后旧证明失效。人工受管状态不能满足证明模式。',
                )}
                dependencies={['deviceGovernance']}
                rules={[
                  {
                    validator: (_, value) =>
                      editorForm.getFieldValue('deviceGovernance') !== 'verified_managed_only' ||
                      /^[A-Za-z0-9+/]{43}=$/.test(String(value ?? ''))
                        ? Promise.resolve()
                        : Promise.reject(
                            new Error(
                              t(
                                'networkAccess.site.managementIssuerRequired',
                                '证明模式需要有效的 Ed25519 公钥',
                              ),
                            ),
                          ),
                  },
                ]}
              >
                <Input maxLength={44} autoComplete="off" />
              </Form.Item>
              <Form.Item name="location" label={t('networkAccess.location', '位置')}>
                <Input maxLength={200} />
              </Form.Item>
              <Form.Item name="description" label={t('networkAccess.descriptionField', '说明')}>
                <Input.TextArea maxLength={1000} autoSize={{ minRows: 2, maxRows: 5 }} />
              </Form.Item>
            </>
          ) : null}
          {editor?.kind === 'space' ? (
            <>
              <Form.Item
                name="siteId"
                label={t('networkAccess.siteId', '站点 ID')}
                rules={[
                  {
                    required: true,
                    whitespace: true,
                    message: t('networkAccess.form.requiredSite', '请输入站点 ID'),
                  },
                ]}
              >
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item
                name="status"
                label={t('networkAccess.status', '状态')}
                rules={[{ required: true }]}
              >
                <Select
                  options={['active', 'disabled'].map((value) => ({ value, label: value }))}
                />
              </Form.Item>
              <Form.Item
                name="cidrsText"
                label="IPv4 CIDR"
                rules={[
                  {
                    validator: (_, value) =>
                      parseTextList(value).length
                        ? Promise.resolve()
                        : Promise.reject(
                            new Error(t('networkAccess.form.requiredCidr', '至少输入一个 CIDR')),
                          ),
                  },
                ]}
              >
                <Input.TextArea
                  placeholder={t('networkAccess.form.cidrsPlaceholder', '10.0.0.0/24，每行一个')}
                  autoSize={{ minRows: 2, maxRows: 6 }}
                />
              </Form.Item>
            </>
          ) : null}
          {editor?.kind === 'resource' ? (
            <>
              <Form.Item
                name="spaceId"
                label={t('networkAccess.spaceId', '网络空间 ID')}
                rules={[
                  {
                    required: true,
                    whitespace: true,
                    message: t('networkAccess.form.requiredSpace', '请输入网络空间 ID'),
                  },
                ]}
              >
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item
                name="kind"
                label={t('networkAccess.kind', '目标类型')}
                rules={[{ required: true }]}
              >
                <Select
                  options={['cidr', 'ip', 'fqdn'].map((value) => ({ value, label: value }))}
                />
              </Form.Item>
              <Form.Item
                name="target"
                label={t('networkAccess.target', '目标')}
                rules={[
                  {
                    required: true,
                    whitespace: true,
                    message: t('networkAccess.form.requiredTarget', '请输入目标'),
                  },
                ]}
              >
                <Input maxLength={255} />
              </Form.Item>
              <Form.Item
                name="protocol"
                label={t('networkAccess.protocol', '协议')}
                rules={[{ required: true }]}
              >
                <Select
                  options={['any', 'tcp', 'udp', 'icmp'].map((value) => ({ value, label: value }))}
                />
              </Form.Item>
              <Form.Item
                name="portsText"
                label={t('networkAccess.ports', '端口')}
                rules={[
                  {
                    validator: (_, value) =>
                      parsePorts(value).every(
                        (port) => Number.isInteger(port) && port >= 1 && port <= 65535,
                      )
                        ? Promise.resolve()
                        : Promise.reject(
                            new Error(
                              t('networkAccess.form.invalidPort', '端口必须为 1-65535 的整数'),
                            ),
                          ),
                  },
                ]}
              >
                <Input placeholder={t('networkAccess.form.portsPlaceholder', '443, 8443')} />
              </Form.Item>
              <Form.Item
                name="pathMode"
                label={t('networkAccess.pathMode', '路径模式')}
                rules={[{ required: true }]}
              >
                <Select
                  options={[
                    'automatic',
                    'site_direct',
                    'wireguard',
                    'wireguard_ztna',
                    'access_proxy',
                  ].map((value) => ({ value, label: value }))}
                />
              </Form.Item>
              <Form.Item name="protected" label="ProtectedSet" valuePropName="checked">
                <Switch />
              </Form.Item>
            </>
          ) : null}
          {editor?.kind === 'gateway' ? (
            <>
              <Form.Item name="region" label={t('networkAccess.gateway.region', '地区')}>
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item
                name="providerCode"
                label={t('networkAccess.gateway.providerCode', '供应商编码')}
              >
                <Input maxLength={64} />
              </Form.Item>
              <Form.Item
                name="providerName"
                label={t('networkAccess.gateway.providerName', '供应商名称')}
              >
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item
                name="probeURL"
                label={t('networkAccess.gateway.probeURL', 'HTTPS 探测地址')}
              >
                <Input maxLength={2048} />
              </Form.Item>
              <Form.Item
                name="selectionPriority"
                label={t('networkAccess.gateway.priority', '优先级（越小越优先）')}
              >
                <InputNumber min={0} max={10000} precision={0} />
              </Form.Item>
              <Form.Item
                name="maxSessions"
                label={t('networkAccess.gateway.maxSessions', '会话上限（0 表示不限）')}
              >
                <InputNumber min={0} max={1000000} precision={0} />
              </Form.Item>
              <Form.Item
                name="acceptNewConnections"
                label={t('networkAccess.gateway.accepting', '接受新连接')}
                valuePropName="checked"
              >
                <Switch />
              </Form.Item>
              <Form.Item
                name="runtimeId"
                label={t('networkAccess.runtimeId', '运行时 ID')}
                rules={[{ required: true, whitespace: true }]}
              >
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item
                name="siteId"
                label={t('networkAccess.siteId', '站点 ID')}
                rules={[{ required: true, whitespace: true }]}
              >
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item
                name="administrativeStatus"
                label={t('networkAccess.gateway.administrativeStatus', '管理状态')}
                rules={[{ required: true }]}
              >
                <Select
                  options={['active', 'disabled'].map((value) => ({ value, label: value }))}
                />
              </Form.Item>
              <Form.Item
                name="publicEndpointHost"
                label={t('networkAccess.gateway.publicEndpointHost', '公网地址')}
                rules={[{ required: true, whitespace: true }]}
              >
                <Input maxLength={255} placeholder="vpn-a.example.com" />
              </Form.Item>
              <Form.Item
                name="publicEndpointPort"
                label={t('networkAccess.gateway.publicEndpointPort', 'WireGuard 端口')}
                rules={[{ required: true, type: 'number', min: 1, max: 65535 }]}
              >
                <InputNumber min={1} max={65535} precision={0} style={{ width: '100%' }} />
              </Form.Item>
              <Form.Item
                name="overlayCidr"
                label={t('networkAccess.gateway.overlayCidr', 'Overlay CIDR')}
                rules={[{ required: true, whitespace: true }]}
              >
                <Input placeholder="100.96.0.0/24" />
              </Form.Item>
              <Form.Item
                name="routingMode"
                label={t('networkAccess.gateway.routingMode', '路由模式')}
                rules={[{ required: true }]}
              >
                <Select options={['routed', 'snat'].map((value) => ({ value, label: value }))} />
              </Form.Item>
              <Form.Item
                name="hubGatewayId"
                label={t('networkAccess.gateway.hub', '总部网关')}
                tooltip={t(
                  'networkAccess.gateway.hubHint',
                  '总部留空；分支选择总部网关，分支间流量经总部转发。',
                )}
              >
                <Select
                  allowClear
                  showSearch={{ optionFilterProp: 'label' }}
                  options={(visibleGatewaysData ?? [])
                    .filter((gateway) => gateway.id !== editor.record?.id && !gateway.hubGatewayId)
                    .map((gateway) => ({
                      value: gateway.id,
                      label: `${gateway.name} · ${gateway.siteId}`,
                    }))}
                />
              </Form.Item>
              <Form.Item
                name="advertisedCidrsText"
                label={t('networkAccess.gateway.advertisedCidrs', '发布网段')}
              >
                <Input.TextArea placeholder="10.10.0.0/16" autoSize={{ minRows: 2, maxRows: 6 }} />
              </Form.Item>
              <Form.Item
                name="mtu"
                label="MTU"
                rules={[{ required: true, type: 'number', min: 1280, max: 1500 }]}
              >
                <InputNumber min={1280} max={1500} precision={0} style={{ width: '100%' }} />
              </Form.Item>
              <Form.Item
                name="persistentKeepaliveSeconds"
                label={t('networkAccess.gateway.keepalive', 'Keepalive（秒）')}
                rules={[{ required: true, type: 'number', min: 0, max: 300 }]}
              >
                <InputNumber min={0} max={300} precision={0} style={{ width: '100%' }} />
              </Form.Item>
              <Form.Item
                name="dnsServersText"
                label={t('networkAccess.gateway.dnsServers', 'DNS 服务器')}
              >
                <Input.TextArea placeholder="10.10.0.53" autoSize={{ minRows: 2, maxRows: 6 }} />
              </Form.Item>
            </>
          ) : null}
        </Form>
      </Modal>
    </ManagementDataPage>
  )
}
export default NetworkAccessPage
