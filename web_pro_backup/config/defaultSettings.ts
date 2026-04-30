import type { ProLayoutProps } from '@ant-design/pro-components';

const settings: ProLayoutProps & {
  pwa?: boolean;
  logo?: string;
} = {
  navTheme: 'light',
  colorPrimary: '#111827',
  layout: 'mix',
  contentWidth: 'Fluid',
  fixedHeader: true,
  fixSiderbar: true,
  colorWeak: false,
  title: 'KubeCrux',
  pwa: false,
  logo: '/logo.svg',
  iconfontUrl: '',
  token: {},
};

export default settings;
