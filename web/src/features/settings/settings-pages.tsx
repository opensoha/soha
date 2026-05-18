import { useEffect, useState } from "react";
import type { MouseEvent, ReactNode } from "react";
import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  message,
} from "antd";
import type { TableColumnsType } from "antd";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useLocation, useNavigate } from "react-router-dom";
import { AdminTable } from "@/components/admin-table";
import {
  hasPermission,
  usePermissionSnapshot,
} from "@/features/auth/permission-snapshot";
import { PageHeader } from "@/components/page-header";
import { api } from "@/services/api-client";
import { StatusTag } from "@/components/status-tag";
import { formatDateTime } from "@/utils/time";
import { tableColumnPresets } from "@/utils/table-columns";
import type { ApiResponse, BrandingSettings } from "@/types";

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
const TRACES_BACKEND_OPTIONS: Array<{ value: string; label: string }> = [
  { value: "jaeger", label: "jaeger" },
  { value: "skywalking", label: "skywalking" },
];

function SectionCallout({
  title,
  description,
}: {
  title: string;
  description: string;
}) {
  return (
    <div className="mb-4 rounded border border-[var(--kc-border-color)] bg-[var(--kc-fill-weak)] p-3 text-sm">
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
      <div className="kc-page">
        <SettingsCard>当前账号没有查看品牌设置的权限。</SettingsCard>
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
          settings ?? { appTitle: "KubeCrux", sidebarTitle: "KubeCrux" }
        }
      >
        <Form.Item name="appTitle" label="网页标题">
          <Input placeholder="浏览器标签页标题" />
        </Form.Item>
        <Form.Item name="sidebarTitle" label="侧边栏标题">
          <Input placeholder="左侧品牌栏文字" />
        </Form.Item>

        <div className="kc-branding-section-title">企业 Logo</div>
        <div className="kc-branding-upload-grid">
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

        <div className="kc-form-actions">
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
    <div className="kc-page">
      <PageHeader
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
    <div className="kc-branding-upload-zone">
      <div className="kc-branding-upload-label">{label}</div>
      <Form.Item name={field} hidden>
        <Input />
      </Form.Item>
      <div className="kc-branding-upload-area-wrap">
        <div
          className={`kc-branding-upload-area ${disabled ? "is-disabled" : ""}`}
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
              className="kc-branding-upload-preview"
              style={{ maxWidth: previewWidth, maxHeight: previewHeight }}
            />
          ) : (
            <div className="kc-branding-upload-placeholder">
              {uploading ? (
                <Spin size="small" />
              ) : (
                <span className="kc-branding-upload-plus">+</span>
              )}
            </div>
          )}
        </div>
        {currentValue && !disabled ? (
          <Button
            size="small"
            danger
            variant="outlined"
            className="kc-branding-upload-remove"
            onClick={handleRemove}
          >
            移除
          </Button>
        ) : null}
      </div>
      <div className="kc-branding-upload-hint">{hint}</div>
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
      <div className="kc-page">
        <SettingsCard>当前账号没有查看登陆设置的权限。</SettingsCard>
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
          <Space>
            <Button
              size="small"
              type="text"
              onClick={() => {
                setEditingProvider(record);
                setProviderModalVisible(true);
              }}
            >
              编辑
            </Button>
            <Button
              size="small"
              type="text"
              onClick={() => {
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
              删除
            </Button>
            {settings?.defaultProviderId !== record.id ? (
              <Button
                size="small"
                type="text"
                onClick={() =>
                  saveMutation.mutate({
                    providers,
                    defaultProviderId: record.id,
                  })
                }
              >
                设为默认
              </Button>
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
            <div className="kc-page-toolbar">
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
            </div>
          ) : null
        }
      >
        <Table
          rowKey="id"
          pagination={false}
          dataSource={providers}
          columns={providerColumns}
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="暂无登录源配置"
              />
            ),
          }}
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
        destroyOnClose
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
                      message="SAML 当前为配置态"
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
                        <Input placeholder="https://kubecrux.example.com/saml/sp" />
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
          <Form.Item name="userIdField" label="用户ID字段">
            <Input placeholder="sub / open_id / unionId / UserId" />
          </Form.Item>
          <Form.Item name="userNameField" label="用户名字段">
            <Input placeholder="name / nick / preferred_username" />
          </Form.Item>
          <Form.Item name="emailField" label="邮箱字段">
            <Input placeholder="email / enterprise_email" />
          </Form.Item>
          <div className="kc-form-actions">
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

  return <div className="kc-page">{content}</div>;
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
      <div className="kc-page">
        <SettingsCard>当前账号没有查看监控设置的权限。</SettingsCard>
      </div>
    );
  }

  const settings = data?.data;

  return (
    <div className="kc-page">
      <PageHeader
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
          <div className="kc-form-actions">
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

interface AISettings {
  provider?: {
    id?: string;
    name?: string;
    providerKind?: string;
    enabled: boolean;
    baseUrl: string;
    apiKey: string;
    model: string;
  };
  providers?: Array<{
    id: string;
    name: string;
    providerKind: string;
    enabled: boolean;
    baseUrl: string;
    apiKey: string;
    model: string;
  }>;
  defaultProviderId?: string;
  enabled: boolean;
  baseUrl: string;
  apiKey: string;
  model: string;
  skillsRegistry?: Array<{
    id: string;
    name: string;
    category?: string;
    ownerModule?: string;
    description?: string;
    enabled: boolean;
    scopes?: string[];
    capabilityRefs?: string[];
    blueprintRefs?: string[];
    scopeRules?: string[];
    inputSchema?: Record<string, unknown>;
    outputSchema?: Record<string, unknown>;
  }>;
}

interface AISkillSetting {
  id: string;
  name: string;
  category?: string;
  ownerModule?: string;
  description?: string;
  enabled: boolean;
  scopes?: string[];
  capabilityRefs?: string[];
  blueprintRefs?: string[];
  scopeRules?: string[];
  inputSchema?: Record<string, unknown>;
  outputSchema?: Record<string, unknown>;
}

interface DataSource {
  id: string;
  name: string;
  sourceKind: string;
  backendType: string;
  enabled: boolean;
  credentialRef?: string;
  mcpAdapter: string;
  scope?: Record<string, unknown>;
  queryBudget?: Record<string, unknown>;
  redactionPolicy?: Record<string, unknown>;
  config?: Record<string, unknown>;
  validationStatus?: string;
  validationMessage?: string;
  lastValidatedAt?: string;
}

interface AnalysisProfile {
  id: string;
  name: string;
  mode: string;
  enabledSources?: string[];
  enabledPlaybooks?: string[];
  remediationPolicy: string;
  enabled: boolean;
  queryBudgets?: Record<string, unknown>;
  outputStyle?: Record<string, unknown>;
}

interface AutomationPolicy {
  id: string;
  name: string;
  triggerType: string;
  analysisKinds?: string[];
  analysisProfileId: string;
  remediationPolicy: string;
  enabled: boolean;
  dedupWindowSeconds: number;
  triggerConditions?: Record<string, unknown>;
  approvalPolicy?: Record<string, unknown>;
}

interface AIProviderConnection {
  id: string;
  name: string;
  providerKind: string;
  enabled: boolean;
  baseUrl: string;
  apiKey: string;
  model: string;
}

function normalizeAIProviderConnection(
  item?: Partial<AIProviderConnection> | null,
): AIProviderConnection {
  return {
    id: String(item?.id || "default"),
    name: String(item?.name || "default"),
    providerKind: String(item?.providerKind || "openai-compatible"),
    enabled: Boolean(item?.enabled),
    baseUrl: String(item?.baseUrl || ""),
    apiKey: String(item?.apiKey || ""),
    model: String(item?.model || ""),
  };
}

const PLAYBOOK_OPTIONS = [
  { value: "release-correlation", label: "release-correlation" },
  { value: "cluster-health", label: "cluster-health" },
  { value: "access-drift", label: "access-drift" },
  { value: "runtime-instability", label: "runtime-instability" },
  { value: "alert-pressure", label: "alert-pressure" },
  { value: "build-queue", label: "build-queue" },
  { value: "error-burst", label: "error-burst" },
  { value: "dependency-timeout", label: "dependency-timeout" },
];

const SEVERITY_OPTIONS = [
  { value: "critical", label: "critical" },
  { value: "warning", label: "warning" },
  { value: "info", label: "info" },
];

const STATUS_OPTIONS = [
  { value: "firing", label: "firing" },
  { value: "resolved", label: "resolved" },
];

function buildDataSourceFormValues(item?: DataSource | null) {
  return {
    id: item?.id,
    name: item?.name ?? "",
    sourceKind: item?.sourceKind ?? "logs",
    backendType: item?.backendType ?? "es",
    enabled: item?.enabled ?? true,
    credentialRef: item?.credentialRef ?? "",
    mcpAdapter: item?.mcpAdapter ?? "logs.v1",
    scopeClusterId: String(item?.scope?.clusterId ?? ""),
    scopeNamespace: String(item?.scope?.namespace ?? ""),
    scopeService: String(item?.scope?.service ?? ""),
    scopeWorkload: String(item?.scope?.workload ?? ""),
    budgetMaxQueries: Number(item?.queryBudget?.maxQueries ?? 12),
    budgetMaxLogBytes: Number(item?.queryBudget?.maxLogBytes ?? 20_000_000),
    budgetTimeoutSeconds: Number(item?.queryBudget?.timeoutSeconds ?? 90),
    redactionMaskFields: Array.isArray(item?.redactionPolicy?.maskFields)
      ? (item?.redactionPolicy?.maskFields as string[])
      : [],
    redactionMaskPatterns: Array.isArray(item?.redactionPolicy?.maskPatterns)
      ? (item?.redactionPolicy?.maskPatterns as string[])
      : [],
    redactionTruncateLongLines: Boolean(
      item?.redactionPolicy?.truncateLongLines ?? true,
    ),
    configEndpoint: String(item?.config?.endpoint ?? ""),
    configIndex: String(item?.config?.index ?? ""),
    configTable: String(item?.config?.table ?? ""),
    configUsername: String(item?.config?.username ?? ""),
    configPassword: String(item?.config?.password ?? ""),
    configBearerToken: String(item?.config?.bearerToken ?? ""),
    configTimestampField: String(item?.config?.timestampField ?? "@timestamp"),
    configMessageField: String(item?.config?.messageField ?? "message"),
    configSeverityField: String(item?.config?.severityField ?? "level"),
    configServiceField: String(item?.config?.serviceField ?? "service"),
    configWorkloadField: String(item?.config?.workloadField ?? "workload"),
    configNamespaceField: String(item?.config?.namespaceField ?? "namespace"),
    configClusterField: String(item?.config?.clusterField ?? "cluster"),
    lokiLabelCluster: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.cluster ?? "cluster",
    ),
    lokiLabelNamespace: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.namespace ?? "namespace",
    ),
    lokiLabelService: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.service ?? "service",
    ),
    lokiLabelWorkload: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.workload ?? "workload",
    ),
    lokiLabelSeverity: String(
      (item?.config?.labelKeys as Record<string, unknown> | undefined)
        ?.severity ?? "level",
    ),
  };
}

function buildDataSourcePayload(values: Record<string, unknown>) {
  const sourceKind = String(values.sourceKind ?? "logs");
  const backendType = String(values.backendType ?? "es");
  const config: Record<string, unknown> = {
    endpoint: values.configEndpoint || undefined,
    timestampField: values.configTimestampField || undefined,
    messageField: values.configMessageField || undefined,
    severityField: values.configSeverityField || undefined,
    serviceField: values.configServiceField || undefined,
    workloadField: values.configWorkloadField || undefined,
    namespaceField: values.configNamespaceField || undefined,
    clusterField: values.configClusterField || undefined,
    username: values.configUsername || undefined,
    password: values.configPassword || undefined,
    bearerToken: values.configBearerToken || undefined,
  };
  if (backendType === "es") config.index = values.configIndex || undefined;
  if (backendType === "clickhouse")
    config.table = values.configTable || undefined;
  if (backendType === "loki") {
    config.labelKeys = {
      cluster: values.lokiLabelCluster || "cluster",
      namespace: values.lokiLabelNamespace || "namespace",
      service: values.lokiLabelService || "service",
      workload: values.lokiLabelWorkload || "workload",
      severity: values.lokiLabelSeverity || "level",
    };
  }
  return {
    id: values.id,
    name: values.name,
    sourceKind,
    backendType,
    enabled: values.enabled,
    credentialRef: values.credentialRef,
    mcpAdapter: values.mcpAdapter,
    scope: {
      clusterId: values.scopeClusterId || undefined,
      namespace: values.scopeNamespace || undefined,
      service: values.scopeService || undefined,
      workload: values.scopeWorkload || undefined,
    },
    queryBudget: {
      maxQueries: Number(values.budgetMaxQueries || 0),
      maxLogBytes: Number(values.budgetMaxLogBytes || 0),
      timeoutSeconds: Number(values.budgetTimeoutSeconds || 0),
    },
    redactionPolicy: {
      maskFields: values.redactionMaskFields || [],
      maskPatterns: values.redactionMaskPatterns || [],
      truncateLongLines: Boolean(values.redactionTruncateLongLines),
    },
    config,
  };
}

function buildProfileFormValues(item?: AnalysisProfile | null) {
  return {
    id: item?.id,
    name: item?.name ?? "",
    mode: item?.mode ?? "root_cause",
    enabledSources: item?.enabledSources ?? [],
    enabledPlaybooks: item?.enabledPlaybooks ?? [],
    remediationPolicy: item?.remediationPolicy ?? "suggest_only",
    defaultTimeRangeMinutes: Number(
      (item as unknown as { defaultTimeRangeMinutes?: number } | undefined)
        ?.defaultTimeRangeMinutes ?? 60,
    ),
    timeoutSeconds: Number(
      (item as unknown as { timeoutSeconds?: number } | undefined)
        ?.timeoutSeconds ?? 90,
    ),
    enabled: item?.enabled ?? true,
    budgetMaxQueries: Number(item?.queryBudgets?.maxQueries ?? 12),
    budgetMaxLogBytes: Number(item?.queryBudgets?.maxLogBytes ?? 20_000_000),
    budgetMaxEvidenceItems: Number(item?.queryBudgets?.maxEvidenceItems ?? 20),
    outputSummaryLevel: String(item?.outputStyle?.summaryLevel ?? "standard"),
    outputIncludeEvidenceDetail: Boolean(
      item?.outputStyle?.includeEvidenceDetail ?? true,
    ),
    outputIncludeRecommendations: Boolean(
      item?.outputStyle?.includeRecommendations ?? true,
    ),
    outputIncludeTimeline: Boolean(item?.outputStyle?.includeTimeline ?? false),
  };
}

function buildProfilePayload(values: Record<string, unknown>) {
  return {
    id: values.id,
    name: values.name,
    mode: values.mode,
    enabledSources: values.enabledSources || [],
    enabledPlaybooks: values.enabledPlaybooks || [],
    remediationPolicy: values.remediationPolicy,
    defaultTimeRangeMinutes: Number(values.defaultTimeRangeMinutes || 60),
    timeoutSeconds: Number(values.timeoutSeconds || 90),
    enabled: values.enabled,
    queryBudgets: {
      maxQueries: Number(values.budgetMaxQueries || 0),
      maxLogBytes: Number(values.budgetMaxLogBytes || 0),
      maxEvidenceItems: Number(values.budgetMaxEvidenceItems || 0),
    },
    outputStyle: {
      summaryLevel: values.outputSummaryLevel,
      includeEvidenceDetail: Boolean(values.outputIncludeEvidenceDetail),
      includeRecommendations: Boolean(values.outputIncludeRecommendations),
      includeTimeline: Boolean(values.outputIncludeTimeline),
    },
  };
}

function buildPolicyFormValues(item?: AutomationPolicy | null) {
  const conditions = item?.triggerConditions ?? {};
  const labels =
    (conditions.labels as Record<string, unknown> | undefined) ?? {};
  const approval = item?.approvalPolicy ?? {};
  return {
    id: item?.id,
    name: item?.name ?? "",
    triggerType: item?.triggerType ?? "alert_webhook",
    analysisKinds: item?.analysisKinds ?? ["root_cause"],
    analysisProfileId: item?.analysisProfileId ?? "",
    remediationPolicy: item?.remediationPolicy ?? "suggest_only",
    enabled: item?.enabled ?? true,
    dedupWindowSeconds: Number(item?.dedupWindowSeconds ?? 900),
    cooldownSeconds: Number(
      (item as unknown as { cooldownSeconds?: number } | undefined)
        ?.cooldownSeconds ?? 0,
    ),
    triggerSeverity: Array.isArray(conditions.severity)
      ? (conditions.severity as string[])
      : [],
    triggerStatus: Array.isArray(conditions.status)
      ? (conditions.status as string[])
      : [],
    triggerMinDurationSeconds: Number(conditions.min_duration_seconds ?? 120),
    triggerLabelKey: Object.keys(labels)[0] ?? "",
    triggerLabelValue: String(Object.values(labels)[0] ?? ""),
    triggerTimeRangeMinutes: Number(conditions.time_range_minutes ?? 60),
    approvalRequired: Boolean(approval.required ?? false),
    approvalRoles: Array.isArray(approval.approverRoles)
      ? (approval.approverRoles as string[])
      : [],
  };
}

function buildPolicyPayload(values: Record<string, unknown>) {
  const labels: Record<string, unknown> = {};
  if (values.triggerLabelKey && values.triggerLabelValue) {
    labels[String(values.triggerLabelKey)] = values.triggerLabelValue;
  }
  return {
    id: values.id,
    name: values.name,
    enabled: values.enabled,
    triggerType: values.triggerType,
    analysisKinds: values.analysisKinds || ["root_cause"],
    analysisProfileId: values.analysisProfileId,
    remediationPolicy: values.remediationPolicy,
    dedupWindowSeconds: Number(values.dedupWindowSeconds || 0),
    cooldownSeconds: Number(values.cooldownSeconds || 0),
    triggerConditions: {
      severity: values.triggerSeverity || [],
      status: values.triggerStatus || [],
      min_duration_seconds: Number(values.triggerMinDurationSeconds || 0),
      time_range_minutes: Number(values.triggerTimeRangeMinutes || 0),
      labels,
    },
    approvalPolicy: {
      required: Boolean(values.approvalRequired),
      approverRoles: values.approvalRoles || [],
    },
  };
}

export function AISettingsPage({ embedded = false }: SettingsPageProps = {}) {
  const queryClient = useQueryClient();
  const permissionSnapshotQuery = usePermissionSnapshot();
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
      setProviderModels(response.data.models ?? []);
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
      <div className="kc-page">
        <SettingsCard>当前账号没有查看 AI 设置的权限。</SettingsCard>
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
      render: (_: unknown, record: DataSource) => (
        <Space>
          {canManageAISettings ? (
            <Button
              size="small"
              variant="outlined"
              loading={
                validateDataSourceMutation.isPending &&
                validateDataSourceMutation.variables === record.id
              }
              onClick={() => validateDataSourceMutation.mutate(record.id)}
            >
              校验连接
            </Button>
          ) : null}
          {canManageAISettings ? (
            <Button
              size="small"
              type="text"
              onClick={() => {
                setEditingDataSource(record);
                setDataSourceSourceKind(record.sourceKind);
                setDataSourceBackendType(record.backendType);
                setDataSourceModalVisible(true);
              }}
            >
              编辑
            </Button>
          ) : null}
          {!canManageAISettings ? "-" : null}
        </Space>
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
          <Button
            size="small"
            type="text"
            onClick={() => {
              setEditingProfile(record);
              setProfileModalVisible(true);
            }}
          >
            编辑
          </Button>
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
          <Button
            size="small"
            type="text"
            onClick={() => {
              setEditingPolicy(record);
              setPolicyModalVisible(true);
            }}
          >
            编辑
          </Button>
        ) : (
          "-"
        ),
    },
  ];

  const providerColumns: TableColumnsType<AIProviderConnection> = [
    { title: "名称", dataIndex: "name" },
    { title: "Provider", dataIndex: "providerKind" },
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
          <Tag color="blue">默认</Tag>
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
          <Space>
            <Button
              data-testid={`ai-provider-edit-${record.id}`}
              size="small"
              type="text"
              onClick={() => {
                setEditingProvider(record);
                setProviderModels([]);
                setProviderModalVisible(true);
              }}
            >
              编辑
            </Button>
            <Button
              data-testid={`ai-provider-delete-${record.id}`}
              size="small"
              type="text"
              onClick={() => {
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
              删除
            </Button>
            {settings?.defaultProviderId !== record.id ? (
              <Button
                data-testid={`ai-provider-default-${record.id}`}
                size="small"
                type="text"
                onClick={() => {
                  saveMutation.mutate({
                    providers: settings?.providers ?? [],
                    defaultProviderId: record.id,
                  });
                }}
              >
                设为默认
              </Button>
            ) : null}
          </Space>
        ) : (
          "-"
        ),
    },
  ];

  const providerCard = (
    <section data-testid="ai-provider-connections-section">
      <SettingsCard title="Provider Connections">
        <div
          data-testid="ai-provider-connections-actions"
          className="kc-form-actions"
          style={{ marginBottom: 12 }}
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
          <Table
            data-testid="ai-provider-table"
            rowKey="id"
            pagination={false}
            dataSource={settings?.providers ?? []}
            columns={providerColumns}
            locale={{
              emptyText: (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description="暂无 Provider 连接"
                />
              ),
            }}
          />
        </div>
      </SettingsCard>
    </section>
  );

  const providerModal = (
    <Modal
      title={editingProvider ? "编辑 Provider 连接" : "新增 Provider 连接"}
      open={providerModalVisible}
      className="kc-ai-provider-modal"
      footer={null}
      onCancel={() => {
        setProviderModalVisible(false);
        setEditingProvider(null);
        setProviderModels([]);
      }}
      destroyOnClose
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
        <Form.Item name="model" label="模型">
          <Input
            data-testid="ai-provider-model"
            placeholder="手动填写模型，或先获取模型列表再选择"
          />
        </Form.Item>
        {providerModels.length > 0 ? (
          <Form.Item label="模型列表">
            <Select
              data-testid="ai-provider-model-options"
              showSearch
              options={providerModels.map((item) => ({
                value: item,
                label: item,
              }))}
              onChange={(value) => providerForm.setFieldValue("model", value)}
            />
          </Form.Item>
        ) : null}
        <Form.Item name="enabled" label="启用" valuePropName="checked">
          <Switch data-testid="ai-provider-enabled" />
        </Form.Item>
        <div data-testid="ai-provider-actions" className="kc-form-actions">
          <Button
            data-testid="ai-provider-fetch-models"
            onClick={async () => {
              const values = await providerForm.validateFields([
                "providerKind",
                "baseUrl",
                "apiKey",
                "model",
                "name",
                "enabled",
              ]);
              fetchModelsMutation.mutate({
                id: String(values.id || editingProvider?.id || ""),
                name: String(values.name || ""),
                providerKind: String(
                  values.providerKind || "openai-compatible",
                ),
                enabled: Boolean(values.enabled),
                baseUrl: String(values.baseUrl || ""),
                apiKey: String(values.apiKey || ""),
                model: String(values.model || ""),
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
              testProviderMutation.mutate({
                provider: {
                  id: String(values.id || editingProvider?.id || ""),
                  name: String(values.name || ""),
                  providerKind: String(
                    values.providerKind || "openai-compatible",
                  ),
                  enabled: Boolean(values.enabled),
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
          rowKey="id"
          dataSource={skillsRegistryDraft}
          empty={
            <Empty description="还没有全局 skills，可先新增 MCP、logs、metrics、traces 这类技能条目。" />
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
                  <Space>
                    <Button
                      size="small"
                      type="text"
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
                    >
                      上移
                    </Button>
                    <Button
                      size="small"
                      type="text"
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
                    >
                      下移
                    </Button>
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
                  <Space>
                    <Button
                      size="small"
                      type="text"
                      onClick={() => {
                        setEditingSkill(record);
                        setSkillsModalVisible(true);
                      }}
                    >
                      编辑
                    </Button>
                    <Button
                      size="small"
                      type="text"
                      danger
                      onClick={() =>
                        setSkillsRegistryDraft((current) =>
                          current.filter((item) => item.id !== record.id),
                        )
                      }
                    >
                      删除
                    </Button>
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
        destroyOnClose
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
          <div className="kc-form-actions">
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
        destroyOnClose
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
              message="SkyWalking 作为 trace 查询后端"
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
          <div className="kc-form-actions">
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
        destroyOnClose
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
          <div className="kc-form-actions">
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
        destroyOnClose
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
          <div className="kc-form-actions">
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
    <div className="kc-page">
      <PageHeader
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
      <div className="kc-page">
        <PageHeader title="设置中心" description="集中配置登陆与品牌能力。" />
        <Card>
          <Spin size="large" />
        </Card>
      </div>
    );
  }

  if (!canViewLoginSettings && !canViewBrandingSettings) {
    return (
      <div className="kc-page">
        <PageHeader title="设置中心" description="集中配置登陆与品牌能力。" />
        <SettingsCard>当前账号没有可访问的设置页权限。</SettingsCard>
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
    <div className="kc-page">
      <PageHeader title="设置中心" description="集中配置登陆与品牌能力。" />
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
