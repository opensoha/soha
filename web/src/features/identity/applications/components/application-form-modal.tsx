import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { accessQueries } from '@/features/access'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import {
  AppstoreOutlined,
  DeleteOutlined,
  LinkOutlined,
  PlusOutlined,
  QuestionCircleOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import type { IdentityApplicationOnboardingInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery } from '@tanstack/react-query'
import type { FormInstance } from 'antd'
import {
  Alert,
  App,
  Avatar,
  Button,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Steps,
  Switch,
  TimePicker,
  Tooltip,
  Typography,
} from 'antd'
import dayjs from 'dayjs'
import type { ReactNode } from 'react'
import { useEffect, useRef, useState, type ChangeEvent } from 'react'
import { identityOutpostQueries } from '../../outposts'
import {
  defaultOIDCClientValues,
  defaultProviderValues,
  OIDCClientFields,
  oidcClientInputFromValues,
  ProviderConfigFields,
  providerInputFromValues,
  type IdentityProvider,
  type OIDCClientFormValues,
  type ProviderFormValues,
} from '../../providers'
import type { IdentityApplication, IdentityApplicationInput } from '../../shared/types'
import {
  buildIdentityApplicationInput,
  defaultIdentityApplicationFormValues,
  IDENTITY_APPLICATION_ICON_ACCEPT,
  identityApplicationAssignmentEffectOptions,
  identityApplicationAssignmentSubjectOptions,
  identityApplicationFormValuesFor,
  identityApplicationProviderTypeOptions,
  readIdentityApplicationIconFile,
  type IdentityApplicationFormValues,
  type IdentityApplicationTagOption,
} from '../application-form-model'
const { Text } = Typography
interface ApplicationIconInputProps {
  id?: string
  value?: string
  onChange?: (value: string) => void
}
function ApplicationIconInput({ id, value = '', onChange }: ApplicationIconInputProps) {
  const { message } = App.useApp()
  const { t } = useI18n()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [reading, setReading] = useState(false)
  const uploadedFileValue = value.startsWith('data:image/')
  const handleFileChange = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.currentTarget.files?.[0]
    event.currentTarget.value = ''
    if (!file) return
    setReading(true)
    try {
      onChange?.(await readIdentityApplicationIconFile(file))
    } catch (error) {
      void message.error(error instanceof Error ? error.message : t('common.failed', '操作失败'))
    } finally {
      setReading(false)
    }
  }
  const clearLabel = t('identity.applications.clearIcon', '清除图标')
  return (
    <div className="soha-identity-app-icon-field">
      <Avatar
        alt={t('identity.applications.iconPreview', '应用图标预览')}
        draggable={false}
        icon={<AppstoreOutlined />}
        shape="square"
        size={32}
        src={value || undefined}
      />
      <Input
        allowClear
        id={id}
        placeholder={
          uploadedFileValue
            ? t('identity.applications.iconUploaded', '已上传本地图片')
            : 'https://example.com/icon.png'
        }
        value={uploadedFileValue ? '' : value}
        onChange={(event) => onChange?.(event.target.value)}
      />
      <Button
        icon={<UploadOutlined />}
        loading={reading}
        onClick={() => fileInputRef.current?.click()}
      >
        {t('identity.applications.uploadIcon', '上传')}
      </Button>
      {value ? (
        <Button
          aria-label={clearLabel}
          icon={<DeleteOutlined />}
          type="text"
          onClick={() => onChange?.('')}
        />
      ) : null}
      <input
        ref={fileInputRef}
        accept={IDENTITY_APPLICATION_ICON_ACCEPT}
        className="soha-identity-app-icon-file-input"
        hidden
        type="file"
        onChange={(event) => void handleFileChange(event)}
      />
    </div>
  )
}
interface AssignmentSubjectSelectProps {
  fieldName: number
  form: FormInstance<IdentityApplicationFormValues>
  onChange?: (value: string[]) => void
  options: Record<
    string,
    Array<{
      label: string
      value: string
    }>
  >
  value?: string[]
}
function AssignmentSubjectSelect({
  fieldName,
  form,
  onChange,
  options,
  value,
}: AssignmentSubjectSelectProps) {
  const { t } = useI18n()
  const subjectType = Form.useWatch(['assignments', fieldName, 'subjectType'], form) ?? 'role'
  const placeholder = {
    role: t('identity.applications.subjectPlaceholder.role', '选择角色（可多选）'),
    tag: t('identity.applications.subjectPlaceholder.tag', '输入标签（可多选）'),
    team: t('identity.applications.subjectPlaceholder.team', '选择团队（可多选）'),
    user: t('identity.applications.subjectPlaceholder.user', '选择用户（可多选）'),
  }[subjectType]
  return (
    <Select
      allowClear
      mode={subjectType === 'tag' ? 'tags' : 'multiple'}
      value={value}
      onChange={onChange}
      options={options[subjectType] ?? []}
      placeholder={placeholder}
      showSearch={{ optionFilterProp: ['label', 'value'] }}
      tokenSeparators={subjectType === 'tag' ? [',', '，'] : undefined}
    />
  )
}
interface ApplicationFormModalProps {
  feedback?: ReactNode
  blocked?: boolean
  application: IdentityApplication | null
  tagOptions: IdentityApplicationTagOption[]
  providerOptions: IdentityProvider[]
  providerOptionsLoading: boolean
  open: boolean
  saving: boolean
  stepUpAvailable: boolean
  stepUpReason?: string
  onCancel: () => void
  onSubmit: (input: IdentityApplicationInput) => void
  onOnboard?: (input: IdentityApplicationOnboardingInput) => void
  canConfigureProvider?: boolean
  samlAvailable?: boolean
}
export function ApplicationFormModal({
  feedback,
  blocked = false,
  application,
  tagOptions,
  providerOptions,
  providerOptionsLoading,
  open,
  saving,
  stepUpAvailable,
  stepUpReason,
  onCancel,
  onSubmit,
  onOnboard,
  canConfigureProvider = false,
  samlAvailable = false,
}: ApplicationFormModalProps) {
  const [form] = Form.useForm<IdentityApplicationFormValues>()
  const [providerForm] = Form.useForm<ProviderFormValues>()
  const [clientForm] = Form.useForm<OIDCClientFormValues>()
  const [step, setStep] = useState(0)
  const [accessMode, setAccessMode] = useState<'all_authenticated' | 'restricted' | undefined>()
  const [configureNow, setConfigureNow] = useState(true)
  const wizard = !application && Boolean(onOnboard)
  const { message } = App.useApp()
  const { t } = useI18n()
  const assignmentHint = t(
    'identity.applications.assignmentHint',
    '每行只配置一种主体类型，中间可多选同类对象；如需其他类型，请添加新行。拒绝规则优先，留空表示所有已登录用户。',
  )
  const conditionHint = t(
    'identity.applications.conditionHint',
    '通过访问对象校验后，还必须同时满足已启用的 MFA、网络与 UTC 时段条件。',
  )
  const providerType = Form.useWatch('providerType', form) ?? 'link'
  const configureProtocol =
    wizard && providerType !== 'link' && canConfigureProvider && configureNow
  const steps = providerType === 'link' ? [0, 2, 3] : [0, 1, 2, 3]
  const assignmentValues = Form.useWatch('assignments', form) ?? []
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canViewOutposts = hasPermission(permissionSnapshot, 'identity.outposts.view')
  const outpostsQuery = useQuery({
    ...identityOutpostQueries.list({}),
    enabled: open && wizard && providerType === 'proxy' && canConfigureProvider && canViewOutposts,
  })
  const visibleOutpostsQueryData =
    outpostsQuery.isEnabled === false ? undefined : readableQueryData(outpostsQuery)
  const canViewUsers = hasPermission(permissionSnapshot, 'access.users.view')
  const canViewRoles = hasPermission(permissionSnapshot, 'access.roles.view')
  const canViewTeams = hasPermission(permissionSnapshot, 'access.groups.view')
  const usersQuery = useQuery({ ...accessQueries.users(open && canViewUsers), retry: false })
  const visibleUsersQueryData =
    usersQuery.isEnabled === false ? undefined : readableQueryData(usersQuery)
  const rolesQuery = useQuery({ ...accessQueries.roles(open && canViewRoles), retry: false })
  const visibleRolesQueryData =
    rolesQuery.isEnabled === false ? undefined : readableQueryData(rolesQuery)
  const teamsQuery = useQuery({ ...accessQueries.teams(open && canViewTeams), retry: false })
  const visibleTeamsQueryData =
    teamsQuery.isEnabled === false ? undefined : readableQueryData(teamsQuery)
  const providerSelectOptions = providerOptions
    .filter((provider) => provider.type === providerType)
    .map((provider) => ({
      disabled: !provider.enabled || provider.status !== 'enabled',
      label: `${provider.name} (${provider.id})`,
      value: provider.id,
    }))
  const providerSelectDisabled =
    !application ||
    providerType === 'link' ||
    (!providerOptionsLoading && providerSelectOptions.length === 0)
  const subjectOptions = {
    user: (canViewUsers ? (visibleUsersQueryData ?? []) : []).map((user) => ({
      label: `${user.displayName || user.username} (${user.email || user.username})`,
      value: user.id,
    })),
    role: (canViewRoles ? (visibleRolesQueryData ?? []) : []).map((role) => ({
      label: role.name,
      value: role.id,
    })),
    team: (canViewTeams ? (visibleTeamsQueryData ?? []) : []).map((team) => ({
      label: team.path || `${team.name} (${team.slug})`,
      value: team.id,
    })),
    tag: [],
  }
  useEffect(() => {
    if (!open) {
      providerForm.resetFields()
      clientForm.resetFields()
      return
    }
    setStep(0)
    setAccessMode(undefined)
    setConfigureNow(true)
    providerForm.resetFields()
    clientForm.resetFields()
    form.setFieldsValue(
      application
        ? identityApplicationFormValuesFor(application)
        : defaultIdentityApplicationFormValues(),
    )
  }, [application, form, open, providerForm, clientForm])
  const submit = (values: IdentityApplicationFormValues) => {
    if (Boolean(values.startTimeUtc?.trim()) !== Boolean(values.endTimeUtc?.trim())) {
      void message.error(
        t('identity.applications.timeWindowRequired', 'UTC 开始与结束时间必须同时填写'),
      )
      return
    }
    if (values.requireMfa && !stepUpAvailable) {
      void message.error(
        stepUpReason || t('identity.applications.mfaUnavailable', '当前运行环境无法启用 MFA 条件'),
      )
      return
    }
    if (!wizard) onSubmit(buildIdentityApplicationInput(values, application))
    else void advanceWizard()
  }
  const protocolInput = async () => {
    if (!configureProtocol) return {}
    if (providerType === 'saml' && !samlAvailable)
      throw new Error('当前环境未启用 SAML，请稍后配置认证。')
    const values = await providerForm.validateFields()
    const provider = providerInputFromValues({
      ...defaultProviderValues(),
      ...values,
      applicationId: '',
      name: form.getFieldValue('name'),
      type: providerType,
      enabled: true,
      status: 'enabled',
    })
    const oidcClient =
      providerType === 'oidc'
        ? oidcClientInputFromValues('', await clientForm.validateFields())
        : undefined
    return { provider, oidcClient }
  }
  const advanceWizard = async () => {
    try {
      if (step === 0) await form.validateFields(['name', 'slug', 'launchUrl'])
      if (step === 1) await protocolInput()
      if (step >= 2) {
        if (!accessMode) throw new Error('请明确选择访问范围。')
        const values = await form.validateFields()
        const input = buildIdentityApplicationInput(values, application)
        if (
          accessMode === 'restricted' &&
          !input.assignments.some((item) => item.effect === 'allow')
        )
          throw new Error('限定主体时至少添加一条允许规则。')
        if (Boolean(values.startTimeUtc) !== Boolean(values.endTimeUtc))
          throw new Error('UTC 开始与结束时间必须同时填写。')
        if (values.requireMfa && !stepUpAvailable)
          throw new Error(stepUpReason || '当前环境无法启用 MFA 条件。')
        if (step === 3) {
          const protocol = await protocolInput()
          onOnboard?.({
            application: {
              ...input,
              status: 'disabled',
              assignments: accessMode === 'all_authenticated' ? [] : input.assignments,
            },
            accessMode,
            ...protocol,
          })
          return
        }
      }
      setStep(steps[steps.indexOf(step) + 1] ?? 3)
    } catch (error) {
      if (error instanceof Error) void message.error(error.message)
    }
  }
  return (
    <Modal
      destroyOnHidden
      footer={null}
      open={open}
      title={
        application
          ? t('identity.applications.edit', '编辑应用')
          : wizard
            ? '接入应用'
            : t('identity.applications.create', '新建应用')
      }
      width={900}
      onCancel={onCancel}
      style={{ top: 32 }}
      styles={operationModalStyles}
      closable={!blocked}
      okButtonProps={{ disabled: blocked }}
      cancelButtonProps={{ disabled: blocked }}
    >
      {feedback}

      <ReadFeedback canRead={outpostsQuery.isEnabled} query={outpostsQuery} label="Outpost" />
      <ReadFeedback canRead={usersQuery.isEnabled} query={usersQuery} label="身份数据" />
      <ReadFeedback canRead={rolesQuery.isEnabled} query={rolesQuery} label="身份数据" />
      <ReadFeedback canRead={teamsQuery.isEnabled} query={teamsQuery} label="身份数据" />

      {wizard && (
        <Steps
          current={steps.indexOf(step)}
          size="small"
          style={{ marginBottom: 24 }}
          items={steps.map((value) => ({
            title: ['应用信息', '认证配置', '访问范围', '确认与接入'][value],
          }))}
        />
      )}
      <Form
        form={form}
        className="soha-identity-app-form"
        initialValues={defaultIdentityApplicationFormValues()}
        layout="vertical"
        onFinish={submit}
        disabled={blocked}
      >
        <div hidden={wizard && step !== 0}>
          <div className="soha-identity-form-grid">
            <Form.Item
              label={t('identity.applications.name', '名称')}
              name="name"
              rules={[
                {
                  required: true,
                  message: t('identity.applications.nameRequired', '请输入应用名称'),
                },
              ]}
            >
              <Input placeholder="Example App" />
            </Form.Item>
            <Form.Item
              label="Slug"
              name="slug"
              rules={
                wizard
                  ? [
                      {
                        required: true,
                        whitespace: true,
                        message: '请输入唯一标识，用于重试后查找应用',
                      },
                    ]
                  : []
              }
            >
              <Input placeholder="example-app" />
            </Form.Item>
            <Form.Item
              label={t('identity.applications.providerType', 'Provider 类型')}
              name="providerType"
            >
              <Select
                disabled={blocked || Boolean(application)}
                onChange={() => {
                  form.setFieldValue('providerId', '')
                  providerForm.resetFields()
                  clientForm.resetFields()
                  if (wizard) void message.info('已清空上一协议的配置。')
                }}
                options={identityApplicationProviderTypeOptions}
              />
            </Form.Item>
            <Form.Item
              getValueProps={(value?: string) => ({ value: value || undefined })}
              label={t('identity.applications.providerId', 'Provider ID')}
              name="providerId"
              hidden={wizard}
              tooltip={t(
                'identity.applications.providerIdHint',
                'Provider 归属具体应用：新建 OIDC/Proxy 应用时请先保存，随后进入 Provider 配置；编辑时可选择同类型 Provider。',
              )}
            >
              <Select
                allowClear
                disabled={blocked || providerSelectDisabled}
                loading={providerOptionsLoading}
                options={providerSelectOptions}
                placeholder={
                  providerType === 'link'
                    ? t('identity.applications.providerIdLinkPlaceholder', 'Link 应用无需 Provider')
                    : !application
                      ? t(
                          'identity.applications.providerIdCreatePlaceholder',
                          '保存应用后配置 Provider',
                        )
                      : providerOptionsLoading
                        ? t('identity.applications.providerIdLoading', '正在加载 Provider')
                        : providerSelectOptions.length
                          ? t('identity.applications.providerIdPlaceholder', '选择 Provider')
                          : t(
                              'identity.applications.providerIdEmpty',
                              '当前应用暂无匹配的 Provider',
                            )
                }
                showSearch={{ optionFilterProp: 'label' }}
              />
            </Form.Item>
            <div className="soha-identity-publish-controls">
              <div className="soha-identity-inline-switch" hidden={wizard}>
                <Text>{t('identity.applications.column.enabled', '启用状态')}</Text>
                <Form.Item
                  getValueFromEvent={(checked: boolean) => (checked ? 'enabled' : 'disabled')}
                  getValueProps={(status: IdentityApplicationFormValues['status']) => ({
                    checked: status === 'enabled',
                  })}
                  name="status"
                  noStyle
                >
                  <Switch aria-label={t('identity.applications.column.enabled', '启用状态')} />
                </Form.Item>
              </div>
              <div className="soha-identity-inline-switch">
                <Text>{t('identity.applications.portalVisible', '门户可见')}</Text>
                <Form.Item name="portalVisible" noStyle valuePropName="checked">
                  <Switch aria-label={t('identity.applications.portalVisible', '门户可见')} />
                </Form.Item>
              </div>
              <div className="soha-identity-inline-switch">
                <Text>{t('identity.applications.featured', '推荐应用')}</Text>
                <Form.Item name="featured" noStyle valuePropName="checked">
                  <Switch aria-label={t('identity.applications.featured', '推荐应用')} />
                </Form.Item>
              </div>
            </div>
            <Form.Item label={t('identity.applications.sortOrder', '排序')} name="sortOrder">
              <InputNumber min={0} precision={0} style={{ width: '100%' }} />
            </Form.Item>
          </div>

          <Form.Item label={t('identity.applications.descriptionField', '描述')} name="description">
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} />
          </Form.Item>

          <Form.Item label={t('identity.applications.launchUrl', '访问地址')} name="launchUrl">
            <Input prefix={<LinkOutlined />} placeholder="https://app.example.com" />
          </Form.Item>

          <div className="soha-identity-form-grid">
            <Form.Item
              getValueFromEvent={(nextValue: string) => nextValue}
              label={t('identity.applications.icon', '图标')}
              name="iconUrl"
            >
              <ApplicationIconInput />
            </Form.Item>
            <Form.Item label={t('identity.applications.tags', '标签')} name="tags">
              <Select
                mode="tags"
                options={tagOptions}
                placeholder={t('identity.applications.tagsPlaceholder', '选择已有标签或输入新标签')}
                showSearch={{ optionFilterProp: 'label' }}
                tokenSeparators={[',', '，']}
              />
            </Form.Item>
          </div>

          {wizard && (
            <Text type="secondary">
              {providerType === 'link'
                ? 'Link 仅提供入口链接，目标系统自行认证。'
                : 'Soha 为下游应用提供身份认证；新应用保存后保持停用。'}
            </Text>
          )}
        </div>
        <div hidden={wizard && step !== 2}>
          {wizard && (
            <Form.Item label="访问范围" required>
              <Select
                aria-label="访问范围"
                value={accessMode}
                placeholder="请选择"
                options={[
                  { label: '所有已登录用户', value: 'all_authenticated' },
                  { label: '限定用户、角色、团队或标签', value: 'restricted' },
                ]}
                onChange={(value) => {
                  setAccessMode(value)
                  if (value === 'all_authenticated') form.setFieldValue('assignments', [])
                }}
              />
            </Form.Item>
          )}
          <div className="soha-identity-access-control-section">
            <Form.List name="assignments">
              {(fields, { add, remove }) => (
                <>
                  <div
                    className="soha-identity-access-control-header"
                    hidden={wizard && accessMode !== 'restricted'}
                  >
                    <Space size={4}>
                      <Text strong>{t('identity.applications.accessControl', '访问控制')}</Text>
                      <Tooltip title={assignmentHint} trigger={['hover', 'focus']}>
                        <QuestionCircleOutlined
                          aria-label={t(
                            'identity.applications.assignmentHintLabel',
                            '访问控制说明',
                          )}
                          tabIndex={0}
                        />
                      </Tooltip>
                    </Space>
                    <Button
                      icon={<PlusOutlined />}
                      size="small"
                      onClick={() => add({ effect: 'allow', subjectIds: [], subjectType: 'role' })}
                      disabled={blocked}
                    >
                      {t('common.add', '添加')}
                    </Button>
                  </div>
                  <div
                    className="soha-identity-assignment-editor"
                    hidden={wizard && accessMode !== 'restricted'}
                  >
                    {fields.map((field) => (
                      <div className="soha-identity-assignment-row" key={field.key}>
                        <Form.Item name={[field.name, 'subjectType']} rules={[{ required: true }]}>
                          <Select
                            aria-label={t('identity.applications.subjectType', '主体类型')}
                            disabled={
                              blocked ||
                              Boolean(
                                assignmentValues[field.name]?.subjectIds?.some((id) => id.trim()),
                              )
                            }
                            options={identityApplicationAssignmentSubjectOptions}
                            onChange={() =>
                              form.setFieldValue(['assignments', field.name, 'subjectIds'], [])
                            }
                          />
                        </Form.Item>
                        <Form.Item
                          name={[field.name, 'subjectIds']}
                          rules={[
                            {
                              required: true,
                              message: t(
                                'identity.applications.subjectRequired',
                                '请选择或输入授权主体',
                              ),
                            },
                          ]}
                        >
                          <AssignmentSubjectSelect
                            fieldName={field.name}
                            form={form}
                            options={subjectOptions}
                          />
                        </Form.Item>
                        <Form.Item name={[field.name, 'effect']}>
                          <Select options={identityApplicationAssignmentEffectOptions} />
                        </Form.Item>
                        <Button
                          aria-label={t('identity.applications.removeAssignment', '删除访问授权')}
                          danger
                          icon={<DeleteOutlined />}
                          onClick={() => remove(field.name)}
                          disabled={blocked}
                        />
                      </div>
                    ))}
                  </div>
                </>
              )}
            </Form.List>
            <div className="soha-identity-policy-conditions-section">
              <Space size={4}>
                <Text strong>{t('identity.applications.accessConditions', '访问条件')}</Text>
                <Tooltip title={conditionHint} trigger={['hover', 'focus']}>
                  <QuestionCircleOutlined
                    aria-label={t('identity.applications.conditionHintLabel', '访问条件说明')}
                    tabIndex={0}
                  />
                </Tooltip>
              </Space>
              <div className="soha-identity-policy-conditions">
                {!stepUpAvailable ? (
                  <Alert
                    showIcon
                    title={t('identity.applications.mfaUnavailableTitle', 'MFA 升级验证不可用')}
                    description={
                      stepUpReason ||
                      t('identity.applications.mfaUnavailable', '当前运行环境无法启用 MFA 条件。')
                    }
                    type="warning"
                  />
                ) : null}
                <Form.Item
                  label={t('identity.applications.requireMfa', '要求 MFA')}
                  name="requireMfa"
                  valuePropName="checked"
                >
                  <Switch disabled={blocked || !stepUpAvailable} />
                </Form.Item>
                <Form.Item
                  label={t('identity.applications.allowedCidrs', '允许的 CIDR')}
                  name="allowedCidrs"
                >
                  <Select mode="tags" placeholder="10.0.0.0/8" tokenSeparators={[',']} />
                </Form.Item>
                <div className="soha-identity-policy-time-window">
                  <Form.Item
                    getValueFromEvent={(_: unknown, value: string) => value}
                    getValueProps={(value?: string) => ({
                      value: value ? dayjs(`2000-01-01T${value}:00`) : null,
                    })}
                    label={t('identity.applications.startTimeUtc', 'UTC 开始时间')}
                    name="startTimeUtc"
                  >
                    <TimePicker
                      format="HH:mm"
                      placeholder={t('identity.applications.timePlaceholder', '选择时间')}
                      showNow={false}
                      style={{ width: '100%' }}
                    />
                  </Form.Item>
                  <Form.Item
                    getValueFromEvent={(_: unknown, value: string) => value}
                    getValueProps={(value?: string) => ({
                      value: value ? dayjs(`2000-01-01T${value}:00`) : null,
                    })}
                    label={t('identity.applications.endTimeUtc', 'UTC 结束时间')}
                    name="endTimeUtc"
                  >
                    <TimePicker
                      format="HH:mm"
                      placeholder={t('identity.applications.timePlaceholder', '选择时间')}
                      showNow={false}
                      style={{ width: '100%' }}
                    />
                  </Form.Item>
                </div>
              </div>
            </div>
          </div>
        </div>
        {!wizard && (
          <div className="soha-identity-form-actions">
            <Button onClick={onCancel} disabled={blocked}>
              {t('common.cancel', '取消')}
            </Button>
            <Button htmlType="submit" loading={saving} type="primary" disabled={blocked}>
              {t('common.save', '保存')}
            </Button>
          </div>
        )}
      </Form>
      {wizard && (
        <>
          <div hidden={step !== 1}>
            {providerType !== 'link' && (
              <>
                {!canConfigureProvider ? (
                  <Alert
                    type="warning"
                    showIcon
                    title="缺少创建认证接入的权限"
                    description="可以先创建停用的应用，稍后由有权限的管理员继续配置。"
                  />
                ) : (
                  <Form.Item label="现在配置认证">
                    <Switch checked={configureNow} onChange={setConfigureNow} />
                  </Form.Item>
                )}
                {configureProtocol && (
                  <>
                    <Form
                      form={providerForm}
                      layout="vertical"
                      initialValues={defaultProviderValues()}
                      disabled={blocked}
                    >
                      <ProviderConfigFields
                        providerType={providerType}
                        outpostLoading={outpostsQuery.isPending}
                        outpostOptions={(canViewOutposts
                          ? (visibleOutpostsQueryData ?? [])
                          : []
                        ).map((item) => ({
                          label: item.name,
                          value: item.id,
                        }))}
                        samlAvailable={samlAvailable}
                      />
                    </Form>
                    {providerType === 'oidc' && (
                      <Form
                        form={clientForm}
                        layout="vertical"
                        initialValues={defaultOIDCClientValues()}
                        disabled={blocked}
                      >
                        <OIDCClientFields form={clientForm} />
                      </Form>
                    )}
                  </>
                )}
              </>
            )}
          </div>
          {step === 3 && (
            <>
              <Descriptions
                column={1}
                items={[
                  { key: 'name', label: '应用', children: form.getFieldValue('name') },
                  { key: 'type', label: '接入方式', children: providerType.toUpperCase() },
                  {
                    key: 'protocol',
                    label: '认证配置',
                    children:
                      providerType === 'link'
                        ? '无需配置'
                        : configureProtocol
                          ? '随应用一同创建'
                          : '稍后继续配置',
                  },
                  {
                    key: 'access',
                    label: '访问范围',
                    children:
                      accessMode === 'all_authenticated'
                        ? '所有已登录用户'
                        : String(
                            buildIdentityApplicationInput(form.getFieldsValue(true)).assignments
                              .length,
                          ) + ' 条主体规则；拒绝优先',
                  },
                  { key: 'status', label: '启用状态', children: '停用；完成接入验证后再启用' },
                ]}
              />
              <Alert type="info" showIcon title="保存后提供接入资料，真实登录尚未验证。" />
            </>
          )}
          <div className="soha-identity-form-actions">
            <Button disabled={blocked || saving} onClick={onCancel}>
              取消
            </Button>
            {step > 0 && (
              <Button
                disabled={blocked || saving}
                onClick={() => setStep(steps[steps.indexOf(step) - 1] ?? 0)}
              >
                上一步
              </Button>
            )}
            <Button
              type="primary"
              loading={saving}
              onClick={() => void advanceWizard()}
              disabled={blocked}
            >
              {step === 3 ? '保存并查看接入说明' : '下一步'}
            </Button>
          </div>
        </>
      )}
    </Modal>
  )
}
