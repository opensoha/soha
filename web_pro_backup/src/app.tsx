import { LinkOutlined } from '@ant-design/icons';
import type { Settings as LayoutSettings } from '@ant-design/pro-components';
import type { MenuDataItem } from '@ant-design/pro-components/es/layout/typing';
import { SettingDrawer } from '@ant-design/pro-components';
import type { RunTimeLayoutConfig } from '@umijs/max';
import { history, Link } from '@umijs/max';
import { App as AntdApp, Button } from 'antd';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import React from 'react';

import {
  AvatarDropdown,
  Footer,
  LangDropdown,
} from '@/components';
import { getStoredAccessToken } from '@/features/auth/auth-api';
import {
  buildAnonymousRuntimeState,
  fetchRuntimeUserInfo,
  getRuntimeLoginPath,
  loadRuntimeBootstrap,
  redirectToLogin,
} from '@/features/auth/runtime';
import { I18nProvider } from '@/i18n';
import { getAccessibleProRouteManifest } from '@/routes/pro-route-manifest';
import defaultSettings from '../config/defaultSettings';
import { errorConfig } from './requestErrorConfig';

dayjs.extend(relativeTime);

const isDev = process.env.NODE_ENV === 'development';
const loginPath = getRuntimeLoginPath();
const publicPaths = new Set(['/login', '/auth/oidc/callback', '/login/callback']);

export async function getInitialState(): Promise<{
  settings?: Partial<LayoutSettings>;
  currentUser?: API.CurrentUser;
  permissionSnapshot?: import('@/types').PermissionSnapshot;
  branding?: import('@/types').BrandingSettings;
  loading?: boolean;
  fetchUserInfo?: () => Promise<API.CurrentUser | undefined>;
  settingDrawerOpen?: boolean;
}> {
  const { location } = history;
  if (!publicPaths.has(location.pathname)) {
    try {
      const bootstrap = await loadRuntimeBootstrap();
      return {
        fetchUserInfo: fetchRuntimeUserInfo,
        ...bootstrap,
      };
    } catch (_error) {
      redirectToLogin();
    }
  }

  return buildAnonymousRuntimeState();
}

export const layout: RunTimeLayoutConfig = ({ initialState, setInitialState }) => {
  return {
    menuDataRender: (): MenuDataItem[] =>
      getAccessibleProRouteManifest(initialState?.permissionSnapshot).map((item) => ({
        path: item.path,
        name: item.name,
        icon: item.icon,
        access: item.access,
        children: item.routes?.map((child) => ({
          path: child.path,
          name: child.name,
          icon: child.icon,
          access: child.access,
        })),
      })),
    menuItemRender: (item, dom) => {
      if (item.path) {
        return (
          <Link to={item.path} prefetch>
            {dom}
          </Link>
        );
      }
      return dom;
    },
    actionsRender: () => [<LangDropdown key="lang" />],
    avatarProps: {
      title: initialState?.currentUser?.name || initialState?.currentUser?.userName || 'User',
      render: (_, avatarChildren) => <AvatarDropdown>{avatarChildren}</AvatarDropdown>,
    },
    footerRender: () => <Footer />,
    onPageChange: () => {
      const { location } = history;
      if (!initialState?.currentUser && !publicPaths.has(location.pathname)) {
        redirectToLogin();
      }
    },
    links: isDev
      ? [
          <Link key="openapi" to="/swagger/index.html" target="_blank">
            <LinkOutlined />
            <span>API 文档</span>
          </Link>,
        ]
      : [],
    menuHeaderRender: undefined,
    childrenRender: (children) => (
      <AntdApp>
        <I18nProvider>{children}</I18nProvider>
        <SettingDrawer
          disableUrlParams
          enableDarkTheme
          collapse={initialState?.settingDrawerOpen}
          onCollapseChange={(open) => {
            setInitialState((s) => ({
              ...s,
              settingDrawerOpen: open,
            }));
          }}
          settings={initialState?.settings}
          onSettingChange={(settings) => {
            setInitialState((s) => ({
              ...s,
              settings,
            }));
          }}
        />
      </AntdApp>
    ),
    ...initialState?.settings,
  };
};

export const request = {
  baseURL: '',
  ...errorConfig,
  requestInterceptors: [
    (config: Record<string, unknown>) => {
      const token = getStoredAccessToken();
      const headers = { ...((config.headers as Record<string, string> | undefined) || {}) };
      if (token) {
        headers.Authorization = `Bearer ${token}`;
      }
      return { ...config, headers };
    },
  ],
};
