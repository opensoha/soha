import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import {
  systemIntegrationQueries,
  type SystemIntegration,
  type SystemIntegrationCategory,
} from '@/features/settings'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import {
  CloudDownloadOutlined,
  DeleteOutlined,
  FileAddOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType, UploadFile } from 'antd'
import {
  App,
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Table,
  Typography,
  Upload,
} from 'antd'
import dayjs from 'dayjs'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { ObjectStorageConfig } from './components/object-storage-config'
import { softwarePackageMutations } from './mutations'
import { softwarePackageQueries } from './queries'
import './styles.css'
import type { SoftwarePackage, SoftwarePackageFilters, SoftwarePackagePublishInput } from './types'
const { Text } = Typography
const platformOptions = [
  { label: 'macOS', value: 'darwin' },
  { label: 'Windows', value: 'windows' },
  { label: 'Linux', value: 'linux' },
]
const architectureOptions = [
  { label: 'ARM64', value: 'arm64' },
  { label: 'x86_64', value: 'amd64' },
]
const installerAccept: Record<string, string> = {
  darwin: '.dmg,.pkg',
  windows: '.exe,.msi',
  linux: '.AppImage,.appimage,.deb,.rpm',
}
interface SoftwareUploadFormValues {
  storageIntegrationId: string
  name: string
  description?: string
  publisher: string
  category?: string
  version: string
  platform?: string
  arch?: string
  variants: Array<{
    file?: UploadFile
    name?: string
    description?: string
    publisher?: string
    category?: string
    version?: string
    platform?: string
    arch?: string
  }>
  url?: string
  fileName?: string
}
interface SoftwareUploadModalProps {
  open: boolean
  saving: boolean
  blocked: boolean
  feedback: React.ReactNode
  existingPackages: SoftwarePackage[]
  storageIntegrations: SystemIntegration[]
  onCancel: () => void
  onSubmit: (inputs: SoftwarePackagePublishInput[]) => void
}
function softwareIdFromName(name: string, packages: SoftwarePackage[]) {
  const normalizedName = name.trim().toLocaleLowerCase()
  const existing = packages.find((item) => item.name.trim().toLocaleLowerCase() === normalizedName)
  if (existing) return existing.softwareId
  const slug = name
    .normalize('NFKD')
    .toLocaleLowerCase()
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64)
  return slug || `software-${crypto.randomUUID().slice(0, 8)}`
}
function packageTargetFromFileName(fileName: string) {
  const normalized = fileName.toLocaleLowerCase()
  const platform = /\.(dmg|pkg)$/.test(normalized)
    ? 'darwin'
    : /\.(exe|msi)$/.test(normalized)
      ? 'windows'
      : /\.(appimage|deb|rpm)$/.test(normalized)
        ? 'linux'
        : undefined
  const arch = /(?:arm64|aarch64)/.test(normalized)
    ? 'arm64'
    : /(?:amd64|x86[_-]?64|x64)/.test(normalized)
      ? 'amd64'
      : undefined
  return { platform, arch }
}
function packageDraftFromFile(file: UploadFile) {
  return {
    file,
    name: file.name.replace(/\.(appimage|dmg|pkg|exe|msi|deb|rpm)$/i, ''),
    ...packageTargetFromFileName(file.name),
  }
}
function SelectedPackageFile({ value }: { value?: UploadFile }) {
  return (
    <Text className="soha-software-package-file" ellipsis title={value?.name}>
      {value?.name}
    </Text>
  )
}
function SoftwareUploadModal({
  open,
  saving,
  blocked,
  feedback,
  existingPackages,
  storageIntegrations,
  onCancel,
  onSubmit,
}: SoftwareUploadModalProps) {
  const { t } = useI18n()
  const [form] = Form.useForm<SoftwareUploadFormValues>()
  const [source, setSource] = useState<'file' | 'url'>('file')
  const [publishMode, setPublishMode] = useState<'single' | 'batch'>('single')
  const packageCount = Form.useWatch('variants', form)?.length ?? 0
  useEffect(() => {
    if (open) {
      form.resetFields()
      form.setFieldValue(
        'storageIntegrationId',
        storageIntegrations.length === 1 ? storageIntegrations[0].id : undefined,
      )
      setSource('file')
      setPublishMode('single')
    }
    // Keep an open draft when storage or package queries refresh.
  }, [form, open])
  const submit = (values: SoftwareUploadFormValues) => {
    if (publishMode === 'batch') {
      const inputs = values.variants.flatMap((variant) => {
        const file = variant.file?.originFileObj
        if (
          !file ||
          !variant.name ||
          !variant.publisher ||
          !variant.version ||
          !variant.platform ||
          !variant.arch
        ) {
          return []
        }
        return [
          {
            source: 'file',
            storageIntegrationId: values.storageIntegrationId,
            softwareId: softwareIdFromName(variant.name, existingPackages),
            name: variant.name,
            description: variant.description,
            publisher: variant.publisher,
            category: variant.category,
            version: variant.version,
            file,
            platform: variant.platform,
            arch: variant.arch,
          } satisfies SoftwarePackagePublishInput,
        ]
      })
      if (inputs.length) onSubmit(inputs)
      return
    }
    const metadata = {
      storageIntegrationId: values.storageIntegrationId,
      softwareId: softwareIdFromName(values.name, existingPackages),
      name: values.name,
      description: values.description,
      publisher: values.publisher,
      category: values.category,
      version: values.version,
    }
    if (source === 'file') {
      const inputs = values.variants.flatMap((variant) => {
        const file = variant.file?.originFileObj
        if (!file || !variant.platform || !variant.arch) return []
        return [
          {
            source,
            ...metadata,
            file,
            platform: variant.platform,
            arch: variant.arch,
          } satisfies SoftwarePackagePublishInput,
        ]
      })
      if (inputs.length) onSubmit(inputs)
      return
    }
    if (values.url && values.platform && values.arch) {
      onSubmit([
        {
          source,
          ...metadata,
          platform: values.platform,
          arch: values.arch,
          url: values.url,
          fileName: values.fileName,
        },
      ])
    }
  }
  return (
    <Modal
      styles={operationModalStyles}
      style={{ top: 32 }}
      closable={!blocked}
      destroyOnHidden
      footer={null}
      mask={{ closable: false }}
      open={open}
      title={t('software.upload.title', '发布软件')}
      width={920}
      onCancel={onCancel}
    >
      {feedback}
      <Form
        disabled={blocked}
        className="soha-software-upload-form"
        form={form}
        initialValues={{ platform: 'darwin', arch: 'arm64', variants: [] }}
        layout="vertical"
        onFinish={submit}
      >
        <Form.Item
          className="soha-software-publish-mode"
          label="发布方式"
          tooltip="单个发布支持本地文件或下载地址；批量发布可一次选择多个独立软件包，并分别配置软件信息。"
        >
          <Segmented<'single' | 'batch'>
            disabled={blocked}
            block
            className="soha-form-segmented"
            options={[
              { label: '单个发布', value: 'single' },
              { label: '批量发布', value: 'batch' },
            ]}
            value={publishMode}
            onChange={(value) => {
              setPublishMode(value)
              setSource('file')
              form.setFieldsValue({ variants: [], url: undefined, fileName: undefined })
            }}
          />
        </Form.Item>

        {publishMode === 'single' ? (
          <>
            <div className="soha-software-form-grid">
              <Form.Item
                label={t('software.field.name', '软件名称')}
                name="name"
                rules={[{ required: true, max: 100, message: '请输入软件名称' }]}
              >
                <Input placeholder="Soha Desktop" />
              </Form.Item>
              <Form.Item
                label={t('software.field.publisher', '发布者')}
                name="publisher"
                rules={[{ required: true, max: 100, message: '请输入发布者' }]}
              >
                <Input placeholder="OpenSoha" />
              </Form.Item>
              <Form.Item
                label={t('software.field.version', '版本')}
                name="version"
                rules={[{ required: true, max: 64, message: '请输入版本号' }]}
              >
                <Input placeholder="1.0.0" />
              </Form.Item>
              <Form.Item label={t('software.field.category', '分类')} name="category">
                <Input maxLength={50} placeholder="Developer Tools" />
              </Form.Item>
            </div>
            <Form.Item label={t('software.field.description', '描述')} name="description">
              <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} maxLength={500} />
            </Form.Item>
          </>
        ) : null}

        <Form.Item
          label="目标存储"
          name="storageIntegrationId"
          rules={[{ required: true, message: '请选择目标对象存储' }]}
        >
          <Select
            disabled={blocked || storageIntegrations.length === 0}
            options={storageIntegrations.map((item) => ({
              label: `${item.name} · ${integrationConfiguration(item, 'region') || '未标注地域'}`,
              value: item.id,
            }))}
            placeholder={storageIntegrations.length ? '选择对象存储' : '请先启用对象存储'}
          />
        </Form.Item>

        {publishMode === 'single' ? (
          <Segmented<'file' | 'url'>
            disabled={blocked}
            block
            className="soha-software-source-segmented"
            options={[
              { icon: <FileAddOutlined />, label: '本地文件', value: 'file' },
              { icon: <CloudDownloadOutlined />, label: '下载地址', value: 'url' },
            ]}
            value={source}
            onChange={(value) => {
              setSource(value)
              form.setFieldsValue({ variants: [], url: undefined, fileName: undefined })
            }}
          />
        ) : null}

        {publishMode === 'batch' || source === 'file' ? (
          <div className="soha-software-package-section">
            <div className="soha-software-package-section-head">
              <Text strong>{t('software.field.file', '安装包')}</Text>
              <Upload
                accept={Object.values(installerAccept).join(',')}
                beforeUpload={() => false}
                fileList={[]}
                maxCount={publishMode === 'single' ? 1 : undefined}
                multiple={publishMode === 'batch'}
                showUploadList={false}
                onChange={({ fileList }) => {
                  const selected = fileList.map(packageDraftFromFile)
                  const variants = form.getFieldValue('variants') ?? []
                  const merged = [
                    ...new Map(
                      [...variants, ...selected].map((item) => [item.file?.uid, item]),
                    ).values(),
                  ]
                  form.setFieldValue(
                    'variants',
                    publishMode === 'single' ? selected.slice(-1) : merged,
                  )
                }}
              >
                <Button icon={<FileAddOutlined />}>
                  {publishMode === 'single' ? '选择安装包' : '选择多个安装包'}
                </Button>
              </Upload>
            </div>
            <Form.List
              name="variants"
              rules={[
                {
                  validator: async (_, variants = []) => {
                    if (!variants.length) throw new Error('请至少选择一个安装包')
                  },
                },
              ]}
            >
              {(fields, { remove }, { errors }) => (
                <>
                  <div className="soha-software-package-list">
                    {fields.map(({ key, name }, index) =>
                      publishMode === 'batch' ? (
                        <section className="soha-software-batch-package" key={key}>
                          <div className="soha-software-batch-package-head">
                            <div className="soha-software-batch-package-title">
                              <Text strong>{`安装包 ${index + 1}`}</Text>
                              <Form.Item
                                name={[name, 'file']}
                                rules={[{ required: true, message: '请选择安装包' }]}
                              >
                                <SelectedPackageFile />
                              </Form.Item>
                            </div>
                            <Button
                              danger
                              icon={<DeleteOutlined />}
                              type="text"
                              onClick={() => remove(name)}
                            >
                              移除
                            </Button>
                          </div>
                          <div className="soha-software-batch-package-grid">
                            <Form.Item
                              className="soha-software-batch-field-double"
                              label={t('software.field.name', '软件名称')}
                              name={[name, 'name']}
                              rules={[{ required: true, max: 100, message: '请输入软件名称' }]}
                            >
                              <Input placeholder="Soha Desktop" />
                            </Form.Item>
                            <Form.Item
                              label={t('software.field.publisher', '发布者')}
                              name={[name, 'publisher']}
                              rules={[{ required: true, max: 100, message: '请输入发布者' }]}
                            >
                              <Input placeholder="OpenSoha" />
                            </Form.Item>
                            <Form.Item
                              label={t('software.field.version', '版本')}
                              name={[name, 'version']}
                              rules={[{ required: true, max: 64, message: '请输入版本号' }]}
                            >
                              <Input placeholder="1.0.0" />
                            </Form.Item>
                            <Form.Item
                              className="soha-software-batch-field-double"
                              label={t('software.field.category', '分类')}
                              name={[name, 'category']}
                            >
                              <Input maxLength={50} placeholder="Developer Tools" />
                            </Form.Item>
                            <Form.Item
                              label={t('software.field.platform', '平台')}
                              name={[name, 'platform']}
                              rules={[{ required: true, message: '请选择平台' }]}
                            >
                              <Select options={platformOptions} />
                            </Form.Item>
                            <Form.Item
                              label={t('software.field.arch', '架构')}
                              name={[name, 'arch']}
                              rules={[{ required: true, message: '请选择架构' }]}
                            >
                              <Select options={architectureOptions} />
                            </Form.Item>
                            <Form.Item
                              className="soha-software-batch-description"
                              label={t('software.field.description', '描述')}
                              name={[name, 'description']}
                            >
                              <Input.TextArea
                                autoSize={{ minRows: 1, maxRows: 3 }}
                                maxLength={500}
                              />
                            </Form.Item>
                          </div>
                        </section>
                      ) : (
                        <div className="soha-software-package-row" key={key}>
                          <Form.Item
                            label="安装包"
                            name={[name, 'file']}
                            rules={[{ required: true, message: '请选择安装包' }]}
                          >
                            <SelectedPackageFile />
                          </Form.Item>
                          <Form.Item
                            label={t('software.field.platform', '平台')}
                            name={[name, 'platform']}
                            rules={[{ required: true, message: '请选择平台' }]}
                          >
                            <Select options={platformOptions} />
                          </Form.Item>
                          <Form.Item
                            label={t('software.field.arch', '架构')}
                            name={[name, 'arch']}
                            rules={[{ required: true, message: '请选择架构' }]}
                          >
                            <Select options={architectureOptions} />
                          </Form.Item>
                          <Button
                            danger
                            icon={<DeleteOutlined />}
                            type="text"
                            onClick={() => remove(name)}
                          >
                            移除
                          </Button>
                        </div>
                      ),
                    )}
                  </div>
                  <Form.ErrorList errors={errors} />
                </>
              )}
            </Form.List>
          </div>
        ) : (
          <div className="soha-software-form-grid soha-software-url-fields">
            <Form.Item
              label={t('software.field.platform', '平台')}
              name="platform"
              rules={[{ required: true, message: '请选择平台' }]}
            >
              <Select options={platformOptions} />
            </Form.Item>
            <Form.Item
              label={t('software.field.arch', '架构')}
              name="arch"
              rules={[{ required: true, message: '请选择架构' }]}
            >
              <Select options={architectureOptions} />
            </Form.Item>
            <Form.Item
              label="HTTPS 下载地址"
              name="url"
              rules={[
                { required: true, message: '请输入下载地址' },
                { pattern: /^https:\/\//, message: '仅支持 HTTPS 地址' },
              ]}
            >
              <Input placeholder="https://downloads.example.com/app.dmg" />
            </Form.Item>
            <Form.Item label="文件名（可选）" name="fileName">
              <Input maxLength={255} placeholder="app.dmg" />
            </Form.Item>
          </div>
        )}
        <div className="soha-software-form-actions">
          <Space>
            <Button disabled={blocked} onClick={onCancel}>
              {t('common.cancel', '取消')}
            </Button>
            <Button
              autoInsertSpace={false}
              htmlType="submit"
              icon={<UploadOutlined />}
              loading={saving}
              type="primary"
            >
              {publishMode === 'batch'
                ? `批量上传并发布${packageCount ? `（${packageCount}）` : ''}`
                : source === 'file'
                  ? t('software.upload.submit', '上传并发布')
                  : t('software.import.submit', '下载并发布')}
            </Button>
          </Space>
        </div>
      </Form>
    </Modal>
  )
}
function formatBytes(value: number) {
  const units = ['B', 'KiB', 'MiB', 'GiB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size >= 10 || index === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[index]}`
}
function SoftwareFileDetailDrawer({
  file,
  storageName,
  canViewDownloadRecords,
  onClose,
}: {
  file: SoftwarePackage | null
  storageName?: string
  canViewDownloadRecords: boolean
  onClose: () => void
}) {
  const { t } = useI18n()
  const recordsQuery = useQuery({
    ...softwarePackageQueries.downloadRecords(file?.id ?? ''),
    enabled: Boolean(file) && canViewDownloadRecords,
  })
  const visibleRecordsQueryData =
    recordsQuery.isEnabled === false ? undefined : readableQueryData(recordsQuery)
  return (
    <Drawer
      destroyOnHidden
      open={Boolean(file)}
      size={680}
      title={t('software.storage.detailTitle', '文件详情')}
      onClose={onClose}
    >
      <ReadFeedback canRead={recordsQuery.isEnabled} query={recordsQuery} label="身份数据" />

      {file ? (
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <div className="soha-software-file-detail-head">
            <Text strong>{file.fileName}</Text>
            <Space size={4} wrap>
              <MetadataTag label={file.version} tone="blue" />
              <MetadataTag label={file.platform} />
              <MetadataTag label={file.arch === 'amd64' ? 'x86_64' : file.arch} tone="cyan" />
            </Space>
          </div>
          <Descriptions
            bordered
            column={{ xs: 1, sm: 2 }}
            size="small"
            items={[
              { key: 'software', label: '所属软件', children: file.name },
              { key: 'storage', label: '存储位置', children: storageName || '-' },
              { key: 'softwareId', label: '软件 ID', children: file.softwareId },
              { key: 'publisher', label: '发布者', children: file.publisher || '-' },
              { key: 'size', label: '占用空间', children: formatBytes(file.sizeBytes) },
              { key: 'downloadCount', label: '下载次数', children: file.downloadCount ?? 0 },
              {
                key: 'sha256',
                label: 'SHA-256',
                span: 'filled',
                children: <Text copyable={{ text: file.sha256 }}>{file.sha256}</Text>,
              },
              {
                key: 'downloadPath',
                label: '下载地址',
                span: 'filled',
                children: (
                  <Text copyable={{ text: file.downloadPath }}>{file.downloadPath || '-'}</Text>
                ),
              },
              {
                key: 'createdAt',
                label: '存入时间',
                children: dayjs(file.createdAt).format('YYYY-MM-DD HH:mm:ss'),
              },
              {
                key: 'updatedAt',
                label: '更新时间',
                children: dayjs(file.updatedAt).format('YYYY-MM-DD HH:mm:ss'),
              },
            ]}
          />
          <Text strong>下载记录</Text>
          {!canViewDownloadRecords ? (
            <ManagementState
              bordered={false}
              compact
              kind="no-permission"
              description="需要审计查看权限。"
              title="无权限查看下载记录"
            />
          ) : recordsQuery.isError && !visibleRecordsQueryData ? null : recordsQuery.isLoading ? (
            <ManagementState bordered={false} compact kind="loading" />
          ) : visibleRecordsQueryData?.items.length ? (
            <Table
              columns={[
                {
                  title: '下载用户',
                  key: 'actor',
                  render: (_, record) => record.actorName || record.actorId,
                },
                {
                  title: '下载时间',
                  dataIndex: 'downloadedAt',
                  render: (value: string) => dayjs(value).format('YYYY-MM-DD HH:mm:ss'),
                },
                {
                  title: '耗时',
                  dataIndex: 'durationMs',
                  render: (value: number) =>
                    value < 1000 ? `${value} ms` : `${(value / 1000).toFixed(1)} s`,
                },
                {
                  title: '下载 IP',
                  dataIndex: 'sourceIp',
                  render: (value?: string) => value || '-',
                },
              ]}
              dataSource={visibleRecordsQueryData.items}
              pagination={false}
              rowKey="id"
              size="small"
            />
          ) : (
            <ManagementState
              bordered={false}
              compact
              kind="empty"
              description="完成下载后会在这里显示。"
              title="暂无下载记录"
            />
          )}
        </Space>
      ) : null}
    </Drawer>
  )
}
export function SoftwareLibraryPage() {
  const { message } = App.useApp()
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [queryForm] = Form.useForm<SoftwarePackageFilters>()
  const [filters, setFilters] = useState<SoftwarePackageFilters>({})
  const [uploadOpen, setUploadOpen] = useState(false)
  const [publishing, setPublishing] = useState(false)
  const [selectedStorageFile, setSelectedStorageFile] = useState<SoftwarePackage | null>(null)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const snapshot = usePermissionSnapshot().data?.data
  const canUpload = hasPermission(snapshot, 'software.package.create')
  const canDelete = hasPermission(snapshot, 'software.package.delete')
  const canViewStorage = hasPermission(snapshot, 'settings.system-integrations.view')
  const canViewDownloadRecords =
    hasPermission(snapshot, 'system.audit.view') || hasPermission(snapshot, 'identity.audit.view')
  const packagesQuery = useQuery(softwarePackageQueries.list(filters))
  const visiblePackagesQueryData =
    packagesQuery.isEnabled === false ? undefined : readableQueryData(packagesQuery)
  const storageQuery = useQuery(softwarePackageQueries.storage())
  const visibleStorageQueryData =
    storageQuery.isEnabled === false ? undefined : readableQueryData(storageQuery)
  const storageIntegrationsQuery = useQuery(
    systemIntegrationQueries.list(
      {
        category: 'storage' as SystemIntegrationCategory,
        providerType: 's3',
      },
      canViewStorage,
    ),
  )
  const visibleStorageIntegrationsQueryData =
    storageIntegrationsQuery.isEnabled === false
      ? undefined
      : readableQueryData(storageIntegrationsQuery)
  const storageIntegrations = visibleStorageIntegrationsQueryData ?? []
  const enabledStorageIntegrations = storageIntegrations.filter((item) => item.enabled)
  const storageById = useMemo(
    () => new Map(storageIntegrations.map((item) => [item.id, item])),
    [storageIntegrations],
  )
  const publishMutation = useOperationMutation(softwarePackageMutations.publish(queryClient), {
    label: ['发布安装包', 'Publish package'],
    target: (v) => v.name + ' / ' + v.version,
  })
  const deleteMutation = useOperationMutation(softwarePackageMutations.remove(queryClient), {
    label: ['删除 软件包', 'Delete package'],
    target: (v) => v,
  })
  const operationBlocked =
    publishMutation.blocked ||
    deleteMutation.blocked ||
    (packagesQuery.isEnabled !== false && queryAccessDenied(packagesQuery))
  const publishPackages = async (inputs: SoftwarePackagePublishInput[]) => {
    if (operationBlocked || publishing) return
    setPublishing(true)
    let published = 0
    try {
      for (const input of inputs) {
        try {
          await publishMutation.mutateAsync(input)
          published += 1
        } catch (error) {
          const remaining = inputs.length - published - 1
          message.warning(
            writeOutcomeUnknown(error instanceof Error ? error : undefined)
              ? `${published} 个已发布，当前项结果不明，${remaining} 个未提交。请先核对软件版本记录。`
              : `${published} 个已发布，当前项失败，${remaining} 个未提交。请核对后重新选择未发布项。`,
          )
          return
        }
      }
      message.success(`${published} 个安装包已发布`)
      setUploadOpen(false)
    } finally {
      setPublishing(false)
    }
  }
  const deleteAction = useCallback(
    (record: SoftwarePackage) => (
      <Popconfirm
        cancelText={t('common.cancel', '取消')}
        disabled={operationBlocked || !canDelete}
        okButtonProps={{
          ...{ danger: true, loading: deleteMutation.isPending },
          disabled: operationBlocked,
        }}
        okText={t('common.delete', '删除')}
        title={`${t('software.delete.title', '删除软件版本')} ${record.name} ${record.version}`}
        onConfirm={() => deleteMutation.mutate(record.id)}
      >
        <ManagementIconButton
          aria-label={t('common.delete', '删除')}
          danger
          disabled={!canDelete}
          icon={<DeleteOutlined />}
          tooltip={t('common.delete', '删除')}
        />
      </Popconfirm>
    ),
    [canDelete, deleteMutation, message, t],
  )
  const columns = useMemo<TableColumnsType<SoftwarePackage>>(
    () => [
      {
        title: t('software.column.file', '存储文件'),
        dataIndex: 'fileName',
        key: 'storage-file',
        width: 300,
        render: (value: string, record) => (
          <Space orientation="vertical" size={2}>
            <Button
              className="soha-software-file-link"
              title={value}
              type="link"
              onClick={() => setSelectedStorageFile(record)}
            >
              {value}
            </Button>
            <Text type="secondary">{record.id}</Text>
          </Space>
        ),
      },
      {
        title: t('software.column.software', '所属软件'),
        dataIndex: 'name',
        key: 'storage-software',
        width: 240,
        render: (_, record) => (
          <Space orientation="vertical" size={2}>
            <Text>{record.name}</Text>
            <MetadataTag label={record.version} tone="blue" />
          </Space>
        ),
      },
      {
        title: '存储位置',
        dataIndex: 'storageIntegrationId',
        key: 'storage-location',
        width: 180,
        render: (value?: string) => storageById.get(value ?? '')?.name || value || '-',
      },
      {
        title: t('software.column.target', '平台 / 架构'),
        key: 'storage-target',
        width: 190,
        render: (_, record) => (
          <Space size={4}>
            <MetadataTag label={record.platform} />
            <MetadataTag label={record.arch === 'amd64' ? 'x86_64' : record.arch} tone="cyan" />
          </Space>
        ),
      },
      {
        title: t('software.storage.size', '占用空间'),
        dataIndex: 'sizeBytes',
        key: 'storage-size',
        width: 140,
        render: (value: number) => formatBytes(value),
      },
      {
        title: 'SHA-256',
        dataIndex: 'sha256',
        key: 'storage-sha256',
        width: 180,
        render: (value: string) => (
          <Text code title={value}>
            {value.slice(0, 12)}...
          </Text>
        ),
      },
      {
        title: t('software.storage.createdAt', '存入时间'),
        dataIndex: 'createdAt',
        key: 'storage-created-at',
        width: 170,
        render: (value: string) => dayjs(value).format('YYYY-MM-DD HH:mm'),
      },
      {
        title: t('common.actions', '操作'),
        key: 'storage-actions',
        fixed: 'right',
        width: 64,
        render: (_, record) => deleteAction(record),
      },
    ],
    [deleteAction, storageById, t],
  )
  const resetFilters = () => {
    queryForm.resetFields()
    setFilters({})
  }
  return (
    <div className="soha-software-library-shell">
      {!(
        uploadOpen &&
        !(
          (packagesQuery.isEnabled !== false && queryAccessDenied(packagesQuery)) ||
          (storageIntegrationsQuery.isEnabled !== false &&
            queryAccessDenied(storageIntegrationsQuery))
        )
      ) && (
        <>
          {' '}
          {publishMutation.feedback}
          {deleteMutation.feedback}{' '}
        </>
      )}

      <ReadFeedback canRead={storageQuery.isEnabled} query={storageQuery} label="身份数据" />
      <ReadFeedback
        canRead={storageIntegrationsQuery.isEnabled}
        query={storageIntegrationsQuery}
        label="身份数据"
      />

      <ManagementDataPage
        className="soha-software-library-page"
        query={{
          actions: (
            <ManagementQueryActions
              disabledReset={!filters.platform && !filters.arch && !filters.storageIntegrationId}
              loading={packagesQuery.isFetching}
              onReset={resetFilters}
            />
          ),
          children: (
            <>
              <ManagementQueryField
                label={t('software.field.platform', '平台')}
                name="platform"
                width={180}
              >
                <Select
                  allowClear
                  options={platformOptions}
                  placeholder={t('software.filter.allPlatforms', '全部平台')}
                />
              </ManagementQueryField>
              {storageIntegrations.length > 1 ? (
                <ManagementQueryField label="存储位置" name="storageIntegrationId" width={220}>
                  <Select
                    allowClear
                    options={storageIntegrations.map((item) => ({
                      label: item.name,
                      value: item.id,
                    }))}
                    placeholder="全部存储"
                  />
                </ManagementQueryField>
              ) : null}
              <ManagementQueryField
                label={t('software.field.arch', '架构')}
                name="arch"
                width={180}
              >
                <Select
                  allowClear
                  options={architectureOptions}
                  placeholder={t('software.filter.allArch', '全部架构')}
                />
              </ManagementQueryField>
            </>
          ),
          form: queryForm,
          initialValues: {},
          onFinish: (values) =>
            setFilters({
              platform: values.platform,
              arch: values.arch,
              storageIntegrationId: values.storageIntegrationId,
            }),
        }}
        table={{
          error: packagesQuery.isEnabled === false ? null : packagesQuery.error,
          hasReadData:
            readableQueryData(packagesQuery) !== undefined && packagesQuery.isEnabled !== false,
          onRetry: () => {
            if (packagesQuery.isEnabled !== false) void packagesQuery.refetch()
          },
          columnSettingIconOnly: true,
          columnSettingPlacement: 'header',
          columns,
          dataSource: visiblePackagesQueryData?.items ?? [],
          empty:
            packagesQuery.isError && !visiblePackagesQueryData ? null : (
              <ManagementState
                description={t('software.empty.description', '尚未发布任何桌面安装包。')}
                kind="empty"
                title={t('software.empty.title', '暂无软件版本')}
              />
            ),
          headerExtra: (
            <ManagementTableToolbar>
              <Button
                autoInsertSpace={false}
                disabled={
                  operationBlocked ||
                  !canUpload ||
                  (canViewStorage && enabledStorageIntegrations.length === 0)
                }
                icon={<UploadOutlined />}
                size="small"
                type="primary"
                onClick={() => setUploadOpen(true)}
              >
                {t('software.upload.action', '上传软件')}
              </Button>
              <ObjectStorageConfig storage={visibleStorageQueryData} />
              <ManagementDensityButton
                aria-label={t('common.tableDensity', '切换表格密度')}
                title={t('common.tableDensity', '切换表格密度')}
                tooltip={t('common.tableDensity', '切换表格密度')}
                onClick={() =>
                  setTableSize((current) => (current === 'small' ? 'middle' : 'small'))
                }
              />
              <ManagementRefreshButton
                aria-label={t('common.refresh', '刷新')}
                loading={
                  packagesQuery.isFetching ||
                  storageQuery.isFetching ||
                  storageIntegrationsQuery.isFetching
                }
                title={t('common.refresh', '刷新')}
                tooltip={t('common.refresh', '刷新')}
                onClick={() => {
                  void (packagesQuery.isEnabled === false ? undefined : packagesQuery.refetch())
                  void (storageQuery.isEnabled === false ? undefined : storageQuery.refetch())
                  void (storageIntegrationsQuery.isEnabled === false
                    ? undefined
                    : storageIntegrationsQuery.refetch())
                }}
              />
            </ManagementTableToolbar>
          ),
          loading: packagesQuery.isLoading || packagesQuery.isFetching,
          pagination: { pageSize: 20 },
          rowKey: 'id',
          scroll: { x: 'max-content' },
          tableSize,
        }}
      />
      <SoftwareUploadModal
        existingPackages={visiblePackagesQueryData?.items ?? []}
        open={
          uploadOpen &&
          !(
            (packagesQuery.isEnabled !== false && queryAccessDenied(packagesQuery)) ||
            (storageIntegrationsQuery.isEnabled !== false &&
              queryAccessDenied(storageIntegrationsQuery))
          )
        }
        saving={publishing}
        blocked={operationBlocked || publishing}
        feedback={publishMutation.feedback}
        storageIntegrations={enabledStorageIntegrations}
        onCancel={() => {
          if (!operationBlocked) (() => setUploadOpen(false))()
        }}
        onSubmit={(inputs) => void publishPackages(inputs)}
      />
      <SoftwareFileDetailDrawer
        canViewDownloadRecords={canViewDownloadRecords}
        file={selectedStorageFile}
        storageName={
          storageById.get(selectedStorageFile?.storageIntegrationId ?? '')?.name ??
          selectedStorageFile?.storageIntegrationId
        }
        onClose={() => {
          if (!operationBlocked) (() => setSelectedStorageFile(null))()
        }}
      />
    </div>
  )
}
function integrationConfiguration(item: SystemIntegration, key: string) {
  return item.configuration.find((field) => field.key === key)?.value ?? ''
}
