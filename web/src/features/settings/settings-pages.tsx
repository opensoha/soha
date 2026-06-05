import { useEffect, useMemo, useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import {
  Alert,
  AutoComplete,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Col,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Tag,
  message,
} from "antd";
import type { TableColumnsType } from "antd";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useLocation, useNavigate } from "react-router-dom";
import { AdminTable } from "@/components/admin-table";
import {
  ManagementDetailHeader,
  ManagementIconButton,
  ManagementState,
  ManagementTableToolbar,
} from "@/components/management-list";
import {
  hasPermission,
  invalidateAuthz,
  usePermissionSnapshot,
} from "@/features/auth/permission-snapshot";
import { api } from "@/services/api-client";
import { StatusTag } from "@/components/status-tag";
import { formatDateTime } from "@/utils/time";
import { tableColumnPresets } from "@/utils/table-columns";
import {
  PLAYBOOK_OPTIONS,
  SEVERITY_OPTIONS,
  STATUS_OPTIONS,
  TRACES_BACKEND_OPTIONS,
  AGENT_RUNTIME_STATE_LABELS,
  agentCapabilityLabels,
  buildDataSourceFormValues,
  buildDataSourcePayload,
  buildPolicyFormValues,
  buildPolicyPayload,
  buildProfileFormValues,
  buildProfilePayload,
  normalizeAIProviderConnection,
  resolveAgentRuntimeState,
  summarizeAgentProviderRuntime,
} from "./ai-settings-model";
import type {
  AgentProviderRuntimeRow,
  AgentRuntimeSummary,
  AIProviderConnection,
  AISettings,
  AISkillSetting,
  AnalysisProfile,
  AutomationPolicy,
  DataSource,
} from "./ai-settings-model";
import type { ApiResponse, BrandingSettings } from "@/types";
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  CheckCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  ReloadOutlined,
  StarOutlined,
} from "@ant-design/icons";
import type {
  WorkbenchAgentRun,
  WorkbenchCatalog,
} from "@/features/copilot/workbench-types";
import "./settings-pages.css";

const WIDE_FORM_LAYOUT = {
  labelAlign: "left" as const,
  labelCol: { flex: "160px" },
  wrapperCol: { flex: "auto" },
};

const DEFAULT_FORM_LAYOUT = {
  labelAlign: "left" as const,
  labelCol: { flex: "140px" },
  wrapperCol: { flex: "auto" },
};

const fullWidthStyle = { width: "100%" };

function SectionCallout({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="mb-4 rounded border border-[var(--soha-border-color)] bg-[var(--soha-fill-weak)] p-3 text-sm">
      <div className="font-medium">{title}</div>
      <div className="mt-1 text-[var(--ant-colorTextSecondary)]">
        {description}
      </div>
    </div>
  );
}

function SettingsCard({
  title,
  extra,
  children,
}: {
  title?: ReactNode;
  extra?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Card title={title} extra={extra}>
      {children}
    </Card>
  );
}

function TagSelect(props: {
  placeholder?: string;
  mode?: "multiple" | "tags";
  options?: Array<{ value: string; label: string }>;
}) {
  return <Select {...props} style={fullWidthStyle} />;
}

/* ─── Login Settings ─── */

interface OIDCSettings {
  enabled: boolean;
  providerName: string;
  issuer: string;
  clientId: string;
  clientSecret: string;
  redirectUrl: string;
  frontendRedirectUrl: string;
  scopes: string[];
  defaultRoles: string[];
}

interface LoginProviderSettings {
  id: string;
  name: string;
  type: string;
  enabled: boolean;
  clientId: string;
  clientSecret: string;
  issuer: string;
  authorizeUrl: string;
  tokenUrl: string;
  userInfoUrl: string;
  profileUrl: string;
  redirectUrl: string;
  frontendRedirectUrl: string;
  scopes: string[];
  defaultRoles: string[];
  userIdField: string;
  userNameField: string;
  emailField: string;
  roleField: string;
  organizationField: string;
  syncRolesOnLogin: boolean;
  syncOrgsOnLogin: boolean;
  roleSyncMode: string;
  orgSyncMode: string;
  metadataUrl: string;
  entityId: string;
  certificate: string;
}

interface IdentitySettingsResponse {
  oidc?: OIDCSettings;
  providers?: LoginProviderSettings[];
  defaultProviderId?: string;
}

interface SettingsPageProps {
  embedded?: boolean | "provider-only";
}

const LOGIN_PROVIDER_TYPE_OPTIONS = [
  { value: "oidc", label: "OIDC" },
  { value: "feishu", label: "飞书 OAuth2" },
  { value: "dingtalk", label: "钉钉 OAuth2" },
  { value: "wecom", label: "企业微信 OAuth2" },
  { value: "oauth2", label: "通用 OAuth2" },
  { value: "saml", label: "SAML" },
];

const LOGIN_SYNC_MODE_OPTIONS = [
  { value: "append", label: "补充绑定" },
  { value: "replace_external", label: "替换该登录源绑定" },
];

function normalizeLoginProvider(
  item?: Partial<LoginProviderSettings> | null,
): LoginProviderSettings {
  return {
    id: String(item?.id || ""),
    name: String(item?.name || ""),
    type: String(item?.type || "oidc"),
    enabled: Boolean(item?.enabled),
    clientId: String(item?.clientId || ""),
    clientSecret: String(item?.clientSecret || ""),
    issuer: String(item?.issuer || ""),
    authorizeUrl: String(item?.authorizeUrl || ""),
    tokenUrl: String(item?.tokenUrl || ""),
    userInfoUrl: String(item?.userInfoUrl || ""),
    profileUrl: String(item?.profileUrl || ""),
    redirectUrl: String(item?.redirectUrl || ""),
    frontendRedirectUrl: String(item?.frontendRedirectUrl || ""),
    scopes: Array.isArray(item?.scopes)
      ? item!.scopes!.map((value) => String(value))
      : [],
    defaultRoles: Array.isArray(item?.defaultRoles)
      ? item!.defaultRoles!.map((value) => String(value))
      : [],
    userIdField: String(item?.userIdField || ""),
    userNameField: String(item?.userNameField || ""),
    emailField: String(item?.emailField || ""),
    roleField: String(item?.roleField || ""),
    organizationField: String(item?.organizationField || ""),
    syncRolesOnLogin: Boolean(item?.syncRolesOnLogin),
    syncOrgsOnLogin: Boolean(item?.syncOrgsOnLogin),
    roleSyncMode: String(item?.roleSyncMode || "append"),
    orgSyncMode: String(item?.orgSyncMode || "append"),
    metadataUrl: String(item?.metadataUrl || ""),
    entityId: String(item?.entityId || ""),
    certificate: String(item?.certificate || ""),
  };
}

function defaultRedirectPath(providerId: string) {
  return `${window.location.origin}/api/v1/auth/login/${providerId || "provider"}/callback`;
}

function defaultFrontendRedirectPath() {
  return `${window.location.origin}/login/callback`;
}

function applyProviderPreset(
  type: string,
  current?: Partial<LoginProviderSettings> | null,
): LoginProviderSettings {
  const provider = normalizeLoginProvider(current);
  switch (type) {
    case "feishu":
      return {
        ...provider,
        type,
        authorizeUrl:
          provider.authorizeUrl ||
          "https://open.feishu.cn/open-apis/authen/v1/authorize",
        tokenUrl:
          provider.tokenUrl ||
          "https://open.feishu.cn/open-apis/authen/v1/oidc/access_token",
        userInfoUrl:
          provider.userInfoUrl ||
          "https://open.feishu.cn/open-apis/authen/v1/user_info",
        scopes:
          provider.scopes.length > 0
            ? provider.scopes
            : ["contact:user.base:readonly"],
        userIdField: provider.userIdField || "open_id",
        userNameField: provider.userNameField || "name",
        emailField: provider.emailField || "enterprise_email",
        roleField: provider.roleField || "role_ids",
        organizationField: provider.organizationField || "department_ids",
      };
    case "dingtalk":
      return {
        ...provider,
        type,
        authorizeUrl:
          provider.authorizeUrl || "https://login.dingtalk.com/oauth2/auth",
        tokenUrl:
          provider.tokenUrl ||
          "https://api.dingtalk.com/v1.0/oauth2/userAccessToken",
        userInfoUrl:
          provider.userInfoUrl ||
          "https://api.dingtalk.com/v1.0/contact/users/me",
        scopes: provider.scopes.length > 0 ? provider.scopes : ["openid"],
        userIdField: provider.userIdField || "unionId",
        userNameField: provider.userNameField || "nick",
        emailField: provider.emailField || "email",
        roleField: provider.roleField || "roleList",
        organizationField: provider.organizationField || "dept_id_list",
      };
    case "wecom":
      return {
        ...provider,
        type,
        authorizeUrl:
          provider.authorizeUrl ||
          "https://open.weixin.qq.com/connect/oauth2/authorize",
        tokenUrl:
          provider.tokenUrl || "https://qyapi.weixin.qq.com/cgi-bin/gettoken",
        userInfoUrl:
          provider.userInfoUrl ||
          "https://qyapi.weixin.qq.com/cgi-bin/user/getuserinfo",
        scopes: provider.scopes.length > 0 ? provider.scopes : ["snsapi_base"],
        userIdField: provider.userIdField || "UserId",
        userNameField: provider.userNameField || "UserId",
        emailField: provider.emailField || "email",
        roleField: provider.roleField || "roles",
        organizationField: provider.organizationField || "department",
      };
    case "saml":
      return {
        ...provider,
        type,
        scopes: [],
        authorizeUrl: "",
        tokenUrl: "",
        userInfoUrl: "",
      };
    default:
      return {
        ...provider,
        type,
        scopes:
          provider.scopes.length > 0
            ? provider.scopes
            : ["openid", "profile", "email"],
        userIdField: provider.userIdField || "sub",
        userNameField: provider.userNameField || "name",
        emailField: provider.emailField || "email",
        roleField: provider.roleField || "roles",
        organizationField: provider.organizationField || "groups",
      };
  }
}

export function BrandingSettingsPage({
  embedded = false,
}: SettingsPageProps = {}) {
  const queryClient = useQueryClient();
  const permissionSnapshotQuery = usePermissionSnapshot();
  const canViewBrandingSettings = hasPermission(
    permissionSnapshotQuery.data?.data,
    "settings.branding.view",
  );
  const canManageBrandingSettings = hasPermission(
    permissionSnapshotQuery.data?.data,
    "settings.branding.manage",
  );

  const { data, isLoading } = useQuery({
    queryKey: ["settings-branding"],
    queryFn: () => api.get<ApiResponse<BrandingSettings>>("/settings/branding"),
  });

  const saveMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) =>
      api.put("/settings/branding", values),
    onSuccess: () => {
      void message.success("品牌设置已保存");
      void queryClient.invalidateQueries({ queryKey: ["settings-branding"] });
    },
    onError: (err: Error) => void message.error(err.message),
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    );
  }

  if (!canViewBrandingSettings) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有查看品牌设置的权限。" />
      </div>
    );
  }

  const settings = data?.data;
  const content = (
    <SettingsCard>
      <Form
        {...WIDE_FORM_LAYOUT}
        onFinish={(values) => {
          if (!canManageBrandingSettings) return;
          saveMutation.mutate(values as Record<string, unknown>);
        }}
        initialValues={
          settings ?? { appTitle: "Soha", sidebarTitle: "Soha" }
        }
      >
        <Form.Item name="appTitle" label="网页标题">
          <Input placeholder="浏览器标签页标题" />
        </Form.Item>
        <Form.Item name="sidebarTitle" label="侧边栏标题">
          <Input placeholder="左侧品牌栏文字" />
        </Form.Item>

        <div className="soha-branding-section-title">企业 Logo</div>
        <div className="soha-branding-upload-grid">
          <BrandingUploadField
            field="loginLogoUrl"
            label="登录页面使用的图标（浅色）"
            hint="格式: JPG/PNG/SVG，推荐大小: 200px * 60px"
            previewWidth={200}
            previewHeight={60}
            disabled={!canManageBrandingSettings}
          />
          <BrandingUploadField
            field="expandedLogoUrl"
            label="登录页左上角使用的图标（深色）以及侧边栏展开后左上角使用的图标（深色）"
            hint="格式: JPG/PNG/SVG，推荐大小: 200px * 60px"
            previewWidth={200}
            previewHeight={60}
            disabled={!canManageBrandingSettings}
          />
          <BrandingUploadField
            field="collapsedLogoUrl"
            label="侧边栏收缩后左上角使用的图标"
            hint="格式: JPG/PNG/SVG，推荐大小: 60px * 60px"
            previewWidth={60}
            previewHeight={60}
            disabled={!canManageBrandingSettings}
          />
          <BrandingUploadField
            field="faviconUrl"
            label="Favicon 图标"
            hint="格式: JPG/PNG/SVG/ICO，推荐大小: 16px*16px、32px*32px、64px*64px"
            previewWidth={64}
            previewHeight={64}
            disabled={!canManageBrandingSettings}
          />
        </div>

        <div className="soha-form-actions">
          {canManageBrandingSettings ? (
            <Button
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
  );

  if (embedded) {
    return content;
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="品牌设置"
        description="配置品牌 Logo、Favicon 与网页标题。"
      />
      {content}
    </div>
  );
}

interface BrandingUploadFieldProps {
  field: string;
  label: string;
  hint: string;
  previewWidth: number;
  previewHeight: number;
  disabled?: boolean;
}

function BrandingUploadField({
  field,
  label,
  hint,
  previewWidth,
  previewHeight,
  disabled,
}: BrandingUploadFieldProps) {
  const [uploading, setUploading] = useState(false);
  const form = Form.useFormInstance();
  const currentValue = Form.useWatch(field, form) as string | undefined;

  const handleUploadClick = () => {
    if (disabled || uploading) return;
    const input = document.createElement("input");
    input.type = "file";
    input.accept = ".jpg,.jpeg,.png,.svg,.ico,.webp";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      if (file.size > 2 * 1024 * 1024) {
        void message.error("文件大小不能超过 2MB");
        return;
      }
      setUploading(true);
      try {
        const formData = new FormData();
        formData.append("file", file);
        const res = await api.upload<ApiResponse<{ url: string }>>(
          "/settings/branding/upload",
          formData,
        );
        form.setFieldValue(field, res.data.url);
        void message.success("图片上传成功");
      } catch (err: any) {
        void message.error(err?.message ?? "上传失败");
      } finally {
        setUploading(false);
      }
    };
    input.click();
  };

  const handleRemove = (e: MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation();
    form.setFieldValue(field, "");
  };

  return (
    <div className="soha-branding-upload-zone">
      <div className="soha-branding-upload-label">{label}</div>
      <Form.Item name={field} hidden>
        <Input />
      </Form.Item>
      <div className="soha-branding-upload-area-wrap">
        <div
          className={`soha-branding-upload-area ${disabled ? "is-disabled" : ""}`}
          style={{
            width: Math.max(previewWidth + 40, 160),
            height: Math.max(previewHeight + 40, 100),
          }}
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
        </div>
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
  );
}

export function LoginSettingsPage({
  embedded = false,
}: SettingsPageProps = {}) {
  const queryClient = useQueryClient();
  const permissionSnapshotQuery = usePermissionSnapshot();
  const [providerForm] = Form.useForm();
  const [providerModalVisible, setProviderModalVisible] = useState(false);
  const [editingProvider, setEditingProvider] =
    useState<LoginProviderSettings | null>(null);
  const canViewLoginSettings = hasPermission(
    permissionSnapshotQuery.data?.data,
    "settings.identity.view",
  );
  const canManageLoginSettings = hasPermission(
    permissionSnapshotQuery.data?.data,
    "settings.identity.manage",
  );

  const { data, isLoading } = useQuery({
    queryKey: ["settings-identity"],
    queryFn: () =>
      api.get<ApiResponse<IdentitySettingsResponse>>("/settings/identity"),
    select: (response: any) => {
      const current = response.data as IdentitySettingsResponse;
      const legacyOIDC = current.oidc;
      const providers =
        Array.isArray(current.providers) && current.providers.length > 0
          ? current.providers.map((item) => normalizeLoginProvider(item))
          : legacyOIDC
            ? [
                normalizeLoginProvider({
                  id: legacyOIDC.providerName || "oidc-default",
                  name: legacyOIDC.providerName || "OIDC",
                  type: "oidc",
                  enabled: legacyOIDC.enabled,
                  issuer: legacyOIDC.issuer,
                  clientId: legacyOIDC.clientId,
                  clientSecret: legacyOIDC.clientSecret,
                  redirectUrl: legacyOIDC.redirectUrl,
                  frontendRedirectUrl: legacyOIDC.frontendRedirectUrl,
                  scopes: legacyOIDC.scopes,
                  defaultRoles: legacyOIDC.defaultRoles,
                  userIdField: "sub",
                  userNameField: "name",
                  emailField: "email",
                }),
              ]
            : [];
      return {
        data: {
          providers,
          defaultProviderId:
            current.defaultProviderId || providers[0]?.id || "",
        },
      };
    },
  });

  const saveMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) =>
      api.put("/settings/identity/providers", values),
    onSuccess: () => {
      void message.success("登陆设置已保存");
      void queryClient.invalidateQueries({ queryKey: ["settings-identity"] });
      void invalidateAuthz(queryClient);
    },
    onError: (err: Error) => void message.error(err.message),
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    );
  }

  if (!canViewLoginSettings) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有查看登陆设置的权限。" />
      </div>
    );
  }

  const settings = data?.data;
  const providers = settings?.providers ?? [];
  const providerColumns: TableColumnsType<LoginProviderSettings> = [
    { title: "名称", dataIndex: "name" },
    {
      title: "类型",
      dataIndex: "type",
      render: (value: string) => {
        const item = LOGIN_PROVIDER_TYPE_OPTIONS.find(
          (current) => current.value === value,
        );
        return item?.label || value;
      },
    },
    {
      title: "回调地址",
      dataIndex: "redirectUrl",
      render: (value: string) => value || "-",
    },
    {
      title: "登录映射",
      dataIndex: "id",
      render: (_: unknown, record: LoginProviderSettings) => {
        const items = [];
        if (record.syncRolesOnLogin) {
          items.push(<Tag key="roles">角色</Tag>);
        }
        if (record.syncOrgsOnLogin) {
          items.push(<Tag key="orgs">组织</Tag>);
        }
        return items.length > 0 ? items : "-";
      },
    },
    {
      title: "启用",
      dataIndex: "enabled",
      render: (value: boolean) => (
        <StatusTag value={value ? "enabled" : "disabled"} />
      ),
    },
    {
      title: "默认",
      dataIndex: "id",
      render: (value: string) =>
        settings?.defaultProviderId === value ? (
          <Tag color="blue">默认</Tag>
        ) : (
          "-"
        ),
    },
    {
      ...tableColumnPresets.action,
      title: "操作",
      dataIndex: "id",
      render: (_: unknown, record: LoginProviderSettings) =>
        canManageLoginSettings ? (
          <Space className="soha-row-action-icons">
            <ManagementIconButton
              aria-label="编辑登录源"
              tooltip="编辑"
              icon={<EditOutlined />}
              size="small"
              onClick={() => {
                setEditingProvider(record);
                setProviderModalVisible(true);
              }}
            />
            <Popconfirm
              title="确认删除登录源？"
              description="删除后会立即保存登录设置。"
              okButtonProps={{ danger: true, loading: saveMutation.isPending }}
              onConfirm={() => {
                const nextProviders = providers.filter(
                  (item) => item.id !== record.id,
                );
                saveMutation.mutate({
                  providers: nextProviders,
                  defaultProviderId:
                    settings?.defaultProviderId === record.id
                      ? nextProviders[0]?.id || ""
                      : settings?.defaultProviderId,
                });
              }}
            >
              <ManagementIconButton
                aria-label="删除登录源"
                tooltip="删除"
                danger
                icon={<DeleteOutlined />}
                loading={saveMutation.isPending}
                size="small"
              />
            </Popconfirm>
            {settings?.defaultProviderId !== record.id ? (
              <ManagementIconButton
                aria-label="设为默认登录源"
                tooltip="设为默认"
                icon={<StarOutlined />}
                size="small"
                onClick={() =>
                  saveMutation.mutate({
                    providers,
                    defaultProviderId: record.id,
                  })
                }
              />
            ) : null}
          </Space>
        ) : (
          "-"
        ),
    },
  ];
  const content = (
    <>
      <SettingsCard
        title="登陆设置"
        extra={
          canManageLoginSettings ? (
            <ManagementTableToolbar>
              <Button
                size="small"
                type="primary"
                onClick={() => {
                  setEditingProvider(null);
                  setProviderModalVisible(true);
                }}
              >
                新增登录源
              </Button>
            </ManagementTableToolbar>
          ) : null
        }
      >
        <AdminTable
          shellClassName="soha-management-table-shell"
          columnSettingIconOnly
          columnSettingPlacement="header"
          rowKey="id"
          tableSize="small"
          pagination={false}
          dataSource={providers}
          columns={providerColumns}
          empty={<ManagementState bordered={false} compact title="暂无登录源配置" description="新增登录源后，登录入口会按启用状态展示。" />}
        />
      </SettingsCard>
      <Modal
        title={editingProvider ? "编辑登录源" : "新增登录源"}
        open={providerModalVisible}
        onCancel={() => {
          setProviderModalVisible(false);
          setEditingProvider(null);
        }}
        footer={null}
        destroyOnHidden
      >
        <Form
          form={providerForm}
          {...DEFAULT_FORM_LAYOUT}
          initialValues={applyProviderPreset(editingProvider?.type || "oidc", {
            ...editingProvider,
            redirectUrl:
              editingProvider?.redirectUrl ||
              defaultRedirectPath(editingProvider?.id || "provider"),
            frontendRedirectUrl:
              editingProvider?.frontendRedirectUrl ||
              defaultFrontendRedirectPath(),
          })}
          onFinish={(values) => {
            if (!canManageLoginSettings) return;
            const sourceType = String(values.type || "oidc");
            const sourceID = String(
              values.id ||
                editingProvider?.id ||
                `${sourceType}-${crypto.randomUUID()}`,
            ).trim();
            const nextProvider = applyProviderPreset(sourceType, {
              ...values,
              id: sourceID,
              redirectUrl: String(
                values.redirectUrl || defaultRedirectPath(sourceID),
              ),
              frontendRedirectUrl: String(
                values.frontendRedirectUrl || defaultFrontendRedirectPath(),
              ),
            });
            const nextProviders = [...providers];
            const index = nextProviders.findIndex(
              (item) => item.id === nextProvider.id,
            );
            if (index >= 0) {
              nextProviders[index] = nextProvider;
            } else {
              nextProviders.push(nextProvider);
            }
            saveMutation.mutate({
              providers: nextProviders,
              defaultProviderId: settings?.defaultProviderId || nextProvider.id,
            });
            setProviderModalVisible(false);
            setEditingProvider(null);
          }}
        >
          <Form.Item
            name="id"
            label="ID"
            rules={[{ required: true, message: "请输入登录源 ID" }]}
          >
            <Input placeholder="oidc-default / feishu-main / saml-corp" />
          </Form.Item>
          <Form.Item
            name="name"
            label="显示名称"
            rules={[{ required: true, message: "请输入显示名称" }]}
          >
            <Input placeholder="企业统一登录 / 飞书 / 钉钉" />
          </Form.Item>
          <Form.Item
            name="type"
            label="类型"
            rules={[{ required: true, message: "请选择登录类型" }]}
          >
            <Select
              options={LOGIN_PROVIDER_TYPE_OPTIONS}
              onChange={(value) => {
                const current = providerForm.getFieldsValue();
                providerForm.setFieldsValue(
                  applyProviderPreset(value, current),
                );
              }}
            />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item
            noStyle
            shouldUpdate={(prev, next) => prev.type !== next.type}
          >
            {({ getFieldValue }) => {
              const type = String(getFieldValue("type") || "oidc");
              return (
                <>
                  {type === "saml" ? (
                    <Alert
                      type="warning"
                      showIcon
                      style={{ marginBottom: 16 }}
                      title="SAML 当前为配置态"
                      description="本次改动已支持 SAML 配置保存和菜单/登录入口展示，但后端断言消费与 ACS 运行链路尚未启用。"
                    />
                  ) : null}
                  {type === "oidc" ? (
                    <Form.Item
                      name="issuer"
                      label="Issuer URL"
                      rules={[{ required: true, message: "请输入 Issuer URL" }]}
                    >
                      <Input placeholder="https://accounts.example.com" />
                    </Form.Item>
                  ) : null}
                  {type !== "saml" ? (
                    <>
                      <Form.Item name="authorizeUrl" label="Authorize URL">
                        <Input placeholder="https://provider.example.com/oauth2/authorize" />
                      </Form.Item>
                      <Form.Item name="tokenUrl" label="Token URL">
                        <Input placeholder="https://provider.example.com/oauth2/token" />
                      </Form.Item>
                      <Form.Item name="userInfoUrl" label="UserInfo URL">
                        <Input placeholder="https://provider.example.com/userinfo" />
                      </Form.Item>
                    </>
                  ) : (
                    <>
                      <Form.Item name="metadataUrl" label="Metadata URL">
                        <Input placeholder="https://idp.example.com/metadata" />
                      </Form.Item>
                      <Form.Item name="entityId" label="Entity ID">
                        <Input placeholder="https://soha.example.com/saml/sp" />
                      </Form.Item>
                      <Form.Item name="certificate" label="证书">
                        <Input.TextArea
                          rows={4}
                          placeholder="粘贴 IdP 证书内容"
                        />
                      </Form.Item>
                    </>
                  )}
                </>
              );
            }}
          </Form.Item>
          <Form.Item name="clientId" label="Client ID">
            <Input />
          </Form.Item>
          <Form.Item name="clientSecret" label="Client Secret">
            <Input.Password />
          </Form.Item>
          <Form.Item name="redirectUrl" label="回调地址">
            <Input />
          </Form.Item>
          <Form.Item name="frontendRedirectUrl" label="前端回跳地址">
            <Input />
          </Form.Item>
          <Form.Item name="scopes" label="Scopes">
            <TagSelect mode="tags" placeholder="openid / profile / email" />
          </Form.Item>
          <Form.Item name="defaultRoles" label="默认角色">
            <TagSelect mode="tags" placeholder="readonly / admin" />
          </Form.Item>
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item
                name="syncRolesOnLogin"
                label="登录补充角色"
                valuePropName="checked"
              >
                <Switch />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item
                name="syncOrgsOnLogin"
                label="登录补充组织"
                valuePropName="checked"
              >
                <Switch />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item name="roleField" label="角色字段">
                <Input placeholder="roles / role_ids / realm_access.roles" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="roleSyncMode" label="角色模式">
                <Select options={LOGIN_SYNC_MODE_OPTIONS} />
              </Form.Item>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col xs={24} md={12}>
              <Form.Item name="organizationField" label="组织字段">
                <Input placeholder="groups / department_ids / department" />
              </Form.Item>
            </Col>
            <Col xs={24} md={12}>
              <Form.Item name="orgSyncMode" label="组织模式">
                <Select options={LOGIN_SYNC_MODE_OPTIONS} />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="userIdField" label="用户ID字段">
            <Input placeholder="sub / open_id / unionId / UserId" />
          </Form.Item>
          <Form.Item name="userNameField" label="用户名字段">
            <Input placeholder="name / nick / preferred_username" />
          </Form.Item>
          <Form.Item name="emailField" label="邮箱字段">
            <Input placeholder="email / enterprise_email" />
          </Form.Item>
          <div className="soha-form-actions">
            <Button
              onClick={() => {
                setProviderModalVisible(false);
                setEditingProvider(null);
              }}
            >
              取消
            </Button>
            {canManageLoginSettings ? (
              <Button
                htmlType="submit"
                type="primary"
                loading={saveMutation.isPending}
              >
                保存
              </Button>
            ) : null}
          </div>
        </Form>
      </Modal>
    </>
  );

  if (embedded) {
    return content;
  }

  return <div className="soha-page">{content}</div>;
}

/* ─── Monitoring Settings (Prometheus) ─── */

interface PrometheusSettings {
  enabled: boolean;
  baseUrl: string;
  bearerToken: string;
  defaultRangeMinutes: number;
  stepSeconds: number;
  clusterLabel: string;
  grafanaBaseUrl: string;
}

export function MonitoringSettingsPage() {
  const queryClient = useQueryClient();
  const permissionSnapshotQuery = usePermissionSnapshot();
  const canViewMonitoringSettings = hasPermission(
    permissionSnapshotQuery.data?.data,
    "settings.monitoring.view",
  );
  const canManageMonitoringSettings = hasPermission(
    permissionSnapshotQuery.data?.data,
    "settings.monitoring.manage",
  );

  const { data, isLoading } = useQuery({
    queryKey: ["settings-monitoring"],
    queryFn: () =>
      api.get<ApiResponse<PrometheusSettings>>("/settings/monitoring"),
    select: (response: any) => ({
      data: response.data.prometheus as PrometheusSettings,
    }),
  });

  const saveMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) =>
      api.put("/settings/monitoring/prometheus", values),
    onSuccess: () => {
      void message.success("监控设置已保存");
      void queryClient.invalidateQueries({ queryKey: ["settings-monitoring"] });
    },
    onError: (err: Error) => void message.error(err.message),
  });

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    );
  }

  if (!canViewMonitoringSettings) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有查看监控设置的权限。" />
      </div>
    );
  }

  const settings = data?.data;

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="监控设置"
        description="配置 Prometheus 地址、默认查询范围和访问凭证。"
      />
      <SettingsCard>
        <Form
          {...DEFAULT_FORM_LAYOUT}
          onFinish={(values) => {
            if (!canManageMonitoringSettings) return;
            saveMutation.mutate(values as Record<string, string>);
          }}
          initialValues={settings ?? {}}
        >
          <Form.Item name="enabled" label="启用监控" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item
            name="baseUrl"
            label="Prometheus URL"
            rules={[{ required: true, message: "请输入 Prometheus URL" }]}
          >
            <Input placeholder="http://prometheus:9090" />
          </Form.Item>
          <Form.Item name="bearerToken" label="Bearer Token">
            <Input.Password />
          </Form.Item>
          <Form.Item name="defaultRangeMinutes" label="默认范围(分钟)">
            <InputNumber style={fullWidthStyle} />
          </Form.Item>
          <Form.Item name="stepSeconds" label="默认步长(秒)">
            <InputNumber style={fullWidthStyle} />
          </Form.Item>
          <Form.Item name="clusterLabel" label="Cluster Label">
            <Input />
          </Form.Item>
          <Form.Item name="grafanaBaseUrl" label="Grafana URL">
            <Input />
          </Form.Item>
          <div className="soha-form-actions">
            {canManageMonitoringSettings ? (
              <Button
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
    </div>
  );
}

/* ─── AI Settings ─── */

type AIGradientTagTone = "amber" | "blue" | "green" | "slate" | "violet";

function AIGradientTag({
  children,
  tone = "slate",
}: {
  children: ReactNode;
  tone?: AIGradientTagTone;
}) {
  return <Tag className={`soha-ai-gradient-tag is-${tone}`}>{children}</Tag>;
}

function agentRuntimeStateTone(state?: string): AIGradientTagTone {
  switch ((state || "").toLowerCase()) {
    case "connected":
    case "healthy":
    case "idle":
    case "in-process":
    case "observed":
    case "ready":
    case "running":
      return "green";
    case "queued":
    case "waiting":
    case "unknown":
      return "amber";
    case "error":
    case "failed":
    case "unavailable":
      return "violet";
    default:
      return "slate";
  }
}

function agentRuntimeStateTag(state?: string) {
  const normalized = (state || "unknown").toLowerCase();
  return (
    <AIGradientTag tone={agentRuntimeStateTone(normalized)}>
      {AGENT_RUNTIME_STATE_LABELS[normalized] || normalized}
    </AIGradientTag>
  );
}

function renderAgentTagList(
  values?: string[],
  max = 4,
  tone: AIGradientTagTone = "slate",
) {
  const items = (values ?? []).filter(Boolean);
  if (items.length === 0) return "-";
  return (
    <div className="flex flex-wrap gap-1">
      {items.slice(0, max).map((item) => (
        <AIGradientTag key={item} tone={tone}>
          {item}
        </AIGradientTag>
      ))}
      {items.length > max ? (
        <AIGradientTag tone={tone}>{`+${items.length - max}`}</AIGradientTag>
      ) : null}
    </div>
  );
}

function uniqueProviderModelOptions(models: string[], currentModel?: string) {
  const values = new Set<string>();
  [...models, currentModel || ""].forEach((item) => {
    const value = String(item || "").trim();
    if (value) {
      values.add(value);
    }
  });
  return [...values].map((item) => ({ value: item, label: item }));
}

export function AISettingsPage({ embedded = false }: SettingsPageProps = {}) {
  const queryClient = useQueryClient();
  const permissionSnapshotQuery = usePermissionSnapshot();
  const showRuntimeSettings = embedded !== "provider-only";
  const [providerForm] = Form.useForm();
  const [providerModalVisible, setProviderModalVisible] = useState(false);
  const [editingProvider, setEditingProvider] =
    useState<AIProviderConnection | null>(null);
  const [providerModels, setProviderModels] = useState<string[]>([]);
  const [dataSourceModalVisible, setDataSourceModalVisible] = useState(false);
  const [profileModalVisible, setProfileModalVisible] = useState(false);
  const [policyModalVisible, setPolicyModalVisible] = useState(false);
  const [editingDataSource, setEditingDataSource] = useState<DataSource | null>(
    null,
  );
  const [editingProfile, setEditingProfile] = useState<AnalysisProfile | null>(
    null,
  );
  const [editingPolicy, setEditingPolicy] = useState<AutomationPolicy | null>(
    null,
  );
  const [skillsModalVisible, setSkillsModalVisible] = useState(false);
  const [editingSkill, setEditingSkill] = useState<AISkillSetting | null>(null);
  const [skillsRegistryDraft, setSkillsRegistryDraft] = useState<
    AISkillSetting[]
  >([]);
  const [dataSourceSourceKind, setDataSourceSourceKind] = useState("logs");
  const [dataSourceBackendType, setDataSourceBackendType] = useState("es");
  const canViewAISettings = hasPermission(
    permissionSnapshotQuery.data?.data,
    "settings.ai.view",
  );
  const canManageAISettings = hasPermission(
    permissionSnapshotQuery.data?.data,
    "settings.ai.manage",
  );
  const canViewAgentRuns = hasPermission(
    permissionSnapshotQuery.data?.data,
    "observe.ai.view",
  );

  useEffect(() => {
    if (dataSourceModalVisible && editingDataSource) {
      setDataSourceSourceKind(editingDataSource.sourceKind);
      setDataSourceBackendType(editingDataSource.backendType);
      return;
    }
    if (dataSourceModalVisible && !editingDataSource) {
      setDataSourceSourceKind("logs");
      setDataSourceBackendType("es");
    }
  }, [dataSourceModalVisible, editingDataSource]);

  const { data, isLoading } = useQuery({
    queryKey: ["settings-ai"],
    queryFn: () => api.get<ApiResponse<AISettings>>("/settings/ai"),
    select: (response: any) => {
      const current = response.data as AISettings;
      const provider = normalizeAIProviderConnection(
        current.provider as Partial<AIProviderConnection> | undefined,
      );
      const providers =
        Array.isArray(current.providers) && current.providers.length > 0
          ? current.providers.map((item) => normalizeAIProviderConnection(item))
          : [provider];
      return {
        data: {
          provider,
          providers,
          defaultProviderId:
            current.defaultProviderId || providers[0]?.id || "default",
          enabled: provider.enabled,
          baseUrl: provider.baseUrl,
          apiKey: provider.apiKey,
          model: provider.model,
          skillsRegistry: current.skillsRegistry ?? [],
        } satisfies AISettings,
      };
    },
  });
  const dataSourcesQuery = useQuery({
    queryKey: ["copilot-data-sources"],
    queryFn: () => api.get<ApiResponse<DataSource[]>>("/copilot/data-sources"),
  });
  const profilesQuery = useQuery({
    queryKey: ["copilot-analysis-profiles"],
    queryFn: () =>
      api.get<ApiResponse<AnalysisProfile[]>>("/copilot/analysis-profiles"),
  });
  const policiesQuery = useQuery({
    queryKey: ["copilot-automation-policies"],
    queryFn: () =>
      api.get<ApiResponse<AutomationPolicy[]>>("/copilot/automation-policies"),
  });
  const capabilitiesQuery = useQuery({
    queryKey: ["copilot-data-source-capabilities"],
    queryFn: () =>
      api.get<
        ApiResponse<
          Array<{
            id: string;
            name: string;
            sourceKind: string;
            supportedBackends?: string[];
          }>
        >
      >("/copilot/data-source-capabilities"),
  });
  const workbenchCatalogQuery = useQuery({
    queryKey: ["copilot-workbench-catalog"],
    queryFn: () =>
      api.get<ApiResponse<WorkbenchCatalog>>("/copilot/workbench/catalog"),
    enabled: canViewAISettings && showRuntimeSettings,
  });
  const agentRunsQuery = useQuery({
    queryKey: ["copilot-agent-runs"],
    queryFn: () =>
      api.get<ApiResponse<WorkbenchAgentRun[]>>("/copilot/agent-runs"),
    enabled: canViewAISettings && canViewAgentRuns && showRuntimeSettings,
  });

  const saveMutation = useMutation({
    mutationFn: (values: Record<string, unknown>) =>
      api.put("/settings/ai/provider", {
        ...values,
        skillsRegistry: skillsRegistryDraft,
      }),
    onSuccess: () => {
      void message.success("AI 设置已保存");
      void queryClient.invalidateQueries({ queryKey: ["settings-ai"] });
    },
    onError: (err: Error) => void message.error(err.message),
  });
  const dataSourceMutation = useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id?: string;
      values: Record<string, unknown>;
    }) =>
      id
        ? api.put(`/copilot/data-sources/${id}`, buildDataSourcePayload(values))
        : api.post("/copilot/data-sources", buildDataSourcePayload(values)),
    onSuccess: () => {
      void message.success("数据源已保存");
      void queryClient.invalidateQueries({
        queryKey: ["copilot-data-sources"],
      });
      setDataSourceModalVisible(false);
      setEditingDataSource(null);
      setDataSourceBackendType("es");
    },
    onError: (err: Error) => void message.error(err.message),
  });
  const validateDataSourceMutation = useMutation({
    mutationFn: (dataSourceID: string) =>
      api.post<ApiResponse<DataSource>>(
        `/copilot/data-sources/${dataSourceID}/validate`,
      ),
    onSuccess: () => {
      void message.success("数据源校验通过");
    },
    onError: (err: Error) => {
      void message.error(err.message);
    },
    onSettled: () => {
      void queryClient.invalidateQueries({
        queryKey: ["copilot-data-sources"],
      });
    },
  });
  const profileMutation = useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id?: string;
      values: Record<string, unknown>;
    }) =>
      id
        ? api.put(
            `/copilot/analysis-profiles/${id}`,
            buildProfilePayload(values),
          )
        : api.post("/copilot/analysis-profiles", buildProfilePayload(values)),
    onSuccess: () => {
      void message.success("分析模板已保存");
      void queryClient.invalidateQueries({
        queryKey: ["copilot-analysis-profiles"],
      });
      setProfileModalVisible(false);
      setEditingProfile(null);
    },
    onError: (err: Error) => void message.error(err.message),
  });
  const policyMutation = useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id?: string;
      values: Record<string, unknown>;
    }) =>
      id
        ? api.put(
            `/copilot/automation-policies/${id}`,
            buildPolicyPayload(values),
          )
        : api.post("/copilot/automation-policies", buildPolicyPayload(values)),
    onSuccess: () => {
      void message.success("自动化策略已保存");
      void queryClient.invalidateQueries({
        queryKey: ["copilot-automation-policies"],
      });
      setPolicyModalVisible(false);
      setEditingPolicy(null);
    },
    onError: (err: Error) => void message.error(err.message),
  });

  const fetchModelsMutation = useMutation({
    mutationFn: (provider: AIProviderConnection) =>
      api.post<ApiResponse<{ models: string[] }>>(
        "/settings/ai/provider/models",
        { provider },
      ),
    onSuccess: (response) => {
      const models = [
        ...new Set(
          (response.data.models ?? [])
            .map((item) => String(item || "").trim())
            .filter(Boolean),
        ),
      ];
      setProviderModels(models);
      const currentModel = String(
        providerForm.getFieldValue("model") || "",
      ).trim();
      if (models.length > 0 && !models.includes(currentModel)) {
        providerForm.setFieldValue("model", models[0]);
      }
      if (models.length === 0) {
        providerForm.setFieldValue("model", undefined);
      }
      void message.success("已获取模型列表");
    },
    onError: (err: Error) => void message.error(err.message),
  });

  const testProviderMutation = useMutation({
    mutationFn: (payload: { provider: AIProviderConnection; prompt: string }) =>
      api.post<ApiResponse<{ ok: boolean; message?: string; reply?: string }>>(
        "/settings/ai/provider/test",
        payload,
      ),
    onSuccess: (response) => {
      void message.success(
        response.data.reply
          ? `测试成功: ${response.data.reply}`
          : "联通性测试成功",
      );
    },
    onError: (err: Error) => void message.error(err.message),
  });

  const settings = data?.data;
  const providerModelOptions = uniqueProviderModelOptions(
    providerModels,
    editingProvider?.model,
  );
  const agentProviders = workbenchCatalogQuery.data?.data?.agentProviders ?? [];
  const agentCapabilities = workbenchCatalogQuery.data?.data?.capabilities ?? [];
  const agentRuns = agentRunsQuery.data?.data ?? [];
  const agentProviderRows = useMemo<AgentProviderRuntimeRow[]>(
    () =>
      agentProviders.map((provider) => {
        const runtimeSummary = summarizeAgentProviderRuntime(
          provider,
          agentRuns,
        );
        return {
          ...provider,
          runtimeSummary,
          runtimeState: resolveAgentRuntimeState(provider, runtimeSummary),
        };
      }),
    [agentProviders, agentRuns],
  );

  useEffect(() => {
    setSkillsRegistryDraft(
      (settings?.skillsRegistry ?? []).map((item) => ({
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
    );
  }, [settings?.skillsRegistry]);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Spin size="large" />
      </div>
    );
  }

  if (!canViewAISettings) {
    return (
      <div className="soha-page">
        <ManagementState kind="no-permission" description="当前账号没有查看 AI 设置的权限。" />
      </div>
    );
  }

  const dataSources = dataSourcesQuery.data?.data ?? [];
  const profiles = profilesQuery.data?.data ?? [];
  const policies = policiesQuery.data?.data ?? [];
  const capabilityOptions = capabilitiesQuery.data?.data ?? [];
  const filteredCapabilityOptions = capabilityOptions.filter(
    (item) => item.sourceKind === dataSourceSourceKind,
  );
  const backendOptions =
    dataSourceSourceKind === "logs"
      ? [
          { value: "es", label: "es" },
          { value: "loki", label: "loki" },
          { value: "clickhouse", label: "clickhouse" },
        ]
      : dataSourceSourceKind === "metrics"
        ? [{ value: "prometheus", label: "prometheus" }]
        : dataSourceSourceKind === "traces"
          ? TRACES_BACKEND_OPTIONS
          : [{ value: "platform", label: "platform" }];

  const dataSourceColumns: TableColumnsType<DataSource> = [
    { title: "名称", dataIndex: "name" },
    { title: "能力层", dataIndex: "mcpAdapter" },
    {
      title: "源类型",
      dataIndex: "sourceKind",
      render: (value: string, record: DataSource) =>
        `${value} / ${record.backendType}`,
    },
    {
      title: "校验状态",
      dataIndex: "validationStatus",
      render: (value: string | undefined, record: DataSource) => {
        const isPending =
          validateDataSourceMutation.isPending &&
          validateDataSourceMutation.variables === record.id;
        if (isPending) return <Tag color="orange">校验中</Tag>;
        if (!value) return <Tag color="default">未校验</Tag>;
        const normalized = value.toLowerCase();
        const color =
          normalized === "success"
            ? "green"
            : normalized === "error"
              ? "red"
              : "default";
        const label =
          normalized === "success"
            ? "已通过"
            : normalized === "error"
              ? "失败"
              : value;
        return (
          <div className="flex max-w-[240px] flex-col gap-1">
            <Tag color={color}>{label}</Tag>
            {record.validationMessage && normalized === "error" ? (
              <div className="text-xs text-[var(--ant-colorTextSecondary)]">
                {record.validationMessage}
              </div>
            ) : null}
          </div>
        );
      },
    },
    {
      title: "最近校验",
      dataIndex: "lastValidatedAt",
      render: (value: string | undefined) =>
        value ? formatDateTime(value) : "-",
    },
    {
      title: "启用",
      dataIndex: "enabled",
      render: (value: boolean) => (
        <StatusTag value={value ? "success" : "default"} />
      ),
    },
    {
      ...tableColumnPresets.action,
      title: "操作",
      dataIndex: "id",
      render: (_: unknown, record: DataSource) =>
        canManageAISettings ? (
          <Space className="soha-row-action-icons">
            <ManagementIconButton
              aria-label="校验数据源连接"
              tooltip="校验连接"
              icon={<CheckCircleOutlined />}
              loading={
                validateDataSourceMutation.isPending &&
                validateDataSourceMutation.variables === record.id
              }
              size="small"
              onClick={() => validateDataSourceMutation.mutate(record.id)}
            />
            <ManagementIconButton
              aria-label="编辑数据源"
              tooltip="编辑"
              icon={<EditOutlined />}
              size="small"
              onClick={() => {
                setEditingDataSource(record);
                setDataSourceSourceKind(record.sourceKind);
                setDataSourceBackendType(record.backendType);
                setDataSourceModalVisible(true);
              }}
            />
          </Space>
        ) : (
          "-"
        ),
    },
  ];

  const profileColumns: TableColumnsType<AnalysisProfile> = [
    { title: "名称", dataIndex: "name" },
    { title: "模式", dataIndex: "mode" },
    {
      title: "数据源",
      dataIndex: "enabledSources",
      render: (value: string[]) => (
        <div className="flex flex-wrap gap-1">
          {(value ?? []).map((item) => (
            <Tag key={item}>{item}</Tag>
          ))}
        </div>
      ),
    },
    {
      title: "Playbooks",
      dataIndex: "enabledPlaybooks",
      render: (value: string[]) => (
        <div className="flex flex-wrap gap-1">
          {(value ?? []).map((item) => (
            <Tag key={item}>{item}</Tag>
          ))}
        </div>
      ),
    },
    { title: "策略", dataIndex: "remediationPolicy" },
    {
      ...tableColumnPresets.action,
      title: "操作",
      dataIndex: "id",
      render: (_: unknown, record: AnalysisProfile) =>
        canManageAISettings ? (
          <ManagementIconButton
            aria-label="编辑分析模板"
            tooltip="编辑"
            icon={<EditOutlined />}
            size="small"
            onClick={() => {
              setEditingProfile(record);
              setProfileModalVisible(true);
            }}
          />
        ) : (
          "-"
        ),
    },
  ];

  const policyColumns: TableColumnsType<AutomationPolicy> = [
    { title: "名称", dataIndex: "name" },
    { title: "触发类型", dataIndex: "triggerType" },
    { title: "分析模板", dataIndex: "analysisProfileId" },
    { title: "Dedup(s)", dataIndex: "dedupWindowSeconds" },
    { title: "策略", dataIndex: "remediationPolicy" },
    {
      title: "启用",
      dataIndex: "enabled",
      render: (value: boolean) => (
        <StatusTag value={value ? "success" : "default"} />
      ),
    },
    {
      ...tableColumnPresets.action,
      title: "操作",
      dataIndex: "id",
      render: (_: unknown, record: AutomationPolicy) =>
        canManageAISettings ? (
          <ManagementIconButton
            aria-label="编辑自动化策略"
            tooltip="编辑"
            icon={<EditOutlined />}
            size="small"
            onClick={() => {
              setEditingPolicy(record);
              setPolicyModalVisible(true);
            }}
          />
        ) : (
          "-"
        ),
    },
  ];

  const providerColumns: TableColumnsType<AIProviderConnection> = [
    { title: "名称", dataIndex: "name" },
    {
      title: "Provider",
      dataIndex: "providerKind",
      render: (value: string) => (
        <AIGradientTag tone="slate">{value || "-"}</AIGradientTag>
      ),
    },
    { title: "模型", dataIndex: "model" },
    {
      title: "Base URL",
      dataIndex: "baseUrl",
      render: (value: string) => value || "-",
    },
    {
      title: "启用",
      dataIndex: "enabled",
      render: (value: boolean) => (
        <StatusTag value={value ? "enabled" : "disabled"} />
      ),
    },
    {
      title: "默认",
      dataIndex: "id",
      render: (value: string) =>
        settings?.defaultProviderId === value ? (
          <AIGradientTag tone="blue">默认</AIGradientTag>
        ) : (
          "-"
        ),
    },
    {
      ...tableColumnPresets.action,
      title: "操作",
      dataIndex: "id",
      render: (_: unknown, record: AIProviderConnection) =>
        canManageAISettings ? (
          <Space className="soha-row-action-icons">
            <ManagementIconButton
              data-testid={`ai-provider-edit-${record.id}`}
              aria-label="编辑 Provider 连接"
              tooltip="编辑"
              icon={<EditOutlined />}
              size="small"
              onClick={() => {
                setEditingProvider(record);
                setProviderModels([]);
                setProviderModalVisible(true);
              }}
            />
            <Popconfirm
              title="确认删除 Provider 连接？"
              description="删除后会立即保存 AI 设置。"
              okButtonProps={{ danger: true, loading: saveMutation.isPending }}
              onConfirm={() => {
                saveMutation.mutate({
                  providers: (settings?.providers ?? []).filter(
                    (item) => item.id !== record.id,
                  ),
                  defaultProviderId:
                    settings?.defaultProviderId === record.id
                      ? (settings?.providers ?? []).find(
                          (item) => item.id !== record.id,
                        )?.id || ""
                      : settings?.defaultProviderId,
                });
              }}
            >
              <ManagementIconButton
                data-testid={`ai-provider-delete-${record.id}`}
                aria-label="删除 Provider 连接"
                tooltip="删除"
                danger
                icon={<DeleteOutlined />}
                loading={saveMutation.isPending}
                size="small"
              />
            </Popconfirm>
            {settings?.defaultProviderId !== record.id ? (
              <ManagementIconButton
                data-testid={`ai-provider-default-${record.id}`}
                aria-label="设为默认 Provider"
                tooltip="设为默认"
                icon={<StarOutlined />}
                size="small"
                onClick={() => {
                  saveMutation.mutate({
                    providers: settings?.providers ?? [],
                    defaultProviderId: record.id,
                  });
                }}
              />
            ) : null}
          </Space>
        ) : (
          "-"
        ),
    },
  ];

  const agentProviderColumns: TableColumnsType<AgentProviderRuntimeRow> = [
    {
      title: "Provider",
      dataIndex: "name",
      width: 220,
      render: (_: unknown, record) => (
        <div className="flex flex-col gap-1">
          <span className="font-medium">{record.name}</span>
          <span className="text-xs text-[var(--ant-colorTextSecondary)]">
            {record.id} / {record.kind}
          </span>
        </div>
      ),
    },
    {
      title: "启用",
      dataIndex: "enabled",
      width: 92,
      render: (value: boolean) => (
        <StatusTag value={value ? "enabled" : "disabled"} />
      ),
    },
    {
      title: "Runtime",
      dataIndex: "runtimeState",
      width: 120,
      render: (value: string, record) => (
        <div className="flex flex-col gap-1">
          <span>{agentRuntimeStateTag(value)}</span>
          {record.runtimeStatus?.reason ? (
            <span className="text-xs text-[var(--ant-colorTextSecondary)]">
              {record.runtimeStatus.reason}
            </span>
          ) : null}
        </div>
      ),
    },
    {
      title: "队列 / 运行 / 失败",
      dataIndex: "runtimeSummary",
      width: 150,
      render: (summary: AgentRuntimeSummary) =>
        `${summary.queuedRuns} / ${summary.runningRuns} / ${summary.recentFailures}`,
    },
    {
      title: "Runner",
      dataIndex: "runtimeSummary",
      width: 160,
      render: (summary: AgentRuntimeSummary) =>
        summary.lastAgentId ? (
          <AIGradientTag tone="green">{summary.lastAgentId}</AIGradientTag>
        ) : (
          "-"
        ),
    },
    {
      title: "能力",
      dataIndex: "capabilities",
      render: (value: string[]) =>
        renderAgentTagList(
          agentCapabilityLabels(value, agentCapabilities),
          5,
          "blue",
        ),
    },
    {
      title: "边界",
      dataIndex: "id",
      width: 190,
      render: (_: unknown, record) => (
        <div className="flex flex-wrap gap-1">
          <AIGradientTag tone={record.supportsAsync ? "violet" : "slate"}>
            {record.supportsAsync ? "async" : "sync"}
          </AIGradientTag>
          {record.supportsSkills ? (
            <AIGradientTag tone="blue">skills</AIGradientTag>
          ) : null}
          {record.supportsToolsets ? (
            <AIGradientTag tone="green">toolsets</AIGradientTag>
          ) : null}
        </div>
      ),
    },
    {
      title: "最近活动",
      dataIndex: "runtimeSummary",
      width: 210,
      render: (summary: AgentRuntimeSummary) => {
        const activity =
          summary.lastHeartbeatAt ||
          summary.lastCompletedAt ||
          summary.lastRun?.updatedAt ||
          summary.lastRun?.createdAt;
        return activity ? formatDateTime(activity) : "-";
      },
    },
  ];

  const agentRunColumns: TableColumnsType<WorkbenchAgentRun> = [
    {
      title: "Run",
      dataIndex: "id",
      width: 190,
      render: (value: string, record) => (
        <div className="flex flex-col gap-1">
          <span className="font-medium">{value}</span>
          {record.sessionId ? (
            <span className="text-xs text-[var(--ant-colorTextSecondary)]">
              session: {record.sessionId}
            </span>
          ) : null}
        </div>
      ),
    },
    {
      title: "Provider",
      dataIndex: "providerId",
      width: 130,
      render: (value: string, record) => value || record.providerKind || "-",
    },
    { title: "能力", dataIndex: "capabilityId", width: 150 },
    {
      title: "状态",
      dataIndex: "status",
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: "Runner",
      dataIndex: "claimedByAgentId",
      width: 170,
      render: (value?: string) =>
        value ? <AIGradientTag tone="green">{value}</AIGradientTag> : "-",
    },
    {
      title: "心跳",
      dataIndex: "lastHeartbeatAt",
      width: 180,
      render: (value?: string) => formatDateTime(value),
    },
    {
      title: "完成",
      dataIndex: "completedAt",
      width: 180,
      render: (value?: string) => formatDateTime(value),
    },
    {
      title: "错误",
      dataIndex: "errorMessage",
      render: (value?: string) => value || "-",
    },
  ];

  const providerCard = (
    <section data-testid="ai-provider-connections-section">
      <SettingsCard
        title={
          <Space size={8} wrap>
            <span>Provider Connections</span>
            <AIGradientTag tone="blue">LLM</AIGradientTag>
            <AIGradientTag tone="slate">Model API</AIGradientTag>
          </Space>
        }
      >
        <div
          data-testid="ai-provider-connections-actions"
          className="soha-management-table-toolbar-actions"
          style={{ justifyContent: "flex-end", marginBottom: 12 }}
        >
          {canManageAISettings ? (
            <Button
              data-testid="ai-provider-add"
              type="primary"
              onClick={() => {
                setEditingProvider(null);
                setProviderModels([]);
                setProviderModalVisible(true);
              }}
            >
              新增连接
            </Button>
          ) : null}
        </div>
        <div data-testid="ai-provider-table-shell">
          <AdminTable
            data-testid="ai-provider-table"
            shellClassName="soha-management-table-shell"
            columnSettingIconOnly
            columnSettingPlacement="header"
            rowKey="id"
            tableSize="small"
            pagination={false}
            dataSource={settings?.providers ?? []}
            columns={providerColumns}
            empty={<ManagementState bordered={false} compact title="暂无 Provider 连接" description="新增 Provider 后，AI 工作台可选择对应模型执行分析。" />}
          />
        </div>
      </SettingsCard>
    </section>
  );

  const agentRuntimeCard = showRuntimeSettings ? (
    <section data-testid="ai-agent-runtime-section">
      <SettingsCard
        title={
          <Space size={8} wrap>
            <span>Agent Runtime Providers</span>
            <AIGradientTag tone="violet">Catalog</AIGradientTag>
            <AIGradientTag tone="green">Runner</AIGradientTag>
          </Space>
        }
        extra={
          <Button
            icon={<ReloadOutlined />}
            loading={
              workbenchCatalogQuery.isFetching || agentRunsQuery.isFetching
            }
            size="small"
            onClick={() => {
              void queryClient.invalidateQueries({
                queryKey: ["copilot-workbench-catalog"],
              });
              void queryClient.invalidateQueries({
                queryKey: ["copilot-agent-runs"],
              });
            }}
          >
            刷新
          </Button>
        }
      >
        <div className="flex flex-col gap-3">
          <Alert
            showIcon
            type="info"
            title="Agent Runtime 连接由后端 catalog 与 soha-agent runner 共同决定。"
            description="Hermes 等外部 Agent 会领取 AgentRun，再通过 callback 写回心跳、Runner ID、工具调用和分析产物。这里展示可选择的 Agent Provider 与最近运行证据。"
          />
          <AdminTable
            data-testid="ai-agent-provider-table"
            shellClassName="soha-management-table-shell"
            columnSettingIconOnly
            columnSettingPlacement="header"
            rowKey="id"
            tableSize="small"
            pagination={false}
            loading={workbenchCatalogQuery.isLoading}
            dataSource={agentProviderRows}
            columns={agentProviderColumns}
            empty={
              <ManagementState
                bordered={false}
                compact
                title="暂无 Agent Runtime Provider"
                description="后端 catalog 暂未返回可用于 AI Workbench 的 Agent Provider。"
              />
            }
          />
          <div className="soha-ai-settings-subhead">
            <span>Recent AgentRuns</span>
            <AIGradientTag tone="green">claim / callback</AIGradientTag>
          </div>
          {canViewAgentRuns ? (
            <AdminTable
              data-testid="ai-agent-run-table"
              shellClassName="soha-management-table-shell"
              columnSettingIconOnly
              columnSettingPlacement="header"
              rowKey="id"
              tableSize="small"
              pageSize={5}
              loading={agentRunsQuery.isLoading}
              dataSource={agentRuns}
              columns={agentRunColumns}
              empty={
                <ManagementState
                  bordered={false}
                  compact
                  title="暂无 AgentRun"
                  description="选择 Hermes 等 Agent Provider 发起显式分析后，Runner claim/callback 记录会出现在这里。"
                />
              }
            />
          ) : (
            <ManagementState
              bordered={false}
              compact
              kind="no-permission"
              title="无法查看 AgentRun 历史"
              description="当前账号缺少 observe.ai.view 权限，只能查看 Agent Provider catalog。"
            />
          )}
        </div>
      </SettingsCard>
    </section>
  ) : null;

  const providerModal = (
    <Modal
      title={editingProvider ? "编辑 Provider 连接" : "新增 Provider 连接"}
      open={providerModalVisible}
      className="soha-ai-provider-modal"
      footer={null}
      onCancel={() => {
        setProviderModalVisible(false);
        setEditingProvider(null);
        setProviderModels([]);
      }}
      destroyOnHidden
      modalRender={(node) => <div data-testid="ai-provider-modal">{node}</div>}
    >
      <Form
        data-testid="ai-provider-form"
        form={providerForm}
        {...DEFAULT_FORM_LAYOUT}
        initialValues={{
          id: editingProvider?.id ?? "",
          name: editingProvider?.name ?? "",
          providerKind: editingProvider?.providerKind ?? "openai-compatible",
          enabled: editingProvider?.enabled ?? true,
          baseUrl: editingProvider?.baseUrl ?? "",
          apiKey: editingProvider?.apiKey ?? "",
          model: editingProvider?.model ?? "",
        }}
        onFinish={(values) => {
          if (!canManageAISettings) return;
          const nextProvider: AIProviderConnection = {
            id: String(values.id || editingProvider?.id || crypto.randomUUID()),
            name: String(values.name || "").trim(),
            providerKind: String(values.providerKind || "openai-compatible"),
            enabled: Boolean(values.enabled),
            baseUrl: String(values.baseUrl || "").trim(),
            apiKey: String(values.apiKey || "").trim(),
            model: String(values.model || "").trim(),
          };
          const providers = [...(settings?.providers ?? [])];
          const index = providers.findIndex(
            (item) => item.id === nextProvider.id,
          );
          if (index >= 0) {
            providers[index] = nextProvider;
          } else {
            providers.push(nextProvider);
          }
          saveMutation.mutate({
            providers,
            defaultProviderId: settings?.defaultProviderId || nextProvider.id,
          });
          setProviderModalVisible(false);
          setEditingProvider(null);
          setProviderModels([]);
        }}
      >
        <Form.Item name="id" hidden>
          <Input />
        </Form.Item>
        <Form.Item
          name="name"
          label="连接名称"
          rules={[{ required: true, message: "请输入连接名称" }]}
        >
          <Input
            data-testid="ai-provider-name"
            placeholder="OpenAI 主账号 / Claude Team A"
          />
        </Form.Item>
        <Form.Item
          name="providerKind"
          label="Provider"
          rules={[{ required: true, message: "请选择 Provider" }]}
        >
          <Select
            data-testid="ai-provider-kind"
            options={[
              { value: "openai-compatible", label: "OpenAI Compatible" },
              { value: "openai", label: "OpenAI" },
              { value: "anthropic", label: "Anthropic" },
              { value: "gemini", label: "Gemini" },
              { value: "grok", label: "Grok" },
              { value: "deepseek", label: "DeepSeek" },
              { value: "qwen", label: "Qwen" },
              { value: "glm", label: "GLM" },
              { value: "kimi", label: "Kimi" },
              { value: "minimax", label: "MiniMax" },
            ]}
          />
        </Form.Item>
        <Form.Item
          name="baseUrl"
          label="Base URL"
          rules={[{ required: true, message: "请输入 Base URL" }]}
        >
          <Input
            data-testid="ai-provider-base-url"
            placeholder="https://api.openai.com/v1 / https://api.anthropic.com/v1 / https://generativelanguage.googleapis.com/v1beta"
          />
        </Form.Item>
        <Form.Item
          name="apiKey"
          label="API Key"
          rules={[{ required: true, message: "请输入 API Key" }]}
        >
          <Input.Password data-testid="ai-provider-api-key" />
        </Form.Item>
        <Form.Item
          name="model"
          label="模型"
          rules={[{ required: true, message: "请选择或输入模型" }]}
        >
          <AutoComplete
            data-testid="ai-provider-model"
            options={providerModelOptions}
            filterOption={(inputValue, option) =>
              String(option?.value ?? "")
                .toLowerCase()
                .includes(inputValue.toLowerCase())
            }
            placeholder={
              providerModelOptions.length > 0
                ? "选择模型，或手动输入"
                : "可手动输入模型，或先获取模型列表"
            }
          />
        </Form.Item>
        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch data-testid="ai-provider-enabled" />
        </Form.Item>
        <div data-testid="ai-provider-actions" className="soha-form-actions">
          <Button
            data-testid="ai-provider-fetch-models"
            onClick={async () => {
              const values = await providerForm.validateFields([
                "providerKind",
                "baseUrl",
                "apiKey",
              ]);
              const currentValues = providerForm.getFieldsValue();
              fetchModelsMutation.mutate({
                id: String(currentValues.id || editingProvider?.id || ""),
                name: String(currentValues.name || ""),
                providerKind: String(
                  values.providerKind || "openai-compatible",
                ),
                enabled: Boolean(currentValues.enabled),
                baseUrl: String(values.baseUrl || ""),
                apiKey: String(values.apiKey || ""),
                model: String(currentValues.model || ""),
              });
            }}
            loading={fetchModelsMutation.isPending}
          >
            获取模型列表
          </Button>
          <Button
            data-testid="ai-provider-test"
            onClick={async () => {
              const values = await providerForm.validateFields([
                "providerKind",
                "baseUrl",
                "apiKey",
                "model",
                "name",
                "enabled",
              ]);
              const currentValues = providerForm.getFieldsValue();
              testProviderMutation.mutate({
                provider: {
                  id: String(currentValues.id || editingProvider?.id || ""),
                  name: String(values.name || currentValues.name || ""),
                  providerKind: String(
                    values.providerKind || "openai-compatible",
                  ),
                  enabled: Boolean(values.enabled ?? currentValues.enabled),
                  baseUrl: String(values.baseUrl || ""),
                  apiKey: String(values.apiKey || ""),
                  model: String(values.model || ""),
                },
                prompt: "hello",
              });
            }}
            loading={testProviderMutation.isPending}
          >
            联通性测试
          </Button>
          <Button
            data-testid="ai-provider-cancel"
            onClick={() => {
              setProviderModalVisible(false);
              setEditingProvider(null);
              setProviderModels([]);
            }}
          >
            取消
          </Button>
          {canManageAISettings ? (
            <Button
              data-testid="ai-provider-save"
              htmlType="submit"
              type="primary"
              loading={saveMutation.isPending}
            >
              保存
            </Button>
          ) : null}
        </div>
      </Form>
    </Modal>
  );

  if (embedded === "provider-only") {
    return (
      <>
        {providerCard}
        {providerModal}
      </>
    );
  }

  const content = (
    <>
      {providerCard}
      {agentRuntimeCard}
      <SettingsCard
        title="Skills Registry"
        extra={
          canManageAISettings ? (
            <Button
              type="primary"
              onClick={() => {
                setEditingSkill(null);
                setSkillsModalVisible(true);
              }}
            >
              新增
            </Button>
          ) : null
        }
      >
        <div className="mb-3 text-sm text-[var(--ant-colorTextSecondary)]">
          全局 skills 由 `settings.ai.manage`
          控制；它们决定工作台可装配的默认能力集合，但不会自动绕过会话级选择与预算限制。
        </div>
        <AdminTable
          shellClassName="soha-management-table-shell"
          rowKey="id"
          dataSource={skillsRegistryDraft}
          empty={
            <ManagementState bordered={false} compact title="暂无全局 Skills" description="可先新增 MCP、logs、metrics、traces 这类技能条目。" />
          }
          columns={[
            { title: "ID", dataIndex: "id" },
            { title: "名称", dataIndex: "name" },
            {
              title: "分类",
              dataIndex: "category",
              render: (value?: string) => value || "-",
            },
            {
              title: "归属模块",
              dataIndex: "ownerModule",
              render: (value?: string) => value || "-",
            },
            {
              title: "说明",
              dataIndex: "description",
              render: (value?: string) => value || "-",
            },
            {
              title: "作用域",
              dataIndex: "scopes",
              render: (value?: string[]) => (
                <div className="flex flex-wrap gap-1">
                  {(value ?? []).map((item) => (
                    <Tag key={item}>{item}</Tag>
                  ))}
                </div>
              ),
            },
            {
              title: "能力引用",
              dataIndex: "capabilityRefs",
              render: (value?: string[]) => (
                <div className="flex flex-wrap gap-1">
                  {(value ?? []).slice(0, 3).map((item) => (
                    <Tag key={item}>{item}</Tag>
                  ))}
                </div>
              ),
            },
            {
              title: "启用",
              dataIndex: "enabled",
              render: (value: boolean) => (
                <StatusTag value={value ? "enabled" : "disabled"} />
              ),
            },
            {
              title: "排序",
              dataIndex: "id",
              render: (_: unknown, record: AISkillSetting) =>
                canManageAISettings ? (
                  <Space className="soha-row-action-icons">
                    <ManagementIconButton
                      aria-label="上移 Skill"
                      tooltip="上移"
                      icon={<ArrowUpOutlined />}
                      size="small"
                      disabled={skillsRegistryDraft[0]?.id === record.id}
                      onClick={() => {
                        setSkillsRegistryDraft((current) => {
                          const index = current.findIndex(
                            (item) => item.id === record.id,
                          );
                          if (index <= 0) return current;
                          const next = [...current];
                          [next[index - 1], next[index]] = [
                            next[index],
                            next[index - 1],
                          ];
                          return next;
                        });
                      }}
                    />
                    <ManagementIconButton
                      aria-label="下移 Skill"
                      tooltip="下移"
                      icon={<ArrowDownOutlined />}
                      size="small"
                      disabled={
                        skillsRegistryDraft[skillsRegistryDraft.length - 1]
                          ?.id === record.id
                      }
                      onClick={() => {
                        setSkillsRegistryDraft((current) => {
                          const index = current.findIndex(
                            (item) => item.id === record.id,
                          );
                          if (index < 0 || index >= current.length - 1)
                            return current;
                          const next = [...current];
                          [next[index], next[index + 1]] = [
                            next[index + 1],
                            next[index],
                          ];
                          return next;
                        });
                      }}
                    />
                  </Space>
                ) : (
                  "-"
                ),
            },
            {
              ...tableColumnPresets.action,
              title: "操作",
              dataIndex: "id",
              render: (_: unknown, record: AISkillSetting) =>
                canManageAISettings ? (
                  <Space className="soha-row-action-icons">
                    <ManagementIconButton
                      aria-label="编辑 Skill"
                      tooltip="编辑"
                      icon={<EditOutlined />}
                      size="small"
                      onClick={() => {
                        setEditingSkill(record);
                        setSkillsModalVisible(true);
                      }}
                    />
                    <Popconfirm
                      title="确认删除 Skill？"
                      description="删除后会从当前草稿列表移除，保存设置后生效。"
                      okButtonProps={{ danger: true }}
                      onConfirm={() =>
                        setSkillsRegistryDraft((current) =>
                          current.filter((item) => item.id !== record.id),
                        )
                      }
                    >
                      <ManagementIconButton
                        aria-label="删除 Skill"
                        tooltip="删除"
                        danger
                        icon={<DeleteOutlined />}
                        size="small"
                      />
                    </Popconfirm>
                  </Space>
                ) : (
                  "-"
                ),
            },
          ]}
        />
      </SettingsCard>
      <SettingsCard
        title="Data Sources"
        extra={
          canManageAISettings ? (
            <Button
              type="primary"
              onClick={() => {
                setEditingDataSource(null);
                setDataSourceSourceKind("logs");
                setDataSourceBackendType("es");
                setDataSourceModalVisible(true);
              }}
            >
              新增
            </Button>
          ) : null
        }
      >
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={dataSourceColumns}
          dataSource={dataSources}
          rowKey="id"
          loading={dataSourcesQuery.isLoading}
        />
      </SettingsCard>
      <SettingsCard
        title="Analysis Profiles"
        extra={
          canManageAISettings ? (
            <Button
              type="primary"
              onClick={() => {
                setEditingProfile(null);
                setProfileModalVisible(true);
              }}
            >
              新增
            </Button>
          ) : null
        }
      >
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={profileColumns}
          dataSource={profiles}
          rowKey="id"
          loading={profilesQuery.isLoading}
        />
      </SettingsCard>
      <SettingsCard
        title="Automation Policies"
        extra={
          canManageAISettings ? (
            <Button
              type="primary"
              onClick={() => {
                setEditingPolicy(null);
                setPolicyModalVisible(true);
              }}
            >
              新增
            </Button>
          ) : null
        }
      >
        <AdminTable
          shellClassName="soha-management-table-shell"
          columns={policyColumns}
          dataSource={policies}
          rowKey="id"
          loading={policiesQuery.isLoading}
        />
      </SettingsCard>

      {providerModal}

      <Modal
        title={editingSkill ? "编辑 Skill" : "新增 Skill"}
        open={skillsModalVisible}
        footer={null}
        onCancel={() => {
          setSkillsModalVisible(false);
          setEditingSkill(null);
        }}
        destroyOnHidden
      >
        <Form
          {...DEFAULT_FORM_LAYOUT}
          initialValues={{
            id: editingSkill?.id ?? "",
            name: editingSkill?.name ?? "",
            category: editingSkill?.category ?? "",
            ownerModule: editingSkill?.ownerModule ?? "",
            description: editingSkill?.description ?? "",
            enabled: editingSkill?.enabled ?? true,
            scopes: editingSkill?.scopes ?? [],
            capabilityRefs: editingSkill?.capabilityRefs ?? [],
            blueprintRefs: editingSkill?.blueprintRefs ?? [],
            scopeRules: editingSkill?.scopeRules ?? [],
            inputSchemaText: JSON.stringify(
              editingSkill?.inputSchema ?? {},
              null,
              2,
            ),
            outputSchemaText: JSON.stringify(
              editingSkill?.outputSchema ?? {},
              null,
              2,
            ),
          }}
          onFinish={(values) => {
            let inputSchema: Record<string, unknown> = {};
            let outputSchema: Record<string, unknown> = {};
            try {
              inputSchema = values.inputSchemaText
                ? JSON.parse(String(values.inputSchemaText))
                : {};
              outputSchema = values.outputSchemaText
                ? JSON.parse(String(values.outputSchemaText))
                : {};
            } catch {
              void message.error("Input/Output Schema 需要是合法 JSON");
              return;
            }
            const next: AISkillSetting = {
              id: String(values.id ?? "").trim(),
              name: String(values.name ?? "").trim(),
              category: String(values.category ?? "").trim(),
              ownerModule: String(values.ownerModule ?? "").trim(),
              description: String(values.description ?? "").trim(),
              enabled: Boolean(values.enabled),
              scopes: Array.isArray(values.scopes)
                ? (values.scopes as string[])
                : [],
              capabilityRefs: Array.isArray(values.capabilityRefs)
                ? (values.capabilityRefs as string[])
                : [],
              blueprintRefs: Array.isArray(values.blueprintRefs)
                ? (values.blueprintRefs as string[])
                : [],
              scopeRules: Array.isArray(values.scopeRules)
                ? (values.scopeRules as string[])
                : [],
              inputSchema,
              outputSchema,
            };
            if (!next.id || !next.name) {
              void message.error("Skill ID 和名称不能为空");
              return;
            }
            const duplicate = skillsRegistryDraft.find(
              (item) => item.id === next.id && item.id !== editingSkill?.id,
            );
            if (duplicate) {
              void message.error(`Skill ID 已存在: ${next.id}`);
              return;
            }
            setSkillsRegistryDraft((current) => {
              const rest = current.filter((item) => item.id !== next.id);
              return [...rest, next];
            });
            setSkillsModalVisible(false);
            setEditingSkill(null);
          }}
        >
          <Form.Item
            name="id"
            label="ID"
            rules={[{ required: true, message: "请输入 ID" }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: "请输入名称" }]}
          >
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
            <Switch />
          </Form.Item>
          <div className="text-sm text-[var(--ant-colorTextSecondary)]">
            ID 需要在全局 registry 中唯一；作用域用于提示这个 skill
            主要服务于哪些工作区或资源，不直接替代权限判断。
          </div>
          <div className="soha-form-actions">
            <Button
              onClick={() => {
                setSkillsModalVisible(false);
                setEditingSkill(null);
              }}
            >
              取消
            </Button>
            <Button htmlType="submit" type="primary">
              保存
            </Button>
          </div>
        </Form>
      </Modal>

      <Modal
        title={editingDataSource ? "编辑数据源" : "新增数据源"}
        open={dataSourceModalVisible}
        footer={null}
        onCancel={() => {
          setDataSourceModalVisible(false);
          setEditingDataSource(null);
          setDataSourceSourceKind("logs");
          setDataSourceBackendType("es");
        }}
        destroyOnHidden
      >
        <Form
          {...DEFAULT_FORM_LAYOUT}
          initialValues={buildDataSourceFormValues(editingDataSource)}
          onFinish={(values) => {
            if (!canManageAISettings) return;
            dataSourceMutation.mutate({
              id: editingDataSource?.id,
              values: values as Record<string, unknown>,
            });
          }}
        >
          <Form.Item name="id" hidden>
            <Input />
          </Form.Item>
          <SectionCallout
            title="1. 基础信息"
            description="先选择数据源的能力类别和后端类型，再填写连接与查询约束。"
          />
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: "请输入名称" }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="sourceKind" label="源类型">
            <Select
              options={[
                { value: "logs", label: "logs" },
                { value: "metrics", label: "metrics" },
                { value: "traces", label: "traces" },
                { value: "platform-native", label: "platform-native" },
              ]}
              onChange={(value) => {
                const next = String(value);
                setDataSourceSourceKind(next);
                setDataSourceBackendType(
                  next === "logs"
                    ? "es"
                    : next === "metrics"
                      ? "prometheus"
                      : next === "traces"
                        ? "jaeger"
                        : "platform",
                );
              }}
            />
          </Form.Item>
          <Form.Item name="backendType" label="后端类型">
            <Select
              options={backendOptions}
              onChange={(value) => setDataSourceBackendType(String(value))}
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
            <Switch />
          </Form.Item>
          <SectionCallout
            title="3. 后端连接"
            description="这里只展示当前后端类型需要的关键字段，避免无关配置干扰。"
          />
          {dataSourceBackendType === "skywalking" ? (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 16 }}
              title="SkyWalking 作为 trace 查询后端"
              description="OpenTelemetry 是采集/导出标准，不是直接查询 backend。这里的 traces backend 请选择 Jaeger 或 SkyWalking，并填它们各自的查询入口。"
            />
          ) : null}
          <Form.Item
            name="configEndpoint"
            label="Endpoint"
            rules={[{ required: true, message: "请输入 Endpoint" }]}
          >
            <Input />
          </Form.Item>
          {dataSourceBackendType === "es" ? (
            <Form.Item
              name="configIndex"
              label="ES Index"
              rules={[{ required: true, message: "请输入 ES Index" }]}
            >
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceBackendType === "clickhouse" ? (
            <Form.Item
              name="configTable"
              label="CK Table"
              rules={[{ required: true, message: "请输入 CK Table" }]}
            >
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceBackendType === "clickhouse" ? (
            <Form.Item name="configUsername" label="Username">
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceBackendType === "clickhouse" ? (
            <Form.Item name="configPassword" label="Password">
              <Input.Password />
            </Form.Item>
          ) : null}
          {dataSourceBackendType !== "clickhouse" &&
          dataSourceBackendType !== "platform" ? (
            <Form.Item name="configBearerToken" label="Bearer Token">
              <Input.Password />
            </Form.Item>
          ) : null}
          {dataSourceSourceKind === "logs" ? (
            <Form.Item name="configTimestampField" label="Timestamp Field">
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceSourceKind === "logs" ? (
            <Form.Item name="configMessageField" label="Message Field">
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceSourceKind === "logs" ? (
            <Form.Item name="configSeverityField" label="Severity Field">
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceSourceKind === "logs" ? (
            <Form.Item name="configServiceField" label="Service Field">
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceSourceKind === "logs" ? (
            <Form.Item name="configWorkloadField" label="Workload Field">
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceSourceKind === "logs" ? (
            <Form.Item name="configNamespaceField" label="Namespace Field">
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceSourceKind === "logs" ? (
            <Form.Item name="configClusterField" label="Cluster Field">
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceBackendType === "loki" ? (
            <Form.Item
              name="lokiLabelCluster"
              label="Loki Cluster Label"
              rules={[{ required: true, message: "请输入 Loki Cluster Label" }]}
            >
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceBackendType === "loki" ? (
            <Form.Item
              name="lokiLabelNamespace"
              label="Loki Namespace Label"
              rules={[
                { required: true, message: "请输入 Loki Namespace Label" },
              ]}
            >
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceBackendType === "loki" ? (
            <Form.Item
              name="lokiLabelService"
              label="Loki Service Label"
              rules={[{ required: true, message: "请输入 Loki Service Label" }]}
            >
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceBackendType === "loki" ? (
            <Form.Item
              name="lokiLabelWorkload"
              label="Loki Workload Label"
              rules={[
                { required: true, message: "请输入 Loki Workload Label" },
              ]}
            >
              <Input />
            </Form.Item>
          ) : null}
          {dataSourceBackendType === "loki" ? (
            <Form.Item
              name="lokiLabelSeverity"
              label="Loki Severity Label"
              rules={[
                { required: true, message: "请输入 Loki Severity Label" },
              ]}
            >
              <Input />
            </Form.Item>
          ) : null}
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <div className="soha-form-actions">
            <Button
              onClick={() => {
                setDataSourceModalVisible(false);
                setEditingDataSource(null);
                setDataSourceSourceKind("logs");
                setDataSourceBackendType("es");
              }}
            >
              取消
            </Button>
            {canManageAISettings ? (
              <Button
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

      <Modal
        title={editingProfile ? "编辑分析模板" : "新增分析模板"}
        open={profileModalVisible}
        footer={null}
        onCancel={() => {
          setProfileModalVisible(false);
          setEditingProfile(null);
        }}
        destroyOnHidden
      >
        <Form
          {...DEFAULT_FORM_LAYOUT}
          initialValues={buildProfileFormValues(editingProfile)}
          onFinish={(values) => {
            if (!canManageAISettings) return;
            profileMutation.mutate({
              id: editingProfile?.id,
              values: values as Record<string, unknown>,
            });
          }}
        >
          <Form.Item name="id" hidden>
            <Input />
          </Form.Item>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: "请输入名称" }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="mode" label="模式">
            <Select
              options={[
                { value: "root_cause", label: "root_cause" },
                { value: "inspection", label: "inspection" },
                { value: "performance", label: "performance" },
                { value: "trace", label: "trace" },
              ]}
            />
          </Form.Item>
          <Form.Item name="enabledSources" label="数据源">
            <Select
              mode="multiple"
              options={dataSources.map((item) => ({
                value: item.id,
                label: `${item.name} (${item.sourceKind}/${item.backendType})`,
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
                { value: "compact", label: "compact" },
                { value: "standard", label: "standard" },
                { value: "detailed", label: "detailed" },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="outputIncludeEvidenceDetail"
            label="Include Evidence Detail"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            name="outputIncludeRecommendations"
            label="Include Recommendations"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            name="outputIncludeTimeline"
            label="Include Timeline"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <div className="soha-form-actions">
            <Button
              onClick={() => {
                setProfileModalVisible(false);
                setEditingProfile(null);
              }}
            >
              取消
            </Button>
            {canManageAISettings ? (
              <Button
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

      <Modal
        title={editingPolicy ? "编辑自动化策略" : "新增自动化策略"}
        open={policyModalVisible}
        footer={null}
        onCancel={() => {
          setPolicyModalVisible(false);
          setEditingPolicy(null);
        }}
        destroyOnHidden
      >
        <Form
          {...DEFAULT_FORM_LAYOUT}
          initialValues={buildPolicyFormValues(editingPolicy)}
          onFinish={(values) => {
            if (!canManageAISettings) return;
            policyMutation.mutate({
              id: editingPolicy?.id,
              values: values as Record<string, unknown>,
            });
          }}
        >
          <Form.Item name="id" hidden>
            <Input />
          </Form.Item>
          <Form.Item
            name="name"
            label="名称"
            rules={[{ required: true, message: "请输入名称" }]}
          >
            <Input />
          </Form.Item>
          <Form.Item name="triggerType" label="触发类型">
            <Select
              options={[{ value: "alert_webhook", label: "alert_webhook" }]}
            />
          </Form.Item>
          <Form.Item name="analysisKinds" label="分析类型">
            <Select
              mode="multiple"
              options={[
                { value: "root_cause", label: "root_cause" },
                { value: "performance", label: "performance" },
                { value: "trace", label: "trace" },
              ]}
            />
          </Form.Item>
          <Form.Item name="analysisProfileId" label="分析模板">
            <Select
              options={profiles.map((item) => ({
                value: item.id,
                label: item.name,
              }))}
            />
          </Form.Item>
          <Form.Item name="remediationPolicy" label="修复策略">
            <Input />
          </Form.Item>
          <Form.Item name="dedupWindowSeconds" label="Dedup 窗口(s)">
            <InputNumber min={0} style={fullWidthStyle} />
          </Form.Item>
          <Form.Item name="cooldownSeconds" label="Cooldown(s)">
            <InputNumber min={0} style={fullWidthStyle} />
          </Form.Item>
          <Form.Item name="triggerSeverity" label="告警级别">
            <Select mode="multiple" options={SEVERITY_OPTIONS} />
          </Form.Item>
          <Form.Item name="triggerStatus" label="告警状态">
            <Select mode="multiple" options={STATUS_OPTIONS} />
          </Form.Item>
          <Form.Item name="triggerMinDurationSeconds" label="最小持续(s)">
            <InputNumber min={0} style={fullWidthStyle} />
          </Form.Item>
          <Form.Item name="triggerLabelKey" label="标签 Key">
            <Input />
          </Form.Item>
          <Form.Item name="triggerLabelValue" label="标签 Value">
            <Input />
          </Form.Item>
          <Form.Item name="triggerTimeRangeMinutes" label="分析时间范围(分钟)">
            <InputNumber min={5} style={fullWidthStyle} />
          </Form.Item>
          <Form.Item
            name="approvalRequired"
            label="需要审批"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item name="approvalRoles" label="审批角色">
            <TagSelect mode="tags" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <div className="soha-form-actions">
            <Button
              onClick={() => {
                setPolicyModalVisible(false);
                setEditingPolicy(null);
              }}
            >
              取消
            </Button>
            {canManageAISettings ? (
              <Button
                htmlType="submit"
                type="primary"
                loading={policyMutation.isPending}
              >
                保存
              </Button>
            ) : null}
          </div>
        </Form>
      </Modal>
    </>
  );

  if (embedded) {
    return content;
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title="AI 设置"
        description="配置 AI 提供商、模型、API Key 与基础接入地址。"
      />
      {content}
    </div>
  );
}

export function SettingsCenterPage() {
  const location = useLocation();
  const navigate = useNavigate();
  const permissionSnapshotQuery = usePermissionSnapshot();
  const snapshot = permissionSnapshotQuery.data?.data;
  const canViewLoginSettings = hasPermission(
    snapshot,
    "settings.identity.view",
  );
  const canViewBrandingSettings = hasPermission(
    snapshot,
    "settings.branding.view",
  );

  if (permissionSnapshotQuery.isLoading) {
    return (
      <div className="soha-page">
        <ManagementDetailHeader title="设置中心" description="集中配置登陆与品牌能力。" />
        <Card>
          <Spin size="large" />
        </Card>
      </div>
    );
  }

  if (!canViewLoginSettings && !canViewBrandingSettings) {
    return (
      <div className="soha-page">
        <ManagementDetailHeader title="设置中心" description="集中配置登陆与品牌能力。" />
        <ManagementState kind="no-permission" description="当前账号没有可访问的设置页权限。" />
      </div>
    );
  }

  if (location.pathname.endsWith("/branding")) {
    return <BrandingSettingsPage />;
  }

  if (location.pathname.endsWith("/login")) {
    return <LoginSettingsPage />;
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader title="设置中心" description="集中配置登陆与品牌能力。" />
      <SettingsCard title="可用设置">
        <Space orientation="vertical" size={12}>
          {canViewLoginSettings ? (
            <Button
              type="link"
              style={{ paddingInline: 0 }}
              onClick={() => navigate("/settings/login")}
            >
              登陆设置
            </Button>
          ) : null}
          {canViewBrandingSettings ? (
            <Button
              type="link"
              style={{ paddingInline: 0 }}
              onClick={() => navigate("/settings/branding")}
            >
              品牌设置
            </Button>
          ) : null}
        </Space>
      </SettingsCard>
    </div>
  );
}

export const __testOnly = {
  tracesBackendOptions: TRACES_BACKEND_OPTIONS,
};
