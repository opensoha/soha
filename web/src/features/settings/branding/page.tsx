import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import type { BrandingSettings } from '@/types'
import { defaultBrandingSettings } from '@/utils/branding'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Form, Input, Spin } from 'antd'
import type { MouseEvent } from 'react'
import { useEffect, useState } from 'react'
import { settingsMutations } from '../mutations'
import { settingsQueries } from '../queries'
import { SettingsCard, WIDE_FORM_LAYOUT } from '../shared/components'
import type { SettingsPageProps } from '../types'
import './styles.css'

export function BrandingSettingsPage({ embedded = false }: SettingsPageProps = {}) {
  const queryClient = useQueryClient()
  const [form] = Form.useForm<BrandingSettings>()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canViewBrandingSettings = hasPermission(
    readableQueryData(permissionSnapshotQuery)?.data,
    'settings.branding.view',
  )
  const permissionSnapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canCreateBrandingSettings = hasPermission(permissionSnapshot, 'settings.branding.create')
  const canUpdateBrandingSettings = hasPermission(permissionSnapshot, 'settings.branding.update')

  const brandingQuery = useQuery(settingsQueries.branding(canViewBrandingSettings))
  const data = canViewBrandingSettings ? readableQueryData(brandingQuery) : undefined
  const isLoading = brandingQuery.isLoading
  const saveMutation = useOperationMutation(settingsMutations.branding.save(queryClient), {
    label: ['保存品牌设置', 'Save branding settings'],
    target: () => '产品品牌',
    receipt: () => ({ message: '品牌设置已保存' }),
  })
  const uploadMutation = useOperationMutation(settingsMutations.branding.upload(), {
    label: ['上传品牌图片', 'Upload brand asset'],
    target: (v) => (v.get('file') as File | null)?.name || '品牌图片',
    receipt: () => ({ message: '图片已上传', evidence: '请保存品牌设置以使用此图片。' }),
  })
  const blocked = saveMutation.blocked || uploadMutation.blocked
  const uploadAsset = async (field: keyof BrandingSettings, data: FormData) => {
    const result = await uploadMutation.mutateAsync(data)
    form.setFields([{ name: field, value: result.url, touched: true }])
  }
  useEffect(() => {
    if (!canViewBrandingSettings || queryAccessDenied(brandingQuery)) form.resetFields()
  }, [canViewBrandingSettings, brandingQuery.error, form])

  if (permissionSnapshotQuery.isLoading || isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    )
  }

  if (
    (permissionSnapshotQuery.isError && !readableQueryData(permissionSnapshotQuery)) ||
    (brandingQuery.isError && !data)
  ) {
    return (
      <div className="soha-page">
        <ReadFeedback query={permissionSnapshotQuery} label="权限" />
        <ReadFeedback query={brandingQuery} label="品牌设置" canRead={canViewBrandingSettings} />
      </div>
    )
  }

  if (!canViewBrandingSettings) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有查看品牌设置的权限。" />
      </div>
    )
  }

  const settings = data
  const canManageBrandingSettings = settings ? canUpdateBrandingSettings : canCreateBrandingSettings
  const content = (
    <SettingsCard>
      <ReadFeedback query={permissionSnapshotQuery} label="权限" />
      <ReadFeedback query={brandingQuery} label="品牌设置" canRead={canViewBrandingSettings} />
      {saveMutation.feedback}
      {uploadMutation.feedback}
      <Form
        form={form}
        disabled={blocked || !canManageBrandingSettings}
        {...WIDE_FORM_LAYOUT}
        onFinish={(values) => {
          if (!canManageBrandingSettings) return
          if (!blocked) saveMutation.mutate(values as BrandingSettings)
        }}
        initialValues={{ ...defaultBrandingSettings, ...settings }}
      >
        <BrandingFormSync settings={settings} />
        <Form.Item name="appTitle" label="网页标题">
          <Input placeholder="浏览器标签页标题" />
        </Form.Item>
        <Form.Item name="sidebarTitle" label="侧边栏标题">
          <Input placeholder="左侧品牌栏文字" />
        </Form.Item>
        <Form.Item name="slogan" label="标语">
          <Input placeholder="登录页主标题" />
        </Form.Item>

        <div className="soha-branding-section-title">企业 Logo</div>
        <Form.Item name="loginLogoUrl" hidden>
          <Input />
        </Form.Item>
        <div className="soha-branding-upload-grid">
          <BrandingUploadField
            field="expandedLogoUrl"
            label="登录页主视觉与展开侧边栏 Logo"
            hint="用于登录页左侧主视觉和展开侧边栏；支持 JPG、PNG、SVG、ICO、WebP；建议尺寸 200 × 60 px；单个文件不超过 2 MB"
            previewWidth={200}
            previewHeight={60}
            disabled={blocked || !canManageBrandingSettings}
            onUpload={uploadAsset}
          />
          <BrandingUploadField
            field="collapsedLogoUrl"
            label="登录卡片与收起侧边栏图标"
            hint="用于登录卡片和收起侧边栏；支持 JPG、PNG、SVG、ICO、WebP；建议尺寸 60 × 60 px；单个文件不超过 2 MB"
            previewWidth={60}
            previewHeight={60}
            disabled={blocked || !canManageBrandingSettings}
            onUpload={uploadAsset}
          />
          <BrandingUploadField
            field="faviconUrl"
            label="Favicon 图标"
            hint="支持 JPG、PNG、SVG、ICO、WebP；建议尺寸 16 × 16、32 × 32 或 64 × 64 px；单个文件不超过 2 MB"
            previewWidth={64}
            previewHeight={64}
            disabled={blocked || !canManageBrandingSettings}
            onUpload={uploadAsset}
          />
        </div>

        <div className="soha-branding-section-title">暗色主题 Logo（可选）</div>
        <div className="soha-branding-upload-grid">
          <BrandingUploadField
            field="darkExpandedLogoUrl"
            label="暗色展开 Logo"
            hint="用于暗色侧边栏和登录页主视觉；未上传时沿用原图。建议使用透明底 PNG、SVG 或 WebP，200 × 60 px，不超过 2 MB"
            previewWidth={200}
            previewHeight={60}
            darkPreview
            disabled={blocked || !canManageBrandingSettings}
            onUpload={uploadAsset}
          />
          <BrandingUploadField
            field="darkCollapsedLogoUrl"
            label="暗色收起图标"
            hint="用于暗色收起侧边栏和登录卡片；未上传时沿用原图。建议使用透明底 PNG、SVG 或 WebP，60 × 60 px，不超过 2 MB"
            previewWidth={60}
            previewHeight={60}
            darkPreview
            disabled={blocked || !canManageBrandingSettings}
            onUpload={uploadAsset}
          />
        </div>

        <div className="soha-form-actions">
          {canManageBrandingSettings ? (
            <Button
              disabled={blocked}
              htmlType="submit"
              type="primary"
              loading={saveMutation.isPending}
            >
              保存设置
            </Button>
          ) : null}
        </div>
      </Form>
    </SettingsCard>
  )

  if (embedded) {
    return content
  }

  return <div className="soha-page">{content}</div>
}

function BrandingFormSync({ settings }: { settings?: BrandingSettings }) {
  const form = Form.useFormInstance<BrandingSettings>()

  useEffect(() => {
    if (settings && !form.isFieldsTouched()) {
      form.setFieldsValue(settings)
    }
  }, [form, settings])

  return null
}

interface BrandingUploadFieldProps {
  field: keyof BrandingSettings
  label: string
  hint: string
  previewWidth: number
  previewHeight: number
  darkPreview?: boolean
  disabled?: boolean
  onUpload: (field: keyof BrandingSettings, data: FormData) => Promise<void>
}

function BrandingUploadField({
  field,
  label,
  hint,
  previewWidth,
  previewHeight,
  darkPreview = false,
  disabled,
  onUpload,
}: BrandingUploadFieldProps) {
  const { message } = App.useApp()
  const [uploading, setUploading] = useState(false)
  const form = Form.useFormInstance()
  const currentValue = Form.useWatch(field, form) as string | undefined

  const handleUploadClick = () => {
    if (disabled || uploading) return
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = '.jpg,.jpeg,.png,.svg,.ico,.webp'
    input.onchange = async () => {
      const file = input.files?.[0]
      if (!file) return
      if (file.size > 2 * 1024 * 1024) {
        void message.error('文件大小不能超过 2MB')
        return
      }
      setUploading(true)
      try {
        const formData = new FormData()
        formData.append('file', file)
        await onUpload(field, formData)
      } catch {
        // The page owns persistent upload feedback.
      } finally {
        setUploading(false)
      }
    }
    input.click()
  }

  const handleRemove = (e: MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation()
    if (!disabled) form.setFields([{ name: field, value: '', touched: true }])
  }

  return (
    <div className="soha-branding-upload-zone">
      <div className="soha-branding-upload-label">{label}</div>
      <Form.Item name={field} hidden>
        <Input />
      </Form.Item>
      <div className="soha-branding-upload-area-wrap">
        <button
          type="button"
          className={`soha-branding-upload-area${darkPreview ? ' is-dark-preview' : ''}`}
          aria-label={`${currentValue ? '替换' : '上传'}${label}`}
          disabled={disabled || uploading}
          onClick={handleUploadClick}
        >
          {currentValue ? (
            <img
              src={currentValue}
              alt={label}
              className="soha-branding-upload-preview"
              style={{ maxWidth: previewWidth, maxHeight: previewHeight }}
            />
          ) : (
            <div className="soha-branding-upload-placeholder">
              {uploading ? (
                <Spin size="small" />
              ) : (
                <span className="soha-branding-upload-plus">+</span>
              )}
            </div>
          )}
        </button>
        {currentValue && !disabled ? (
          <Button
            size="small"
            danger
            variant="outlined"
            className="soha-branding-upload-remove"
            onClick={handleRemove}
          >
            移除
          </Button>
        ) : null}
      </div>
      <div className="soha-branding-upload-hint">{hint}</div>
    </div>
  )
}
