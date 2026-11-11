import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { consolePermissionLabelMap } from '@/features/auth/permission-catalog'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import type { PersonalAccessToken } from '@/features/copilot'
import { useAuthStore } from '@/stores/auth-store'
import type { LinkedIdentity, UserProfile, UserSession } from '@/types'
import { loginProviderLabel, loginProviderTagColor } from '@/utils/login-provider'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime, formatRelativeTime } from '@/utils/time'
import {
  ApiOutlined,
  EditOutlined,
  IdcardOutlined,
  KeyOutlined,
  LinkOutlined,
  LockOutlined,
  MailOutlined,
  PhoneOutlined,
  PlusOutlined,
  QuestionCircleOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  StopOutlined,
  UploadOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { DescriptionsProps, TableColumnsType, TabsProps } from 'antd'
import {
  Alert,
  App,
  Avatar,
  Button,
  Card,
  Descriptions,
  Dropdown,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Skeleton,
  Slider,
  Space,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { ChangeEvent, CSSProperties } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { authKeys } from './keys'
import { authProfileApi } from './profile-api'
import { CLISessionsPanel } from './cli-sessions-panel'
import { useProfileKeyRotation } from './use-profile-key-rotation'
import './user-profile-page.css'

const { Paragraph, Text, Title } = Typography

type GatewayTokenExpiration = '7d' | '30d' | '90d' | 'custom'
type AvatarFit = 'cover' | 'contain' | 'fill'
type AvatarCrop = { x: number; y: number; zoom: number }

interface GatewayTokenFormValues {
  purpose?: 'llm' | 'cli'
  name: string
  permissionKeys?: string[]
  scopes?: string[]
  expiresIn?: GatewayTokenExpiration
  customExpiresAt?: string
}

interface ProfileFormValues {
  displayName?: string
  email: string
  phone?: string
  avatarUrl?: string
  avatarFit?: AvatarFit
}

interface AvatarFormValues {
  avatarUrl?: string
  avatarFit?: AvatarFit
}

interface PasswordFormValues {
  currentPassword: string
  newPassword: string
  confirmPassword: string
}

const gatewayPermissionOptions = ['ai.gateway.invoke', 'ai.gateway.view'].map((value) => ({
  label: consolePermissionLabelMap[value]
    ? `${consolePermissionLabelMap[value]} (${value})`
    : value,
  value,
}))

const gatewayExpirationOptions = [
  { label: '7 天', value: '7d' },
  { label: '30 天', value: '30d' },
  { label: '90 天', value: '90d' },
  { label: '自定义', value: 'custom' },
]

const avatarFileMaxBytes = 512 * 1024
const avatarOutputSize = 160
const defaultAvatarCrop: AvatarCrop = { x: 0, y: 0, zoom: 1 }

function compact(value?: null | string) {
  return String(value || '').trim()
}

function valueOrUnset(value?: null | string) {
  return compact(value) || '未设置'
}

function providerTag(value?: string) {
  return <Tag color={loginProviderTagColor(value)}>{loginProviderLabel(value)}</Tag>
}

function tagList(items?: string[], empty = '暂无') {
  const values = (items ?? []).map(compact).filter(Boolean)
  if (values.length === 0) {
    return <Text type="secondary">{empty}</Text>
  }
  return (
    <Space size={[6, 6]} wrap>
      {values.map((item) => (
        <Tag key={item}>{item}</Tag>
      ))}
    </Space>
  )
}

function compactTagList(items?: string[], max = 3) {
  const values = (items ?? []).map(compact).filter(Boolean)
  if (values.length === 0) {
    return <Text type="secondary">-</Text>
  }
  return (
    <Space className="soha-profile-compact-tags" size={[4, 4]} wrap>
      {values.slice(0, max).map((item) => (
        <Tag key={item}>{item}</Tag>
      ))}
      {values.length > max ? <Tag>+{values.length - max}</Tag> : null}
    </Space>
  )
}

function metadataText(metadata: Record<string, unknown> | undefined, key: string) {
  const value = metadata?.[key]
  return typeof value === 'string' && value.trim() ? value.trim() : '-'
}

function gatewayTokenStatus(record: Pick<PersonalAccessToken, 'expiresAt' | 'revokedAt'>) {
  if (record.revokedAt) return 'revoked'
  if (record.expiresAt) {
    const expiresAt = new Date(record.expiresAt).getTime()
    if (Number.isFinite(expiresAt) && expiresAt <= Date.now()) return 'expired'
  }
  return 'active'
}

function gatewayTokenExpiresAt(values: GatewayTokenFormValues) {
  const expiresIn = values.expiresIn ?? '90d'
  if (expiresIn === 'custom') return compact(values.customExpiresAt) || undefined
  const days = expiresIn === '7d' ? 7 : expiresIn === '90d' ? 90 : 30
  const expiresAt = new Date()
  expiresAt.setDate(expiresAt.getDate() + days)
  return expiresAt.toISOString()
}

function validDateTime(value?: string) {
  if (!value) return true
  return Number.isFinite(new Date(value).getTime()) && new Date(value).getTime() > Date.now()
}

function defaultGatewayTokenName(profile?: UserProfile) {
  const base = compact(profile?.username) || compact(profile?.displayName) || 'user'
  return `${base}-gateway-key`
}

function normalizeAvatarFit(value?: string): AvatarFit {
  return value === 'contain' || value === 'fill' ? value : 'cover'
}

function avatarStyle(fit?: string) {
  return { '--soha-avatar-fit': normalizeAvatarFit(fit) } as CSSProperties
}

function readFileAsDataURL(file: File) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error ?? new Error('读取头像文件失败'))
    reader.readAsDataURL(file)
  })
}

function isDataAvatarURL(value?: string) {
  return compact(value).toLowerCase().startsWith('data:image/')
}

function loadImage(source: string) {
  return new Promise<HTMLImageElement>((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error('头像图片加载失败'))
    image.src = source
  })
}

async function cropAvatarDataURL(source: string, crop: AvatarCrop) {
  const image = await loadImage(source)
  const canvas = document.createElement('canvas')
  canvas.width = avatarOutputSize
  canvas.height = avatarOutputSize
  const context = canvas.getContext('2d')
  if (!context) {
    throw new Error('浏览器不支持头像裁剪')
  }
  const baseScale = Math.max(
    avatarOutputSize / image.naturalWidth,
    avatarOutputSize / image.naturalHeight,
  )
  const scale = baseScale * crop.zoom
  const width = image.naturalWidth * scale
  const height = image.naturalHeight * scale
  context.drawImage(
    image,
    avatarOutputSize / 2 + crop.x - width / 2,
    avatarOutputSize / 2 + crop.y - height / 2,
    width,
    height,
  )
  return canvas.toDataURL('image/png')
}

function avatarURLValid(value?: string) {
  const source = compact(value)
  if (!source) return true
  const lower = source.toLowerCase()
  if (lower.startsWith('data:image/') && lower.includes(';base64,')) return true
  try {
    const parsed = new URL(source)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:'
  } catch {
    return false
  }
}

function authUserFromProfile(profile: UserProfile) {
  return {
    userId: profile.userId,
    userName: valueOrUnset(profile.displayName || profile.username),
    username: profile.username,
    displayName: profile.displayName,
    email: profile.email,
    phone: profile.phone,
    avatarUrl: profile.avatarUrl,
    avatarFit: profile.avatarFit,
    status: profile.status,
    roles: profile.roles,
    teams: profile.teams,
    projects: profile.projects,
    tags: profile.tags,
  }
}

function tokenSummaryText(tokens: PersonalAccessToken[]) {
  const activeCount = tokens.filter((item) => gatewayTokenStatus(item) === 'active').length
  const expiredCount = tokens.filter((item) => gatewayTokenStatus(item) === 'expired').length
  const revokedCount = tokens.filter((item) => gatewayTokenStatus(item) === 'revoked').length
  return { activeCount, expiredCount, revokedCount }
}

export function UserProfilePage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [searchParams, setSearchParams] = useSearchParams()
  const setAuthUser = useAuthStore((state) => state.setUser)
  const authenticatedUserId = useAuthStore((state) => state.user?.userId)
  const [profileForm] = Form.useForm<ProfileFormValues>()
  const [avatarForm] = Form.useForm<AvatarFormValues>()
  const [passwordForm] = Form.useForm<PasswordFormValues>()
  const [tokenForm] = Form.useForm<GatewayTokenFormValues>()
  const [profileOpen, setProfileOpen] = useState(false)
  const [avatarOpen, setAvatarOpen] = useState(false)
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [createTokenOpen, setCreateTokenOpen] = useState(false)
  const [oneTimeToken, setOneTimeToken] = useState<{
    title: string
    value: string
    prefix?: string
  } | null>(null)
  const [avatarCrop, setAvatarCrop] = useState<AvatarCrop>(defaultAvatarCrop)
  const avatarFileInputRef = useRef<HTMLInputElement | null>(null)
  const avatarDragRef = useRef<{ clientX: number; clientY: number; crop: AvatarCrop } | null>(null)
  const avatarUrlValue = Form.useWatch('avatarUrl', avatarForm)
  const tokenPurpose = Form.useWatch('purpose', tokenForm)

  const profileQuery = useQuery({
    queryKey: authKeys.profile(),
    queryFn: authProfileApi.get,
  })
  const identityProvidersQuery = useQuery({
    queryKey: [...authKeys.profile(), 'identity-providers'],
    queryFn: authProfileApi.listIdentityProviders,
  })
  const permissionSnapshotQuery = usePermissionSnapshot()
  const snapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canViewGatewayKeys = hasPermission(snapshot, 'ai.gateway.view')
  const canIssueGatewayKeys = hasPermission(snapshot, 'ai.gateway.invoke')

  const gatewayTokensQuery = useQuery({
    queryKey: authKeys.profileGatewayTokens(),
    queryFn: authProfileApi.listGatewayTokens,
    enabled: canViewGatewayKeys,
  })

  const profile = readableQueryData(profileQuery)?.data
  const denied = queryAccessDenied(profileQuery)
  const tokenDenied =
    queryAccessDenied(gatewayTokensQuery) ||
    !canIssueGatewayKeys ||
    denied ||
    !authenticatedUserId ||
    profile?.userId !== authenticatedUserId
  const secretGeneration = useRef(0)
  const secretAllowed = useRef(!tokenDenied)
  secretAllowed.current = !tokenDenied
  const gatewayTokens = canViewGatewayKeys
    ? (readableQueryData(gatewayTokensQuery)?.data ?? [])
    : []
  const gatewayTokenSummary = tokenSummaryText(gatewayTokens)
  const displayName = valueOrUnset(profile?.displayName || profile?.username)
  const avatarText = displayName === '未设置' ? 'U' : displayName.charAt(0).toUpperCase()
  const primaryIdentity = profile?.identities?.[0]
  const linkableProviders = (readableQueryData(identityProvidersQuery)?.data ?? []).filter(
    (provider) =>
      provider.enabled &&
      provider.type !== 'password' &&
      !profile?.identities?.some(
        (identity) =>
          identity.providerId === provider.id && identity.providerType === provider.type,
      ),
  )
  const currentAvatarFit = normalizeAvatarFit(profile?.avatarFit)

  const openProfileEditor = () => {
    if (profileBlocked) return
    profileForm.setFieldsValue({
      displayName: profile?.displayName,
      email: profile?.email ?? '',
      phone: profile?.phone,
    })
    setProfileOpen(true)
  }

  const openAvatarEditor = () => {
    if (profileBlocked) return
    avatarForm.setFieldsValue({
      avatarUrl: profile?.avatarUrl ?? '',
      avatarFit: normalizeAvatarFit(profile?.avatarFit),
    })
    setAvatarCrop(defaultAvatarCrop)
    setAvatarOpen(true)
  }

  const openPasswordEditor = () => {
    if (profileBlocked) return
    passwordForm.resetFields()
    setPasswordOpen(true)
  }

  useEffect(() => {
    if (searchParams.get('changePassword') !== '1') return
    passwordForm.resetFields()
    setPasswordOpen(true)
    const next = new URLSearchParams(searchParams)
    next.delete('changePassword')
    setSearchParams(next, { replace: true })
  }, [passwordForm, searchParams, setSearchParams])

  useEffect(() => {
    const linkedProvider = searchParams.get('linked')
    if (!linkedProvider) return
    void queryClient.invalidateQueries({ queryKey: authKeys.profile() })
    void message.success('登录方式关联成功')
    const next = new URLSearchParams(searchParams)
    next.delete('linked')
    setSearchParams(next, { replace: true })
  }, [message, queryClient, searchParams, setSearchParams])

  const linkIdentity = useOperationMutation(
    {
      mutationFn: async (providerId: string) => {
        const response = await authProfileApi.beginIdentityLink(providerId)
        window.location.assign(response.data.url)
      },
    },
    { label: ['关联登录', 'Link identity'], target: (id) => id },
  )
  const beginIdentityLink = (providerId: string) => {
    if (!denied && !linkIdentity.blocked) linkIdentity.mutate(providerId)
  }

  const openCreateToken = (purpose: 'llm' | 'cli' = 'llm') => {
    if (tokenBlocked || tokenDenied) return
    createTokenMutation.reset()
    tokenForm.setFieldsValue({
      purpose,
      name:
        purpose === 'cli' ? `${profile?.username || 'user'}-cli` : defaultGatewayTokenName(profile),
      permissionKeys: purpose === 'cli' ? [] : ['ai.gateway.invoke'],
      scopes: [],
      expiresIn: '90d',
      customExpiresAt: undefined,
    })
    setCreateTokenOpen(true)
  }

  const refreshGatewayTokens = () =>
    queryClient.invalidateQueries({ queryKey: authKeys.profileGatewayTokens() })

  const handleAvatarFileChange = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return
    if (!file.type.startsWith('image/')) {
      message.error('请选择图片文件')
      return
    }
    if (file.size > avatarFileMaxBytes) {
      message.error('头像文件不能超过 512KB')
      return
    }
    try {
      avatarForm.setFieldsValue({ avatarUrl: await readFileAsDataURL(file) })
      setAvatarCrop(defaultAvatarCrop)
    } catch (readError) {
      message.error(readError instanceof Error ? readError.message : '读取头像文件失败')
    }
  }

  const handleAvatarSubmit = async (values: AvatarFormValues) => {
    if (profileBlocked) return
    const source = compact(values.avatarUrl)
    try {
      updateProfileMutation.mutate({
        displayName: profile?.displayName,
        email: profile?.email ?? '',
        phone: profile?.phone,
        avatarUrl:
          source && isDataAvatarURL(source) ? await cropAvatarDataURL(source, avatarCrop) : source,
        avatarFit: 'cover',
      })
    } catch (cropError) {
      message.error(cropError instanceof Error ? cropError.message : '头像裁剪失败')
    }
  }

  const updateProfileMutation = useOperationMutation(
    {
      mutationFn: async (values: ProfileFormValues) => {
        const res = await authProfileApi.update({
          displayName: compact(values.displayName),
          email: compact(values.email),
          phone: compact(values.phone),
          avatarUrl:
            values.avatarUrl === undefined
              ? compact(profile?.avatarUrl)
              : compact(values.avatarUrl),
          avatarFit:
            values.avatarFit === undefined
              ? currentAvatarFit
              : normalizeAvatarFit(values.avatarFit),
        })
        return {
          ...res,
          avatarSaved: !compact(values.avatarUrl) || Boolean(compact(res.data?.avatarUrl)),
        }
      },
      onSuccess: (res) => {
        if (!res.avatarSaved) return
        queryClient.setQueryData(authKeys.profile(), res)
        void queryClient.invalidateQueries({ queryKey: authKeys.profile() })
        if (res.data) setAuthUser(authUserFromProfile(res.data))
        setProfileOpen(false)
        setAvatarOpen(false)
        profileForm.resetFields()
        avatarForm.resetFields()
      },
    },
    {
      label: ['更新账号资料', 'Update profile'],
      target: () => profile?.userId || '当前账号',
      resultError: (res) =>
        res.avatarSaved ? undefined : new Error('头像保存未生效，请核对账号资料'),
      receipt: () => ({ message: '账号资料已更新' }),
    },
  )
  const changePasswordMutation = useOperationMutation(
    {
      gcTime: 0,
      mutationFn: async (values: PasswordFormValues) => {
        await authProfileApi.changePassword({
          currentPassword: values.currentPassword,
          newPassword: values.newPassword,
        })
      },
      onSuccess: () => {
        setPasswordOpen(false)
        passwordForm.resetFields()
      },
    },
    {
      label: ['修改密码', 'Change password'],
      target: () => profile?.userId || '当前账号',
      receipt: () => ({ message: '密码修改请求已返回' }),
    },
  )
  const createTokenMutation = useOperationMutation(
    {
      gcTime: 0,
      mutationFn: async (values: GatewayTokenFormValues) => {
        const generation = secretGeneration.current
        const owner = useAuthStore.getState().user?.userId
        const res = await authProfileApi.createGatewayToken({
          name: values.name,
          permissionKeys: values.permissionKeys ?? [],
          scopes: values.scopes ?? [],
          expiresAt: gatewayTokenExpiresAt(values),
        })
        if (
          generation === secretGeneration.current &&
          owner === useAuthStore.getState().user?.userId &&
          secretAllowed.current &&
          res.data?.value
        ) {
          setOneTimeToken({
            title: values.purpose === 'cli' ? 'CLI 个人访问凭据已生成' : 'LLM Gateway Key 已生成',
            value: res.data.value,
            prefix: res.data.token?.tokenPrefix,
          })
        }
        return res.data?.token
      },
      onSuccess: () => {
        setCreateTokenOpen(false)
        tokenForm.resetFields()
        void refreshGatewayTokens()
      },
    },
    {
      label: ['生成 Gateway key', 'Create Gateway key'],
      target: (values) => values.name,
      receipt: (token) => ({ message: 'Gateway key 记录已返回', evidence: token?.id }),
    },
  )
  const revokeTokenMutation = useOperationMutation(
    {
      mutationFn: authProfileApi.revokeGatewayToken,
      onSuccess: () => {
        void refreshGatewayTokens()
      },
    },
    {
      label: ['吊销 Gateway key', 'Revoke Gateway key'],
      target: (id) => id,
      receipt: () => ({ message: 'Gateway key 吊销请求已返回' }),
    },
  )
  const rotateTokenMutation = useProfileKeyRotation(
    authenticatedUserId || '',
    (value, prefix) => {
      if (secretAllowed.current) setOneTimeToken({ title: '个人访问凭据已轮换', value, prefix })
    },
    () => {
      void refreshGatewayTokens()
    },
  )
  const profileBlocked = denied || updateProfileMutation.blocked || changePasswordMutation.blocked
  const tokenBlocked =
    createTokenMutation.blocked || rotateTokenMutation.blocked || revokeTokenMutation.blocked
  useEffect(() => {
    if (denied || queryAccessDenied(updateProfileMutation)) {
      setProfileOpen(false)
      setAvatarOpen(false)
      profileForm.resetFields()
      avatarForm.resetFields()
    }
    if (denied || queryAccessDenied(changePasswordMutation)) {
      setPasswordOpen(false)
      passwordForm.resetFields()
    }
  }, [
    denied,
    updateProfileMutation.error,
    changePasswordMutation.error,
    profileForm,
    avatarForm,
    passwordForm,
  ])
  useEffect(() => {
    if (
      tokenDenied ||
      queryAccessDenied(createTokenMutation) ||
      queryAccessDenied(rotateTokenMutation)
    ) {
      secretGeneration.current += 1
      setOneTimeToken(null)
      setCreateTokenOpen(false)
      tokenForm.resetFields()
    }
  }, [tokenDenied, createTokenMutation.error, rotateTokenMutation.error, tokenForm])
  useEffect(
    () => () => {
      secretGeneration.current += 1
    },
    [],
  )
  const operationFeedback = (
    <>
      {!profileOpen && !avatarOpen ? updateProfileMutation.feedback : null}
      {!passwordOpen ? changePasswordMutation.feedback : null}
      {!createTokenOpen ? createTokenMutation.feedback : null}
      {revokeTokenMutation.feedback}
      {rotateTokenMutation.feedback}
      {linkIdentity.feedback}
    </>
  )

  const identityColumns = useMemo<TableColumnsType<LinkedIdentity>>(
    () => [
      {
        title: '登录方式',
        dataIndex: 'providerType',
        width: 120,
        render: (value: string) => providerTag(value),
      },
      {
        title: '提供方',
        dataIndex: 'providerId',
        width: 150,
        render: (value: string) => valueOrUnset(value),
      },
      {
        title: '外部账号',
        dataIndex: 'providerUserId',
        ellipsis: true,
        render: (value: string) => valueOrUnset(value),
      },
      {
        title: '关联资料',
        key: 'profile',
        width: 220,
        render: (_: unknown, record) => (
          <Space orientation="vertical" size={0}>
            <Text>{valueOrUnset(record.displayName || record.email)}</Text>
            {record.email ? <Text type="secondary">{record.email}</Text> : null}
          </Space>
        ),
      },
      {
        title: '最近登录',
        dataIndex: 'lastLoginAt',
        width: 180,
        render: (value: string) => formatDateTime(value),
      },
    ],
    [],
  )

  const sessionColumns = useMemo<TableColumnsType<UserSession>>(
    () => [
      {
        title: '登录方式',
        dataIndex: 'providerType',
        width: 120,
        render: (value: string) => providerTag(value),
      },
      {
        title: '来源',
        key: 'source',
        width: 110,
        render: (_: unknown, record) => metadataText(record.metadata, 'source'),
      },
      {
        title: 'IP',
        key: 'sourceIp',
        width: 140,
        ellipsis: true,
        render: (_: unknown, record) => metadataText(record.metadata, 'sourceIp'),
      },
      {
        title: '设备',
        key: 'userAgent',
        ellipsis: true,
        render: (_: unknown, record) => metadataText(record.metadata, 'userAgent'),
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 110,
        render: (value: string) => <StatusTag value={value} />,
      },
      {
        title: '最近活跃',
        dataIndex: 'lastSeenAt',
        width: 170,
        render: (value: string) => (
          <Space orientation="vertical" size={0}>
            <Text>{formatDateTime(value)}</Text>
            <Text type="secondary">{formatRelativeTime(value)}</Text>
          </Space>
        ),
      },
      {
        title: '过期时间',
        dataIndex: 'expiresAt',
        width: 170,
        render: (value: string) => formatDateTime(value),
      },
    ],
    [],
  )

  const gatewayTokenColumns = useMemo<TableColumnsType<PersonalAccessToken>>(
    () => [
      {
        title: 'Key',
        dataIndex: 'name',
        width: 210,
        ellipsis: true,
        render: (_: string, record) => (
          <Space orientation="vertical" size={0}>
            <Text strong>{record.name || record.id}</Text>
            <Text type="secondary" copyable={{ text: record.tokenPrefix }}>
              {record.tokenPrefix}
            </Text>
          </Space>
        ),
      },
      {
        title: '权限',
        dataIndex: 'permissionKeys',
        className: 'soha-profile-gateway-tags-cell',
        width: 180,
        render: (value: string[]) => compactTagList(value, 3),
      },
      {
        title: 'Scopes',
        dataIndex: 'scopes',
        className: 'soha-profile-gateway-tags-cell',
        width: 110,
        render: (value: string[]) => compactTagList(value, 2),
      },
      {
        title: '最近使用',
        dataIndex: 'lastUsedAt',
        width: 135,
        render: (value: string) => (
          <Space orientation="vertical" size={0}>
            <Text>{formatDateTime(value)}</Text>
            {value ? <Text type="secondary">{formatRelativeTime(value)}</Text> : null}
          </Space>
        ),
      },
      {
        title: '过期时间',
        dataIndex: 'expiresAt',
        width: 135,
        render: (value: string) => formatDateTime(value),
      },
      {
        title: '状态',
        key: 'status',
        width: 80,
        render: (_: unknown, record) => <StatusTag value={gatewayTokenStatus(record)} />,
      },
      {
        title: '操作',
        key: 'actions',
        align: 'center',
        className: 'soha-table-actions-column',
        width: 64,
        render: (_: unknown, record) => (
          <Space className="soha-row-action-icons" size={2}>
            <Popconfirm
              title="轮换个人访问凭据？"
              description="旧 Key 会立即撤销；新 Key 默认 90 天有效，明文只展示一次。"
              okButtonProps={{
                loading: rotateTokenMutation.isPending,
                disabled: tokenBlocked || tokenDenied,
              }}
              onConfirm={() => rotateTokenMutation.mutate(record)}
            >
              <ManagementIconButton
                size="small"
                tooltip="轮换"
                aria-label="轮换 Gateway key"
                icon={<ReloadOutlined />}
                loading={rotateTokenMutation.isPending}
                disabled={!canIssueGatewayKeys || !!record.revokedAt}
              />
            </Popconfirm>
            <Popconfirm
              title="吊销 Gateway key？"
              description="吊销后，终端和其他客户端均无法继续使用此 Key。"
              okButtonProps={{
                danger: true,
                loading: revokeTokenMutation.isPending,
                disabled: tokenBlocked || tokenDenied,
              }}
              onConfirm={() => revokeTokenMutation.mutate(record.id)}
            >
              <ManagementIconButton
                size="small"
                tooltip="吊销"
                aria-label="吊销 Gateway key"
                danger
                icon={<StopOutlined />}
                loading={revokeTokenMutation.isPending}
                disabled={!canIssueGatewayKeys || !!record.revokedAt}
              />
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [canIssueGatewayKeys, revokeTokenMutation, rotateTokenMutation, tokenBlocked, tokenDenied],
  )

  const descriptionItems = useMemo<DescriptionsProps['items']>(
    () => [
      {
        key: 'userId',
        label: 'ID',
        children: <Text copyable>{valueOrUnset(profile?.userId)}</Text>,
        span: 2,
      },
      { key: 'username', label: '用户名', children: valueOrUnset(profile?.username) },
      { key: 'displayName', label: '显示名', children: displayName },
      { key: 'email', label: '邮箱', children: valueOrUnset(profile?.email) },
      { key: 'phone', label: '电话', children: valueOrUnset(profile?.phone) },
      {
        key: 'status',
        label: '账号状态',
        children: profile?.status ? <StatusTag value={profile.status} /> : '未设置',
      },
      { key: 'lastLoginAt', label: '最近登录', children: formatDateTime(profile?.lastLoginAt) },
      {
        key: 'provider',
        label: '主要登录方式',
        children: providerTag(primaryIdentity?.providerType),
      },
    ],
    [displayName, primaryIdentity?.providerType, profile],
  )

  const permissionItems = useMemo<DescriptionsProps['items']>(
    () => [
      { key: 'roles', label: '角色', children: tagList(profile?.roles) },
      { key: 'teams', label: '组织', children: tagList(profile?.teams) },
      { key: 'projects', label: '项目', children: tagList(profile?.projects) },
      { key: 'tags', label: '标签', children: tagList(profile?.tags) },
    ],
    [profile],
  )

  const tabItems: TabsProps['items'] = [
    {
      key: 'identities',
      label: '关联登录',
      children: (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <ReadFeedback label="可关联登录方式" query={identityProvidersQuery} />
          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <Dropdown
              disabled={
                linkableProviders.length === 0 ||
                linkIdentity.blocked ||
                queryAccessDenied(identityProvidersQuery)
              }
              menu={{
                items: linkableProviders.map((provider) => ({
                  key: provider.id || provider.name,
                  label: provider.name,
                })),
                onClick: ({ key }) => void beginIdentityLink(key),
              }}
              trigger={['click']}
            >
              <Button className="soha-profile-link-button" icon={<LinkOutlined />}>
                关联登录
              </Button>
            </Dropdown>
          </div>
          <AdminTable
            enableColumnSelection={false}
            columns={identityColumns}
            dataSource={profile?.identities ?? []}
            empty={<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无关联登录方式" />}
            pagination={false}
            rowKey="id"
            scroll={{ x: 760 }}
            tableSize="small"
          />
        </Space>
      ),
    },
    {
      key: 'sessions',
      label: '活跃会话',
      children: (
        <AdminTable
          enableColumnSelection={false}
          columns={sessionColumns}
          dataSource={profile?.sessions ?? []}
          empty={<Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无活跃会话" />}
          pagination={false}
          rowKey="id"
          scroll={{ x: 900 }}
          tableSize="small"
        />
      ),
    },
    {
      key: 'cli-sessions',
      label: 'CLI 会话',
      children: <CLISessionsPanel key={authenticatedUserId} />,
    },
    {
      key: 'access',
      label: '权限归属',
      children: <Descriptions bordered column={1} items={permissionItems} size="small" />,
    },
  ]

  if (profileQuery.isError && !profile) {
    return (
      <div className="soha-page">
        <ReadFeedback label="个人中心" query={profileQuery} />
        {operationFeedback}
      </div>
    )
  }

  if (profileQuery.isLoading || !profile) {
    return (
      <div className="soha-page soha-profile-page">
        <Skeleton active paragraph={{ rows: 10 }} />
      </div>
    )
  }

  const gatewayKeysUnavailable = !permissionSnapshotQuery.isLoading && !canViewGatewayKeys
  const gatewayKeysSummary = canViewGatewayKeys
    ? readableQueryData(gatewayTokensQuery)
      ? `${gatewayTokenSummary.activeCount} active / ${gatewayTokens.length} total`
      : '未读取'
    : '未授权'
  const avatarPreviewURL =
    avatarOpen && avatarUrlValue !== undefined
      ? compact(avatarUrlValue)
      : compact(profile.avatarUrl)
  const avatarCanCrop = isDataAvatarURL(avatarPreviewURL)

  return (
    <div className="soha-page soha-profile-page">
      <ReadFeedback label="个人中心" query={profileQuery} />
      {operationFeedback}
      <div className="soha-profile-layout">
        <Card className="soha-profile-summary-card" size="small">
          <div className="soha-profile-identity">
            <button
              className="soha-profile-avatar-button"
              type="button"
              aria-label="更换头像"
              disabled={profileBlocked}
              onClick={openAvatarEditor}
            >
              <Avatar
                className="soha-profile-avatar"
                size={64}
                src={compact(profile.avatarUrl) || undefined}
                style={avatarStyle(currentAvatarFit)}
              >
                {avatarText}
              </Avatar>
              <span className="soha-profile-avatar-edit">
                <EditOutlined />
              </span>
            </button>
            <div className="soha-profile-identity__main">
              <Title level={4}>{displayName}</Title>
              <Text type="secondary">{profile.email || profile.username}</Text>
            </div>
          </div>
          <div className="soha-profile-summary-list">
            <div className="soha-profile-summary-item">
              <UserOutlined />
              <span>{valueOrUnset(profile.username)}</span>
            </div>
            <div className="soha-profile-summary-item">
              <MailOutlined />
              <span>{valueOrUnset(profile.email)}</span>
            </div>
            <div className="soha-profile-summary-item">
              <PhoneOutlined />
              <span>{valueOrUnset(profile.phone)}</span>
            </div>
            <div className="soha-profile-summary-item">
              <KeyOutlined />
              <span>{loginProviderLabel(primaryIdentity?.providerType)}</span>
            </div>
          </div>
          <div className="soha-profile-kpi-grid">
            <div>
              <Text type="secondary">活跃会话</Text>
              <strong>{profile.sessions?.length ?? 0}</strong>
            </div>
            <div>
              <Text type="secondary">Gateway keys</Text>
              <strong>{gatewayKeysSummary}</strong>
            </div>
          </div>
          <div className="soha-profile-tag-section">
            <Text type="secondary">角色</Text>
            {tagList(profile.roles)}
          </div>
          <div className="soha-profile-tag-section">
            <Text type="secondary">组织</Text>
            {tagList(profile.teams)}
          </div>
        </Card>

        <div className="soha-profile-main">
          <Card
            size="small"
            title={
              <Space>
                <IdcardOutlined />
                <span>账号资料</span>
              </Space>
            }
            extra={
              <Space className="soha-profile-actions" size={8} wrap>
                <Button
                  size="small"
                  aria-label="编辑资料"
                  icon={<EditOutlined />}
                  disabled={profileBlocked}
                  onClick={openProfileEditor}
                >
                  编辑信息
                </Button>
                <Button
                  size="small"
                  type="primary"
                  aria-label="修改密码"
                  icon={<LockOutlined />}
                  disabled={profileBlocked}
                  onClick={openPasswordEditor}
                >
                  修改密码
                </Button>
              </Space>
            }
          >
            <Descriptions
              bordered
              column={{ xs: 1, md: 2 }}
              items={descriptionItems}
              size="small"
            />
          </Card>

          <Card
            className="soha-profile-gateway-card"
            size="small"
            title={
              <Space>
                <ApiOutlined />
                <span>个人访问凭据（PAT）</span>
              </Space>
            }
            extra={
              <Space wrap>
                <Button
                  size="small"
                  disabled={!canIssueGatewayKeys || tokenBlocked || tokenDenied}
                  onClick={() => openCreateToken('llm')}
                >
                  生成 LLM Key
                </Button>
                <Button
                  size="small"
                  type="primary"
                  icon={<PlusOutlined />}
                  disabled={!canIssueGatewayKeys || tokenBlocked || tokenDenied}
                  onClick={() => openCreateToken('cli')}
                >
                  生成 CLI PAT
                </Button>
              </Space>
            }
          >
            <div className="soha-profile-gateway-overview">
              <div>
                <Space size={4}>
                  <Text strong>用于终端、MCP 或 LLM 的个人访问凭据</Text>
                  <Tooltip
                    title="CLI PAT 只能使用你明确选择且当前拥有的权限；LLM Key 默认仅调用 AI Gateway。两者共用 PAT 记录，实际权限以每条记录为准。明文仅展示一次。"
                    trigger={['hover', 'focus']}
                  >
                    <QuestionCircleOutlined aria-label="个人访问凭据说明" tabIndex={0} />
                  </Tooltip>
                </Space>
              </div>
              <Space size={6} wrap>
                <Tag color="success">active {gatewayTokenSummary.activeCount}</Tag>
                <Tag>expired {gatewayTokenSummary.expiredCount}</Tag>
                <Tag>revoked {gatewayTokenSummary.revokedCount}</Tag>
              </Space>
            </div>

            {gatewayKeysUnavailable ? (
              <Alert
                type="warning"
                showIcon
                title="当前账号没有个人访问凭据查看权限"
                description="需要 ai.gateway.view 查看已有 key，需要 ai.gateway.invoke 生成、轮换或吊销个人 key。"
              />
            ) : (
              <AdminTable
                enableColumnSelection={false}
                columns={gatewayTokenColumns}
                error={gatewayTokensQuery.error}
                hasReadData={gatewayTokensQuery.data !== undefined}
                onRefresh={() => void gatewayTokensQuery.refetch()}
                refreshing={gatewayTokensQuery.isFetching}
                dataSource={gatewayTokens}
                loading={gatewayTokensQuery.isLoading || permissionSnapshotQuery.isLoading}
                empty={
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无个人访问凭据" />
                }
                pagination={false}
                rowKey="id"
                scroll={{ x: 920 }}
                tableSize="small"
                tableLayout="fixed"
              />
            )}
          </Card>

          <Card
            size="small"
            title={
              <Space>
                <SafetyCertificateOutlined />
                <span>安全与访问</span>
              </Space>
            }
          >
            <Tabs items={tabItems} size="small" />
          </Card>
        </div>
      </div>

      <Modal
        title="更换头像"
        open={avatarOpen && !denied}
        closable={!updateProfileMutation.blocked}
        keyboard={!updateProfileMutation.blocked}
        mask={{ closable: !updateProfileMutation.blocked }}
        okButtonProps={{ disabled: profileBlocked }}
        cancelButtonProps={{ disabled: updateProfileMutation.blocked }}
        okText="保存"
        cancelText="取消"
        confirmLoading={updateProfileMutation.isPending}
        destroyOnHidden
        onCancel={() => {
          if (updateProfileMutation.blocked) return
          setAvatarOpen(false)
          avatarForm.resetFields()
        }}
        onOk={() => avatarForm.submit()}
      >
        {updateProfileMutation.feedback}
        <Form
          disabled={profileBlocked}
          form={avatarForm}
          layout="vertical"
          size="small"
          onFinish={(values) => void handleAvatarSubmit(values)}
        >
          <div className="soha-profile-avatar-editor">
            {avatarCanCrop ? (
              <div
                className="soha-profile-avatar-crop"
                onPointerDown={(event) => {
                  if (profileBlocked) return
                  avatarDragRef.current = {
                    clientX: event.clientX,
                    clientY: event.clientY,
                    crop: avatarCrop,
                  }
                  event.currentTarget.setPointerCapture(event.pointerId)
                }}
                onPointerMove={(event) => {
                  const drag = avatarDragRef.current
                  if (!drag || profileBlocked) return
                  setAvatarCrop({
                    ...drag.crop,
                    x: drag.crop.x + event.clientX - drag.clientX,
                    y: drag.crop.y + event.clientY - drag.clientY,
                  })
                }}
                onPointerUp={() => {
                  avatarDragRef.current = null
                }}
              >
                <img
                  src={avatarPreviewURL}
                  alt="头像预览"
                  draggable={false}
                  style={{
                    transform: `translate(${avatarCrop.x}px, ${avatarCrop.y}px) scale(${avatarCrop.zoom})`,
                  }}
                />
              </div>
            ) : (
              <Avatar
                className="soha-profile-avatar soha-profile-avatar-preview"
                size={96}
                src={avatarPreviewURL || undefined}
                style={avatarStyle(currentAvatarFit)}
              >
                {avatarText}
              </Avatar>
            )}
            <Space size={8} wrap>
              <Button
                disabled={profileBlocked}
                icon={<UploadOutlined />}
                onClick={() => avatarFileInputRef.current?.click()}
              >
                上传图片
              </Button>
              <Button
                disabled={profileBlocked}
                onClick={() => {
                  avatarForm.setFieldsValue({ avatarUrl: '' })
                  setAvatarCrop(defaultAvatarCrop)
                }}
              >
                清除头像
              </Button>
            </Space>
            {avatarCanCrop ? (
              <div className="soha-profile-avatar-zoom">
                <Text type="secondary">缩放</Text>
                <Slider
                  min={1}
                  max={3}
                  step={0.01}
                  value={avatarCrop.zoom}
                  tooltip={{ formatter: null }}
                  onChange={(value) => setAvatarCrop((crop) => ({ ...crop, zoom: Number(value) }))}
                />
              </div>
            ) : null}
            <input
              ref={avatarFileInputRef}
              className="soha-profile-avatar-file"
              hidden
              style={{ display: 'none' }}
              type="file"
              accept="image/png,image/jpeg,image/webp,image/gif"
              onChange={handleAvatarFileChange}
            />
          </div>
          <Form.Item
            name="avatarUrl"
            label="图片 URL"
            rules={[
              {
                validator: (_, value) =>
                  avatarURLValid(value)
                    ? Promise.resolve()
                    : Promise.reject(new Error('请输入 http(s) 图片地址或上传图片文件')),
              },
            ]}
          >
            <Input
              allowClear
              prefix={<LinkOutlined />}
              placeholder="https://example.com/avatar.png"
            />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="编辑账号资料"
        open={profileOpen && !denied}
        closable={!updateProfileMutation.blocked}
        keyboard={!updateProfileMutation.blocked}
        mask={{ closable: !updateProfileMutation.blocked }}
        okButtonProps={{ disabled: profileBlocked }}
        cancelButtonProps={{ disabled: updateProfileMutation.blocked }}
        okText="保存"
        cancelText="取消"
        confirmLoading={updateProfileMutation.isPending}
        destroyOnHidden
        onCancel={() => {
          if (updateProfileMutation.blocked) return
          setProfileOpen(false)
          profileForm.resetFields()
        }}
        onOk={() => profileForm.submit()}
      >
        {updateProfileMutation.feedback}
        <Form
          disabled={profileBlocked}
          form={profileForm}
          layout="vertical"
          size="small"
          onFinish={(values) => updateProfileMutation.mutate(values)}
        >
          <Form.Item name="displayName" label="显示名">
            <Input allowClear maxLength={64} placeholder="显示在控制台中的名称" />
          </Form.Item>
          <Form.Item
            name="email"
            label="邮箱"
            rules={[
              { required: true, message: '请输入邮箱' },
              { type: 'email', message: '请输入有效邮箱' },
            ]}
          >
            <Input allowClear maxLength={120} />
          </Form.Item>
          <Form.Item name="phone" label="电话">
            <Input allowClear maxLength={32} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title="修改密码"
        open={passwordOpen && !denied}
        closable={!changePasswordMutation.blocked}
        keyboard={!changePasswordMutation.blocked}
        mask={{ closable: !changePasswordMutation.blocked }}
        okButtonProps={{ disabled: profileBlocked }}
        cancelButtonProps={{ disabled: changePasswordMutation.blocked }}
        okText="保存"
        cancelText="取消"
        confirmLoading={changePasswordMutation.isPending}
        destroyOnHidden
        onCancel={() => {
          if (changePasswordMutation.blocked) return
          setPasswordOpen(false)
          passwordForm.resetFields()
        }}
        onOk={() => passwordForm.submit()}
      >
        {changePasswordMutation.feedback}
        <Form
          disabled={profileBlocked}
          form={passwordForm}
          layout="vertical"
          size="small"
          onFinish={(values) => changePasswordMutation.mutate(values)}
        >
          <Form.Item
            name="currentPassword"
            label="当前密码"
            rules={[{ required: true, message: '请输入当前密码' }]}
          >
            <Input.Password autoComplete="current-password" />
          </Form.Item>
          <Form.Item
            name="newPassword"
            label="新密码"
            rules={[
              { required: true, message: '请输入新密码' },
              { min: 8, message: '新密码至少 8 位' },
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            name="confirmPassword"
            label="确认新密码"
            dependencies={['newPassword']}
            rules={[
              { required: true, message: '请再次输入新密码' },
              ({ getFieldValue }) => ({
                validator: (_, value) =>
                  !value || getFieldValue('newPassword') === value
                    ? Promise.resolve()
                    : Promise.reject(new Error('两次输入的新密码不一致')),
              }),
            ]}
          >
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={tokenPurpose === 'cli' ? '生成 CLI 个人访问凭据' : '生成 LLM Gateway Key'}
        open={createTokenOpen && !tokenDenied}
        closable={!createTokenMutation.blocked}
        keyboard={!createTokenMutation.blocked}
        mask={{ closable: !createTokenMutation.blocked }}
        okButtonProps={{ disabled: tokenBlocked || tokenDenied }}
        cancelButtonProps={{ disabled: createTokenMutation.blocked }}
        okText="生成 key"
        cancelText="取消"
        confirmLoading={createTokenMutation.isPending}
        destroyOnHidden
        onCancel={() => {
          if (createTokenMutation.blocked) return
          setCreateTokenOpen(false)
          tokenForm.resetFields()
        }}
        onOk={() => tokenForm.submit()}
      >
        {createTokenMutation.feedback}
        <Form
          disabled={tokenBlocked || tokenDenied}
          form={tokenForm}
          layout="vertical"
          initialValues={{ permissionKeys: ['ai.gateway.invoke'], scopes: [], expiresIn: '90d' }}
          onFinish={(values) => createTokenMutation.mutate(values)}
        >
          <Form.Item name="purpose" hidden>
            <Input />
          </Form.Item>
          <Alert
            showIcon
            type="info"
            title={
              tokenPurpose === 'cli' ? '选择这个终端实际需要的权限' : '仅用于 LLM Gateway 调用'
            }
            description={
              tokenPurpose === 'cli'
                ? '创建后通过 soha login --with-token 从标准输入导入。新 Key 默认 90 天有效；不会继承你的全部权限。'
                : '默认仅授予 ai.gateway.invoke，不自动获得平台管理权限。'
            }
          />
          <Form.Item
            name="name"
            label="Key 名称"
            rules={[{ required: true, message: '请输入 key 名称' }]}
          >
            <Input placeholder="例如 codex-local" />
          </Form.Item>
          <Form.Item
            name="permissionKeys"
            label="权限 keys"
            rules={[{ required: true, type: 'array', min: 1, message: '请至少选择一项权限' }]}
          >
            <Select
              mode="multiple"
              maxCount={256}
              options={
                tokenPurpose === 'cli'
                  ? (snapshot?.permissionKeys ?? [])
                      .filter((key) => key !== '*')
                      .map((value) => ({
                        value,
                        label: `${consolePermissionLabelMap[value] || value} (${value})`,
                      }))
                  : gatewayPermissionOptions.filter((item) => hasPermission(snapshot, item.value))
              }
            />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes">
            <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="留空表示不额外收窄" />
          </Form.Item>
          <Form.Item name="expiresIn" label="有效期">
            <Select options={gatewayExpirationOptions} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, next) => prev.expiresIn !== next.expiresIn}>
            {({ getFieldValue }) =>
              getFieldValue('expiresIn') === 'custom' ? (
                <Form.Item
                  name="customExpiresAt"
                  label="自定义过期时间"
                  rules={[
                    { required: true, message: '请输入自定义过期时间' },
                    {
                      validator: (_, value) =>
                        validDateTime(value)
                          ? Promise.resolve()
                          : Promise.reject(new Error('请输入将来的 RFC3339 时间')),
                    },
                  ]}
                >
                  <Input placeholder="例如 2026-06-30T00:00:00Z" />
                </Form.Item>
              ) : null
            }
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        title={oneTimeToken?.title}
        open={!!oneTimeToken && !tokenDenied}
        okText="我已保存"
        cancelButtonProps={{ style: { display: 'none' } }}
        destroyOnHidden
        onCancel={() => setOneTimeToken(null)}
        onOk={() => setOneTimeToken(null)}
      >
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Alert
            type="warning"
            showIcon
            title="明文只展示一次"
            description="关闭后无法再次查看完整 key；如果丢失，需要轮换或重新生成。"
          />
          {oneTimeToken?.prefix ? (
            <Text type="secondary">Prefix: {oneTimeToken.prefix}</Text>
          ) : null}
          <Input.TextArea
            className="soha-profile-secret-value"
            readOnly
            autoSize={{ minRows: 3, maxRows: 6 }}
            value={!tokenDenied ? oneTimeToken?.value : undefined}
          />
          <Paragraph
            copyable={{ text: !tokenDenied ? (oneTimeToken?.value ?? '') : '' }}
            type="secondary"
          >
            复制完整 key
          </Paragraph>
        </Space>
      </Modal>
    </div>
  )
}
